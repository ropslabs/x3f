package ethereum

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ropslabs/x3f/internal/software"
)

// Clients are the Ethereum clients ParseClientVersion recognizes; other
// software, even with a known name, is Unknown here.
var Clients = []software.Name{
	// Execution clients.
	software.Geth, software.Reth, software.Nethermind, software.Erigon, software.Besu, software.Ethrex,
	// Consensus clients.
	software.Lighthouse, software.Prysm, software.Teku, software.Nimbus, software.Lodestar, software.Grandine,
}

var (
	// versionRe matches a version segment such as "v1.17.10-stable-1df30b18",
	// "2.0.6-70a5c7ea" or "v2.2.2+be201120". Group 1 is the numeric part,
	// group 2 everything after it (suffixes, commit, build metadata).
	versionRe = regexp.MustCompile(`^[vV]?(\d+\.\d+\.\d+)(.*)$`)
	// commitRe matches an abbreviated or full git commit hash.
	commitRe = regexp.MustCompile(`^[0-9a-fA-F]{6,40}$`)
)

// ParseClientVersion parses a client version string. It never fails: if the
// client is not recognised, Name is software.Unknown; if no version is found,
// Version is empty. Raw is always set, so callers can still show it.
//
// Client detection ignores case ("GETH", "geth" and "Geth" all match).
func ParseClientVersion(raw string) software.Reported {
	v := software.Reported{Raw: raw, Name: software.Unknown}

	// Split on "/" and whitespace. Prysm uses "Prysm/v7.3.2-844d8b8 (linux amd64)",
	// everyone else separates parts with "/". strings.FieldsFunc drops empty
	// fields, so "a//b" and leading spaces are harmless.
	fields := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == '/' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return v
	}

	name := software.Name(strings.ToLower(fields[0]))
	if slices.Contains(Clients, name) {
		v.Name = name
	}

	// The version is the first field after the name that looks like one.
	// It is not always fields[1]: Geth and Besu insert an optional
	// user-defined identity, e.g. "Geth/my-node/v1.17.10-stable/...".
	for i := 1; i < len(fields); i++ {
		m := versionRe.FindStringSubmatch(fields[i])
		if m == nil {
			continue
		}
		v.Version = m[1]
		v.Suffix, v.Commit = splitSuffix(m[2])

		// Lodestar puts the commit into the next "/" field:
		// "Lodestar/v1.51.0/7923280".
		if v.Commit == "" && i+1 < len(fields) && commitRe.MatchString(fields[i+1]) {
			v.Commit = fields[i+1]
		}
		break
	}
	return v
}

// splitSuffix splits the text after the numeric version into the suffix and
// the commit hash. Examples:
//
//	"-stable-1df30b18"          -> "stable", "1df30b18"
//	"-unstable-1df30b18-20261120" -> "unstable-20261120", "1df30b18"
//	"-rc.1-67da032"             -> "rc.1", "67da032"
//	"+be201120"                 -> "", "be201120"   (Nethermind build metadata)
//	"-0abc0d-stateofus"         -> "stateofus", "0abc0d"
//	"-dev.80c248bb"             -> "dev.80c248bb", ""
func splitSuffix(rest string) (suffix, commit string) {
	// Semantic-versioning build metadata comes after "+". Nethermind puts
	// the commit there.
	rest, build, hasBuild := strings.Cut(rest, "+")

	var parts []string
	for _, p := range strings.Split(rest, "-") {
		if p == "" {
			continue
		}
		// The first hex-looking part is the commit. A date such as
		// "20261120" also looks like hex; Geth puts it after the commit,
		// so it normally ends up in the suffix. Known limitation: a
		// numeric part of 6+ digits without a commit before it is taken
		// as the commit (see TestParseClientVersionKnownLimitations).
		if commit == "" && commitRe.MatchString(p) {
			commit = p
			continue
		}
		parts = append(parts, p)
	}

	if hasBuild {
		if commit == "" && commitRe.MatchString(build) {
			commit = build
		} else if build != "" {
			parts = append(parts, "+"+build)
		}
	}
	return strings.Join(parts, "-"), commit
}
