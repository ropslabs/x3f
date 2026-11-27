package ethereum

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/software"
)

// refusedAddr is an address that refusingClient never connects to: dialing
// it fails with "connection refused". This is deterministic, unlike closing
// a test server and reusing its port, which the OS may hand to another test
// server in the meantime (seen as flaky failures).
const refusedAddr = "refused.invalid:8545"

// refusingClient returns a Client whose connections to refusedAddr are
// refused; other addresses are dialed normally.
func refusingClient() *Client {
	var d net.Dialer
	return NewClient(&http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == refusedAddr {
				return nil, &net.OpError{Op: "dial", Net: network, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
			}
			return d.DialContext(ctx, network, addr)
		},
	}})
}

// readFixture returns the contents of a file in testdata/ or fails the test.
func readFixture(t *testing.T, name string) []byte {
	// t.Helper marks this as a helper, so failures are reported at the
	// caller's line instead of inside this function.
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// rpcServer starts a mock JSON-RPC server that checks the request method
// and replies with body. t.Cleanup stops it when the test ends.
func rpcServer(t *testing.T, wantMethod string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The fields of a JSON-RPC 2.0 request the mock checks.
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.JSONRPC != "2.0" || req.Method != wantMethod {
			t.Errorf("unexpected request %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// beaconServer starts a mock beacon API that serves body at wantPath.
func beaconServer(t *testing.T, wantPath string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != wantPath {
			t.Errorf("got %s %s, want GET %s", r.Method, r.URL.Path, wantPath)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hangingServer starts a server that never answers, to provoke timeouts.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body first: the server only notices that the client
		// disconnected (and cancels r.Context()) once the body is consumed.
		io.Copy(io.Discard, r.Body)
		// select waits for whichever channel is ready first.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	// Cleanup functions run last-in, first-out: release the handler
	// first, then close the server (Close waits for running handlers).
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv
}

func TestExecutionVersion(t *testing.T) {
	tests := []struct {
		fixture    string
		wantRaw    string
		wantClient software.Name
	}{
		{"geth.json", "Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3", software.Geth},
		{"reth.json", "reth/v2.11.0-b9abbcc1/x86_64-unknown-linux-gnu", software.Reth},
		{"nethermind.json", "Nethermind/v2.2.2+be201120/linux-x64/dotnet10.0.14", software.Nethermind},
		{"erigon.json", "erigon/3.8.2-8a3f3b2c/linux-amd64/go1.27.4", software.Erigon},
		{"besu.json", "besu/v23.1.1-dev-ac23d311/osx-x86_64/graalvm-java-17", software.Besu},
		// Recorded live (SOURCES.md). Field order and whitespace differ
		// between providers; the decoder must not care.
		{"live/geth-publicnode-mainnet.json", "Geth/v1.17.9-stable-9dffe6e9/linux-amd64/go1.25.14", software.Geth},
		{"live/reth-publicnode-sepolia.json", "reth/v2.11.0-b9abbcc/x86_64-unknown-linux-gnu", software.Reth},
		{"live/reth-flashbots-mainnet.json", "reth/v2.6.0-73a3a00/x86_64-unknown-linux-gnu", software.Reth},
		{"live/proxy-drpc-mainnet.json", "Geth/v10.0.0/drpc", software.Geth},
		{"live/proxy-mevblocker-mainnet.json", "MEVblocker", software.Unknown},
		{"live/proxy-cloudflare-mainnet.json", "cloudflare-ethereum", software.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := rpcServer(t, "web3_clientVersion", readFixture(t, filepath.Join("execution", tt.fixture)))

			got, err := NewClient(nil).ExecutionVersion(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantRaw {
				t.Errorf("got %q, want %q", got, tt.wantRaw)
			}
			if c := ParseClientVersion(got).Name; c != tt.wantClient {
				t.Errorf("client = %s, want %s", c, tt.wantClient)
			}
		})
	}
}

func TestConsensusVersion(t *testing.T) {
	tests := []struct {
		fixture    string
		wantRaw    string
		wantClient software.Name
	}{
		{"spec-example.json", "Lighthouse/v0.1.5 (Linux x86_64)", software.Lighthouse},
		{"lighthouse.json", "Lighthouse/v8.3.3-57c2abc/x86_64-linux", software.Lighthouse},
		{"prysm.json", "Prysm/v7.3.2-844d8b8 (linux amd64)", software.Prysm},
		{"teku.json", "teku/v26.10.3/linux-x86_64/-eclipseadoptium-openjdk64bitservervm-java-25", software.Teku},
		{"nimbus.json", "Nimbus/v26.10.2-0abc0d-stateofus", software.Nimbus},
		{"lodestar.json", "Lodestar/v1.51.0/7923280", software.Lodestar},
		{"grandine.json", "Grandine/2.0.6-70a5c7ea/x86_64-linux", software.Grandine},
		// Recorded live (SOURCES.md).
		{"live/lighthouse-publicnode-mainnet.json", "Lighthouse/v8.3.3-57c2abc/x86_64-linux", software.Lighthouse},
		{"live/nimbus-unstable-mainnet.json", "Nimbus/v26.10.2-f64d64-stateofus", software.Nimbus},
		{"live/lodestar-chainsafe-mainnet.json", "Lodestar/v1.51.0/7923280", software.Lodestar},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			body := readFixture(t, filepath.Join("consensus", tt.fixture))
			srv := beaconServer(t, "/eth/v1/node/version", body)

			got, err := NewClient(nil).ConsensusVersion(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantRaw {
				t.Errorf("got %q, want %q", got, tt.wantRaw)
			}
			if c := ParseClientVersion(got).Name; c != tt.wantClient {
				t.Errorf("client = %s, want %s", c, tt.wantClient)
			}
		})
	}
}

// Provider-style endpoints put a key into the path or query. The API path
// must be appended after the key, and the query must be kept.
func TestConsensusVersionKeepsPathPrefixAndQuery(t *testing.T) {
	body := readFixture(t, filepath.Join("consensus", "lighthouse.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/KEY123/eth/v1/node/version" || r.URL.Query().Get("apikey") != "abc" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write(body)
	}))
	defer srv.Close()

	if _, err := NewClient(nil).ConsensusVersion(context.Background(), srv.URL+"/KEY123/?apikey=abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrors(t *testing.T) {
	// handler returns an http.HandlerFunc that always answers with status
	// and body. Bodies below are synthetic, except the testdata fixture.
	handler := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, body)
		}
	}

	tests := []struct {
		name      string
		handler   http.Handler
		consensus bool // query the beacon API instead of JSON-RPC
		wantErr   string
		exact     bool // wantErr must be the whole message
	}{
		{
			name:    "json-rpc method not found",
			handler: handler(200, string(readFixture(t, filepath.Join("execution", "error-method-not-found.json")))),
			wantErr: "JSON-RPC error -32601: the method web3_clientVersion does not exist/is not available",
		},
		{
			name:    "live: provider plan error",
			handler: handler(200, string(readFixture(t, filepath.Join("execution", "live", "error-drpc-sepolia-free-plan.json")))),
			wantErr: "JSON-RPC error 35: chain is not available on free plan, please upgrade to paid plan",
		},
		{
			// Seen live from a public endpoint behind Cloudflare. The code has
			// no standard text; the message must not end with a space.
			name:    "status code without standard text",
			handler: handler(525, "error code: 525"),
			wantErr: "web3_clientVersion: unexpected HTTP status 525",
			exact:   true,
		},
		{
			name:    "json-rpc without result",
			handler: handler(200, `{"jsonrpc":"2.0","id":1}`),
			wantErr: "neither result nor error",
		},
		{
			name:    "json-rpc result of wrong type",
			handler: handler(200, `{"jsonrpc":"2.0","id":1,"result":42}`),
			wantErr: "decode result",
		},
		{
			name:    "http error status",
			handler: handler(500, "internal error"),
			wantErr: "unexpected HTTP status 500 Internal Server Error",
		},
		{
			name:    "invalid json",
			handler: handler(200, "<html>not json</html>"),
			wantErr: "decode response",
		},
		{
			name: "redirect is not followed",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
			}),
			wantErr: "unexpected HTTP status 302 Found",
		},
		{
			name:      "beacon without version",
			handler:   handler(200, `{"data":{}}`),
			consensus: true,
			wantErr:   "response has no data.version",
		},
		{
			name:      "beacon http error status",
			handler:   handler(503, ""),
			consensus: true,
			wantErr:   "/eth/v1/node/version: unexpected HTTP status 503",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient(nil)
			var err error
			if tt.consensus {
				_, err = c.ConsensusVersion(context.Background(), srv.URL)
			} else {
				_, err = c.ExecutionVersion(context.Background(), srv.URL)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.exact && err.Error() != tt.wantErr {
				t.Errorf("error = %q, want exactly %q", err, tt.wantErr)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	srv := hangingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := NewClient(nil).ExecutionVersion(ctx, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

// TestErrorsDoNotLeakURL makes sure that no error contains the endpoint URL
// or any part of it. Provider URLs often carry API keys in the path, the
// query, the user info or the subdomain.
func TestErrorsDoNotLeakURL(t *testing.T) {
	const (
		pathKey  = "pathsecret0123456789"
		queryKey = "querysecret987654"
		userKey  = "usersecret42"
		subKey   = "subdomainsecret77"
	)
	secrets := []string{pathKey, queryKey, userKey, subKey}

	// assertClean fails if err is nil or mentions anything sensitive.
	assertClean := func(t *testing.T, err error, extra ...string) {
		t.Helper()
		if err == nil {
			t.Fatal("expected an error")
		}
		for _, s := range append(secrets, extra...) {
			if strings.Contains(err.Error(), s) {
				t.Errorf("error leaks %q: %v", s, err)
			}
		}
	}
	// withKeys appends a secret path and query to a server URL.
	withKeys := func(base string) string {
		return base + "/v3/" + pathKey + "?apikey=" + queryKey
	}

	t.Run("connection refused", func(t *testing.T) {
		_, err := refusingClient().ExecutionVersion(context.Background(), withKeys("http://"+refusedAddr))
		assertClean(t, err, refusedAddr, "refused.invalid")
		if !strings.Contains(err.Error(), "connection failed") {
			t.Errorf("error = %v, want connection failed", err)
		}
	})

	t.Run("dns failure with secret subdomain", func(t *testing.T) {
		host := subKey + ".rpc.example"
		// A fake dialer returns the DNS error that a real lookup would
		// produce, so the test needs no network access.
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{
					Err: "no such host", Name: host, IsNotFound: true,
				}}
			},
		}
		c := NewClient(&http.Client{Transport: transport})

		_, err := c.ConsensusVersion(context.Background(), withKeys("https://"+host))
		assertClean(t, err, host)
		if !strings.Contains(err.Error(), "DNS lookup failed") {
			t.Errorf("error = %v, want DNS lookup failed", err)
		}
	})

	t.Run("timeout with user info", func(t *testing.T) {
		srv := hangingServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		endpoint := strings.Replace(withKeys(srv.URL), "http://", "http://user:"+userKey+"@", 1)
		_, err := NewClient(nil).ExecutionVersion(ctx, endpoint)
		assertClean(t, err)
	})

	t.Run("server echoes URL in json-rpc error", func(t *testing.T) {
		// Synthetic: a misbehaving server that repeats the request URL.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			msg, _ := json.Marshal("invalid key in " + r.URL.String())
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":`+string(msg)+`}}`)
		}))
		defer srv.Close()

		_, err := NewClient(nil).ExecutionVersion(context.Background(), withKeys(srv.URL))
		assertClean(t, err)
		if !strings.Contains(err.Error(), "invalid key in") {
			t.Errorf("error = %v, want the rest of the server message", err)
		}
	})

	t.Run("fork config calls", func(t *testing.T) {
		// Synthetic: the server echoes the URL in a JSON-RPC error, and
		// answers the beacon spec request with an echoed 404 page.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				http.Error(w, "no route "+r.URL.String(), http.StatusNotFound)
				return
			}
			msg, _ := json.Marshal("method not found at " + r.URL.String())
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":`+string(msg)+`}}`)
		}))
		defer srv.Close()

		_, err := NewClient(nil).ExecutionForkConfig(context.Background(), withKeys(srv.URL))
		assertClean(t, err)
		_, err = NewClient(nil).ConsensusForkConfig(context.Background(), withKeys(srv.URL))
		assertClean(t, err)
	})

	t.Run("unparsable endpoint", func(t *testing.T) {
		_, err := NewClient(nil).ExecutionVersion(context.Background(), "http://bad host/"+pathKey)
		assertClean(t, err)
	})

	t.Run("redacted error does not wrap original", func(t *testing.T) {
		// The original *url.Error contains the full URL; it must not be
		// reachable via errors.As either.
		_, err := refusingClient().ExecutionVersion(context.Background(), withKeys("http://"+refusedAddr))
		for e := err; e != nil; e = errors.Unwrap(e) {
			if strings.Contains(e.Error(), pathKey) {
				t.Errorf("wrapped error leaks secret: %v", e)
			}
		}
	})
}

