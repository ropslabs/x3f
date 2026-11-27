// Package releases fetches GitHub releases of Ethereum clients (with an
// on-disk cache) and compares versions against them.
package releases

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ropslabs/x3f/internal/software"
)

// Version is a numeric MAJOR.MINOR.PATCH version plus an optional suffix
// such as "rc.1", "stable" or "hotfix".
type Version struct {
	Major, Minor, Patch uint64
	Suffix              string
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Suffix != "" {
		s += "-" + v.Suffix
	}
	return s
}

// tagRe matches release tags and version strings: "v1.17.10", "26.11.2",
// "v3.4.0-rc.5", "25.10.0-RC2", "3.0.0.rc0" (Grandine uses a dot).
var tagRe = regexp.MustCompile(`^[vV]?(\d+)\.(\d+)\.(\d+)(.*)$`)

// ParseVersion parses a tag or version string. Tags that are not versions,
// such as "nightly" or "zkvm-guests-2.3.0-preview+6", return ok=false.
func ParseVersion(s string) (v Version, ok bool) {
	m := tagRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, false
	}
	// The regexp guarantees digits; ParseUint only fails on overflow.
	nums := make([]uint64, 3)
	for i := range nums {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return Version{}, false
		}
		nums[i] = n
	}
	return Version{
		Major:  nums[0],
		Minor:  nums[1],
		Patch:  nums[2],
		Suffix: strings.TrimLeft(m[4], "-.+"),
	}, true
}

// Compare returns -1, 0 or +1 depending on whether a is older than, equal
// to or newer than b. The numeric parts are compared first. For equal
// numbers, a pre-release (or dev build, or unknown suffix) is older than the
// release, and a hotfix is newer than the plain release, as in SemVer. Two
// pre-releases of the same version are ordered by their suffix: rc.1 <
// rc.2 < rc.10, alpha < beta < rc.
func Compare(client software.Name, a, b Version) int {
	if c := cmp.Compare(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Patch, b.Patch); c != 0 {
		return c
	}
	if c := cmp.Compare(rank(client, a.Suffix), rank(client, b.Suffix)); c != 0 {
		return c
	}
	if rank(client, a.Suffix) == 0 {
		return comparePreRelease(a.Suffix, b.Suffix)
	}
	return 0
}

// comparePreRelease orders two non-release suffixes. Both are split into
// runs of letters and runs of digits, ignoring case and the separators
// "-", "." and "+": "rc.10" -> [rc 10], "RC2" -> [rc 2]. Runs are compared
// pairwise, digits numerically and letters alphabetically; a digit run sorts
// before a letter run, and a shorter list before a longer one. This follows
// SemVer pre-release precedence and also handles "rc1" without a dot.
func comparePreRelease(a, b string) int {
	ra, rb := runs(a), runs(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		x, y := ra[i], rb[i]
		xd, yd := isDigits(x), isDigits(y)
		switch {
		case xd && yd:
			// Compare by length first: avoids overflow for long numbers
			// and works because runs have no leading sign.
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if c := cmp.Compare(len(x), len(y)); c != 0 {
				return c
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		case xd:
			return -1
		case yd:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return cmp.Compare(len(ra), len(rb))
}

// runs splits s into lowercase runs of letters and runs of digits.
func runs(s string) []string {
	var out []string
	cur := ""
	curDigit := false
	for _, r := range strings.ToLower(s) {
		digit := r >= '0' && r <= '9'
		letter := r >= 'a' && r <= 'z'
		if !digit && !letter {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		if cur != "" && digit != curDigit {
			out = append(out, cur)
			cur = ""
		}
		cur += string(r)
		curDigit = digit
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func rank(client software.Name, suffix string) int {
	if software.ClassifySuffix(client, suffix) != software.KindRelease {
		return 0
	}
	if strings.Contains(strings.ToLower(suffix), "hotfix") {
		return 2
	}
	return 1
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
