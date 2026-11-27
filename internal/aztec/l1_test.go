package aztec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ropslabs/x3f/internal/httpjson"
)

// TestSelectors checks the selector constants against the recorded
// web3_sha3 answers of an Ethereum node: the selector is the first 4 bytes
// of the Keccak-256 hash of the signature.
func TestSelectors(t *testing.T) {
	for file, sel := range map[string]string{
		"live/l1/web3_sha3-getCanonicalRollup-publicnode-mainnet.json": selGetCanonicalRollup,
		"live/l1/web3_sha3-getVersion-publicnode-mainnet.json":         selGetVersion,
	} {
		var resp struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(readFixture(t, file), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Result) != 66 || resp.Result[:10] != sel {
			t.Errorf("%s: hash %s, selector constant %s", file, resp.Result, sel)
		}
	}
}

// mockL1 answers eth_chainId and eth_call like an Ethereum node. replies
// maps "eth_chainId" or "<to> <data>" of an eth_call to a response body;
// anything else gets a JSON-RPC error.
func mockL1(t *testing.T, replies map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request: %v", err)
		}
		key := req.Method
		if req.Method == "eth_call" {
			var call struct{ To, Data string }
			var block string
			if len(req.Params) != 2 || json.Unmarshal(req.Params[0], &call) != nil ||
				json.Unmarshal(req.Params[1], &block) != nil || block != "latest" {
				t.Errorf("bad eth_call params %s", req.Params)
			}
			key = call.To + " " + call.Data
		}
		body, ok := replies[key]
		if !ok {
			body = []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"unexpected request"}}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestL1Recorded(t *testing.T) {
	// Registry addresses from the recorded getNodeInfo answers; the
	// recorded L1 answers name the canonical rollup and its version.
	tests := []struct {
		net, registry, rollup string
		chainID, version      uint64
	}{
		{"mainnet", "0x35b22e09ee0390539439e24f06da43d83f90e298", "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f", 1, 2070860440},
		{"sepolia", "0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba", "0x1f435a7f2da358caf887a470faeee7d5d5f4efba", 11155111, 679261766},
	}
	for _, tt := range tests {
		t.Run(tt.net, func(t *testing.T) {
			srv := mockL1(t, map[string][]byte{
				"eth_chainId": readFixture(t, "live/l1/eth_chainId-publicnode-"+tt.net+".json"),
				tt.registry + " " + selGetCanonicalRollup: readFixture(t, "live/l1/eth_call-getCanonicalRollup-publicnode-"+tt.net+".json"),
				tt.rollup + " " + selGetVersion:           readFixture(t, "live/l1/eth_call-getVersion-publicnode-"+tt.net+".json"),
			})
			c := NewClient(nil)
			ctx := context.Background()
			if id, err := c.L1ChainID(ctx, srv.URL); err != nil || id != tt.chainID {
				t.Errorf("L1ChainID = %d, %v; want %d", id, err, tt.chainID)
			}
			// Addresses may come in mixed case (the docs use checksummed
			// ones); the call is made in lowercase.
			rollup, err := c.CanonicalRollup(ctx, srv.URL, "0x"+strings.ToUpper(tt.registry[2:]))
			if err != nil || rollup != tt.rollup {
				t.Errorf("CanonicalRollup = %s, %v; want %s", rollup, err, tt.rollup)
			}
			if v, err := c.RollupVersion(ctx, srv.URL, tt.rollup); err != nil || v != tt.version {
				t.Errorf("RollupVersion = %d, %v; want %d", v, err, tt.version)
			}
		})
	}
}

func TestL1Errors(t *testing.T) {
	const registry = "0x35b22e09ee0390539439e24f06da43d83f90e298"
	const zero = "0x0000000000000000000000000000000000000000"
	word := func(hex string) []byte {
		return []byte(`{"jsonrpc":"2.0","id":1,"result":"0x` + hex + `"}`)
	}
	srv := mockL1(t, map[string][]byte{
		// Recorded: the registry has no getVersion() and reverts; the zero
		// address has no code.
		registry + " " + selGetVersion: readFixture(t, "live/l1/eth_call-revert-publicnode-mainnet.json"),
		zero + " " + selGetVersion:     readFixture(t, "live/l1/eth_call-no-code-publicnode-mainnet.json"),
		// Synthetic malformed results.
		zero + " " + selGetCanonicalRollup:                            word("ff" + strings.Repeat("0", 22) + strings.Repeat("1", 40)),
		"0x1111111111111111111111111111111111111111 " + selGetVersion: word(strings.Repeat("f", 64)),
		"0x2222222222222222222222222222222222222222 " + selGetVersion: word("1234"),
		"0x3333333333333333333333333333333333333333 " + selGetVersion: word("zz"),
	})
	c := NewClient(nil)
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{"revert", func() error { _, err := c.RollupVersion(ctx, srv.URL, registry); return err },
			"getVersion: JSON-RPC error 3: execution reverted"},
		{"no contract", func() error { _, err := c.RollupVersion(ctx, srv.URL, zero); return err },
			"getVersion: empty result: no contract at this address?"},
		{"not an address", func() error { _, err := c.CanonicalRollup(ctx, srv.URL, zero); return err },
			"getCanonicalRollup: result is not an address"},
		{"version too large", func() error {
			_, err := c.RollupVersion(ctx, srv.URL, "0x1111111111111111111111111111111111111111")
			return err
		}, "getVersion: version does not fit into 64 bits"},
		{"short result", func() error {
			_, err := c.RollupVersion(ctx, srv.URL, "0x2222222222222222222222222222222222222222")
			return err
		}, "getVersion: result has 2 bytes, want 32"},
		{"not hex", func() error {
			_, err := c.RollupVersion(ctx, srv.URL, "0x3333333333333333333333333333333333333333")
			return err
		}, "getVersion: result is not hex"},
		{"invalid address", func() error { _, err := c.RollupVersion(ctx, srv.URL, "0x1234"); return err },
			`getVersion: invalid contract address "0x1234"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestL1ChainIDInvalid(t *testing.T) {
	for _, result := range []string{`"1"`, `"0x"`, `"0xzz"`, `"0x1ffffffffffffffff"`} {
		srv := mockL1(t, map[string][]byte{"eth_chainId": []byte(`{"jsonrpc":"2.0","id":1,"result":` + result + `}`)})
		if id, err := NewClient(nil).L1ChainID(context.Background(), srv.URL); err == nil {
			t.Errorf("result %s: chain ID %d, want an error", result, id)
		}
	}
}

func TestL1Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewClient(nil).CanonicalRollup(context.Background(), url+"/v3/secret-key", "0x35b22e09ee0390539439e24f06da43d83f90e298")
	if !httpjson.IsTransportError(err) {
		t.Errorf("error %v is not a transport error", err)
	}
	if err != nil && (strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), strings.TrimPrefix(url, "http://"))) {
		t.Errorf("error leaks the URL: %v", err)
	}
}
