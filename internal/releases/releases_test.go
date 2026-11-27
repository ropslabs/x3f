package releases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/software"
)

// loadFixture reads a recorded GitHub releases response (SOURCES.md).
func loadFixture(t *testing.T, repo software.Repo) []Release {
	t.Helper()
	data, err := os.ReadFile(fixturePath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var rels []Release
	if err := json.Unmarshal(data, &rels); err != nil {
		t.Fatal(err)
	}
	return rels
}

func fixturePath(repo software.Repo) string {
	return filepath.Join("testdata", "github", repo.Owner+"_"+repo.Name+".json")
}

func TestEveryClientHasRepoAndFixture(t *testing.T) {
	clients := []software.Name{
		software.Geth, software.Reth, software.Nethermind, software.Erigon, software.Besu, software.Ethrex,
		software.Lighthouse, software.Prysm, software.Teku, software.Nimbus, software.Lodestar, software.Grandine,
		software.Aztec,
	}
	for _, c := range clients {
		repo, ok := software.ReleaseRepo(c)
		if !ok {
			t.Errorf("no release repository for %s", c)
			continue
		}
		if _, err := os.Stat(fixturePath(repo)); err != nil {
			t.Errorf("no release fixture for %s (%s)", c, repo)
		}
		repo, ok = software.AdvisoryRepo(c)
		if !ok {
			t.Errorf("no advisory repository for %s", c)
			continue
		}
		if _, err := os.Stat(filepath.Join("testdata", "github", "advisories", repo.Owner+"_"+repo.Name+".json")); err != nil {
			t.Errorf("no advisory fixture for %s (%s)", c, repo)
		}
	}
	if _, ok := software.ReleaseRepo(software.Unknown); ok {
		t.Error("unknown client must not have a release repository")
	}
	if _, ok := software.AdvisoryRepo(software.Unknown); ok {
		t.Error("unknown client must not have an advisory repository")
	}
}

func TestLatestStableFromRecordedReleases(t *testing.T) {
	// Expected values checked by hand against the recorded data. The
	// fixtures contain the traps described in SOURCES.md (rc tags without
	// pre-release flag, flagged clean tags, non-version tags), so a wrong
	// filter would pick something else in several of these.
	tests := []struct {
		client software.Name
		want   string
	}{
		{software.Geth, "1.17.10"},
		{software.Reth, "2.11.0"},
		{software.Nethermind, "2.2.2"}, // 2.3.0-rc is newer but a pre-release
		{software.Erigon, "3.8.2"},
		{software.Besu, "26.11.2"},
		{software.Ethrex, "31.0.0"},    // v32.0.0-rc.2 is newer but a pre-release
		{software.Lighthouse, "8.3.3"}, // v8.4.0-rc.0 is newer but a pre-release
		{software.Prysm, "7.3.2"},
		{software.Teku, "26.10.3"},
		{software.Nimbus, "26.10.2"}, // "nightly" tag is ignored
		{software.Lodestar, "1.51.0"},
		{software.Grandine, "2.0.6"}, // 3.0.0-rc.4 is a pre-release
		{software.Aztec, "6.1.0"},    // aztec-node: v6.0.0-rc.1 is a flagged pre-release
	}
	for _, tt := range tests {
		t.Run(string(tt.client), func(t *testing.T) {
			repo, _ := software.ReleaseRepo(tt.client)
			got, ok := LatestStable(tt.client, loadFixture(t, repo))
			if !ok || got.String() != tt.want {
				t.Errorf("LatestStable = %s, %v; want %s", got, ok, tt.want)
			}
		})
	}

	// The aztec-packages monorepo published the node until v5 and shows
	// every Aztec channel. Its v0.0.1-private release is not flagged as a
	// pre-release, but the Aztec rules do not take it as stable.
	monorepo := loadFixture(t, software.Repo{Owner: "AztecProtocol", Name: "aztec-packages"})
	if got, ok := LatestStable(software.Aztec, monorepo); !ok || got.String() != "6.1.0" {
		t.Errorf("LatestStable(aztec-packages) = %s, %v; want 6.1.0", got, ok)
	}
}

func TestStableFilter(t *testing.T) {
	// Synthetic lists, each built so that only the filter decides the result.
	at := func(s string) time.Time { tm, _ := time.Parse(time.DateOnly, s); return tm }
	tests := []struct {
		name string
		rels []Release
		want string // "" means none found
	}{
		{
			name: "rc tag without pre-release flag is ignored (reth v1.0.0-rc.2)",
			rels: []Release{{Tag: "v0.9.0"}, {Tag: "v1.0.0-rc.2"}},
			want: "0.9.0",
		},
		{
			name: "clean tag with pre-release flag is ignored (besu 24.5.0)",
			rels: []Release{{Tag: "24.4.0"}, {Tag: "24.5.0", Prerelease: true}},
			want: "24.4.0",
		},
		{
			name: "drafts are ignored",
			rels: []Release{{Tag: "1.0.0"}, {Tag: "1.1.0", Draft: true}},
			want: "1.0.0",
		},
		{
			name: "highest version wins over newest date (backport)",
			rels: []Release{{Tag: "3.1.0", PublishedAt: at("2026-01-01")}, {Tag: "2.9.9", PublishedAt: at("2026-06-01")}},
			want: "3.1.0",
		},
		{
			name: "hotfix counts as stable and newer",
			rels: []Release{{Tag: "23.10.3"}, {Tag: "23.10.3-hotfix"}},
			want: "23.10.3-hotfix",
		},
		{
			name: "unknown suffix is not stable (prysm v5.1.2-mekong)",
			rels: []Release{{Tag: "v5.1.1"}, {Tag: "v5.1.2-mekong"}},
			want: "5.1.1",
		},
		{
			name: "no stable release at all",
			rels: []Release{{Tag: "nightly"}, {Tag: "1.0.0-rc.1"}},
			want: "",
		},
		{
			name: "empty list",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LatestStable(software.Besu, tt.rels)
			if tt.want == "" {
				if ok {
					t.Errorf("got %s, want none", got)
				}
				return
			}
			if !ok || got.String() != tt.want {
				t.Errorf("got %s, %v; want %s", got, ok, tt.want)
			}
		})
	}
}

