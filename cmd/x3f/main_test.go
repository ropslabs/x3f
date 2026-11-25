package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

// fakeGitHub serves the recorded releases and security advisories from
// internal/releases/testdata (sources: internal/releases/testdata/SOURCES.md).
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
		// /repos/{owner}/{repo}/security-advisories -> advisories/{owner}_{repo}.json
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "repos" {
			http.NotFound(w, r)
			return
		}
		dir := filepath.Join("..", "..", "internal", "releases", "testdata", "github")
		switch parts[3] {
		case "releases":
		case "security-advisories":
			dir = filepath.Join(dir, "advisories")
		default:
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join(dir, parts[1]+"_"+parts[2]+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(gh.Close)
	return gh
}

// deadAddr is a node address whose connections are always refused (see
// testEnv). Closing a test server and reusing its port would be flaky: the
// OS may give the port to another test server in the meantime.
const deadAddr = "dead-node.invalid:8545"

func testEnv(gh *fakeGitHub, at time.Time) env {
	var d net.Dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == deadAddr {
				return nil, &net.OpError{Op: "dial", Net: network, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
			}
			return d.DialContext(ctx, network, addr)
		},
	}
	return env{
		client:        ethereum.NewClient(&http.Client{Transport: transport}),
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
	return mocks{
		gethSynced: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/geth.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
			"eth_config":         fixture(t, "execution/live/eth-config-publicnode-mainnet.json"),
		}),
		gethIndexing: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/geth.json"),
			"eth_syncing":        fixture(t, "execution/syncing-geth-indexing.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
			"eth_config":         fixture(t, "execution/live/eth-config-publicnode-mainnet.json"),
		}),
		// besu/v23.1.1-dev-ac23d311: a dev build, years behind.
		// No eth_config: the release predates EIP-7910 (version fallback).
		besuOutdated: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/besu.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
			"eth_config":         fixture(t, "execution/live/eth-config-mevblocker-mainnet.json"),
		}),
		// Blocks web3_clientVersion but lets eth_syncing through.
		proxyBlocksVersion: mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/error-method-not-found.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
			"eth_config":         fixture(t, "execution/live/eth-config-publicnode-mainnet.json"),
		}),
		lighthouseSynced: mockClient(t, map[string][]byte{
			"/eth/v1/node/version":            fixture(t, "consensus/lighthouse.json"),
			"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
			"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-publicnode-mainnet.json"),
			"/eth/v1/config/spec":             fixture(t, "consensus/live/spec-publicnode-mainnet.json"),
		}),
		nimbusSynced: mockClient(t, map[string][]byte{
			"/eth/v1/node/version":            fixture(t, "consensus/nimbus.json"),
			"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
			"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-publicnode-mainnet.json"),
			"/eth/v1/config/spec":             fixture(t, "consensus/live/spec-publicnode-mainnet.json"),
		}),
		broken:   mockClient(t, nil), // 404 for everything
		deadAddr: deadAddr,
	}
}

