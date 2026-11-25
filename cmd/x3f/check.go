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
	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/network"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/status"
)

// layerResult holds what we learned about one client (execution or
// consensus) of a node. S is the type of the sync status and F the type of
// the fork configuration; Go generics let both layers share this struct
// and the code that fills it.
type layerResult[S, F any] struct {
	Version    ethereum.ClientVersion
	VersionErr error
	Sync       S
	SyncErr    error
	ChainID    uint64
	ChainIDErr error
	// ForkConfig is the fork schedule the client reports; nil if the
	// request failed (ForkConfigErr) or was skipped.
	ForkConfig    *F
	ForkConfigErr error
	// Unreachable is set when the version request could not reach the
	// client at all (timeout, connection or DNS failure). The sync and
	// chain ID requests would fail the same way and wait for the same
	// timeout, so they are not sent. Errors the server returned do not
	// skip them: some proxies block single methods such as
	// web3_clientVersion but allow eth_syncing.
	Unreachable bool
}

// nodeResult holds the results for one configured node. It deliberately has
// no URL fields: everything that is printed comes from here, so endpoint
// URLs (which may contain API keys) cannot end up in the output.
type nodeResult struct {
	Name string
	// Network is the expected network from the config ("" if not set).
	Network   string
	Execution layerResult[ethereum.ExecutionSyncStatus, ethereum.ExecutionForkConfig]
	Consensus layerResult[ethereum.ConsensusSyncStatus, ethereum.ConsensusForkConfig]
}

// checkNodes queries all nodes concurrently and returns one result per node,
// in config order. Each request gets its own timeout.
func checkNodes(ctx context.Context, client *ethereum.Client, nodes []config.Node, timeout time.Duration) []nodeResult {
	results := make([]nodeResult, len(nodes))

	// A WaitGroup waits for a set of goroutines to finish. wg.Go starts f
	// in a new goroutine and tracks it; wg.Wait blocks until all are done.
	var wg sync.WaitGroup
	for i, n := range nodes {
		results[i].Name = n.Name
		results[i].Network = n.Network
		// Since Go 1.22 every loop iteration has its own i and n, so the
		// goroutines below capture this iteration's values.
		//
		// Execution and consensus side run in parallel. The two goroutines
		// write to different fields of results[i]; distinct fields are
		// distinct memory, so no mutex is needed.
		wg.Go(func() {
			results[i].Execution = queryLayer(ctx, timeout, n.ExecutionURL,
				client.ExecutionVersion, client.ExecutionSync, client.ExecutionChainID, client.ExecutionForkConfig)
		})
		wg.Go(func() {
			results[i].Consensus = queryLayer(ctx, timeout, n.BeaconURL,
				client.ConsensusVersion, client.ConsensusSync, client.ConsensusChainID, client.ConsensusForkConfig)
		})
	}
	wg.Wait()
	return results
}

// queryLayer asks one client for its version, then its sync status, chain
// ID and fork configuration. The later requests are skipped only if the
// client was unreachable.
// fetchVersion and fetchSync are method values such as
// client.ExecutionVersion: functions bound to their receiver, which can be
// passed around like any other function.
func queryLayer[S, F any](ctx context.Context, timeout time.Duration, endpoint string,
	fetchVersion func(context.Context, string) (string, error),
	fetchSync func(context.Context, string) (S, error),
	fetchChainID func(context.Context, string) (uint64, error),
	fetchForkConfig func(context.Context, string) (F, error)) layerResult[S, F] {

	var r layerResult[S, F]

	raw, err := withTimeout(ctx, timeout, endpoint, fetchVersion)
	if err != nil {
		r.VersionErr = err
		if ethereum.IsTransportError(err) {
			r.Unreachable = true
			return r
		}
	} else {
		r.Version = ethereum.ParseClientVersion(raw)
	}
	r.Sync, r.SyncErr = withTimeout(ctx, timeout, endpoint, fetchSync)
	r.ChainID, r.ChainIDErr = withTimeout(ctx, timeout, endpoint, fetchChainID)
	if cfg, err := withTimeout(ctx, timeout, endpoint, fetchForkConfig); err != nil {
		r.ForkConfigErr = err
	} else {
		r.ForkConfig = &cfg
	}
	return r
}

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

// fetchReleases fetches the releases of every client that was detected,
// once per repository and concurrently.
func fetchReleases(ctx context.Context, src *releases.Source, results []nodeResult, timeout time.Duration) map[releases.Repo]releaseData {
	return fetchPerRepo(ctx, results, timeout, src.Releases)
}

// fetchAdvisories fetches the security advisories the same way.
func fetchAdvisories(ctx context.Context, src *releases.Source, results []nodeResult, timeout time.Duration) map[releases.Repo]advisoryData {
	return fetchPerRepo(ctx, results, timeout, src.Advisories)
}

