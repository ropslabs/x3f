package status

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/software"
)

// Advisory rules (agreed for build step 5):
//
//	red:    affected by a critical or high advisory whose fix is released
//	yellow: affected by a medium or low advisory, or one without severity,
//	        whose fix is released; affected by a critical or high advisory
//	        (or one without severity) without a released fix ("no fix
//	        released yet, check the advisory for mitigations");
//	        advisories unknown or data older than 7 days
//	note:   affected by a medium or low advisory without a released fix
//	        (the operator cannot act on it; it still counts in the cell
//	        and the metrics); an advisory x3f cannot decide ("all" without
//	        a fix); GitHub not reachable, cached data younger than 7 days;
//	        advisories ignored in the config (they do not count otherwise);
//	        an expired ignore entry (the advisory counts again)
//
// There is no grace period: a published advisory with a released fix is
// exactly what x3f is meant to find.

// MaxListedAdvisories is how many advisories a finding names by ID and
// title; the rest are counted.
const MaxListedAdvisories = 3

// AdvisoryInput is everything needed to assess the advisories of one
// client.
type AdvisoryInput struct {
	Version software.Reported
	// Err is the error of the version request; then nothing is assessed,
	// the version rules already report it.
	Err error
	// Advisories is the GitHub data; nil if it could not be obtained.
	Advisories *releases.AdvisoryResult
	// AdvisoriesErr explains why Advisories is nil.
	AdvisoriesErr error
	// Releases, if known, are the software's own releases (not
	// necessarily of the advisory's repository); they are used to check
	// that a fix is released.
	Releases *releases.Result
	// Ignore is the node's ignore list (global and node entries merged).
	Ignore []config.AdvisoryIgnore
	Now    time.Time
}

// AdvisoryResult is the assessment plus the text for the table cell.
type AdvisoryResult struct {
	Assessment
	// Cell is e.g. "5 (high)", "none known" or "?"; empty if the client
	// was not assessed (unreachable, not recognized, no repository).
	Cell string
	// Counts is the number of advisories that affect the client and are
	// not ignored, per severity: "critical", "high", "medium", "low" and
	// "none" (no severity given), all present. nil if the advisories are
	// unknown or the client was not assessed.
	Counts map[string]int
}

// affectedBy is one advisory that affects the running version.
type affectedBy struct {
	adv      releases.Advisory
	fix      releases.Version
	released bool // a fix exists and is released
	// packages are the affected (or, if unclear, the undecided) packages,
	// for software whose findings name them.
	packages []string
	// ignore is the config entry, for advisories that are ignored.
	ignore config.AdvisoryIgnore
}