// TestRunReport checks the whole report end to end and makes sure that no
// part of any endpoint URL and not the GitHub token appear in the output.
func TestRunReport(t *testing.T) {
	const key = "apikey0123456789secret"     // gitleaks:allow (test dummy)
	const token = "ghp_secrettoken987654321" // gitleaks:allow (test dummy)
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
		{"NODE", []string{"STATUS", "NODE", "NETWORK", "EXECUTION", "EL SYNC", "CONSENSUS", "CL SYNC", "FORK", "ADVISORIES"}},
		{"good-node", []string{"OK", "good-node", "mainnet", "Geth 1.17.10 (stable)", "synced", "Lighthouse 8.3.3", "synced", "none scheduled", "none known"}},
		{"warn-node", []string{"WARN", "warn-node", "mainnet", "Geth 1.17.10 (stable)", "indexing", "Lighthouse 8.3.3", "synced", "none scheduled", "none known"}},
		{"outdated-node", []string{"CRIT", "outdated-node", "mainnet", "Besu 23.1.1 (dev) -> 26.11.2", "synced", "Nimbus 26.10.2 (stateofus)", "synced", "EL below Fusaka minimum", "EL 38 (high)"}},
		{"down-node", []string{"CRIT", "down-node", "?", "ERROR", "-", "ERROR", "ERROR", "-", "-"}},
		{"proxy-node", []string{"WARN", "proxy-node", "mainnet", "ERROR", "synced", "Lighthouse 8.3.3", "synced", "none scheduled", "none known"}},
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
		// Real data: mainnet Fusaka lists Besu 25.11.0 as minimum.
		"CRIT  outdated-node fork: Besu 23.1.1 is older than the minimum release for Fusaka (25.11.0): it cannot follow mainnet",
		"NOTE  outdated-node fork: fork check of the execution client by version only: eth_config: JSON-RPC error -32601: The method eth_config does not exist/is not available",
		"WARN  down-node network: consensus network detection failed: /eth/v1/config/deposit_contract: unexpected HTTP status 404 Not Found",
		// Real data: 38 Besu advisories affect 23.1.1-dev.
		"CRIT  outdated-node execution advisories: Besu 23.1.1 is affected by 38 published advisories (13 high, 19 medium, 6 low), fixed in 26.11.0 or later: GHSA-",
		"; https://github.com/besu-eth/besu/security/advisories\n  NOTE  outdated-node execution advisories: cannot tell whether Besu 23.1.1 is affected by GHSA-v5gq-r843-8f73",
		"Status: CRIT (1 OK, 2 WARN, 2 CRIT)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}

	// One GitHub request per list and detected client repository, even
	// though Geth and Lighthouse run on several nodes.
	gh.mu.Lock()
	wantRequests := map[string]int{
		"/repos/ethereum/go-ethereum/releases":             1,
		"/repos/besu-eth/besu/releases":                    1,
		"/repos/sigp/lighthouse/releases":                  1,
		"/repos/status-im/nimbus-eth2/releases":            1,
		"/repos/ethereum/go-ethereum/security-advisories":  1,
		"/repos/besu-eth/besu/security-advisories":         1,
		"/repos/sigp/lighthouse/security-advisories":       1,
		"/repos/status-im/nimbus-eth2/security-advisories": 1,
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

// TestRunIgnoreAdvisories checks the ignore lists end to end with the
// recorded advisories.
func TestRunIgnoreAdvisories(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	cfg := func(extraNode string) string {
		return writeConfig(t, `
ignore_advisories:
  - {id: GHSA-v5gq-r843-8f73, reason: "no private networks", expires: 2027-03-01}
  - {id: GHSA-zzzz-zzzz-zzzz, reason: "typo"}
nodes:
  - name: besu
    execution_url: `+m.besuOutdated.URL+`
    beacon_url: `+m.nimbusSynced.URL+`
    ignore_advisories:
      - {id: GHSA-4h4f-925g-8h9p, reason: "was waiting for 26.8.0", expires: 2026-09-01}
  - name: geth
    execution_url: `+m.gethSynced.URL+`
    beacon_url: `+m.lighthouseSynced.URL+`
    ignore_advisories:
      - {id: GHSA-mp5j-q856-vvx7, reason: "meant for the besu node"}
`+extraNode)
	}
	run1 := func(path string) (int, string) {
		var out bytes.Buffer
		code := run(context.Background(), []string{"-config", path, "-timeout", "2s", "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
		return code, out.String()
	}

	code, out := run1(cfg(""))
	if code != exitCrit {
		t.Errorf("exit code = %d, want %d\n%s", code, exitCrit, out)
	}
	// The ignored "all" advisory no longer counts; the expired entry does.
	if cells := rowCells(t, out, "besu"); cells[8] != "EL 38 (high)" {
		t.Errorf("besu advisories cell = %q, want %q", cells[8], "EL 38 (high)")
	}
	for _, want := range []string{
		`NOTE  ignore_advisories: GHSA-zzzz-zzzz-zzzz not found in the advisories of the clients in use (Besu, Geth, Lighthouse, Nimbus): a typo, or for a client none of your nodes runs? (reason "typo")`,
		"NOTE  besu execution advisories: the ignore_advisories entry for GHSA-4h4f-925g-8h9p expired on 2026-09-01, the advisory counts again",
		`NOTE  besu execution advisories: 1 ignored advisory for Besu 23.1.1 (ignore_advisories): GHSA-v5gq-r843-8f73 (medium: "SEC-50`,
		`reason "no private networks", until 2027-03-01`,
		"NOTE  geth ignore_advisories: GHSA-mp5j-q856-vvx7 is not a published advisory of Geth or Lighthouse; check the ID for a typo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"cannot tell whether", "besu ignore_advisories", "GHSA-v5gq-r843-8f73 is not"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q:\n%s", unwanted, out)
		}
	}

	// With a node whose clients are unknown, an unknown global ID might
	// belong to them: no typo note then.
	_, out = run1(cfg("  - {name: down, execution_url: http://" + m.deadAddr + ", beacon_url: " + m.broken.URL + "}\n"))
	if strings.Contains(out, "GHSA-zzzz-zzzz-zzzz") || !strings.Contains(out, "geth ignore_advisories: GHSA-mp5j") {
		t.Errorf("with an unreachable node:\n%s", out)
	}

	// Without advisory data nothing can be checked.
	gh.down.Store(true)
	defer gh.down.Store(false)
	_, out = run1(cfg(""))
	if strings.Contains(out, "is not a published advisory") || strings.Contains(out, "not found in the advisories") {
		t.Errorf("GitHub down:\n%s", out)
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
	if code != exitOK || !strings.Contains(out, "NOTE  a execution: GitHub not reachable, using release data from 2026-11-24") ||
		!strings.Contains(out, "NOTE  a execution advisories: GitHub not reachable, using advisory data from 2026-11-24") {
		t.Errorf("3 days: exit %d, want 0 with notes\n%s", code, out)
	}

	code, out = runAt(now.Add(8*24*time.Hour), cache)
	if code != exitWarn || !strings.Contains(out, "WARN  a execution: release data is 8 days old") ||
		!strings.Contains(out, "WARN  a consensus advisories: advisory data is 8 days old") {
		t.Errorf("8 days: exit %d, want 1 with warnings\n%s", code, out)
	}

	code, out = runAt(now, t.TempDir()) // empty cache
	if code != exitWarn || !strings.Contains(out, "latest release unknown") ||
		!strings.Contains(out, "WARN  a execution advisories: advisories unknown: GitHub security advisories of ethereum/go-ethereum: unexpected HTTP status 503") ||
		rowCells(t, out, "a")[8] != "EL ?, CL ?" {
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
	for _, want := range []string{"0  OK", "1  WARNING", "2  CRITICAL", "3  UNKNOWN", "GITHUB_TOKEN", "X3F_TELEGRAM_BOT_TOKEN", "NO_COLOR", "-grace-days", "-cache-ttl", "-state-file", "-no-notify", "-notify-test"} {
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
		"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
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

func TestRunNetwork(t *testing.T) {
	gh := newFakeGitHub(t)
	m := newMocks(t)
	// network is the name in the live fixtures' file names.
	el := func(network string) string {
		return mockClient(t, map[string][]byte{
			"web3_clientVersion": fixture(t, "execution/geth.json"),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-"+network+".json"),
			"eth_config":         fixture(t, "execution/live/eth-config-publicnode-"+network+".json"),
		}).URL
	}
	cl := func(deposit, spec string) string {
		return mockClient(t, map[string][]byte{
			"/eth/v1/node/version":            fixture(t, "consensus/lighthouse.json"),
			"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
			"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-"+deposit+".json"),
			"/eth/v1/config/spec":             fixture(t, "consensus/live/spec-"+spec+".json"),
		}).URL
	}
	// Synthetic devnet with chain ID 1337 (0x539).
	devEL := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/geth.json"),
		"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		"eth_chainId":        []byte(`{"jsonrpc":"2.0","id":1,"result":"0x539"}`),
	})
	devCL := mockClient(t, map[string][]byte{
		"/eth/v1/node/version":            fixture(t, "consensus/lighthouse.json"),
		"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
		"/eth/v1/config/deposit_contract": []byte(`{"data":{"chain_id":"1337","address":"0x0000000000000000000000000000000000000000"}}`),
	})

	tests := []struct {
		name      string
		node      string
		wantCode  int
		wantCell  string
		wantFinds []string
		wantFork  string // FORK cell, if checked
	}{
		{
			name:     "hoodi on both sides, as configured",
			node:     "{name: n, execution_url: " + el("hoodi") + ", beacon_url: " + cl("lodestar-hoodi", "lodestar-hoodi") + ", network: hoodi}",
			wantCode: exitOK,
			wantCell: "hoodi",
		},
		{
			name:      "execution and consensus on different networks",
			node:      "{name: n, execution_url: " + el("sepolia") + ", beacon_url: " + cl("publicnode-mainnet", "publicnode-mainnet") + "}",
			wantCode:  exitCrit,
			wantCell:  "EL sepolia / CL mainnet",
			wantFinds: []string{"CRIT  n network: execution client is on sepolia, consensus client on mainnet"},
			// No single schedule to compare against: no fork findings.
			wantFork: "-",
		},
		{
			name:     "on another network than configured",
			node:     "{name: n, execution_url: " + m.gethSynced.URL + ", beacon_url: " + m.lighthouseSynced.URL + ", network: hoodi}",
			wantCode: exitCrit,
			wantCell: "mainnet",
			wantFinds: []string{
				"CRIT  n network: expected hoodi, execution client is on mainnet",
				"CRIT  n network: expected hoodi, consensus client is on mainnet",
			},
		},
		{
			name:      "unknown network is a note",
			node:      "{name: n, execution_url: " + devEL.URL + ", beacon_url: " + devCL.URL + "}",
			wantCode:  exitOK,
			wantCell:  "chain 1337",
			wantFinds: []string{"NOTE  n network: unknown network (chain 1337): no fork data available"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, "nodes:\n  - "+tt.node+"\n")
			var out bytes.Buffer
			code := run(context.Background(), []string{"-config", path, "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
			got := out.String()
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d\n%s", code, tt.wantCode, got)
			}
			cells := rowCells(t, got, "n")
			if cells[2] != tt.wantCell {
				t.Errorf("network cell = %q, want %q", cells[2], tt.wantCell)
			}
			if tt.wantFork != "" && (cells[7] != tt.wantFork || strings.Contains(got, " n fork:")) {
				t.Errorf("fork cell = %q, want %q and no fork findings\n%s", cells[7], tt.wantFork, got)
			}
			for _, f := range tt.wantFinds {
				if !strings.Contains(got, f) {
					t.Errorf("missing finding %q\n%s", f, got)
				}
			}
		})
	}
}

// Fork readiness with the built-in schedule (real data, see
// internal/forks/SOURCES.md): Glamsterdam on Hoodi was postponed on
// 2026-10-22; go-ethereum 1.17.9 contained the old schedule, 1.17.10
// removed it.
func TestRunForksBuiltIn(t *testing.T) {
	gh := newFakeGitHub(t)
	gethVersion := func(v string) string {
		return mockClient(t, map[string][]byte{
			// Synthetic version strings in the real Geth format.
			"web3_clientVersion": []byte(`{"jsonrpc":"2.0","id":1,"result":"Geth/v` + v + `-stable-1df30b18/linux-amd64/go1.27.3"}`),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-hoodi.json"),
		}).URL
	}
	lodestarHoodi := mockClient(t, map[string][]byte{
		"/eth/v1/node/version":            fixture(t, "consensus/lodestar.json"),
		"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
		"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-lodestar-hoodi.json"),
	}).URL

	tests := []struct {
		version  string
		wantCode int
		wantCell string
		wantFind string
	}{
		{"1.17.8", exitCrit, "none scheduled", ""}, // outdated (grace), but no old schedule per the data
		{"1.17.9", exitCrit, "EL has old Glamsterdam schedule",
			"CRIT  n fork: Geth 1.17.9 still contains the postponed Glamsterdam schedule (2026-10-26 17:42 UTC); update to 1.17.10 or later; the former activation time has passed, so the node has most likely left the chain"},
		{"1.17.10", exitOK, "none scheduled", ""},
		{"1.16.4", exitCrit, "EL below Fusaka minimum",
			"CRIT  n fork: Geth 1.16.4 is older than the minimum release for Fusaka (1.16.5): it cannot follow hoodi"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			path := writeConfig(t, "nodes:\n  - {name: n, execution_url: "+gethVersion(tt.version)+", beacon_url: "+lodestarHoodi+"}\n")
			var out bytes.Buffer
			code := run(context.Background(), []string{"-config", path, "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
			got := out.String()
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d\n%s", code, tt.wantCode, got)
			}
			if cells := rowCells(t, got, "n"); cells[7] != tt.wantCell {
				t.Errorf("fork cell = %q, want %q", cells[7], tt.wantCell)
			}
			if tt.wantFind != "" && !strings.Contains(got, tt.wantFind) {
				t.Errorf("missing finding %q\n%s", tt.wantFind, got)
			}
		})
	}
}

// writeForks writes a synthetic fork schedule for mainnet with one fork
// "Testfork" that activates `in` after now. minVersions is a YAML flow map
// such as "{geth: 1.18.0}" or "". Epoch and timestamp are computed so that
// the loader's consistency check passes.
func writeForks(t *testing.T, updated string, in time.Duration, minVersions string) string {
	t.Helper()
	const genesis = 1606824023 // mainnet
	epoch := (now.Add(in).Unix() - genesis) / 384
	ts := genesis + epoch*384
	minLine := ""
	if minVersions != "" {
		minLine = "        min_versions: " + minVersions + "\n"
	}
	yaml := fmt.Sprintf(`updated: %s
networks:
  mainnet:
    chain_id: 1
    genesis_time: %d
    forks:
      - name: Testfork
        execution: Amsterdam
        consensus: Gloas
        epoch: %d
        timestamp: %d
%s        sources: [https://example.org/synthetic]
`, updated, genesis, epoch, ts, minLine)
	path := filepath.Join(t.TempDir(), "forks.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Readiness for a scheduled fork, with synthetic schedules via -forks.
//
// The mocks answer neither eth_config nor /eth/v1/config/spec, so readiness
// is judged by version (the fallback), with a note per layer.
func TestRunForksScheduled(t *testing.T) {
	gh := newFakeGitHub(t)
	el := mockClient(t, map[string][]byte{ // Geth 1.17.10
		"web3_clientVersion": fixture(t, "execution/geth.json"),
		"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
	})
	cl := mockClient(t, map[string][]byte{ // Lighthouse 8.3.3
		"/eth/v1/node/version":            fixture(t, "consensus/lighthouse.json"),
		"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
		"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-publicnode-mainnet.json"),
	})
	path := writeConfig(t, "nodes:\n  - {name: n, execution_url: "+el.URL+", beacon_url: "+cl.URL+"}\n")
	day := 24 * time.Hour

	tests := []struct {
		name     string
		in       time.Duration
		minVers  string
		extra    []string
		wantCode int
		wantCell string
		wantFind string
	}{
		{"ready", 6 * day, "{geth: 1.17.10, lighthouse: 8.3.0-rc.0}", nil, exitOK, "Testfork in 5 days: ready",
			""},
		{"only CL release missing", 6 * day, "{geth: 1.17.10}", nil, exitWarn, "Testfork in 5 days: CL waiting for release",
			"WARN  n fork: Testfork on mainnet in 5 days: no Lighthouse release announced yet (waiting for client release)"},
		{"not ready, far away", 20 * day, "{geth: 1.18.0}", nil, exitWarn, "Testfork in 19 days: EL not ready, CL waiting for release",
			"WARN  n fork: Geth 1.17.10 is not ready for Testfork on mainnet"},
		{"not ready, close", 6 * day, "{geth: 1.18.0}", nil, exitCrit, "Testfork in 5 days: EL not ready, CL waiting for release",
			"CRIT  n fork: Geth 1.17.10 is not ready for Testfork on mainnet"},
		{"no release yet stays yellow", 2 * day, "", nil, exitWarn, "Testfork in 1 day: EL waiting for release, CL waiting for release",
			"WARN  n fork: Testfork on mainnet in 1 day: no Geth release announced yet (waiting for client release)"},
		{"threshold flag", 6 * day, "{geth: 1.18.0}", []string{"-fork-critical-days", "3"}, exitWarn, "Testfork in 5 days: EL not ready, CL waiting for release",
			"WARN  n fork: Geth 1.17.10 is not ready"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forksPath := writeForks(t, "2026-11-24", tt.in, tt.minVers)
			args := append([]string{"-config", path, "-cache-dir", t.TempDir(), "-forks", forksPath}, tt.extra...)
			var out bytes.Buffer
			code := run(context.Background(), args, &out, &out, testEnv(gh, now))
			got := out.String()
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d\n%s", code, tt.wantCode, got)
			}
			if cells := rowCells(t, got, "n"); cells[7] != tt.wantCell {
				t.Errorf("fork cell = %q, want %q", cells[7], tt.wantCell)
			}
			if tt.wantFind != "" && !strings.Contains(got, tt.wantFind) {
				t.Errorf("missing finding %q\n%s", tt.wantFind, got)
			}
			for _, note := range []string{
				"NOTE  n fork: fork check of the execution client by version only: eth_config: unexpected HTTP status 404 Not Found",
				"NOTE  n fork: fork check of the consensus client by version only: /eth/v1/config/spec: unexpected HTTP status 404 Not Found",
			} {
				if !strings.Contains(got, note) {
					t.Errorf("missing note %q\n%s", note, got)
				}
			}
		})
	}
}

// Fork readiness from the node's own answer (eth_config and
// /eth/v1/config/spec), with the built-in schedule on Hoodi.
func TestRunForksNodeAnswer(t *testing.T) {
	gh := newFakeGitHub(t)
	hoodiConfig := fixture(t, "execution/live/eth-config-publicnode-hoodi.json")
	// Derived from the live recording: current = the former Glamsterdam
	// time, as a node with the postponed schedule would report it.
	staleConfig := bytes.Replace(hoodiConfig, []byte(`"activationTime":1762955544`), []byte(`"activationTime":1793036568`), 1)
	if bytes.Equal(staleConfig, hoodiConfig) {
		t.Fatal("test bug: activationTime not found in fixture")
	}
	el := func(version string, config []byte) string {
		return mockClient(t, map[string][]byte{
			"web3_clientVersion": []byte(`{"jsonrpc":"2.0","id":1,"result":"` + version + `"}`),
			"eth_syncing":        fixture(t, "execution/syncing-false.json"),
			"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-hoodi.json"),
			"eth_config":         config,
		}).URL
	}
	cl := mockClient(t, map[string][]byte{
		"/eth/v1/node/version":            fixture(t, "consensus/lodestar.json"),
		"/eth/v1/node/syncing":            fixture(t, "consensus/syncing-synced.json"),
		"/eth/v1/config/deposit_contract": fixture(t, "consensus/live/deposit-contract-lodestar-hoodi.json"),
		"/eth/v1/config/spec":             fixture(t, "consensus/live/spec-lodestar-hoodi.json"),
	}).URL

	tests := []struct {
		name     string
		version  string // in the real Geth / dRPC format
		config   []byte
		wantCell string
		wantFind []string
		dontFind string
	}{
		{
			// The node does not schedule the postponed fork, so the
			// version range rule of 4b does not apply.
			name: "geth 1.17.9 without the old schedule", version: "Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3", config: hoodiConfig,
			wantCell: "none scheduled", dontFind: "postponed",
		},
		{
			name: "geth 1.17.9 with the old schedule", version: "Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3", config: staleConfig,
			wantCell: "EL has old Glamsterdam schedule",
			wantFind: []string{"CRIT  n fork: Geth 1.17.9 still schedules the postponed Glamsterdam at its former time (2026-10-26 17:42 UTC, eth_config current); update to 1.17.10 or later"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, "nodes:\n  - {name: n, execution_url: "+el(tt.version, tt.config)+", beacon_url: "+cl+"}\n")
			var out bytes.Buffer
			run(context.Background(), []string{"-config", path, "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
			got := out.String()
			if cells := rowCells(t, got, "n"); cells[7] != tt.wantCell {
				t.Errorf("fork cell = %q, want %q\n%s", cells[7], tt.wantCell, got)
			}
			for _, want := range tt.wantFind {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q\n%s", want, got)
				}
			}
			if tt.dontFind != "" && strings.Contains(got, tt.dontFind) {
				t.Errorf("unexpected %q\n%s", tt.dontFind, got)
			}
			if strings.Contains(got, "by version only") {
				t.Errorf("node answered, no fallback expected\n%s", got)
			}
		})
	}
}

