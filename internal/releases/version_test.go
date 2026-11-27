package releases

import (
	"testing"

	"github.com/ropslabs/x3f/internal/software"
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
		client software.Name
		a, b   string
		want   int
	}{
		{software.Geth, "1.17.10", "1.17.9", 1}, // numeric, not lexical
		{software.Geth, "1.17.9", "1.17.10", -1},
		{software.Geth, "2.0.0", "1.99.99", 1},
		{software.Geth, "1.17.10-stable", "v1.17.10", 0},
		{software.Lighthouse, "8.3.3-rc.1", "8.3.3", -1}, // pre-release before release
		{software.Lighthouse, "8.4.0-rc.0", "8.3.3", 1},
		{software.Geth, "1.17.11-unstable", "1.17.10", 1},
		{software.Besu, "23.10.3-hotfix", "23.10.3", 1}, // hotfix after release
		{software.Nimbus, "26.10.2-stateofus", "v26.10.2", 0},
		{software.Prysm, "7.3.2-mekong", "7.3.2", -1},
		// Pre-releases of the same version (tags from the recorded data).
		{software.Grandine, "3.0.0-rc.1", "3.0.0-rc.2", -1},
		{software.Lighthouse, "8.0.0-rc.2", "8.0.0-rc.10", -1}, // numeric, not lexical
		{software.Erigon, "3.1.0-rc1", "3.1.0-rc2", -1},        // no dot
		{software.Besu, "25.10.0-RC2", "25.10.0-rc1", 1},       // case-insensitive
		{software.Erigon, "3.0.0-alpha7", "3.0.0-beta1", -1},
		{software.Reth, "0.2.0-beta.9", "0.2.0-rc.1", -1},
		{software.Lighthouse, "8.3.0-rc.0", "8.3.0-rc.0", 0},
		{software.Grandine, "2.0.0.rc0", "2.0.0-rc.0", 0},   // dot form equals dash form
		{software.Lighthouse, "1.0.0-rc", "1.0.0-rc.1", -1}, // shorter first
	}
	for _, tt := range tests {
		if got := Compare(tt.client, v(tt.a), v(tt.b)); got != tt.want {
			t.Errorf("Compare(%s, %s, %s) = %d, want %d", tt.client, tt.a, tt.b, got, tt.want)
		}
	}
}