// fetchPerRepo calls fetch once for the repository of every detected
// client, concurrently. fetch is a method value such as src.Releases; Go
// infers T from its result type.
func fetchPerRepo[T any](ctx context.Context, results []nodeResult, timeout time.Duration,
	fetch func(context.Context, releases.Repo) (T, error)) map[releases.Repo]repoData[T] {

	var repos []releases.Repo
	for _, r := range results {
		for _, v := range []ethereum.ClientVersion{r.Execution.Version, r.Consensus.Version} {
			if repo, ok := releases.RepoFor(v.Client); ok && !slices.Contains(repos, repo) {
				repos = append(repos, repo)
			}
		}
	}

	data := make(map[releases.Repo]repoData[T], len(repos))
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
func refreshIfAhead(ctx context.Context, src *releases.Source, results []nodeResult, rel map[releases.Repo]releaseData, timeout time.Duration) {
	var stale []releases.Repo
	for _, r := range results {
		for _, v := range []ethereum.ClientVersion{r.Execution.Version, r.Consensus.Version} {
			repo, ok := releases.RepoFor(v.Client)
			d := rel[repo]
			if !ok || d.result == nil || !d.result.Cached || slices.Contains(stale, repo) {
				continue
			}
			running, ok := releases.ParseVersion(v.Version)
			running.Suffix = v.Suffix
			if ok && releases.NewerThanAll(v.Client, d.result.Releases, running) {
				stale = append(stale, repo)
			}
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
	node     string
	level    status.Level
	cells    [7]string // network, execution, EL sync, consensus, CL sync, fork, advisories
	findings []layerFindings
}

// layerFindings are the findings of one part of a node, e.g. "execution
// sync". The advisories column has two: one per client.
type layerFindings struct {
	name string
	status.Assessment
}

// assessParams are the settings for assess.
type assessParams struct {
	now            time.Time
	grace          time.Duration
	schedule       *forks.Schedule
	forkCriticalIn time.Duration
}

// assess evaluates all results. It returns the rows and the overall level.
func assess(results []nodeResult, rel map[releases.Repo]releaseData, adv map[releases.Repo]advisoryData, p assessParams) ([]row, status.Level) {
	now, grace := p.now, p.grace
	versionInput := func(v ethereum.ClientVersion, err error) status.VersionInput {
		in := status.VersionInput{Version: v, Err: err, Now: now, Grace: grace}
		if repo, ok := releases.RepoFor(v.Client); ok {
			d := rel[repo]
			in.Releases, in.ReleasesErr = d.result, d.err
		}
		return in
	}
	advisoryInput := func(v ethereum.ClientVersion, err error) status.AdvisoryInput {
		in := status.AdvisoryInput{Version: v, Err: err, Now: now}
		if repo, ok := releases.RepoFor(v.Client); ok {
			d := adv[repo]
			in.Advisories, in.AdvisoriesErr = d.result, d.err
			in.Releases = rel[repo].result
		}
		return in
	}

	overall := status.OK
	rows := make([]row, 0, len(results))
	for _, r := range results {
		ev := versionInput(r.Execution.Version, r.Execution.VersionErr)
		cv := versionInput(r.Consensus.Version, r.Consensus.VersionErr)
		chainID, haveChain := detectedChain(r)
		fork := status.AssessForks(status.ForkInput{
			Schedule: p.schedule, ChainID: chainID, HaveChain: haveChain,
			Execution: r.Execution.Version, Consensus: r.Consensus.Version,
			ExecutionConfig: r.Execution.ForkConfig, ExecutionConfigErr: r.Execution.ForkConfigErr,
			ConsensusConfig: r.Consensus.ForkConfig, ConsensusConfigErr: r.Consensus.ForkConfigErr,
			Now: now, CriticalWithin: p.forkCriticalIn,
		})
		ea := status.AssessAdvisories(advisoryInput(r.Execution.Version, r.Execution.VersionErr))
		ca := status.AssessAdvisories(advisoryInput(r.Consensus.Version, r.Consensus.VersionErr))
		a := []layerFindings{
			{"network", status.AssessNetwork(status.NetworkInput{
				Expected:    r.Network,
				ExecutionID: r.Execution.ChainID, ExecutionErr: r.Execution.ChainIDErr, ExecutionSkipped: r.Execution.Unreachable,
				ConsensusID: r.Consensus.ChainID, ConsensusErr: r.Consensus.ChainIDErr, ConsensusSkipped: r.Consensus.Unreachable,
			})},
			{"execution", status.AssessVersion(ev)},
			{"execution sync", status.AssessExecutionSync(status.SyncInput{Skipped: r.Execution.Unreachable, Err: r.Execution.SyncErr}, r.Execution.Sync)},
			{"consensus", status.AssessVersion(cv)},
			{"consensus sync", status.AssessConsensusSync(status.SyncInput{Skipped: r.Consensus.Unreachable, Err: r.Consensus.SyncErr}, r.Consensus.Sync)},
			{"fork", fork.Assessment},
			{"execution advisories", ea.Assessment},
			{"consensus advisories", ca.Assessment},
		}
		rw := row{
			node: r.Name,
			cells: [7]string{
				formatNetwork(r),
				formatVersion(r.Execution.Version, r.Execution.VersionErr, ev),
				formatSync(r.Execution.Unreachable, r.Execution.SyncErr, func() string { return formatExecutionSync(r.Execution.Sync) }),
				formatVersion(r.Consensus.Version, r.Consensus.VersionErr, cv),
				formatSync(r.Consensus.Unreachable, r.Consensus.SyncErr, func() string { return formatConsensusSync(r.Consensus.Sync) }),
				fork.Cell,
				formatAdvisories(ea.Cell, ca.Cell),
			},
			findings: a,
		}
		for _, x := range a {
			rw.level = max(rw.level, x.Level)
		}
		overall = max(overall, rw.level)
		rows = append(rows, rw)
	}
	return rows, overall
}

// detectedChain returns the node's chain ID for the fork check, preferring
// the execution side. If the two sides disagree, there is no single
// schedule to compare against: AssessNetwork already reports it as
// critical, and a fork check would only add follow-up errors.
func detectedChain(r nodeResult) (uint64, bool) {
	haveEL := !r.Execution.Unreachable && r.Execution.ChainIDErr == nil
	haveCL := !r.Consensus.Unreachable && r.Consensus.ChainIDErr == nil
	switch {
	case haveEL && haveCL && r.Execution.ChainID != r.Consensus.ChainID:
		return 0, false
	case haveEL:
		return r.Execution.ChainID, true
	case haveCL:
		return r.Consensus.ChainID, true
	}
	return 0, false
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

// printReport writes the table, the findings and a summary line. global
// holds findings that concern the run, not a single node.
func printReport(w io.Writer, rows []row, global []status.Finding, overall status.Level, color bool) error {
	// tabwriter aligns tab-separated columns. Arguments: minwidth, tabwidth,
	// padding, padchar, flags.
	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNODE\tNETWORK\tEXECUTION\tEL SYNC\tCONSENSUS\tCL SYNC\tFORK\tADVISORIES")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.level, r.node, strings.Join(r.cells[:], "\t"))
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

// formatVersion renders a version cell, e.g. "Geth 1.17.10 (stable)", with
// the latest release appended when the client is behind.
func formatVersion(v ethereum.ClientVersion, err error, in status.VersionInput) string {
	if err != nil {
		return "ERROR"
	}
	if v.Client == ethereum.Unknown || !v.HasVersion() {
		// Show what the client reported, so the user can still see it.
		return fmt.Sprintf("unrecognized: %q", v.Raw)
	}
	s := v.Client.String() + " " + v.Version
	if v.Suffix != "" {
		s += " (" + v.Suffix + ")"
	}
	if latest, ok := status.Latest(in); ok {
		running, _ := releases.ParseVersion(v.Version)
		running.Suffix = v.Suffix
		if releases.Compare(v.Client, running, latest) < 0 {
			s += " -> " + latest.String()
		}
	}
	return s
}

// formatAdvisories combines the advisory cells of both clients, e.g.
// "EL 5 (high)", "EL 1 (low), CL ?" or "none known". "-" means neither
// client could be assessed.
func formatAdvisories(el, cl string) string {
	var parts []string
	for _, p := range []struct{ layer, cell string }{{"EL", el}, {"CL", cl}} {
		if p.cell != "" && p.cell != "none known" {
			parts = append(parts, p.layer+" "+p.cell)
		}
	}
	switch {
	case len(parts) > 0:
		return strings.Join(parts, ", ")
	case el != "" || cl != "":
		return "none known"
	default:
		return "-"
	}
}

// formatNetwork renders the network cell: the network both clients are on,
// "EL x / CL y" if they disagree, the one that is known if only one side
// answered, "?" if detection failed and "-" if both are unreachable.
func formatNetwork(r nodeResult) string {
	el, cl := r.Execution, r.Consensus
	haveEL := !el.Unreachable && el.ChainIDErr == nil
	haveCL := !cl.Unreachable && cl.ChainIDErr == nil
	switch {
	case haveEL && haveCL && el.ChainID != cl.ChainID:
		return "EL " + network.Describe(el.ChainID) + " / CL " + network.Describe(cl.ChainID)
	case haveEL:
		return network.Describe(el.ChainID)
	case haveCL:
		return network.Describe(cl.ChainID)
	case el.Unreachable && cl.Unreachable:
		return "-"
	default:
		return "?"
	}
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

func formatExecutionSync(s ethereum.ExecutionSyncStatus) string {
	switch {
	case !s.Syncing:
		return "synced"
	case s.Indexing():
		return "indexing"
	}
	behind, ok := s.BlocksBehind()
	if !ok {
		return "syncing (target unknown)"
	}
	return fmt.Sprintf("syncing (%s behind)", plural(behind, "block"))
}

func formatConsensusSync(s ethereum.ConsensusSyncStatus) string {
	parts := []string{"synced"}
	if s.IsSyncing {
		parts[0] = fmt.Sprintf("syncing (%s behind)", plural(s.SyncDistance, "slot"))
	}
	if s.IsOptimistic != nil && *s.IsOptimistic {
		parts = append(parts, "optimistic")
	}
	if s.ELOffline != nil && *s.ELOffline {
		parts = append(parts, "EL offline")
	}
	return strings.Join(parts, ", ")
}

// plural formats a count with a singular or plural noun: "1 block", "2 blocks".
func plural(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