func TestRunForksFileErrorsAndStaleness(t *testing.T) {
	gh := newFakeGitHub(t)
	m := newMocks(t)
	path := writeConfig(t, "nodes:\n  - {name: n, execution_url: "+m.gethSynced.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")

	run1 := func(args ...string) (int, string) {
		var out bytes.Buffer
		code := run(context.Background(), append([]string{"-config", path, "-cache-dir", t.TempDir()}, args...), &out, &out, testEnv(gh, now))
		return code, out.String()
	}

	code, out := run1("-forks", filepath.Join(t.TempDir(), "missing.yaml"))
	if code != exitUnknown || !strings.Contains(out, "read forks file") {
		t.Errorf("missing file: exit %d\n%s", code, out)
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte("updated: 2026-11-24\nnetworks:\n  holesky: {chain_id: 17000, genesis_time: 1, forks: []}\n"), 0o600)
	code, out = run1("-forks", bad)
	if code != exitUnknown || !strings.Contains(out, `network "holesky" is not one of`) {
		t.Errorf("bad file: exit %d\n%s", code, out)
	}

	// The mocks' eth_config/spec (live mainnet) do not schedule the
	// synthetic Testfork, so the node is not ready: yellow, 99 days ahead.
	code, out = run1("-forks", writeForks(t, "2026-09-01", 100*24*time.Hour, "{geth: 1.17.10, lighthouse: 8.3.3}"))
	if code != exitWarn || !strings.Contains(out, "NOTE  fork data last updated 2026-09-01 (84 days ago)") {
		t.Errorf("stale data: exit %d\n%s", code, out)
	}
}

// Ethrex end to end: version parsing, release lookup in lambdaclass/ethrex
// and the fork check with the node's own answer.
func TestRunEthrex(t *testing.T) {
	gh := newFakeGitHub(t)
	m := newMocks(t)
	ethrex := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/ethrex.json"), // release binary of v31.0.0
		"eth_syncing":        fixture(t, "execution/syncing-false.json"),
		"eth_chainId":        fixture(t, "execution/live/chainid-publicnode-mainnet.json"),
		"eth_config":         fixture(t, "execution/live/eth-config-publicnode-mainnet.json"),
	})
	path := writeConfig(t, "nodes:\n  - {name: n, execution_url: "+ethrex.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	var out bytes.Buffer
	code := run(context.Background(), []string{"-config", path, "-cache-dir", t.TempDir()}, &out, &out, testEnv(gh, now))
	got := out.String()
	if code != exitOK {
		t.Errorf("exit %d, want %d\n%s", code, exitOK, got)
	}
	want := []string{"OK", "n", "mainnet", "Ethrex 31.0.0 (HEAD)", "synced", "Lighthouse 8.3.3", "synced", "none scheduled", "none known"}
	if cells := rowCells(t, got, "n"); !slices.Equal(cells, want) {
		t.Errorf("row:\n got  %q\n want %q", cells, want)
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.requests["/repos/lambdaclass/ethrex/releases"] != 1 {
		t.Errorf("GitHub requests = %v", gh.requests)
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	code := run(context.Background(), []string{"-version"}, &out, &out, env{})
	if code != exitOK || !regexp.MustCompile(`^x3f \S+ \(commit \S+, go\S+, \w+/\w+\)\n$`).MatchString(out.String()) {
		t.Errorf("exit %d, output %q", code, out.String())
	}

	saved := version
	t.Cleanup(func() { version = saved })
	version = "v0.1.0" // as set by -ldflags "-X main.version=v0.1.0"
	if got := buildVersion(); got != "v0.1.0" {
		t.Errorf("buildVersion = %q", got)
	}
}

func TestFormatAdvisories(t *testing.T) {
	tests := []struct{ el, cl, want string }{
		{"none known", "none known", "none known"},
		{"5 (high)", "none known", "EL 5 (high)"},
		{"none known", "1 (low)", "CL 1 (low)"},
		{"2 (medium)", "?", "EL 2 (medium), CL ?"},
		{"", "none known", "none known"}, // execution client not assessed
		{"", "?", "CL ?"},
		{"", "", "-"},
	}
	for _, tt := range tests {
		if got := formatAdvisories(tt.el, tt.cl); got != tt.want {
			t.Errorf("formatAdvisories(%q, %q) = %q, want %q", tt.el, tt.cl, got, tt.want)
		}
	}
}

// fakeTelegram is a mock Bot API for the CLI tests. It answers with a
// fixture from internal/notify/testdata/telegram (sources in SOURCES.md
// there) and records the message texts.
type fakeTelegram struct {
	*httptest.Server
	mu      sync.Mutex
	status  int
	fixture string
	texts   []string
	paths   []string
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{status: http.StatusOK, fixture: "ok.json"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.texts = append(f.texts, body.Text)
		f.paths = append(f.paths, r.URL.Path)
		data, err := os.ReadFile(filepath.Join("..", "..", "internal", "notify", "testdata", "telegram", f.fixture))
		if err != nil {
			t.Error(err)
		}
		w.WriteHeader(f.status)
		w.Write(data)
	}))
	t.Cleanup(f.Close)
	return f
}