// AssessAdvisories applies the advisory rules.
func AssessAdvisories(in AdvisoryInput) AdvisoryResult {
	var r AdvisoryResult
	v := in.Version
	repo, known := software.AdvisoryRepo(v.Name)
	if in.Err != nil || !known || !v.HasVersion() {
		return r
	}
	running, ok := releases.ParseVersion(v.Version)
	if !ok {
		return r
	}
	running.Suffix = v.Suffix
	name := fmt.Sprintf("%s %s", v.Name, v.Version)

	if in.Advisories == nil {
		r.add(Warn, "advisories unknown: %v", in.AdvisoriesErr)
		r.Cell = "?"
		return r
	}
	if err := in.Advisories.RefreshErr; err != nil {
		age := in.Now.Sub(in.Advisories.FetchedAt)
		if age > MaxReleaseDataAge {
			r.add(Warn, "advisory data is %s old, GitHub not reachable: %v", days(age), err)
		} else {
			r.add(OK, "GitHub not reachable, using advisory data from %s: %v",
				in.Advisories.FetchedAt.Format(time.DateOnly), err)
		}
	}

	ignore := make(map[string]config.AdvisoryIgnore, len(in.Ignore))
	for _, e := range in.Ignore {
		ignore[strings.ToLower(e.ID)] = e
	}

	var fixed, unfixed, unclear, ignored []affectedBy
	var expired []config.AdvisoryIgnore
	for _, a := range in.Advisories.Advisories {
		m, fix, hasFix := a.Check(v.Name, running)
		var packages []string
		if software.ListsPackages(v.Name) && m != releases.NotAffected {
			packages = a.Packages(v.Name, running, m)
		}
		if e, ok := ignore[strings.ToLower(a.GHSAID)]; ok {
			if !e.Expired(in.Now) {
				if m != releases.NotAffected {
					ignored = append(ignored, affectedBy{adv: a, packages: packages, ignore: e})
				}
				continue
			}
			expired = append(expired, e)
		}
		switch m {
		case releases.Unclear:
			unclear = append(unclear, affectedBy{adv: a, packages: packages})
		case releases.Affected:
			released := hasFix && (in.Releases == nil || releases.ReleasedAtLeast(v.Name, in.Releases.Releases, fix))
			ab := affectedBy{adv: a, fix: fix, released: released, packages: packages}
			if released {
				fixed = append(fixed, ab)
			} else {
				unfixed = append(unfixed, ab)
			}
		}
	}
	link := fmt.Sprintf("https://github.com/%s/security/advisories", repo)

	if len(fixed) > 0 {
		level := Warn
		if worst(fixed) >= severityRank("high") {
			level = Crit
		}
		fix := fixed[0].fix
		for _, f := range fixed[1:] {
			if releases.Compare(v.Name, f.fix, fix) > 0 {
				fix = f.fix
			}
		}
		r.add(level, "%s is affected by %s, fixed in %s or later: %s; %s",
			name, countAdvisories(fixed, ""), fix, listAdvisories(fixed), link)
	}
	// Without a fix there is no update to install. Severe advisories stay
	// yellow, as mitigations may be needed; medium and low ones are a
	// note, or a node would stay yellow with nothing the operator can do.
	// Without a severity x3f cannot tell, so it stays yellow.
	var unfixedSevere, unfixedMinor []affectedBy
	for _, u := range unfixed {
		if rank := severityRank(u.adv.Severity); rank > 0 && rank < severityRank("high") {
			unfixedMinor = append(unfixedMinor, u)
		} else {
			unfixedSevere = append(unfixedSevere, u)
		}
	}
	for _, g := range []struct {
		level Level
		list  []affectedBy
	}{{Warn, unfixedSevere}, {OK, unfixedMinor}} {
		if len(g.list) > 0 {
			r.add(g.level, "%s is affected by %s: no fix released yet, check the advisory for mitigations: %s; %s",
				name, countAdvisories(g.list, " without a released fix"), listAdvisories(g.list), link)
		}
	}
	// Notes last: they matter less than what is known to affect the node.
	for _, e := range expired {
		r.add(OK, "the ignore_advisories entry for %s expired on %s, the advisory counts again (reason was %q)",
			e.ID, e.Expires.Format(time.DateOnly), clean(e.Reason, 80))
	}
	if len(ignored) > 0 {
		r.add(OK, "%s for %s (ignore_advisories): %s", countIgnored(len(ignored)), name, listIgnored(ignored))
	}
	for _, u := range unclear {
		r.add(OK, "cannot tell whether %s is affected by %s, check it yourself (affected versions %s, patched %s)",
			name, describeAdvisory(u), quoteRanges(u.adv, false), quoteRanges(u.adv, true))
	}

	all := append(slices.Clone(fixed), unfixed...)
	r.Counts = map[string]int{"none": 0}
	for _, s := range severities {
		r.Counts[s] = 0
	}
	for _, a := range all {
		if rank := severityRank(a.adv.Severity); rank > 0 {
			r.Counts[severityName(rank)]++
		} else {
			r.Counts["none"]++
		}
	}
	switch {
	case len(all) == 0:
		r.Cell = "none known"
	case worst(all) > 0:
		r.Cell = fmt.Sprintf("%d (%s)", len(all), severityName(worst(all)))
	default:
		r.Cell = fmt.Sprintf("%d", len(all))
	}
	return r
}

// severities from most to least severe, as GitHub names them.
var severities = []string{"critical", "high", "medium", "low"}

// severityRank orders severities: critical 4 ... low 1, anything else 0.
func severityRank(s string) int {
	i := slices.Index(severities, strings.ToLower(s))
	if i < 0 {
		return 0
	}
	return len(severities) - i
}

func severityName(rank int) string {
	if rank <= 0 || rank > len(severities) {
		return "no severity"
	}
	return severities[len(severities)-rank]
}

func worst(as []affectedBy) int {
	w := 0
	for _, a := range as {
		w = max(w, severityRank(a.adv.Severity))
	}
	return w
}

// countAdvisories says how many advisories there are, by severity:
// "5 published advisories (2 high, 3 medium)".
func countAdvisories(as []affectedBy, qualifier string) string {
	counts := map[int]int{}
	for _, a := range as {
		counts[severityRank(a.adv.Severity)]++
	}
	var parts []string
	for rank := len(severities); rank >= 0; rank-- {
		if n := counts[rank]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, severityName(rank)))
		}
	}
	noun := "published advisories"
	if len(as) == 1 {
		noun = "published advisory"
	}
	return fmt.Sprintf("%d %s%s (%s)", len(as), noun, qualifier, strings.Join(parts, ", "))
}

