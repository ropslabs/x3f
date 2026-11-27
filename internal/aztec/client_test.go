package aztec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/software"
)

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// reply is a mock answer: HTTP status and body.
type reply struct {
	status int
	body   []byte
}

// mockNode serves one reply per JSON-RPC method and records the methods
// called. A method without a reply gets the node's "method not found"
// answer: HTTP 400 with the error object (see testdata/SOURCES.md).
func mockNode(t *testing.T, replies map[string]reply) (*httptest.Server, func() []string) {
	t.Helper()
	notFound := readFixture(t, "method-not-found.json")
	var mu sync.Mutex
	var called []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JSONRPC != "2.0" {
			t.Errorf("bad request %+v, %v", req, err)
		}
		mu.Lock()
		called = append(called, req.Method)
		mu.Unlock()
		rp, ok := replies[req.Method]
		if !ok {
			rp = reply{http.StatusBadRequest, notFound}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rp.status)
		w.Write(rp.body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(called)
	}
}

func TestNodeInfo(t *testing.T) {
	mainnet := readFixture(t, "live/aztec_getNodeInfo-drpc-mainnet.json")
	legacy := readFixture(t, "live/node_getNodeInfo-drpc-mainnet.json")
	testnet := readFixture(t, "live/aztec_getNodeInfo-drpc-testnet.json")
	wantMainnet := NodeInfo{
		NodeVersion: "6.0.0", L1ChainID: 1, RollupVersion: 2070860440,
		L1Contracts: L1Contracts{
			Rollup:             "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f",
			Registry:           "0x35b22e09ee0390539439e24f06da43d83f90e298",
			Governance:         "0x1102471eb3378fee427121c9efcea452e4b6b75e",
			GovernanceProposer: "0x06ef1dcf87e419c48b94a331b252819fadbd63ef",
		},
	}

	tests := []struct {
		name       string
		replies    map[string]reply
		want       NodeInfo
		wantCalled []string
		wantErr    string
	}{
		{
			name:       "aztec_ namespace (v5 and later)",
			replies:    map[string]reply{"aztec_getNodeInfo": {200, mainnet}},
			want:       wantMainnet,
			wantCalled: []string{"aztec_getNodeInfo"},
		},
		{
			name:    "testnet",
			replies: map[string]reply{"aztec_getNodeInfo": {200, testnet}},
			want: NodeInfo{
				NodeVersion: "6.0.0", L1ChainID: 11155111, RollupVersion: 679261766,
				L1Contracts: L1Contracts{
					Rollup:             "0x1f435a7f2da358caf887a470faeee7d5d5f4efba",
					Registry:           "0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba",
					Governance:         "0xcaf7447721447b22cd0076ac7c63877c3afd329f",
					GovernanceProposer: "0x01c7d4ca153748d2377968fef22894cb162e9480",
				},
			},
			wantCalled: []string{"aztec_getNodeInfo"},
		},
		{
			// A node before v5 only knows node_*; it answers aztec_*
			// with HTTP 400 and -32601.
			name:       "falls back to node_ (before v5)",
			replies:    map[string]reply{"node_getNodeInfo": {200, legacy}},
			want:       wantMainnet,
			wantCalled: []string{"aztec_getNodeInfo", "node_getNodeInfo"},
		},
		{
			name:       "neither namespace",
			replies:    map[string]reply{},
			wantCalled: []string{"aztec_getNodeInfo", "node_getNodeInfo"},
			// The message is the one of the second answer; the derived
			// fixture names aztec_getNodeInfo, as the source would for
			// that request.
			wantErr: "getNodeInfo: JSON-RPC error -32601: Method not found: aztec_getNodeInfo",
		},
		{
			// Only "method not found" falls back; any other answer is the
			// node's real state.
			name:       "other HTTP errors do not fall back",
			replies:    map[string]reply{"aztec_getNodeInfo": {http.StatusServiceUnavailable, []byte("<html>busy</html>")}},
			wantCalled: []string{"aztec_getNodeInfo"},
			wantErr:    "getNodeInfo: unexpected HTTP status 503 Service Unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, called := mockNode(t, tt.replies)
			got, err := NewClient(nil).NodeInfo(context.Background(), srv.URL)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if c := called(); !slices.Equal(c, tt.wantCalled) {
				t.Errorf("called %v, want %v", c, tt.wantCalled)
			}
		})
	}
}

func TestNodeInfoUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewClient(nil).NodeInfo(context.Background(), url+"/secret-key")
	if !httpjson.IsTransportError(err) {
		t.Errorf("error %v is not a transport error", err)
	}
	if err != nil && (strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), strings.TrimPrefix(url, "http://"))) {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestParseNodeVersion(t *testing.T) {
	tests := []struct {
		raw  string
		want software.Reported
	}{
		{"6.0.0", software.Reported{Raw: "6.0.0", Name: software.Aztec, Version: "6.0.0"}}, // live
		// Synthetic: formats of the release tags (see internal/releases/testdata).
		{"6.1.0-rc.2", software.Reported{Raw: "6.1.0-rc.2", Name: software.Aztec, Version: "6.1.0", Suffix: "rc.2"}},
		{"v6.1.0", software.Reported{Raw: "v6.1.0", Name: software.Aztec, Version: "6.1.0"}},
		{"6.2.0-nightly.20261126", software.Reported{Raw: "6.2.0-nightly.20261126", Name: software.Aztec, Version: "6.2.0", Suffix: "nightly.20261126"}},
		{"unknown", software.Reported{Raw: "unknown", Name: software.Aztec}},
	}
	for _, tt := range tests {
		if got := ParseNodeVersion(tt.raw); got != tt.want {
			t.Errorf("ParseNodeVersion(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}