// answer sets the reply for the following requests.
func (f *fakeTelegram) answer(status int, fixture string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.fixture = status, fixture
}

// sent returns the texts received since the last call.
func (f *fakeTelegram) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	texts := f.texts
	f.texts = nil
	return texts
}

const telegramToken = "123456789:AAtelegram-test-token" // gitleaks:allow (test dummy)

func TestRunTelegram(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	tg := newFakeTelegram(t)
	state := filepath.Join(t.TempDir(), "state", "x3f.json")
	cache := t.TempDir()

	// Node "a" is the outdated Besu node (CRIT) or a healthy one (OK).
	cfg := func(execution, beacon string) string {
		return writeConfig(t, "telegram:\n  chat_id: -1001234567890\nnodes:\n  - {name: a, execution_url: "+execution+", beacon_url: "+beacon+"}\n")
	}
	bad := cfg(m.besuOutdated.URL, m.nimbusSynced.URL)
	good := cfg(m.gethSynced.URL, m.lighthouseSynced.URL)
	runAt := func(path string, after time.Duration, args ...string) (int, string) {
		e := testEnv(gh, now.Add(after))
		e.telegramBaseURL, e.telegramToken = tg.URL, telegramToken
		var out bytes.Buffer
		args = append([]string{"-config", path, "-timeout", "2s", "-cache-dir", cache, "-state-file", state}, args...)
		code := run(context.Background(), args, &out, &out, e)
		if strings.Contains(out.String(), "AAtelegram") {
			t.Errorf("output leaks the token:\n%s", out.String())
		}
		return code, out.String()
	}
	h := time.Hour

	// First run: the CRIT node is reported.
	code, out := runAt(bad, 0)
	sent := tg.sent()
	if code != exitCrit || len(sent) != 1 || !strings.Contains(out, "NOTE  Telegram message sent about a") {
		t.Fatalf("first run: exit %d, sent %q\n%s", code, sent, out)
	}
	for _, want := range []string{
		"🔴 <b>a</b>: CRIT (was OK)\n",
		"\n• CRIT execution: Besu dev build of 23.1.1 (&#34;dev&#34;)",
		"\n• CRIT execution advisories: Besu 23.1.1 is affected by 38 published advisories",
	} {
		if !strings.Contains(sent[0], want) {
			t.Errorf("message lacks %q:\n%s", want, sent[0])
		}
	}
	if tg.paths[0] != "/bot"+telegramToken+"/sendMessage" {
		t.Errorf("path = %q", tg.paths[0])
	}

	// Unchanged an hour later: no message.
	if _, out := runAt(bad, h); len(tg.sent()) != 0 || strings.Contains(out, "Telegram") {
		t.Errorf("unchanged: sent a message\n%s", out)
	}

	// -no-notify neither sends nor touches the state; the next run still
	// sees the change.
	before := readFile(t, state)
	if code, _ := runAt(good, 2*h, "-no-notify"); code != exitOK || len(tg.sent()) != 0 || readFile(t, state) != before {
		t.Errorf("-no-notify: exit %d or state changed", code)
	}

	// Back to good, but Telegram rejects the token: the nodes are fine,
	// the failed notification turns the run yellow.
	tg.answer(http.StatusUnauthorized, "unauthorized.json")
	code, out = runAt(good, 3*h)
	if code != exitWarn || !strings.Contains(out, "WARN  notification failed, will retry on the next run: Telegram: HTTP 401: Unauthorized") ||
		!strings.Contains(out, "Status: WARN (1 OK, 0 WARN, 0 CRIT)") || len(tg.sent()) != 1 {
		t.Errorf("Telegram failure: exit %d\n%s", code, out)
	}

	// Telegram works again: the all-clear is delivered, with the time the
	// node was first seen OK (the -no-notify run does not count).
	tg.answer(http.StatusOK, "ok.json")
	code, out = runAt(good, 4*h)
	sent = tg.sent()
	if code != exitOK || len(sent) != 1 || sent[0] != "✅ <b>a</b>: OK (was CRIT, since 2026-11-24 15:00 UTC)" {
		t.Errorf("all clear: exit %d, sent %q\n%s", code, sent, out)
	}
}

