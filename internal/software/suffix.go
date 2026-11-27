package software

import (
	"regexp"
	"slices"
	"strings"
)

// Kind classifies a version suffix.
type Kind int

// The iota keyword numbers constants automatically: 0, 1, 2, 3.
const (
	KindRelease    Kind = iota // "", "stable", "hotfix", Nimbus codename
	KindPreRelease             // alpha, beta, rc, pre, preview
	KindDevBuild               // dev, unstable, snapshot, nightly
	KindUnknown                // anything else, e.g. "mekong", "exp"
)

func (k Kind) String() string {
	switch k {
	case KindRelease:
		return "release"
	case KindPreRelease:
		return "pre-release"
	case KindDevBuild:
		return "dev build"
	default:
		return "unrecognized suffix"
	}
}

var (
	preReleasePrefixes = []string{"alpha", "beta", "rc", "pre"} // "pre" covers "preview"
	devBuildPrefixes   = []string{"dev", "unstable", "snapshot", "nightly"}
	releaseWords       = []string{"stable", "hotfix"}
	hexRe              = regexp.MustCompile(`^[0-9a-f]{6,40}$`)
)

// ClassifySuffix decides what a suffix means for the given software.
//
// The suffix is split at "-", "." and "+" and each part is checked, ignoring
// case: "rc.1", "RC1" and "rc0" are all pre-releases. Numbers and commit
// hashes are skipped. If parts disagree, the most cautious kind wins:
// dev build, then pre-release, then unknown, then release.
func ClassifySuffix(client Name, suffix string) Kind {
	// Nimbus always appends its codename (versionBlob, e.g. "stateofus");
	// it never marks pre-releases there.
	if client == Nimbus {
		return KindRelease
	}
	kind := classifyParts(suffix)

	// Ethrex puts a "channel" between version and commit: "stable"
	// (promoted release image), "release" (final tag build), "HEAD"
	// (release binaries, built from a detached tag checkout), "rc.N"
	// (release candidate) or else the git branch of the build.
	if client == Ethrex {
		switch strings.ToLower(suffix) {
		case "", "stable", "release", "head":
			return KindRelease
		}
		if kind == KindPreRelease {
			return kind
		}
		return KindDevBuild
	}

	if client == Aztec {
		return classifyAztec(suffix)
	}
	return kind
}

// classifyAztec applies Aztec's release channels. Only a version without
// suffix is a release: aztec-packages publishes stable releases as plain
// x.y.z, and GitHub's pre-release flag is not reliable (v0.0.1-private is
// not flagged). Release candidates are "rc.N"; nightlies ("nightly.<date>",
// sometimes with "-N"), "dev" and "commit.<hash>" are dev builds. Anything
// else, e.g. "private", "devnet.4-patch.0" or "spartan.<date>", is unknown.
func classifyAztec(suffix string) Kind {
	if suffix == "" {
		return KindRelease
	}
	first, _, _ := strings.Cut(strings.ToLower(suffix), ".")
	first, _, _ = strings.Cut(first, "-")
	switch first {
	case "rc":
		return KindPreRelease
	case "nightly", "dev", "commit":
		return KindDevBuild
	}
	return KindUnknown
}

// classifyParts is the client-independent part of ClassifySuffix.
func classifyParts(suffix string) Kind {
	var dev, pre, unknown bool
	parts := strings.FieldsFunc(strings.ToLower(suffix), func(r rune) bool {
		return r == '-' || r == '.' || r == '+'
	})
	for _, p := range parts {
		switch {
		case isDigits(p) || hexRe.MatchString(p):
			// Build numbers, dates and commits say nothing about the kind.
		case hasAnyPrefix(p, devBuildPrefixes):
			dev = true
		case hasAnyPrefix(p, preReleasePrefixes):
			pre = true
		case slices.Contains(releaseWords, p):
			// Explicit release marker.
		default:
			unknown = true
		}
	}
	switch {
	case dev:
		return KindDevBuild
	case pre:
		return KindPreRelease
	case unknown:
		return KindUnknown
	default:
		return KindRelease
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
