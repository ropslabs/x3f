package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/software"
)

// gethReleases loads the recorded geth releases from the releases package
// (see internal/releases/testdata/SOURCES.md). Relevant entries:
//
//	v1.17.8  2026-10-08T16:35:36Z
//	v1.17.9  2026-10-21T15:08:46Z
//	v1.17.10 2026-10-23T12:07:39Z (latest)
func gethReleases(t *testing.T) []releases.Release {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "releases", "testdata", "github", "ethereum_go-ethereum.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rels []releases.Release
	if err := json.Unmarshal(data, &rels); err != nil {
		t.Fatal(err)
	}
	return rels
}

// transportErr returns a real TransportError by querying a closed port.
func transportErr(t *testing.T) error {
	t.Helper()
	_, err := ethereum.NewClient(nil).ExecutionVersion(t.Context(), "http://127.0.0.1:1")
	if !httpjson.IsTransportError(err) {
		t.Fatalf("expected transport error, got %v", err)
	}
	return err
}

func TestAssessVersion(t *testing.T) {
	rels := gethReleases(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	fresh := &releases.Result{Releases: rels, FetchedAt: now.Add(-10 * time.Minute)}
	geth := func(raw string) software.Reported { return ethereum.ParseClientVersion(raw) }
	grace := 30 * 24 * time.Hour

	tests := []struct {
		name      string
		in        VersionInput
		wantLevel Level
		wantMsgs  []string // substrings, one per expected finding, in order
	}{
		{
			name:      "up to date",
			in:        VersionInput{Version: geth("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), Releases: fresh},
			wantLevel: OK,
		},
		{
			// For 1.17.8 the first newer release is 1.17.9 (tag v1.17.9), published
			// 33 days and some hours before now.
			name:      "outdated beyond grace",
			in:        VersionInput{Version: geth("Geth/v1.17.8-stable-aaaaaaaa/linux-amd64/go1.27.3"), Releases: fresh},
			wantLevel: Crit,
			wantMsgs:  []string{"Geth 1.17.8 outdated: latest 1.17.10; first newer release 1.17.9 published 33 days ago (grace period 30 days)"},
		},
		{
			name: "outdated within grace",
			in: VersionInput{Version: geth("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), Releases: fresh,
				Now: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)},
			wantLevel: Warn,
			wantMsgs:  []string{"first newer release 1.17.10 published 6 days ago"},
		},
		{
			name:      "dev build ahead of latest",
			in:        VersionInput{Version: geth("Geth/v1.17.11-unstable-1df30b18-20261120/linux-amd64/go1.27.3"), Releases: fresh},
			wantLevel: Warn,
			wantMsgs:  []string{`Geth 1.17.11 is a dev build ("unstable-20261120")`},
		},
		{
			name:      "pre-release",
			in:        VersionInput{Version: geth("Lighthouse/v8.4.0-rc.0-67da032/x86_64-linux"), Releases: &releases.Result{Releases: []releases.Release{{Tag: "v8.3.3", PublishedAt: now}}, FetchedAt: now}},
			wantLevel: Warn,
			wantMsgs:  []string{`Lighthouse 8.4.0 is a pre-release ("rc.0")`},
		},
		{
			// Recorded data: v1.17.8 is out, so a dev build of 1.17.8 is
			// behind its own release (published 46 days before now).
			name:      "dev build whose release is out, beyond grace",
			in:        VersionInput{Version: geth("Geth/v1.17.8-unstable-aaaaaaaa-20261001/linux-amd64/go1.27.3"), Releases: fresh},
			wantLevel: Crit,
			wantMsgs:  []string{`Geth dev build of 1.17.8 ("unstable-20261001"): stable 1.17.8 released 46 days ago; latest 1.17.10 (grace period 30 days)`},
		},
		{
			name: "pre-release whose release is out, within grace",
			in: VersionInput{Version: geth("Lighthouse/v8.3.3-rc.1-67da032/x86_64-linux"),
				Releases: &releases.Result{Releases: []releases.Release{{Tag: "v8.3.3", PublishedAt: now.Add(-5 * 24 * time.Hour)}}, FetchedAt: now}},
			wantLevel: Warn,
			wantMsgs:  []string{`Lighthouse pre-release of 8.3.3 ("rc.1"): stable 8.3.3 released 5 days ago; latest 8.3.3 (grace period 30 days)`},
		},
		{
			// Synthetic: 8.2.0 was never released, the first newer stable
			// release is 8.2.1. Nothing to merge: two separate findings.
			name: "pre-release of a version that was never released",
			in: VersionInput{Version: geth("Lighthouse/v8.2.0-rc.1-67da032/x86_64-linux"),
				Releases: &releases.Result{Releases: []releases.Release{
					{Tag: "v8.2.1", PublishedAt: now.Add(-40 * 24 * time.Hour)},
					{Tag: "v8.3.3", PublishedAt: now.Add(-5 * 24 * time.Hour)},
				}, FetchedAt: now}},
			wantLevel: Crit,
			wantMsgs: []string{`Lighthouse 8.2.0 is a pre-release ("rc.1")`,
				"Lighthouse 8.2.0 outdated: latest 8.3.3; first newer release 8.2.1 published 40 days ago"},
		},
		{
			name:      "release without date",
			in:        VersionInput{Version: geth("Lighthouse/v8.3.2-67da032/x86_64-linux"), Releases: &releases.Result{Releases: []releases.Release{{Tag: "v8.3.3"}}, FetchedAt: now}},
			wantLevel: Warn,
			wantMsgs:  []string{"release date of 8.3.3 unknown"},
		},
		{
			name:      "unknown suffix",
			in:        VersionInput{Version: geth("Prysm/v7.3.2-mekong-844d8b8 (linux amd64)"), Releases: &releases.Result{Releases: []releases.Release{{Tag: "v7.3.2", PublishedAt: now.Add(-48 * time.Hour)}}, FetchedAt: now}},
			wantLevel: Warn,
			// "mekong" ranks below the plain release, so 7.3.2-mekong is
			// behind 7.3.2 (2 days, within grace): one combined finding.
			wantMsgs: []string{`Prysm build of 7.3.2 with unrecognized suffix ("mekong"): stable 7.3.2 released 2 days ago; latest 7.3.2 (grace period 30 days)`},
		},
		{
			// Recorded live: dRPC's proxy answers "Geth/v10.0.0/drpc".
			name:      "version newer than any known release",
			in:        VersionInput{Version: geth("Geth/v10.0.0/drpc"), Releases: fresh},
			wantLevel: Warn,
			wantMsgs:  []string{"Geth 10.0.0 is newer than any known release (newest 1.17.10): proxy or custom build?"},
		},
		{
			name:      "nimbus codename is fine",
			in:        VersionInput{Version: geth("Nimbus/v26.10.2-0abc0d-stateofus"), Releases: &releases.Result{Releases: []releases.Release{{Tag: "v26.10.2"}}, FetchedAt: now}},
			wantLevel: OK,
		},
		{
			name:      "unreachable client is red",
			in:        VersionInput{Err: transportErr(t)},
			wantLevel: Crit,
			wantMsgs:  []string{"unreachable: web3_clientVersion: connection failed"},
		},
		{
			name:      "rejected version query is yellow",
			in:        VersionInput{Err: errors.New("web3_clientVersion: JSON-RPC error -32601")},
			wantLevel: Warn,
			wantMsgs:  []string{"version query failed"},
		},
		{
			name:      "unknown client",
			in:        VersionInput{Version: geth("SomeClient/v1.0.0"), Releases: fresh},
			wantLevel: Warn,
			wantMsgs:  []string{"not recognized"},
		},
		{
			name:      "no release data at all",
			in:        VersionInput{Version: geth("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), ReleasesErr: errors.New("timeout")},
			wantLevel: Warn,
			wantMsgs:  []string{"latest release unknown: timeout"},
		},
		{
			// GitHub down, cache 3 days old: only a note, still green.
			name: "stale cache younger than 7 days is a note",
			in: VersionInput{Version: geth("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"),
				Releases: &releases.Result{Releases: rels, FetchedAt: now.Add(-3 * 24 * time.Hour), RefreshErr: errors.New("HTTP 503")}},
			wantLevel: OK,
			wantMsgs:  []string{"GitHub not reachable, using release data from 2026-11-21: HTTP 503"},
		},
		{
			name: "stale cache older than 7 days is yellow",
			in: VersionInput{Version: geth("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"),
				Releases: &releases.Result{Releases: rels, FetchedAt: now.Add(-8 * 24 * time.Hour), RefreshErr: errors.New("HTTP 503")}},
			wantLevel: Warn,
			wantMsgs:  []string{"release data is 8 days old"},
		},
		{
			name:      "no stable release",
			in:        VersionInput{Version: geth("Geth/v1.0.0-stable/x"), Releases: &releases.Result{Releases: []releases.Release{{Tag: "nightly"}}, FetchedAt: now}},
			wantLevel: Warn,
			wantMsgs:  []string{"no stable release found"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.Now.IsZero() {
				tt.in.Now = now
			}
			tt.in.Grace = grace
			a := AssessVersion(tt.in)
			checkAssessment(t, a, tt.wantLevel, tt.wantMsgs)
		})
	}
}

func TestAssessExecutionSync(t *testing.T) {
	tests := []struct {
		name      string
		in        SyncInput
		s         ethereum.ExecutionSyncStatus
		wantLevel Level
		wantMsgs  []string
	}{
		{"synced", SyncInput{}, ethereum.ExecutionSyncStatus{}, OK, nil},
		{"indexing", SyncInput{}, ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 10, HighestBlock: 10, TxIndexRemainingBlocks: 1}, Warn, []string{"indexes"}},
		{"32 behind is yellow", SyncInput{}, ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 68, HighestBlock: 100}, Warn, []string{"32 blocks behind"}},
		{"33 behind is red", SyncInput{}, ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 67, HighestBlock: 100}, Crit, []string{"33 blocks behind"}},
		{"target unknown", SyncInput{}, ethereum.ExecutionSyncStatus{Syncing: true}, Warn, []string{"target block unknown"}},
		{"skipped", SyncInput{Skipped: true}, ethereum.ExecutionSyncStatus{}, OK, nil},
		{"malformed response", SyncInput{Err: errors.New("eth_syncing: invalid hex quantity")}, ethereum.ExecutionSyncStatus{}, Warn, []string{"sync status query failed"}},
		{"unreachable", SyncInput{Err: transportErr(t)}, ethereum.ExecutionSyncStatus{}, Crit, []string{"sync status unreachable"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssessment(t, AssessExecutionSync(tt.in, tt.s), tt.wantLevel, tt.wantMsgs)
		})
	}
}

func TestAssessConsensusSync(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name      string
		s         ethereum.ConsensusSyncStatus
		wantLevel Level
		wantMsgs  []string
	}{
		{"synced", ethereum.ConsensusSyncStatus{IsOptimistic: &no, ELOffline: &no}, OK, nil},
		{"synced, flags not reported", ethereum.ConsensusSyncStatus{}, OK, nil},
		{"optimistic", ethereum.ConsensusSyncStatus{IsOptimistic: &yes}, Warn, []string{"optimistic"}},
		{"el offline", ethereum.ConsensusSyncStatus{ELOffline: &yes}, Crit, []string{"execution client offline"}},
		{"32 slots", ethereum.ConsensusSyncStatus{IsSyncing: true, SyncDistance: 32}, Warn, []string{"32 slots behind"}},
		{"33 slots", ethereum.ConsensusSyncStatus{IsSyncing: true, SyncDistance: 33}, Crit, []string{"33 slots behind"}},
		{
			"everything at once",
			ethereum.ConsensusSyncStatus{IsSyncing: true, SyncDistance: 1, IsOptimistic: &yes, ELOffline: &yes},
			Crit, []string{"syncing, 1 slot behind", "optimistic", "offline"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssessment(t, AssessConsensusSync(SyncInput{}, tt.s), tt.wantLevel, tt.wantMsgs)
		})
	}
}

func checkAssessment(t *testing.T, a Assessment, wantLevel Level, wantMsgs []string) {
	t.Helper()
	if a.Level != wantLevel {
		t.Errorf("level = %s, want %s (findings %+v)", a.Level, wantLevel, a.Findings)
	}
	if len(a.Findings) != len(wantMsgs) {
		t.Fatalf("got %d findings %+v, want %d", len(a.Findings), a.Findings, len(wantMsgs))
	}
	for i, want := range wantMsgs {
		if !strings.Contains(a.Findings[i].Message, want) {
			t.Errorf("finding %d = %q, want it to contain %q", i, a.Findings[i].Message, want)
		}
	}
}

func TestAssessNetwork(t *testing.T) {
	const mainnet, sepolia, hoodi, devnet = 1, 11155111, 560048, 1337
	tests := []struct {
		name      string
		in        NetworkInput
		wantLevel Level
		wantMsgs  []string
	}{
		{"same network", NetworkInput{ExecutionID: mainnet, ConsensusID: mainnet}, OK, nil},
		{"as configured", NetworkInput{Expected: "hoodi", ExecutionID: hoodi, ConsensusID: hoodi}, OK, nil},
		{
			"mismatch", NetworkInput{ExecutionID: sepolia, ConsensusID: mainnet},
			Crit, []string{"execution client is on sepolia, consensus client on mainnet"},
		},
		{
			"mismatch and not as configured", NetworkInput{Expected: "mainnet", ExecutionID: sepolia, ConsensusID: mainnet},
			Crit, []string{"consensus client on mainnet", "expected mainnet, execution client is on sepolia"},
		},
		{
			// Only one side answered: still compared with the config.
			"one side, wrong network", NetworkInput{Expected: "hoodi", ExecutionID: mainnet, ConsensusSkipped: true},
			Crit, []string{"expected hoodi, execution client is on mainnet"},
		},
		{
			"both unreachable: already reported elsewhere", NetworkInput{ExecutionSkipped: true, ConsensusSkipped: true},
			OK, nil,
		},
		{
			"query rejected", NetworkInput{ExecutionErr: errors.New("eth_chainId: JSON-RPC error -32601"), ConsensusID: mainnet},
			Warn, []string{"execution network detection failed"},
		},
		{
			"query unreachable", NetworkInput{ExecutionID: mainnet, ConsensusErr: transportErr(t)},
			Crit, []string{"consensus network query unreachable"},
		},
		{
			"unknown network, one note", NetworkInput{ExecutionID: devnet, ConsensusID: devnet},
			OK, []string{"unknown network (chain 1337): no fork data available"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssessment(t, AssessNetwork(tt.in), tt.wantLevel, tt.wantMsgs)
		})
	}
}

func TestLevelText(t *testing.T) {
	for _, l := range []Level{OK, Warn, Crit} {
		b, err := l.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Level
		if err := got.UnmarshalText(b); err != nil || got != l {
			t.Errorf("%v: round trip gave %v, %v", l, got, err)
		}
	}
	var l Level
	if err := l.UnmarshalText([]byte("warn")); err == nil {
		t.Error("lowercase level accepted")
	}
}

func TestPlainTag(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"v1.17.10", "1.17.10"},
		{"V1.17.10", "1.17.10"},
		{"v8.4.0-rc.0", "8.4.0-rc.0"},
		{"26.11.2", "26.11.2"}, // Besu tags have no "v"
		{"v", "v"},
		{"", ""},
	} {
		if got := plainTag(tc.tag); got != tc.want {
			t.Errorf("plainTag(%q) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}
