package ethereum

import (
	"regexp"
	"strings"
)

// ClientName identifies an Ethereum client implementation.
//
// It is a named string type: the compiler keeps it apart from plain strings,
// so a ClientName cannot be mixed up with, say, a node name by accident.
type ClientName string

// Known clients. The values are lowercase and stable; use String for display.
const (
	Unknown ClientName = "unknown"

	// Execution clients.
	Geth       ClientName = "geth"
	Reth       ClientName = "reth"
	Nethermind ClientName = "nethermind"
	Erigon     ClientName = "erigon"
	Besu       ClientName = "besu"
	Ethrex     ClientName = "ethrex"

	// Consensus clients.
	Lighthouse ClientName = "lighthouse"
	Prysm      ClientName = "prysm"
	Teku       ClientName = "teku"
	Nimbus     ClientName = "nimbus"
	Lodestar   ClientName = "lodestar"
	Grandine   ClientName = "grandine"
)

var displayNames = map[ClientName]string{
	Unknown:    "unknown",
	Geth:       "Geth",
	Reth:       "Reth",
	Nethermind: "Nethermind",
	Erigon:     "Erigon",
	Besu:       "Besu",
	Ethrex:     "Ethrex",
	Lighthouse: "Lighthouse",
	Prysm:      "Prysm",
	Teku:       "Teku",
	Nimbus:     "Nimbus",
	Lodestar:   "Lodestar",
	Grandine:   "Grandine",
}

// String returns the display name. Implementing String() makes fmt use it
// automatically for %s and %v (it satisfies the fmt.Stringer interface).
func (c ClientName) String() string {
	if s, ok := displayNames[c]; ok {
		return s
	}
	return string(c)
}

// ClientVersion is a parsed client version string, as returned by
// web3_clientVersion or /eth/v1/node/version.
//
// Examples (see testdata/SOURCES.md for where they come from):
//
//	Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3
//	  -> Client=geth Version=1.17.10 Suffix=stable Commit=1df30b18
//	Nimbus/v26.10.2-0abc0d-stateofus
//	  -> Client=nimbus Version=26.10.2 Suffix=stateofus Commit=0abc0d
type ClientVersion struct {
	// Raw is the unmodified string reported by the client.
	Raw string
	// Client is the detected implementation, or Unknown.
	Client ClientName
	// Version is the numeric MAJOR.MINOR.PATCH part without a "v" prefix,
	// e.g. "1.17.10". Empty if no version could be found.
	Version string
	// Suffix holds everything after the numeric version that is not the
	// commit, joined with "-", e.g. "stable", "unstable-20261120", "rc.1",
	// "dev", "stateofus". It is kept verbatim: whether a suffix means
	// "pre-release" depends on the client and is decided when comparing.
	Suffix string
	// Commit is the abbreviated git commit hash, if the string contains one.
	Commit string
}

// HasVersion reports whether a numeric version was found.
func (v ClientVersion) HasVersion() bool { return v.Version != "" }

var (
	// versionRe matches a version segment such as "v1.17.10-stable-1df30b18",
	// "2.0.6-70a5c7ea" or "v2.2.2+be201120". Group 1 is the numeric part,
	// group 2 everything after it (suffixes, commit, build metadata).
	versionRe = regexp.MustCompile(`^[vV]?(\d+\.\d+\.\d+)(.*)$`)
	// commitRe matches an abbreviated or full git commit hash.
	commitRe = regexp.MustCompile(`^[0-9a-fA-F]{6,40}$`)
)

// ParseClientVersion parses a client version string. It never fails: if the
// client is not recognised, Client is Unknown; if no version is found,
// Version is empty. Raw is always set, so callers can still show it.
//
// Client detection ignores case ("GETH", "geth" and "Geth" all match).
func ParseClientVersion(raw string) ClientVersion {
	v := ClientVersion{Raw: raw, Client: Unknown}

	// Split on "/" and whitespace. Prysm uses "Prysm/v7.3.2-844d8b8 (linux amd64)",
	// everyone else separates parts with "/". strings.FieldsFunc drops empty
	// fields, so "a//b" and leading spaces are harmless.
	fields := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == '/' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return v
	}

	name := ClientName(strings.ToLower(fields[0]))
	if _, known := displayNames[name]; known && name != Unknown {
		v.Client = name
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
