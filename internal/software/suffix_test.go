package software

import "testing"

func TestClassifySuffix(t *testing.T) {
	tests := []struct {
		client Name
		suffix string
		want   Kind
	}{
		// Release.
		{Geth, "", KindRelease},
		{Geth, "stable", KindRelease},
		{Besu, "hotfix", KindRelease},
		{Nimbus, "stateofus", KindRelease}, // codename, not a marker
		{Nimbus, "rc.1", KindRelease},      // Nimbus suffix is always ignored
		// Ethrex channels (see internal/ethereum/testdata/SOURCES.md).
		{Ethrex, "HEAD", KindRelease},    // release binary
		{Ethrex, "stable", KindRelease},  // promoted release image
		{Ethrex, "release", KindRelease}, // final tag build
		{Ethrex, "rc.2", KindPreRelease},
		{Ethrex, "main", KindDevBuild}, // any other channel is a branch
		{Ethrex, "fix-snap-sync", KindDevBuild},

		// Aztec: suffixes of real aztec-packages tags (recorded releases
		// in internal/releases/testdata) and advisory ranges. Only the
		// plain version is a release.
		{Aztec, "", KindRelease},
		{Aztec, "rc.2", KindPreRelease},             // v5.0.0-rc.2
		{Aztec, "nightly.20260618", KindDevBuild},   // v4.4.0-nightly.20260618
		{Aztec, "nightly.20260512-2", KindDevBuild}, // v4.3.0-nightly.20260512-2
		{Aztec, "dev", KindDevBuild},                // v0.0.1-dev
		{Aztec, "commit.fff30aa", KindDevBuild},     // v0.0.1-commit.fff30aa
		{Aztec, "private", KindUnknown},             // v0.0.1-private, not flagged as pre-release
		{Aztec, "devnet.4-patch.0", KindUnknown},    // v4.0.0-devnet.4-patch.0
		{Aztec, "spartan.20260207", KindUnknown},    // advisory range ">= 4.0.0-spartan.20260207"
		{Aztec, "stable", KindUnknown},              // a release word for other software, not for Aztec
		{Aztec, "0", KindUnknown},                   // advisory range "< 7.0.0-0": below 7.0.0

		// Pre-release, in all spellings seen in the recorded tags.
		{Lighthouse, "rc.1", KindPreRelease},
		{Erigon, "rc1", KindPreRelease},
		{Besu, "RC1", KindPreRelease},
		{Grandine, "rc0", KindPreRelease},
		{Nethermind, "rc", KindPreRelease},
		{Erigon, "alpha7", KindPreRelease},
		{Reth, "alpha.1", KindPreRelease},
		{Lighthouse, "beta.0", KindPreRelease},
		{Besu, "BETA", KindPreRelease},

		// Dev builds, including node version suffixes from SOURCES.md of
		// the ethereum package.
		{Geth, "unstable", KindDevBuild},
		{Geth, "unstable-20261120", KindDevBuild},
		{Besu, "dev", KindDevBuild},
		{Lodestar, "dev.80c248bb", KindDevBuild},
		{Nethermind, "unstable", KindDevBuild},
		{Teku, "SNAPSHOT", KindDevBuild},

		// Unknown.
		{Prysm, "mekong", KindUnknown},
		{Lighthouse, "exp", KindUnknown},
		{Lighthouse, "tree.1", KindUnknown},

		// Mixed: the most cautious kind wins.
		{Geth, "rc.1-unstable", KindDevBuild},
		{Prysm, "mekong-rc.1", KindPreRelease},
	}
	for _, tt := range tests {
		if got := ClassifySuffix(tt.client, tt.suffix); got != tt.want {
			t.Errorf("ClassifySuffix(%s, %q) = %s, want %s", tt.client, tt.suffix, got, tt.want)
		}
	}
}
