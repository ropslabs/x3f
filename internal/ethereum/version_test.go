package ethereum

import (
	"testing"

	"github.com/ropslabs/x3f/internal/software"
)

func TestParseClientVersion(t *testing.T) {
	// Inputs are the fixtures and literal examples listed in
	// testdata/SOURCES.md, or variants built from the formats documented
	// there (a comment names the rule). Cases named "synthetic" test parser
	// behaviour only and do not claim to be real client output.
	tests := []struct {
		name string
		raw  string
		want software.Reported
	}{
		// Execution clients.
		{
			name: "geth release",
			raw:  "Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3",
			want: software.Reported{Name: software.Geth, Version: "1.17.10", Suffix: "stable", Commit: "1df30b18"},
		},
		{
			// Unstable builds append the git date after the commit
			// (internal/version/version.go WithCommit).
			name: "geth unstable with date",
			raw:  "Geth/v1.17.11-unstable-1df30b18-20261120/linux-amd64/go1.27.3",
			want: software.Reported{Name: software.Geth, Version: "1.17.11", Suffix: "unstable-20261120", Commit: "1df30b18"},
		},
		{
			// --identity inserts a segment before the version (node/config.go NodeName).
			name: "geth with identity",
			raw:  "Geth/my-node/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3",
			want: software.Reported{Name: software.Geth, Version: "1.17.10", Suffix: "stable", Commit: "1df30b18"},
		},
		{
			name: "reth release",
			raw:  "reth/v2.11.0-b9abbcc1/x86_64-unknown-linux-gnu",
			want: software.Reported{Name: software.Reth, Version: "2.11.0", Commit: "b9abbcc1"},
		},
		{
			name: "ethrex release binary",
			raw:  "ethrex/v31.0.0-HEAD-2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/x86_64-unknown-linux-gnu/rustc-v1.93.0",
			want: software.Reported{Name: software.Ethrex, Version: "31.0.0", Suffix: "HEAD", Commit: "2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c"},
		},
		{
			// Same format, channel "stable" from the promoted image.
			name: "ethrex promoted image",
			raw:  "ethrex/v31.0.0-stable-2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/x86_64-unknown-linux-gnu/rustc-v1.93.0",
			want: software.Reported{Name: software.Ethrex, Version: "31.0.0", Suffix: "stable", Commit: "2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c"},
		},
		{
			// Channel "rc.2" from a release candidate tag build.
			name: "ethrex release candidate",
			raw:  "ethrex/v32.0.0-rc.2-0123456789abcdef0123456789abcdef01234567/aarch64-unknown-linux-gnu/rustc-v1.93.0",
			want: software.Reported{Name: software.Ethrex, Version: "32.0.0", Suffix: "rc.2", Commit: "0123456789abcdef0123456789abcdef01234567"},
		},
		{
			// Channel = git branch for other builds.
			name: "ethrex branch build",
			raw:  "ethrex/v31.0.0-main-0123456789abcdef0123456789abcdef01234567/x86_64-unknown-linux-gnu/rustc-v1.93.0",
			want: software.Reported{Name: software.Ethrex, Version: "31.0.0", Suffix: "main", Commit: "0123456789abcdef0123456789abcdef01234567"},
		},
		{
			name: "reth literal example with pre-release",
			raw:  "reth/v0.1.0-alpha.1-428a6dc2f/aarch64-apple-darwin",
			want: software.Reported{Name: software.Reth, Version: "0.1.0", Suffix: "alpha.1", Commit: "428a6dc2f"},
		},
		{
			name: "nethermind commit as build metadata",
			raw:  "Nethermind/v2.2.2+be201120/linux-x64/dotnet10.0.14",
			want: software.Reported{Name: software.Nethermind, Version: "2.2.2", Commit: "be201120"},
		},
		{
			name: "erigon without v prefix",
			raw:  "erigon/3.8.2-8a3f3b2c/linux-amd64/go1.27.4",
			want: software.Reported{Name: software.Erigon, Version: "3.8.2", Commit: "8a3f3b2c"},
		},
		{
			name: "besu literal dev build",
			raw:  "besu/v23.1.1-dev-ac23d311/osx-x86_64/graalvm-java-17",
			want: software.Reported{Name: software.Besu, Version: "23.1.1", Suffix: "dev", Commit: "ac23d311"},
		},
		{
			// Identity segment per BesuVersionUtils.nodeName (L116-L118).
			name: "besu with identity",
			raw:  "besu/my-node/v26.11.2/linux-x86_64/openjdk-java-21",
			want: software.Reported{Name: software.Besu, Version: "26.11.2"},
		},

		// Consensus clients.
		{
			name: "beacon API spec example",
			raw:  "Lighthouse/v0.1.5 (Linux x86_64)",
			want: software.Reported{Name: software.Lighthouse, Version: "0.1.5"},
		},
		{
			name: "lighthouse release",
			raw:  "Lighthouse/v8.3.3-57c2abc/x86_64-linux",
			want: software.Reported{Name: software.Lighthouse, Version: "8.3.3", Commit: "57c2abc"},
		},
		{
			// Allowed by the version regex in lighthouse_version/src/lib.rs.
			name: "lighthouse release candidate",
			raw:  "Lighthouse/v8.0.0-rc.1-67da032/x86_64-linux",
			want: software.Reported{Name: software.Lighthouse, Version: "8.0.0", Suffix: "rc.1", Commit: "67da032"},
		},
		{
			name: "prysm with platform in parentheses",
			raw:  "Prysm/v7.3.2-844d8b8 (linux amd64)",
			want: software.Reported{Name: software.Prysm, Version: "7.3.2", Commit: "844d8b8"},
		},
		{
			name: "teku",
			raw:  "teku/v26.10.3/linux-x86_64/-eclipseadoptium-openjdk64bitservervm-java-25",
			want: software.Reported{Name: software.Teku, Version: "26.10.3"},
		},
		{
			name: "nimbus codename after commit",
			raw:  "Nimbus/v26.10.2-0abc0d-stateofus",
			want: software.Reported{Name: software.Nimbus, Version: "26.10.2", Suffix: "stateofus", Commit: "0abc0d"},
		},
		{
			name: "lodestar commit in own segment",
			raw:  "Lodestar/v1.51.0/7923280",
			want: software.Reported{Name: software.Lodestar, Version: "1.51.0", Commit: "7923280"},
		},
		{
			name: "lodestar literal dev example",
			raw:  "Lodestar/v0.36.0-dev.80c248bb/80c248bb",
			want: software.Reported{Name: software.Lodestar, Version: "0.36.0", Suffix: "dev.80c248bb", Commit: "80c248bb"},
		},
		{
			name: "grandine without v prefix",
			raw:  "Grandine/2.0.6-70a5c7ea/x86_64-linux",
			want: software.Reported{Name: software.Grandine, Version: "2.0.6", Commit: "70a5c7ea"},
		},

		// Recorded live on 2026-11-24 (testdata/SOURCES.md, "Live recordings").
		{
			name: "live: geth built with an older Go",
			raw:  "Geth/v1.17.9-stable-9dffe6e9/linux-amd64/go1.25.14",
			want: software.Reported{Name: software.Geth, Version: "1.17.9", Suffix: "stable", Commit: "9dffe6e9"},
		},
		{
			// Live Reth reports a 7-character commit, not 8 as derived
			// from build.rs.
			name: "live: reth with 7-character commit",
			raw:  "reth/v2.11.0-b9abbcc/x86_64-unknown-linux-gnu",
			want: software.Reported{Name: software.Reth, Version: "2.11.0", Commit: "b9abbcc"},
		},
		{
			name: "live: nimbus unstable branch build",
			raw:  "Nimbus/v26.10.2-f64d64-stateofus",
			want: software.Reported{Name: software.Nimbus, Version: "26.10.2", Suffix: "stateofus", Commit: "f64d64"},
		},
		{
			// dRPC's load balancer answers with a made-up Geth version.
			// The parser can only report what the string says.
			name: "live: drpc proxy pretends to be geth 10.0.0",
			raw:  "Geth/v10.0.0/drpc",
			want: software.Reported{Name: software.Geth, Version: "10.0.0"},
		},
		{
			name: "live: mev blocker proxy",
			raw:  "MEVblocker",
			want: software.Reported{Name: software.Unknown},
		},
		{
			name: "live: cloudflare gateway",
			raw:  "cloudflare-ethereum",
			want: software.Reported{Name: software.Unknown},
		},

		// Case-insensitive client detection (synthetic).
		{
			name: "synthetic: upper case name",
			raw:  "GETH/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3",
			want: software.Reported{Name: software.Geth, Version: "1.17.10", Suffix: "stable", Commit: "1df30b18"},
		},
		{
			name: "synthetic: mixed case name and V prefix",
			raw:  "LightHouse/V8.3.3-57c2abc/x86_64-linux",
			want: software.Reported{Name: software.Lighthouse, Version: "8.3.3", Commit: "57c2abc"},
		},

		// Unknown or broken input (synthetic).
		{
			name: "synthetic: unknown client keeps version",
			raw:  "SomeClient/v1.2.3-beta.2/linux",
			want: software.Reported{Name: software.Unknown, Version: "1.2.3", Suffix: "beta.2"},
		},
		{
			name: "synthetic: known client without version",
			raw:  "Geth/linux-amd64",
			want: software.Reported{Name: software.Geth},
		},
		{
			name: "synthetic: empty string",
			raw:  "",
			want: software.Reported{Name: software.Unknown},
		},
		{
			name: "synthetic: whitespace only",
			raw:  "   ",
			want: software.Reported{Name: software.Unknown},
		},
		{
			name: "synthetic: literal 'unknown' is not a client",
			raw:  "unknown/v1.0.0",
			want: software.Reported{Name: software.Unknown, Version: "1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseClientVersion(tt.raw)
			tt.want.Raw = tt.raw // Raw must always be the unmodified input.
			if got != tt.want {
				t.Errorf("ParseClientVersion(%q)\n got  %+v\n want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

// TestParseClientVersionKnownLimitations pins down behaviour that is known to
// be wrong, so that it is documented and any change to it is noticed. If you
// fix one of these, move the case to TestParseClientVersion with the correct
// expectation.
func TestParseClientVersionKnownLimitations(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want software.Reported
	}{
		{
			// Commit detection treats the first part with 6+ hex characters
			// as the commit. A purely numeric suffix of 6+ digits matches
			// too. Geth produces such a string when it knows the git date
			// but no commit: WithCommit (internal/version/version.go
			// L46-L55) skips a commit shorter than 8 characters but still
			// appends the date for non-stable builds.
			// Correct would be: Suffix "unstable-20261120", Commit "".
			name: "numeric suffix taken as commit",
			raw:  "Geth/v1.17.11-unstable-20261120/linux-amd64/go1.27.3",
			want: software.Reported{Name: software.Geth, Version: "1.17.11", Suffix: "unstable", Commit: "20261120"},
		},
		{
			// Ethrex puts the git branch into the version segment; a branch
			// name with "/" splits it ("release/v32.0.0" is a real branch
			// name in the Ethrex repository). The suffix becomes "release",
			// which counts as a release, and the commit is lost.
			// Correct would be: Suffix "release/v32.0.0" (a dev build).
			name: "ethrex branch name with slash",
			raw:  "ethrex/v32.0.0-release/v32.0.0-0123456789abcdef0123456789abcdef01234567/x86_64-unknown-linux-gnu/rustc-v1.93.0",
			want: software.Reported{Name: software.Ethrex, Version: "32.0.0", Suffix: "release"},
		},
		{
			// Synthetic: the same happens for any numeric part, e.g. a
			// build number. Correct would be: Suffix "build-123456".
			name: "synthetic: numeric build number taken as commit",
			raw:  "SomeClient/v1.2.3-build-123456/linux",
			want: software.Reported{Name: software.Unknown, Version: "1.2.3", Suffix: "build", Commit: "123456"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseClientVersion(tt.raw)
			tt.want.Raw = tt.raw
			if got != tt.want {
				t.Errorf("behaviour changed for known limitation %q\n got  %+v\n want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