func TestIsTransportError(t *testing.T) {
	dnsFail := NewClient(&http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{Err: "no such host", Name: "x.example", IsNotFound: true}}
		},
	}})
	rpcError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(readFixture(t, filepath.Join("execution", "error-method-not-found.json")))
	}))
	defer rpcError.Close()
	status500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer status500.Close()
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "not json")
	}))
	defer badJSON.Close()
	hanging := hangingServer(t)

	tests := []struct {
		name   string
		client *Client
		url    string
		ctx    time.Duration // 0 means no timeout
		want   bool
	}{
		{"connection refused", refusingClient(), "http://" + refusedAddr, 0, true},
		{"dns failure", dnsFail, "http://x.example", 0, true},
		{"timeout", NewClient(nil), hanging.URL, 50 * time.Millisecond, true},
		{"json-rpc error", NewClient(nil), rpcError.URL, 0, false},
		{"http 500", NewClient(nil), status500.URL, 0, false},
		{"malformed response", NewClient(nil), badJSON.URL, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.ctx > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctx)
				defer cancel()
			}
			_, err := tt.client.ExecutionVersion(ctx, tt.url)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := httpjson.IsTransportError(err); got != tt.want {
				t.Errorf("httpjson.IsTransportError(%v) = %v, want %v", err, got, tt.want)
			}
		})
	}
}
