package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/notify"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/software"
	"github.com/ropslabs/x3f/internal/status"
)

// withTimeout runs fetch with its own timeout. T is a type parameter: the
// same function works for fetchers returning a string or a sync status.
func withTimeout[T any](ctx context.Context, timeout time.Duration, endpoint string,
	fetch func(context.Context, string) (T, error)) (T, error) {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	// cancel releases the timer as soon as the request is done.
	defer cancel()
	return fetch(ctx, endpoint)
}

// repoData is the outcome of fetching one list (releases or advisories)
// of one repository.
type repoData[T any] struct {
	result *T // nil if no data could be obtained
	err    error
}

// releaseData and advisoryData are aliases (note the "="): other names for
// the instantiated generic types, not new types.
type (
	releaseData  = repoData[releases.Result]
	advisoryData = repoData[releases.AdvisoryResult]
)

// fetchReleases fetches the releases of every piece of software that was
// detected, once per repository and concurrently.
func fetchReleases(ctx context.Context, src *releases.Source, nodes []checker, timeout time.Duration) map[software.Repo]releaseData {
	return fetchPerRepo(ctx, nodes, timeout, src.Releases)
}

// fetchAdvisories fetches the security advisories the same way.
func fetchAdvisories(ctx context.Context, src *releases.Source, nodes []checker, timeout time.Duration) map[software.Repo]advisoryData {
	return fetchPerRepo(ctx, nodes, timeout, src.Advisories)
}

// fetchPerRepo calls fetch once for the repository of every detected piece
// of software, concurrently. fetch is a method value such as src.Releases;
// Go infers T from its result type.
func fetchPerRepo[T any](ctx context.Context, nodes []checker, timeout time.Duration,
	fetch func(context.Context, software.Repo) (T, error)) map[software.Repo]repoData[T] {

	var repos []software.Repo
	for _, s := range allInstalled(nodes) {
		if repo, ok := software.RepoFor(s.version.Name); ok && !slices.Contains(repos, repo) {
			repos = append(repos, repo)
		}
	}

	data := make(map[software.Repo]repoData[T], len(repos))
	// Several goroutines write to the same map, which Go does not allow
	// without synchronisation; the mutex makes the writes take turns.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res, err := fetch(ctx, repo)
			d := repoData[T]{err: err}
			if err == nil {
				d.result = &res
			}
			mu.Lock()
			data[repo] = d
			mu.Unlock()
		})
	}
	wg.Wait()
	return data
}

// refreshIfAhead re-fetches, ignoring the TTL, every repository whose
// cached data has no release as new as a version a node reports. Without
// this, a release published after the last fetch would look like a proxy
// or custom build until the cache expires.
func refreshIfAhead(ctx context.Context, src *releases.Source, nodes []checker, rel map[software.Repo]releaseData, timeout time.Duration) {
	var stale []software.Repo
	for _, s := range allInstalled(nodes) {
		v := s.version
		repo, ok := software.RepoFor(v.Name)
		d := rel[repo]
		if !ok || d.result == nil || !d.result.Cached || slices.Contains(stale, repo) {
			continue
		}
		running, ok := releases.ParseVersion(v.Version)
		running.Suffix = v.Suffix
		if ok && releases.NewerThanAll(v.Name, d.result.Releases, running) {
			stale = append(stale, repo)
		}
	}
	// Usually zero or one repository; sequential is fine.
	for _, repo := range stale {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		res, err := src.Refresh(ctx, repo)
		cancel()
		if err == nil {
			rel[repo] = releaseData{result: &res}
		}
	}
}

// row is one table row with its assessments.
type row struct {
	node string
	// typ and columns come from the node's checker; rows of one type form
	// one table.
	typ     string
	columns []string
	level   status.Level
	// cells has one entry per column.
	cells    []string
	findings []layerFindings
	// advisories holds the advisory counts per severity of each assessed
	// component, in the order of the node's software, for the metrics.
	advisories []componentCounts
	// nextForks are the next scheduled forks of the networks the node is
	// on, for the metrics.
	nextForks []nextFork
}

// componentCounts are the advisory counts per severity of one component.
type componentCounts struct {
	component string
	counts    map[string]int
}

// nextFork is the next scheduled fork of a network.
type nextFork struct {
	network, fork string
	at            time.Time
}

