package releases

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ropslabs/x3f/internal/software"
)

// Range is a parsed vulnerable_version_range of an advisory. GitHub does
// not enforce a format for repository advisories, so maintainers write it
// in several ways; see ParseRange.
type Range struct {
	// all is set for "all" (or an empty range): every version up to the
	// fix is affected.
	all bool
	// exact lists the affected versions, e.g. "21.10.0,21.10.1".
	exact []Version
	// constraints must all hold, e.g. ">= 1.14.0, < 1.14.13".
	constraints []constraint
}

type constraint struct {
	op string // "<", "<=", ">", ">=" or "="
	v  Version
}

var (
	// andRe matches the word "and" between two comparators, as in Erigon's
	// ">=v3.1.0 and  <v3.4.0".
	andRe = regexp.MustCompile(`(?i)\s+and\s+`)
	// itemRe matches one comparator or plain version: "< 1.13.15",
	// "<0.35.0", ">= v1.9.4", "22.4.0-RC2".
	itemRe = regexp.MustCompile(`^(<=|>=|==|<|>|=)?\s*([vV]?\d+\.\d+\.\d+\S*)$`)
)

// ParseRange parses a vulnerable_version_range. Understood spellings (all
// seen in real advisories of client repositories):
//
//	< 1.13.15   <0.35.0   <= 26.10.2   < v7.0.0-beta.5
//	>= 1.14.0, < 1.14.13          comparators joined by "," ...
//	>=v3.1.0 and  <v3.4.0         ... or by "and"
//	21.10.0,21.10.1               a list of affected versions
//	all                           every version (bounded by the fix)
//
// An empty range counts as "all". ok is false for anything else, including
// a mix of plain versions and comparators.
func ParseRange(s string) (r Range, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "all") {
		return Range{all: true}, true
	}
	for item := range strings.SplitSeq(andRe.ReplaceAllString(s, ","), ",") {
		m := itemRe.FindStringSubmatch(strings.TrimSpace(item))
		if m == nil {
			return Range{}, false
		}
		v, ok := ParseVersion(m[2])
		if !ok {
			return Range{}, false
		}
		switch m[1] {
		case "":
			r.exact = append(r.exact, v)
		case "==":
			r.constraints = append(r.constraints, constraint{"=", v})
		default:
			r.constraints = append(r.constraints, constraint{m[1], v})
		}
	}
	if len(r.exact) > 0 && len(r.constraints) > 0 {
		return Range{}, false
	}
	return r, true
}

// hasUpperBound reports whether the range limits affected versions from
// above. Without one, only patched_versions says where the fix starts.
func (r Range) hasUpperBound() bool {
	if len(r.exact) > 0 {
		return true
	}
	for _, c := range r.constraints {
		if c.op == "<" || c.op == "<=" || c.op == "=" {
			return true
		}
	}
	return false
}

// holds reports whether v satisfies one constraint.
func (c constraint) holds(client software.Name, v Version) bool {
	n := Compare(client, v, c.v)
	switch c.op {
	case "<":
		return n < 0
	case "<=":
		return n <= 0
	case ">":
		return n > 0
	case ">=":
		return n >= 0
	default:
		return n == 0
	}
}

// parseFixes parses patched_versions: "26.8.1", ">= 1.16.9", ">=8.3.0",
// "v7.0.0-beta.5", or several of these separated by ",". Empty means no
// fix is named. ok is false if a part is not understood.
func parseFixes(s string) (fixes []Version, ok bool) {
	if strings.TrimSpace(s) == "" {
		return nil, true
	}
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), ">="))
		v, ok := ParseVersion(part)
		if !ok || part == "" {
			return nil, false
		}
		fixes = append(fixes, v)
	}
	return fixes, true
}

// Match is the outcome of checking an advisory against a version.
type Match int

const (
	NotAffected Match = iota
	Affected
	// Unclear means x3f could not decide: the range or the patched
	// versions are not understood, or the range is "all" without a fix.
	Unclear
)

// Check reports whether running is affected by the advisory. If it is,
// fix is the version to update to and hasFix is false if the advisory
// names no patched version above running.
//
// Each entry of Vulnerabilities is checked; the advisory affects running
// if any entry does. A range without an upper bound (">= 24.5.2" or "all")
// is bounded by the lowest patched version: below it, a version is
// affected. If no patched version is named, ">= 24.5.2" means what it
// says, while "all" stays Unclear.
func (a Advisory) Check(client software.Name, running Version) (m Match, fix Version, hasFix bool) {
	affected, unclear, withoutFix := false, false, false
	for _, e := range a.Vulnerabilities {
		entryM, entryFix, entryHasFix := checkEntry(client, e, running)
		switch {
		case entryM == Unclear:
			unclear = true
		case entryM != Affected:
		case !entryHasFix:
			affected, withoutFix = true, true
		default:
			// The highest fix covers every affected entry.
			if !hasFix || Compare(client, entryFix, fix) > 0 {
				fix, hasFix = entryFix, true
			}
			affected = true
		}
	}
	switch {
	case affected && withoutFix:
		// One affected entry without a fix: the advisory has none yet.
		return Affected, Version{}, false
	case affected:
		return Affected, fix, true
	case unclear:
		return Unclear, Version{}, false
	default:
		return NotAffected, Version{}, false
	}
}

// Packages returns the names of the packages whose entries match running
// as m (Affected or Unclear), each once, in the order of the advisory.
// Entries without a package name are left out.
func (a Advisory) Packages(client software.Name, running Version, m Match) []string {
	var names []string
	for _, e := range a.Vulnerabilities {
		entryM, _, _ := checkEntry(client, e, running)
		if entryM == m && e.Package.Name != "" && !slices.Contains(names, e.Package.Name) {
			names = append(names, e.Package.Name)
		}
	}
	return names
}

// checkEntry checks one vulnerability entry; see Check.
func checkEntry(client software.Name, e AdvisoryVulnerability, running Version) (Match, Version, bool) {
	r, ok := ParseRange(e.VulnerableVersionRange)
	if !ok {
		return Unclear, Version{}, false
	}
	fixes, ok := parseFixes(e.PatchedVersions)
	if !ok {
		return Unclear, Version{}, false
	}

	var affected bool
	switch {
	case !r.hasUpperBound() && len(fixes) > 0:
		lowest := fixes[0]
		for _, f := range fixes[1:] {
			if Compare(client, f, lowest) < 0 {
				lowest = f
			}
		}
		affected = Compare(client, running, lowest) < 0
		for _, c := range r.constraints {
			affected = affected && c.holds(client, running)
		}
	case r.all:
		// "all" without a fix: x3f cannot tell anything.
		return Unclear, Version{}, false
	case len(r.exact) > 0:
		for _, v := range r.exact {
			affected = affected || Compare(client, running, v) == 0
		}
	default:
		affected = true
		for _, c := range r.constraints {
			affected = affected && c.holds(client, running)
		}
	}
	if !affected {
		return NotAffected, Version{}, false
	}

	// The fix to recommend is the lowest patched version above running:
	// with backports ("1.13.15, 1.14.2") that is the one in running's line
	// or the next one.
	var fix Version
	found := false
	for _, f := range fixes {
		if Compare(client, f, running) > 0 && (!found || Compare(client, f, fix) < 0) {
			fix, found = f, true
		}
	}
	return Affected, fix, found
}