func TestFirstMissing(t *testing.T) {
	repo, _ := software.ReleaseRepo(software.Geth)
	geth := loadFixture(t, repo)
	repo, _ = software.ReleaseRepo(software.Lighthouse)
	lighthouse := loadFixture(t, repo)
	repo, _ = software.ReleaseRepo(software.Aztec)
	aztec := loadFixture(t, repo)

	tests := []struct {
		name     string
		client   software.Name
		rels     []Release
		running  string
		wantTag  string // "" means up to date
		wantTime string
	}{
		{"geth two releases behind", software.Geth, geth, "1.17.8-stable", "v1.17.9", "2026-10-21T15:08:46Z"},
		{"geth one release behind", software.Geth, geth, "1.17.9-stable", "v1.17.10", "2026-10-23T12:07:39Z"},
		{"geth up to date", software.Geth, geth, "1.17.10-stable", "", ""},
		{"geth dev build ahead", software.Geth, geth, "1.17.11-unstable", "", ""},
		// The rc of the latest release is behind the final release; the
		// v8.4.0-rc.0 pre-release (flagged) must not count as missing.
		{"lighthouse rc of latest", software.Lighthouse, lighthouse, "8.3.3-rc.1", "v8.3.3", "2026-11-09T07:12:58Z"},
		{"lighthouse up to date", software.Lighthouse, lighthouse, "8.3.3", "", ""},
		// The node release of aztec-node, not the aztec-packages release
		// of the same number five days earlier.
		{"aztec one release behind", software.Aztec, aztec, "6.0.0", "v6.1.0", "2026-11-24T08:36:18Z"},
		{"aztec up to date", software.Aztec, aztec, "6.1.0", "", ""},
		// aztec-node has no releases before v6.
		{"aztec before v6", software.Aztec, aztec, "5.2.0", "v6.0.0", "2026-10-22T11:28:30Z"},
		{"aztec rc of the first release", software.Aztec, aztec, "6.0.0-rc.1", "v6.0.0", "2026-10-22T11:28:30Z"},
		// A nightly of the next version is ahead of every release.
		{"aztec nightly ahead", software.Aztec, aztec, "6.2.0-nightly.20261126", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, ok := ParseVersion(tt.running)
			if !ok {
				t.Fatalf("bad version %q", tt.running)
			}
			got, found := FirstMissing(tt.client, tt.rels, running)
			if tt.wantTag == "" {
				if found {
					t.Errorf("got %s, want none", got.Tag)
				}
				return
			}
			if !found || got.Tag != tt.wantTag || got.PublishedAt.Format(time.RFC3339) != tt.wantTime {
				t.Errorf("got %s at %s (found=%v), want %s at %s", got.Tag, got.PublishedAt.Format(time.RFC3339), found, tt.wantTag, tt.wantTime)
			}
		})
	}
}

func TestNewestKnown(t *testing.T) {
	repo, _ := software.ReleaseRepo(software.Lighthouse)
	rels := loadFixture(t, repo)
	// The recorded Lighthouse data contains the pre-release v8.4.0-rc.0,
	// newer than the latest stable v8.3.3.
	newest, ok := NewestKnown(software.Lighthouse, rels)
	if !ok || newest.String() != "8.4.0-rc.0" {
		t.Fatalf("NewestKnown = %s, %v; want 8.4.0-rc.0", newest, ok)
	}

	tests := []struct {
		running string
		want    bool
	}{
		{"8.3.3", false},
		{"8.4.0-rc.0", false}, // equal to a known pre-release
		{"8.4.0", true},       // final release not in the data
		{"10.0.0", true},
	}
	for _, tt := range tests {
		v, _ := ParseVersion(tt.running)
		if got := NewerThanAll(software.Lighthouse, rels, v); got != tt.want {
			t.Errorf("NewerThanAll(%s) = %v, want %v", tt.running, got, tt.want)
		}
	}
	if NewerThanAll(software.Lighthouse, nil, Version{Major: 1}) {
		t.Error("NewerThanAll with no data must be false")
	}
}