// layerFindings are the findings of one part of a node, e.g. "execution
// sync". The advisories column has two: one per client.
type layerFindings struct {
	name string
	status.Assessment
	// notRun is set if the check could not run, e.g. the sync status of
	// an unreachable client. Its level is then OK with no findings; the
	// metrics report it as unknown.
	notRun bool
}

// assessParams are the settings for assess.
type assessParams struct {
	now            time.Time
	grace          time.Duration
	schedule       *forks.Schedule
	forkCriticalIn time.Duration
	// ignore is the global ignore_advisories list.
	ignore []config.AdvisoryIgnore
}

// assessAll evaluates all nodes. It returns the rows, in config order, and
// the overall level.
func assessAll(nodes []checker, up upstream, p assessParams) ([]row, status.Level) {
	overall := status.OK
	rows := make([]row, 0, len(nodes))
	for _, n := range nodes {
		rw := n.assess(up, p)
		rw.node, rw.typ, rw.columns = n.name(), n.typ(), n.columns()
		for _, x := range rw.findings {
			rw.level = max(rw.level, x.Level)
		}
		overall = max(overall, rw.level)
		rows = append(rows, rw)
	}
	return rows, overall
}

// notifyChanges sends a Telegram message about the nodes whose status
// changed since the last message and updates the state file. It returns
// global findings: a note naming the nodes in the message, or a warning if
// it could not be sent or the state could not be read or written.
func notifyChanges(ctx context.Context, tg *notify.Telegram, cfg *config.Telegram, rows []row, stateFile string, now time.Time, timeout time.Duration) []status.Finding {
	prev, err := notify.LoadState(stateFile)
	if err != nil {
		return []status.Finding{{Level: status.Warn, Message: fmt.Sprintf("notifications skipped: %v", err)}}
	}
	nodes := make([]notify.Node, 0, len(rows))
	for _, r := range rows {
		n := notify.Node{Name: r.node, Level: r.level}
		for _, f := range r.findings {
			n.Checks = append(n.Checks, notify.Check{Name: f.name, Assessment: f.Assessment})
		}
		nodes = append(nodes, n)
	}
	minLevel := status.Warn
	if cfg.MinLevel == "crit" {
		minLevel = status.Crit
	}
	changes, next := notify.Evaluate(prev, nodes, notify.Options{MinLevel: minLevel, RemindEvery: cfg.Remind(), Now: now})

	var out []status.Finding
	if len(changes) > 0 {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		err := tg.Send(ctx, notify.Format(changes, now, notify.MaxMessageLength))
		cancel()
		if err != nil {
			next = notify.Undelivered(prev, next, changes)
			out = append(out, status.Finding{Level: status.Warn,
				Message: fmt.Sprintf("notification failed, will retry on the next run: %v", err)})
		} else {
			var names []string
			for _, c := range changes {
				names = append(names, c.Node.Name)
			}
			out = append(out, status.Finding{Level: status.OK,
				Message: "Telegram message sent about " + strings.Join(names, ", ")})
		}
	}
	if err := notify.SaveState(stateFile, next); err != nil {
		out = append(out, status.Finding{Level: status.Warn,
			Message: fmt.Sprintf("notification state not saved, changes may be reported again: %v", err)})
	}
	return out
}

// checkIgnoreList reports entries of an ignore list that are not
// advisories of any of the software sw, so typos show up; global marks the
// global list. It does not run (ran is false) if a piece of software could
// not be identified or its advisories are unknown: the ID might belong to
// it.
func checkIgnoreList(entries []config.AdvisoryIgnore, sw []installed, adv map[software.Repo]advisoryData, global bool) (a status.Assessment, ran bool) {
	if len(entries) == 0 {
		return status.Assessment{}, true
	}
	var lists [][]releases.Advisory
	var seen []software.Repo
	var names []string
	for _, s := range sw {
		v := s.version
		repo, ok := software.RepoFor(v.Name)
		if !ok || adv[repo].result == nil {
			return status.Assessment{}, false
		}
		if !slices.Contains(seen, repo) {
			seen = append(seen, repo)
			lists = append(lists, adv[repo].result.Advisories)
			names = append(names, v.Name.String())
		}
	}
	slices.Sort(names)
	return status.AssessIgnoreList(entries, lists, names, global), true
}

// ANSI escape codes for terminal colors.
const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
)

