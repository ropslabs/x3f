package releases

import (
	"testing"

	"github.com/ropslabs/x3f/internal/ethereum"
)

func TestParseVersion(t *testing.T) {
	// Tags as found in the recorded GitHub responses (testdata/SOURCES.md).
	tests := []struct {
		in     string
		want   Version
		wantOK bool
	}{
		{"v1.17.10", Version{1, 17, 10, ""}, true},
		{"26.11.2", Version{26, 11, 2, ""}, true},
		{"v3.4.0-rc.5", Version{3, 4, 0, "rc.5"}, true},
		{"25.10.0-RC2", Version{25, 10, 0, "RC2"}, true},
		{"v3.2.0-rc1", Version{3, 2, 0, "rc1"}, true},
		{"0.4.0.rc0", Version{0, 4, 0, "rc0"}, true}, // Grandine's dot form
		{"23.10.3-hotfix", Version{23, 10, 3, "hotfix"}, true},
		{"2.3.0-rc", Version{2, 3, 0, "rc"}, true},
		{"nightly", Version{}, false},
		{"altair-beta", Version{}, false},
		{"zkvm-guests-2.3.0-preview+6", Version{}, false},
		{"bootnode-1.0.0", Version{}, false},
		{"", Version{}, false},
		{"1.2", Version{}, false},
		{"v99999999999999999999.0.0", Version{}, false}, // overflow
	}
	for _, tt := range tests {
		got, ok := ParseVersion(tt.in)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("ParseVersion(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestClassifySuffix(t *testing.T) {
	tests := []struct {
		client ethereum.ClientName
		suffix string
		want   SuffixKind
	}{
		// Release.
		{ethereum.Geth, "", KindRelease},
		{ethereum.Geth, "stable", KindRelease},
		{ethereum.Besu, "hotfix", KindRelease},
		{ethereum.Nimbus, "stateofus", KindRelease}, // codename, not a marker
		{ethereum.Nimbus, "rc.1", KindRelease},      // Nimbus suffix is always ignored

		// Pre-release, in all spellings seen in the recorded tags.
		{ethereum.Lighthouse, "rc.1", KindPreRelease},
		{ethereum.Erigon, "rc1", KindPreRelease},
		{ethereum.Besu, "RC1", KindPreRelease},
		{ethereum.Grandine, "rc0", KindPreRelease},
		{ethereum.Nethermind, "rc", KindPreRelease},
		{ethereum.Erigon, "alpha7", KindPreRelease},
		{ethereum.Reth, "alpha.1", KindPreRelease},
		{ethereum.Lighthouse, "beta.0", KindPreRelease},
		{ethereum.Besu, "BETA", KindPreRelease},

		// Dev builds, including node version suffixes from SOURCES.md of
		// the ethereum package.
		{ethereum.Geth, "unstable", KindDevBuild},
		{ethereum.Geth, "unstable-20261120", KindDevBuild},
		{ethereum.Besu, "dev", KindDevBuild},
		{ethereum.Lodestar, "dev.80c248bb", KindDevBuild},
		{ethereum.Nethermind, "unstable", KindDevBuild},
		{ethereum.Teku, "SNAPSHOT", KindDevBuild},

		// Unknown.
		{ethereum.Prysm, "mekong", KindUnknown},
		{ethereum.Lighthouse, "exp", KindUnknown},
		{ethereum.Lighthouse, "tree.1", KindUnknown},

		// Mixed: the most cautious kind wins.
		{ethereum.Geth, "rc.1-unstable", KindDevBuild},
		{ethereum.Prysm, "mekong-rc.1", KindPreRelease},
	}
	for _, tt := range tests {
		if got := ClassifySuffix(tt.client, tt.suffix); got != tt.want {
			t.Errorf("ClassifySuffix(%s, %q) = %s, want %s", tt.client, tt.suffix, got, tt.want)
		}
	}
}

func TestCompare(t *testing.T) {
	v := func(s string) Version {
		t.Helper()
		x, ok := ParseVersion(s)
		if !ok {
			t.Fatalf("bad version %q", s)
		}
		return x
	}
	tests := []struct {
		client ethereum.ClientName
		a, b   string
		want   int
	}{
		{ethereum.Geth, "1.17.10", "1.17.9", 1}, // numeric, not lexical
		{ethereum.Geth, "1.17.9", "1.17.10", -1},
		{ethereum.Geth, "2.0.0", "1.99.99", 1},
		{ethereum.Geth, "1.17.10-stable", "v1.17.10", 0},
		{ethereum.Lighthouse, "8.3.3-rc.1", "8.3.3", -1}, // pre-release before release
		{ethereum.Lighthouse, "8.4.0-rc.0", "8.3.3", 1},
		{ethereum.Geth, "1.17.11-unstable", "1.17.10", 1},
		{ethereum.Besu, "23.10.3-hotfix", "23.10.3", 1}, // hotfix after release
		{ethereum.Nimbus, "26.10.2-stateofus", "v26.10.2", 0},
		{ethereum.Prysm, "7.3.2-mekong", "7.3.2", -1},
		// Pre-releases of the same version (tags from the recorded data).
		{ethereum.Grandine, "3.0.0-rc.1", "3.0.0-rc.2", -1},
		{ethereum.Lighthouse, "8.0.0-rc.2", "8.0.0-rc.10", -1}, // numeric, not lexical
		{ethereum.Erigon, "3.1.0-rc1", "3.1.0-rc2", -1},        // no dot
		{ethereum.Besu, "25.10.0-RC2", "25.10.0-rc1", 1},       // case-insensitive
		{ethereum.Erigon, "3.0.0-alpha7", "3.0.0-beta1", -1},
		{ethereum.Reth, "0.2.0-beta.9", "0.2.0-rc.1", -1},
		{ethereum.Lighthouse, "8.3.0-rc.0", "8.3.0-rc.0", 0},
		{ethereum.Grandine, "2.0.0.rc0", "2.0.0-rc.0", 0},   // dot form equals dash form
		{ethereum.Lighthouse, "1.0.0-rc", "1.0.0-rc.1", -1}, // shorter first
	}
	for _, tt := range tests {
		if got := Compare(tt.client, v(tt.a), v(tt.b)); got != tt.want {
			t.Errorf("Compare(%s, %s, %s) = %d, want %d", tt.client, tt.a, tt.b, got, tt.want)
		}
	}
}