// listAdvisories names the most severe advisories (newest first within a
// severity) with their titles, so the reader can judge whether they apply
// to their setup, and counts the rest.
func listAdvisories(as []affectedBy) string {
	sorted := slices.Clone(as)
	slices.SortStableFunc(sorted, func(a, b affectedBy) int {
		if c := cmp.Compare(severityRank(b.adv.Severity), severityRank(a.adv.Severity)); c != 0 {
			return c
		}
		if c := b.adv.PublishedAt.Compare(a.adv.PublishedAt); c != 0 {
			return c
		}
		return strings.Compare(a.adv.GHSAID, b.adv.GHSAID)
	})
	var parts []string
	for _, a := range sorted[:min(len(sorted), MaxListedAdvisories)] {
		parts = append(parts, describeAdvisory(a))
	}
	s := strings.Join(parts, ", ")
	if rest := len(sorted) - MaxListedAdvisories; rest > 0 {
		s += fmt.Sprintf(" and %d more", rest)
	}
	return s
}

func countIgnored(n int) string {
	if n == 1 {
		return "1 ignored advisory"
	}
	return fmt.Sprintf("%d ignored advisories", n)
}

// listIgnored names every ignored advisory with the reason from the
// config. Unlike listAdvisories it lists all of them: the user chose each
// one, and a short list hides nothing.
func listIgnored(as []affectedBy) string {
	var parts []string
	for _, a := range as {
		s := fmt.Sprintf("%s, reason %q", describeAdvisory(a), clean(a.ignore.Reason, 80))
		if !a.ignore.Expires.IsZero() {
			s += ", until " + a.ignore.Expires.Format(time.DateOnly)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// AssessIgnoreList reports ignore entries whose ID is in none of lists,
// the published advisories of clients, so that typos do not go unnoticed.
// global marks the global list: its entries apply to every node, so an
// unknown ID may also be a correct one for a client no node runs at the
// moment. The caller must only call it with complete data: a missing list
// would make every entry for that client look unknown.
func AssessIgnoreList(entries []config.AdvisoryIgnore, lists [][]releases.Advisory, clients []string, global bool) Assessment {
	known := make(map[string]bool)
	for _, l := range lists {
		for _, a := range l {
			known[strings.ToLower(a.GHSAID)] = true
		}
	}
	var r Assessment
	for _, e := range entries {
		if known[strings.ToLower(e.ID)] {
			continue
		}
		reason := clean(e.Reason, 80)
		if global {
			r.add(OK, "%s not found in the advisories of the clients in use (%s): a typo, or for a client none of your nodes runs? (reason %q)",
				e.ID, strings.Join(clients, ", "), reason)
		} else {
			r.add(OK, "%s is not a published advisory of %s; check the ID for a typo (reason %q)",
				e.ID, orList(clients), reason)
		}
	}
	return r
}

// orList joins names as "A", "A or B", "A, B or C".
func orList(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// describeAdvisory renders e.g.
// `GHSA-2gjw-fg97-vg3r (high, CVE-2026-26314: "DoS via malicious p2p message")`,
// with the packages if there are any:
// `GHSA-73xc-j27q-wg75 (medium: "UltraHonk …"; packages @aztec/bb.js, barretenberg-rs)`.
func describeAdvisory(ab affectedBy) string {
	a := ab.adv
	meta := severityName(severityRank(a.Severity))
	if a.CVEID != "" {
		meta += ", " + clean(a.CVEID, 30)
	}
	s := fmt.Sprintf("%s (%s: %q", clean(a.GHSAID, 30), meta, clean(a.Summary, 70))
	if len(ab.packages) > 0 {
		s += "; " + listPackages(ab.packages)
	}
	return s + ")"
}

// MaxListedPackages is how many packages describeAdvisory names; the rest
// are counted.
const MaxListedPackages = 3

// listPackages renders "package a" or "packages a, b and 2 more". Package
// names are free text from GitHub and are cleaned.
func listPackages(names []string) string {
	var parts []string
	for _, n := range names[:min(len(names), MaxListedPackages)] {
		parts = append(parts, clean(n, 40))
	}
	s := strings.Join(parts, ", ")
	if rest := len(names) - MaxListedPackages; rest > 0 {
		s += fmt.Sprintf(" and %d more", rest)
	}
	if len(names) == 1 {
		return "package " + s
	}
	return "packages " + s
}

// quoteRanges lists the ranges (or patched versions) of all entries.
func quoteRanges(a releases.Advisory, patched bool) string {
	var parts []string
	for _, e := range a.Vulnerabilities {
		s := e.VulnerableVersionRange
		if patched {
			s = e.PatchedVersions
		}
		parts = append(parts, fmt.Sprintf("%q", clean(s, 40)))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// clean makes text from GitHub safe to print: control characters (which
// could move the cursor or recolor the terminal) and invisible format
// characters (such as right-to-left overrides) become spaces, runs of
// whitespace collapse, and the result is cut to limit characters.
func clean(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}
