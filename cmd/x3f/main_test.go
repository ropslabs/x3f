package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/status"
)

// now is the fixed "current time" for all CLI tests. The recorded GitHub
// releases were fetched on this day.
var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// fixture reads a recorded node response from the ethereum package's
// testdata (sources: internal/ethereum/testdata/SOURCES.md).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "ethereum", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mockClient starts a server that answers like a client: JSON-RPC requests
// are matched by method, GET requests by the end of the path (so a path
// prefix such as /v3/<key> is allowed). Unknown requests get a 404.
func mockClient(t *testing.T, responses map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.Method == http.MethodPost {
			var req struct {
				Method string `json:"method"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			key = req.Method
		}
		for k, body := range responses {
			if key == k || strings.HasSuffix(key, k) {
				w.Write(body)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeGitHub serves the recorded releases from internal/releases/testdata
// (sources: internal/releases/testdata/SOURCES.md).
type fakeGitHub struct {
	*httptest.Server
	down     atomic.Bool
	mu       sync.Mutex
	requests map[string]int
	auth     []string
	// override replaces the recorded body for a path, e.g. to simulate a
	// release list from before the newest release.
	override map[string][]byte
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	gh := &fakeGitHub{requests: map[string]int{}, override: map[string][]byte{}}
	gh.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gh.mu.Lock()
		gh.requests[r.URL.Path]++
		gh.auth = append(gh.auth, r.Header.Get("Authorization"))
		gh.mu.Unlock()
		if gh.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		gh.mu.Lock()
		body, overridden := gh.override[r.URL.Path]
		gh.mu.Unlock()
		if overridden {
			w.Write(body)
			return
		}
		// /repos/{owner}/{repo}/releases -> {owner}_{repo}.json
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "repos" || parts[3] != "releases" {
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("..", "..", "internal", "releases", "testdata", "github", parts[1]+"_"+parts[2]+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(gh.Close)
	return gh
}

func testEnv(gh *fakeGitHub, at time.Time) env {
	return env{
		client:        ethereum.NewClient(nil),
		githubBaseURL: gh.URL,
		now:           func() time.Time { return at },
	}
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x3f.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// rowCells returns the table cells of one row. tabwriter pads columns with
// at least two spaces, so two or more spaces separate cells.
func rowCells(t *testing.T, out, node string) []string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		cells := regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(l), -1)
		if len(cells) > 1 && cells[1] == node {
			return cells
		}
	}
	t.Fatalf("no row for %s:\n%s", node, out)
	return nil
}

// mocks are node servers answering with recorded fixtures.
type mocks struct {
	gethSynced, gethIndexing, besuOutdated, proxyBlocksVersion *httptest.Server
	lighthouseSynced, nimbusSynced, broken                     *httptest.Server
	deadAddr                                                   string
}

func newMocks(t *testing.T) mocks {
	t.Helper()
	dead := httptest.NewServer(http.NotFoundHandler())
	addr := dead.Listener.Addr().String()
	dead.Close() // nothing listens there any more: connections are refused

	return mocks{
		gethSynced: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/geth.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		}),
		gethIndexing: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/geth.json"),
			"eth_syncing":        fixture(t, "execution/syncing-geth-indexing.json"),
		}),
		// besu/v23.1.1-dev-ac23d311: a dev build, years behind.
		besuOutdated: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/besu.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		}),
		// Blocks web3_clientVersion but lets eth_syncing through.
		proxyBlocksVersion: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/error-method-not-found.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		}),
		lighthouseSynced: mockClient(t, map[string][]byte{
			"/eth/v1/node/version": fixture(t, "consensus/lighthouse.json"),
			"/eth/v1/node/syncing": fixture(t, "consensus/syncing-synced.json"),
		}),
		nimbusSynced: mockClient(t, map[string][]byte{
			"/eth/v1/node/version": fixture(t, "consensus/nimbus.json"),
			"/eth/v1/node/syncing": fixture(t, "consensus/syncing-synced.json"),
		}),
		broken:   mockClient(t, nil), // 404 for everything
		deadAddr: addr,
	}
}

// TestRunReport checks the whole report end to end and makes sure that no
// part of any endpoint URL and not the GitHub token appear in the output.
func TestRunReport(t *testing.T) {
	const key = "apikey0123456789secret"
	const token = "ghp_secrettoken987654321"
	m := newMocks(t)
	gh := newFakeGitHub(t)

	path := writeConfig(t, `
nodes:
  - name: good-node
    execution_url: `+m.gethSynced.URL+`/v3/`+key+`
    beacon_url: `+m.lighthouseSynced.URL+`/?key=`+key+`
  - name: warn-node
    execution_url: `+m.gethIndexing.URL+`
    beacon_url: `+m.lighthouseSynced.URL+`
  - name: outdated-node
    execution_url: `+m.besuOutdated.URL+`/`+key+`
    beacon_url: `+m.nimbusSynced.URL+`
  - name: down-node
    execution_url: http://`+m.deadAddr+`/v3/`+key+`
    beacon_url: `+m.broken.URL+`/`+key+`
  - name: proxy-node
    execution_url: `+m.proxyBlocksVersion.URL+`/`+key+`
    beacon_url: `+m.lighthouseSynced.URL+`
`)
	e := testEnv(gh, now)
	e.githubToken = token
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"-config", path, "-timeout", "2s", "-cache-dir", t.TempDir()}, &out, &errOut, e)
	got := out.String()
	if code != exitCrit {
		t.Errorf("exit code = %d, want %d\n%s%s", code, exitCrit, got, errOut.String())
	}

	rows := []struct {
		node string
		want []string
	}{
		{"NODE", []string{"STATUS", "NODE", "EXECUTION", "EL SYNC", "CONSENSUS", "CL SYNC"}},
		{"good-node", []string{"OK", "good-node", "Geth 1.17.10 (stable)", "synced", "Lighthouse 8.3.3", "synced"}},
		{"warn-node", []string{"WARN", "warn-node", "Geth 1.17.10 (stable)", "indexing", "Lighthouse 8.3.3", "synced"}},
		{"outdated-node", []string{"CRIT", "outdated-node", "Besu 23.1.1 (dev) -> 26.11.2", "synced", "Nimbus 26.10.2 (stateofus)", "synced"}},
		{"down-node", []string{"CRIT", "down-node", "ERROR", "-", "ERROR", "ERROR"}},
		{"proxy-node", []string{"WARN", "proxy-node", "ERROR", "synced", "Lighthouse 8.3.3", "synced"}},
	}
	for _, r := range rows {
		if cells := rowCells(t, got, r.node); !slices.Equal(cells, r.want) {
			t.Errorf("row %s:\n got  %q\n want %q", r.node, cells, r.want)
		}
	}

	for _, want := range []string{
		"WARN  warn-node execution sync: blocks complete, still building indexes",
		`CRIT  outdated-node execution: Besu dev build of 23.1.1 ("dev"): stable 23.1.1 released`,
		"; latest 26.11.2 (grace period 30 days)",
		"CRIT  down-node execution: unreachable: web3_clientVersion: connection failed",
		"WARN  down-node consensus: version query failed: /eth/v1/node/version: unexpected HTTP status 404",
		"WARN  down-node consensus sync: sync status query failed",
		"WARN  proxy-node execution: version query failed: web3_clientVersion: JSON-RPC error -32601",
		"Status: CRIT (1 OK, 2 WARN, 2 CRIT)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}

	// One GitHub request per detected client repository, even though Geth
	// and Lighthouse run on several nodes.
	gh.mu.Lock()
	wantRequests := map[string]int{
		"/repos/ethereum/go-ethereum/releases":  1,
		"/repos/besu-eth/besu/releases":         1,
		"/repos/sigp/lighthouse/releases":       1,
		"/repos/status-im/nimbus-eth2/releases": 1,
	}
	if !maps.Equal(gh.requests, wantRequests) {
		t.Errorf("GitHub requests = %v, want %v", gh.requests, wantRequests)
	}
	for _, a := range gh.auth {
		if a != "Bearer "+token {
			t.Errorf("Authorization header = %q", a)
		}
	}
	gh.mu.Unlock()

	all := got + errOut.String()
	for _, secret := range []string{key, token, m.deadAddr, "127.0.0.1", "http://"} {
		if strings.Contains(all, secret) {
			t.Errorf("output leaks %q:\n%s", secret, all)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	tests := []struct {
		name  string
		nodes string
		want  int
	}{
		{"all green", "  - {name: a, execution_url: " + m.gethSynced.URL + ", beacon_url: " + m.lighthouseSynced.URL + "}\n", exitOK},
		{"one yellow", "  - {name: a, execution_url: " + m.gethIndexing.URL + ", beacon_url: " + m.lighthouseSynced.URL + "}\n", exitWarn},
		{"one red", "  - {name: a, execution_url: http://" + m.deadAddr + ", beacon_url: " + m.lighthouseSynced.URL + "}\n", exitCrit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, "nodes:\n"+tt.nodes)
			var out bytes.Buffer
			code := run(context.Background(), []string{"-config", path, "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
			if code != tt.want {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.want, out.String())
			}
		})
	}
}

// When GitHub is down, cached data younger than 7 days only produces a note;
// older or missing data turns the status yellow.
func TestRunGitHubOutage(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	path := writeConfig(t, "nodes:\n  - {name: a, execution_url: "+m.gethSynced.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	cache := t.TempDir()

	runAt := func(at time.Time, cacheDir string) (int, string) {
		var out bytes.Buffer
		code := run(context.Background(), []string{"-config", path, "-cache-dir", cacheDir}, &out, &out, testEnv(gh, at))
		return code, out.String()
	}

	// GitHub up: fills the cache.
	if code, out := runAt(now, cache); code != exitOK {
		t.Fatalf("initial run: exit %d\n%s", code, out)
	}

	gh.down.Store(true)

	code, out := runAt(now.Add(3*24*time.Hour), cache)
	if code != exitOK || !strings.Contains(out, "NOTE  a execution: GitHub not reachable, using release data from 2026-11-24") {
		t.Errorf("3 days: exit %d, want 0 with a note\n%s", code, out)
	}

	code, out = runAt(now.Add(8*24*time.Hour), cache)
	if code != exitWarn || !strings.Contains(out, "WARN  a execution: release data is 8 days old") {
		t.Errorf("8 days: exit %d, want 1 with a warning\n%s", code, out)
	}

	code, out = runAt(now, t.TempDir()) // empty cache
	if code != exitWarn || !strings.Contains(out, "latest release unknown") {
		t.Errorf("no cache: exit %d, want 1\n%s", code, out)
	}
}

func TestRunUsageErrors(t *testing.T) {
	gh := newFakeGitHub(t)
	tests := []struct {
		name    string
		args    []string
		want    int
		wantErr string
	}{
		{"missing config", []string{"-config", filepath.Join(t.TempDir(), "nope.yaml")}, exitUnknown, "read config"},
		{"unknown flag", []string{"-bogus"}, exitUnknown, "flag provided but not defined"},
		{"zero timeout", []string{"-timeout", "0s"}, exitUnknown, "-timeout must be positive"},
		{"negative grace", []string{"-grace-days", "-1"}, exitUnknown, "-grace-days must not be negative"},
		{"help", []string{"-help"}, exitOK, "Exit codes (Nagios plugin convention)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(context.Background(), tt.args, &out, &errOut, testEnv(gh, now))
			if code != tt.want || !strings.Contains(errOut.String(), tt.wantErr) {
				t.Errorf("exit %d, stderr %q; want %d and %q", code, errOut.String(), tt.want, tt.wantErr)
			}
		})
	}
}

func TestHelpDocumentsExitCodesAndEnvironment(t *testing.T) {
	var errOut bytes.Buffer
	run(context.Background(), []string{"-help"}, &bytes.Buffer{}, &errOut, env{})
	for _, want := range []string{"0  OK", "1  WARNING", "2  CRITICAL", "3  UNKNOWN", "GITHUB_TOKEN", "NO_COLOR", "-grace-days", "-cache-ttl"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, errOut.String())
		}
	}
}

// Colors must not change the layout: without escape codes the colored
// output is identical to the plain one.
func TestColorKeepsAlignment(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	path := writeConfig(t, `
nodes:
  - {name: good-node, execution_url: `+m.gethSynced.URL+`, beacon_url: `+m.lighthouseSynced.URL+`}
  - {name: warn, execution_url: `+m.gethIndexing.URL+`, beacon_url: `+m.lighthouseSynced.URL+`}
`)
	cache := t.TempDir()
	runWith := func(color bool) string {
		e := testEnv(gh, now)
		e.color = color
		var out bytes.Buffer
		run(context.Background(), []string{"-config", path, "-cache-dir", cache}, &out, &out, e)
		return out.String()
	}
	plain, colored := runWith(false), runWith(true)

	if !strings.Contains(colored, colorGreen+"OK"+colorReset) || !strings.Contains(colored, colorYellow+"WARN"+colorReset) {
		t.Errorf("colored output lacks colored status words:\n%q", colored)
	}
	stripped := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colored, "")
	if stripped != plain {
		t.Errorf("colored output differs from plain output:\n%s\n---\n%s", stripped, plain)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("plain output contains escape codes")
	}
}

func TestFormatVersion(t *testing.T) {
	latest := &releases.Result{Releases: []releases.Release{{Tag: "v1.17.10"}}}
	tests := []struct {
		name string
		raw  string
		err  error
		rel  *releases.Result
		want string
	}{
		{"current with suffix", "Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3", nil, latest, "Geth 1.17.10 (stable)"},
		{"outdated shows latest", "Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3", nil, latest, "Geth 1.17.9 (stable) -> 1.17.10"},
		{"latest unknown", "Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3", nil, nil, "Geth 1.17.9 (stable)"},
		{"without suffix", "Prysm/v7.3.2-844d8b8 (linux amd64)", nil, nil, "Prysm 7.3.2"},
		{"unknown client shows raw string", "SomeClient/v1.2.3", nil, nil, `unrecognized: "SomeClient/v1.2.3"`},
		{"error", "", errors.New("timeout"), nil, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ethereum.ParseClientVersion(tt.raw)
			in := status.VersionInput{Version: v, Err: tt.err, Releases: tt.rel}
			if got := formatVersion(v, tt.err, in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSync(t *testing.T) {
	never := func() string { t.Error("format must not be called"); return "" }
	if got := formatSync(true, nil, never); got != "-" {
		t.Errorf("skipped: got %q", got)
	}
	if got := formatSync(false, errors.New("x"), never); got != "ERROR" {
		t.Errorf("error: got %q", got)
	}
	if got := formatSync(false, nil, func() string { return "ok" }); got != "ok" {
		t.Errorf("status: got %q", got)
	}
}

func TestFormatExecutionSync(t *testing.T) {
	tests := []struct {
		name string
		in   ethereum.ExecutionSyncStatus
		want string
	}{
		{"not syncing", ethereum.ExecutionSyncStatus{}, "synced"},
		{"behind", ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 0x1518, HighestBlock: 0x9567a3}, "syncing (9785995 blocks behind)"},
		{"one block", ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 9, HighestBlock: 10}, "syncing (1 block behind)"},
		{"indexing", ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 10, HighestBlock: 10, TxIndexRemainingBlocks: 5}, "indexing"},
		{"target unknown", ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 10}, "syncing (target unknown)"},
		{"current ahead of highest", ethereum.ExecutionSyncStatus{Syncing: true, CurrentBlock: 11, HighestBlock: 10}, "syncing (0 blocks behind)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatExecutionSync(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatConsensusSync(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		in   ethereum.ConsensusSyncStatus
		want string
	}{
		{"synced", ethereum.ConsensusSyncStatus{IsOptimistic: &no, ELOffline: &no}, "synced"},
		{"synced, flags missing", ethereum.ConsensusSyncStatus{}, "synced"},
		{"syncing", ethereum.ConsensusSyncStatus{IsSyncing: true, SyncDistance: 1234}, "syncing (1234 slots behind)"},
		{"optimistic", ethereum.ConsensusSyncStatus{IsOptimistic: &yes, ELOffline: &no}, "synced, optimistic"},
		{"el offline", ethereum.ConsensusSyncStatus{ELOffline: &yes}, "synced, EL offline"},
		{"all flags", ethereum.ConsensusSyncStatus{IsSyncing: true, SyncDistance: 1, IsOptimistic: &yes, ELOffline: &yes}, "syncing (1 slot behind), optimistic, EL offline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatConsensusSync(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// A node reporting a version newer than every cached release triggers an
// immediate refresh of that repository, ignoring the TTL.
func TestRunRefreshesCacheForNewRelease(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	const gethPath = "/repos/ethereum/go-ethereum/releases"

	// GitHub's view before v1.17.10 existed: the recorded list without its
	// first entry.
	var all []releases.Release
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "releases", "testdata", "github", "ethereum_go-ethereum.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &all); err != nil || all[0].Tag != "v1.17.10" {
		t.Fatalf("unexpected fixture: %v", err)
	}
	before, _ := json.Marshal(all[1:])
	gh.override[gethPath] = before

	path := writeConfig(t, "nodes:\n  - {name: a, execution_url: "+m.gethSynced.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	cache := t.TempDir()
	runOnce := func() (int, string) {
		var out bytes.Buffer
		code := run(context.Background(), []string{"-config", path, "-cache-dir", cache}, &out, &out, testEnv(gh, now))
		return code, out.String()
	}

	// Run 1: GitHub really does not know 1.17.10 yet (fresh fetch, so no
	// refresh): yellow.
	code, out := runOnce()
	if code != exitWarn || !strings.Contains(out, "Geth 1.17.10 is newer than any known release (newest 1.17.9)") {
		t.Fatalf("run 1: exit %d\n%s", code, out)
	}

	// The release is published. Run 2 is within the cache TTL, but the
	// node's version is ahead of the cache, so x3f asks GitHub again.
	gh.mu.Lock()
	delete(gh.override, gethPath)
	gh.mu.Unlock()
	code, out = runOnce()
	if code != exitOK {
		t.Errorf("run 2: exit %d, want 0\n%s", code, out)
	}
	gh.mu.Lock()
	if n := gh.requests[gethPath]; n != 2 {
		t.Errorf("go-ethereum requests = %d, want 2", n)
	}
	gh.mu.Unlock()
}

// The live-recorded dRPC answer stays yellow even after a refresh.
func TestRunProxyVersion(t *testing.T) {
	gh := newFakeGitHub(t)
	drpc := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/live/proxy-drpc-mainnet.json"),
		"eth_syncing":        fixture(t, "execution/syncing-false.json"),
	})
	m := newMocks(t)
	path := writeConfig(t, "nodes:\n  - {name: drpc, execution_url: "+drpc.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	cache := t.TempDir()

	var out bytes.Buffer
	for i := 0; i < 2; i++ { // second run: from cache, then refreshed
		out.Reset()
		code := run(context.Background(), []string{"-config", path, "-cache-dir", cache}, &out, &out, testEnv(gh, now))
		if code != exitWarn {
			t.Errorf("run %d: exit %d, want 1\n%s", i+1, code, out.String())
		}
	}
	if !strings.Contains(out.String(), "WARN  drpc execution: Geth 10.0.0 is newer than any known release (newest 1.17.10): proxy or custom build?") {
		t.Errorf("missing proxy warning:\n%s", out.String())
	}
	gh.mu.Lock()
	if n := gh.requests["/repos/ethereum/go-ethereum/releases"]; n != 2 {
		t.Errorf("go-ethereum requests = %d, want 2 (fetch, then refresh on the cached run)", n)
	}
	gh.mu.Unlock()
}
