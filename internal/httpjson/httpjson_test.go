package httpjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// JSON-RPC errors are typed, whether the server sends them with HTTP 200
// (Geth) or 400 (Aztec). Other statuses and bodies that are no JSON-RPC
// answer give the status error. Fixtures are those of the ethereum and
// aztec packages (sources in their testdata/SOURCES.md).
func TestCallErrors(t *testing.T) {
	fixture := func(path ...string) []byte {
		data, err := os.ReadFile(filepath.Join(path...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	geth := fixture("..", "ethereum", "testdata", "execution", "error-method-not-found.json")
	aztec := fixture("..", "aztec", "testdata", "method-not-found.json")

	tests := []struct {
		name     string
		status   int
		body     []byte
		wantCode int // 0: no RPCError
		wantErr  string
	}{
		{"error with 200", 200, geth, -32601, "JSON-RPC error -32601: "},
		{"error with 400", 400, aztec, -32601, "JSON-RPC error -32601: Method not found: aztec_getNodeInfo"},
		{"400 without JSON", 400, []byte("<html>bad request</html>"), 0, "unexpected HTTP status 400 Bad Request"},
		{"500 with a JSON-RPC body", 500, aztec, 0, "unexpected HTTP status 500 Internal Server Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write(tt.body)
			}))
			defer srv.Close()
			var out string
			err := NewClient(nil).Call(context.Background(), srv.URL, "aztec_getNodeInfo", &out)
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want prefix %q", err, tt.wantErr)
			}
			code, ok := RPCErrorCode(err)
			if ok != (tt.wantCode != 0) || code != tt.wantCode {
				t.Errorf("RPCErrorCode = %d, %v; want %d", code, ok, tt.wantCode)
			}
			if IsTransportError(err) {
				t.Errorf("%v must not be a transport error", err)
			}
		})
	}
}

// TestCallParams checks the request body: an empty list without
// parameters (not null), and the parameters in order otherwise.
func TestCallParams(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		got = append(got, string(req.Params))
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer srv.Close()
	c := NewClient(nil)
	var out string
	if err := c.Call(context.Background(), srv.URL, "eth_chainId", &out); err != nil {
		t.Fatal(err)
	}
	call := map[string]string{"to": "0x35b22e09ee0390539439e24f06da43d83f90e298", "data": "0x01"}
	if err := c.Call(context.Background(), srv.URL, "eth_call", &out, call, "latest"); err != nil {
		t.Fatal(err)
	}
	want := []string{`[]`, `[{"data":"0x01","to":"0x35b22e09ee0390539439e24f06da43d83f90e298"},"latest"]`}
	if !slices.Equal(got, want) {
		t.Errorf("params %q, want %q", got, want)
	}
}