func colorFor(l status.Level) string {
	switch l {
	case status.OK:
		return colorGreen
	case status.Warn:
		return colorYellow
	default:
		return colorRed
	}
}

// label is the text for a level; findings with level OK are notes.
func label(l status.Level, finding bool) string {
	if finding && l == status.OK {
		return "NOTE"
	}
	return l.String()
}

// paint wraps s in the color for l, if colors are enabled.
func paint(s string, l status.Level, color bool) string {
	if !color {
		return s
	}
	return colorFor(l) + s + colorReset
}

// printReport writes the tables, the findings and a summary line. global
// holds findings that concern the run, not a single node.
func printReport(w io.Writer, rows []row, global []status.Finding, overall status.Level, color bool) error {
	// One table per node type, in the order the types first appear in
	// the config.
	var types []string
	for _, r := range rows {
		if !slices.Contains(types, r.typ) {
			types = append(types, r.typ)
		}
	}
	for i, typ := range types {
		if i > 0 {
			fmt.Fprintln(w)
		}
		var group []row
		for _, r := range rows {
			if r.typ == typ {
				group = append(group, r)
			}
		}
		if err := printTable(w, group, color); err != nil {
			return err
		}
	}

	var findings []string
	for _, f := range global {
		lbl := fmt.Sprintf("%-4s", label(f.Level, true))
		findings = append(findings, fmt.Sprintf("  %s  %s", paint(lbl, f.Level, color), f.Message))
	}
	counts := map[status.Level]int{}
	for _, r := range rows {
		counts[r.level]++
		for _, a := range r.findings {
			for _, f := range a.Findings {
				lbl := fmt.Sprintf("%-4s", label(f.Level, true))
				findings = append(findings, fmt.Sprintf("  %s  %s %s: %s",
					paint(lbl, f.Level, color), r.node, a.name, f.Message))
			}
		}
	}
	if len(findings) > 0 {
		fmt.Fprintln(w, "\nFindings:")
		for _, f := range findings {
			fmt.Fprintln(w, f)
		}
	}

	fmt.Fprintf(w, "\nStatus: %s (%d OK, %d WARN, %d CRIT)\n",
		paint(overall.String(), overall, color), counts[status.OK], counts[status.Warn], counts[status.Crit])
	return nil
}

// printTable writes the table of rows, which are all of one node type.
func printTable(w io.Writer, rows []row, color bool) error {
	// tabwriter aligns tab-separated columns. Arguments: minwidth, tabwidth,
	// padding, padchar, flags.
	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNODE\t"+strings.Join(rows[0].columns, "\t"))
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.level, r.node, strings.Join(r.cells, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Colors are added after alignment: escape codes have no visible width,
	// but tabwriter would count them and misalign the columns. The status
	// word is at the start of each row, so it can be colored afterwards.
	lines := strings.Split(strings.TrimSuffix(table.String(), "\n"), "\n")
	fmt.Fprintln(w, lines[0])
	for i, line := range lines[1:] {
		l := rows[i].level
		word := l.String()
		fmt.Fprintln(w, paint(word, l, color)+strings.TrimPrefix(line, word))
	}
	return nil
}

// formatVersion renders a version cell, e.g. "Geth 1.17.10 (stable)", with
// the latest release appended when the client is behind.
func formatVersion(v software.Reported, err error, in status.VersionInput) string {
	if err != nil {
		return "ERROR"
	}
	if v.Name == software.Unknown || !v.HasVersion() {
		// Show what the client reported, so the user can still see it.
		return fmt.Sprintf("unrecognized: %q", v.Raw)
	}
	s := v.Name.String() + " " + v.Version
	if v.Suffix != "" {
		s += " (" + v.Suffix + ")"
	}
	if latest, ok := status.Latest(in); ok {
		running, _ := releases.ParseVersion(v.Version)
		running.Suffix = v.Suffix
		if releases.Compare(v.Name, running, latest) < 0 {
			s += " -> " + latest.String()
		}
	}
	return s
}

// formatSync handles the cases shared by both sync columns; format is only
// called when there is a status to show.
func formatSync(skipped bool, err error, format func() string) string {
	switch {
	case skipped:
		return "-"
	case err != nil:
		return "ERROR"
	default:
		return format()
	}
}

// plural formats a count with a singular or plural noun: "1 block", "2 blocks".
func plural(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