func TestRunTelegramSetup(t *testing.T) {
	m := newMocks(t)
	gh := newFakeGitHub(t)
	tg := newFakeTelegram(t)
	path := writeConfig(t, "telegram: {chat_id: \"@x3f_alerts\", min_level: crit}\nnodes:\n  - {name: a, execution_url: "+m.gethSynced.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	run1 := func(token string, args ...string) (int, string) {
		e := testEnv(gh, now)
		e.telegramBaseURL, e.telegramToken = tg.URL, token
		var out bytes.Buffer
		args = append([]string{"-config", path, "-cache-dir", t.TempDir(), "-state-file", filepath.Join(t.TempDir(), "s.json")}, args...)
		code := run(context.Background(), args, &out, &out, e)
		return code, out.String()
	}

	code, out := run1("")
	if code != exitUnknown || !strings.Contains(out, "X3F_TELEGRAM_BOT_TOKEN is not set") {
		t.Errorf("no token: exit %d\n%s", code, out)
	}
	if code, out := run1("", "-no-notify"); code != exitOK {
		t.Errorf("no token, -no-notify: exit %d\n%s", code, out)
	}

	code, out = run1(telegramToken, "-notify-test")
	sent := tg.sent()
	if code != exitOK || out != "Test message sent.\n" || len(sent) != 1 || !strings.Contains(sent[0], "x3f test message") {
		t.Errorf("-notify-test: exit %d, sent %q\n%s", code, sent, out)
	}

	tg.answer(http.StatusBadRequest, "chat-not-found.json")
	code, out = run1(telegramToken, "-notify-test")
	if code != exitUnknown || out != "x3f: Telegram: HTTP 400: Bad Request: chat not found\n" || len(tg.sent()) != 1 {
		t.Errorf("-notify-test, wrong chat: exit %d\n%s", code, out)
	}

	// min_level crit: a WARN node is not worth a message.
	warnPath := writeConfig(t, "telegram: {chat_id: 1, min_level: crit}\nnodes:\n  - {name: w, execution_url: "+m.gethIndexing.URL+", beacon_url: "+m.lighthouseSynced.URL+"}\n")
	if code, out := run1(telegramToken, "-config", warnPath); code != exitWarn || len(tg.sent()) != 0 {
		t.Errorf("min_level crit, WARN node: exit %d\n%s", code, out)
	}

	// An unreadable state file: nothing is sent, the run turns yellow.
	state := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(state, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	tg.answer(http.StatusOK, "ok.json")
	code, out = run1(telegramToken, "-state-file", state)
	if code != exitWarn || !strings.Contains(out, "WARN  notifications skipped: state file "+state) || len(tg.sent()) != 0 {
		t.Errorf("bad state file: exit %d\n%s", code, out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
