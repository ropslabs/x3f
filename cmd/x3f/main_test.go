package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ropslabs/x3f/internal/ethereum"
)

// fixture reads a recorded response from the ethereum package's testdata
// (sources: internal/ethereum/testdata/SOURCES.md).
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

// TestRun checks the table end to end and makes sure that neither the table
// nor the error list nor the returned error contains any part of the URLs.
func TestRun(t *testing.T) {
	const key = "apikey0123456789secret"

	gethIndexing := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/geth.json"),
		"eth_syncing":        fixture(t, "execution/syncing-geth-indexing.json"),
	})
	lighthouseSyncing := mockClient(t, map[string][]byte{
		"/eth/v1/node/version": fixture(t, "consensus/lighthouse.json"),
		"/eth/v1/node/syncing": fixture(t, "consensus/syncing-spec-example.json"),
	})
	// Versions work, sync answers are malformed (synthetic).
	nethermindBadSync := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/nethermind.json"),
		"eth_syncing":        []byte(`{"jsonrpc":"2.0","id":1,"result":"yes"}`),
	})
	nimbusBadSync := mockClient(t, map[string][]byte{
		"/eth/v1/node/version": fixture(t, "consensus/nimbus.json"),
		"/eth/v1/node/syncing": []byte(`{"data":{}}`),
	})
	broken := mockClient(t, nil) // 404 for everything
	// A proxy that blocks web3_clientVersion but lets eth_syncing through.
	// Answers are the method-not-found and "not syncing" fixtures.
	proxyBlocksVersion := mockClient(t, map[string][]byte{
		"web3_clientVersion": fixture(t, "execution/error-method-not-found.json"),
		"eth_syncing":        fixture(t, "execution/syncing-false.json"),
	})
	lighthouseSynced := mockClient(t, map[string][]byte{
		"/eth/v1/node/version": fixture(t, "consensus/lighthouse.json"),
		"/eth/v1/node/syncing": fixture(t, "consensus/syncing-synced.json"),
	})

	// A server that is closed right away: connections to it are refused.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadAddr := dead.Listener.Addr().String()
	dead.Close()

	config := `
nodes:
  - name: good-node
    execution_url: ` + gethIndexing.URL + `/v3/` + key + `
    beacon_url: ` + lighthouseSyncing.URL + `/?key=` + key + `
  - name: bad-node
    execution_url: http://` + deadAddr + `/v3/` + key + `
    beacon_url: ` + broken.URL + `/` + key + `
  - name: sync-fail-node
    execution_url: ` + nethermindBadSync.URL + `/` + key + `
    beacon_url: ` + nimbusBadSync.URL + `/` + key + `
  - name: proxy-node
    execution_url: ` + proxyBlocksVersion.URL + `/` + key + `
    beacon_url: ` + lighthouseSynced.URL + `/` + key + `
`
	path := filepath.Join(t.TempDir(), "x3f.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"-config", path, "-timeout", "2s"}, &out, ethereum.NewClient(nil))
	// bad-node: 1 (execution unreachable, sync skipped) + 2 (consensus 404
	// for version and sync); sync-fail-node: 2; proxy-node: 1.
	if err == nil || err.Error() != "6 request(s) failed" {
		t.Errorf("run error = %v, want \"6 request(s) failed\"", err)
	}

	got := out.String()
	lines := strings.Split(got, "\n")
	// rowCells returns the table cells of one node's row. tabwriter pads
	// columns with at least two spaces, so two or more spaces separate cells.
	rowCells := func(node string) []string {
		for _, l := range lines {
			if strings.HasPrefix(l, node+" ") {
				return regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(l), -1)
			}
		}
		t.Fatalf("no row for %s:\n%s", node, got)
		return nil
	}
	tests := []struct {
		node string
		want []string
	}{
		{"NODE", []string{"NODE", "EXECUTION", "EL SYNC", "CONSENSUS", "CL SYNC"}},
		{"good-node", []string{"good-node", "Geth 1.17.10 (stable)", "indexing", "Lighthouse 8.3.3", "syncing (1 slot behind), optimistic, EL offline"}},
		// Unreachable execution client: sync skipped ("-"). The consensus
		// client answered with HTTP 404, so its sync was still queried.
		{"bad-node", []string{"bad-node", "ERROR", "-", "ERROR", "ERROR"}},
		{"sync-fail-node", []string{"sync-fail-node", "Nethermind 2.2.2", "ERROR", "Nimbus 26.10.2 (stateofus)", "ERROR"}},
		// The blocked version call must not prevent the sync query.
		{"proxy-node", []string{"proxy-node", "ERROR", "synced", "Lighthouse 8.3.3", "synced"}},
	}
	for _, tt := range tests {
		if got := rowCells(tt.node); !slices.Equal(got, tt.want) {
			t.Errorf("row %s:\n got  %q\n want %q", tt.node, got, tt.want)
		}
	}

	for _, want := range []string{
		"bad-node execution: web3_clientVersion: connection failed",
		"bad-node consensus: /eth/v1/node/version: unexpected HTTP status 404",
		"sync-fail-node execution sync: eth_syncing: result is neither false nor an object",
		"sync-fail-node consensus sync: /eth/v1/node/syncing: response lacks",
		"bad-node consensus sync: /eth/v1/node/syncing: unexpected HTTP status 404",
		"proxy-node execution: web3_clientVersion: JSON-RPC error -32601",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	// Skipped sync requests are not reported as separate errors.
	if strings.Contains(got, "bad-node execution sync") {
		t.Errorf("skipped sync request reported as error:\n%s", got)
	}

	// No part of any endpoint may appear anywhere.
	all := got + "\n" + err.Error()
	for _, secret := range []string{key, deadAddr, "127.0.0.1", "http://"} {
		if strings.Contains(all, secret) {
			t.Errorf("output leaks %q:\n%s", secret, all)
		}
	}
}

func TestRunInvalidTimeout(t *testing.T) {
	err := run(context.Background(), []string{"-timeout", "0s"}, &bytes.Buffer{}, ethereum.NewClient(nil))
	if err == nil || !strings.Contains(err.Error(), "-timeout must be positive") {
		t.Errorf("error = %v", err)
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		err  error
		want string
	}{
		{"with suffix", "Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3", nil, "Geth 1.17.10 (stable)"},
		{"without suffix", "Prysm/v7.3.2-844d8b8 (linux amd64)", nil, "Prysm 7.3.2"},
		{"unknown client shows raw string", "SomeClient/v1.2.3", nil, `unrecognized: "SomeClient/v1.2.3"`},
		{"error", "", errors.New("timeout"), "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatVersion(ethereum.ParseClientVersion(tt.raw), tt.err); got != tt.want {
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
