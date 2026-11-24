package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// serve starts a server that answers every request with status and body.
func serve(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRun checks the table end to end and makes sure that neither the table
// nor the error list nor the returned error contains any part of the URLs.
func TestRun(t *testing.T) {
	const key = "apikey0123456789secret"

	geth := serve(t, 200, fixture(t, "execution/geth.json"))
	lighthouse := serve(t, 200, fixture(t, "consensus/lighthouse.json"))
	broken := serve(t, 500, nil)

	// A server that is closed right away: connections to it are refused.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadAddr := dead.Listener.Addr().String()
	dead.Close()

	config := `
nodes:
  - name: good-node
    execution_url: ` + geth.URL + `/v3/` + key + `
    beacon_url: ` + lighthouse.URL + `/?key=` + key + `
  - name: bad-node
    execution_url: http://` + deadAddr + `/v3/` + key + `
    beacon_url: ` + broken.URL + `/` + key + `
`
	path := filepath.Join(t.TempDir(), "x3f.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"-config", path, "-timeout", "2s"}, &out, ethereum.NewClient(nil))
	if err == nil || err.Error() != "2 request(s) failed" {
		t.Errorf("run error = %v, want \"2 request(s) failed\"", err)
	}

	got := out.String()
	for _, want := range []string{
		"NODE", "EXECUTION", "CONSENSUS",
		"good-node", "Geth 1.17.10 (stable)", "Lighthouse 8.3.3",
		"bad-node", "ERROR",
		"bad-node execution: web3_clientVersion: connection failed",
		"bad-node consensus: /eth/v1/node/version: unexpected HTTP status 500",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}

	// No part of any endpoint may appear anywhere.
	all := got + "\n" + err.Error()
	for _, secret := range []string{
		key,
		deadAddr,
		strings.TrimPrefix(geth.URL, "http://"),
		strings.TrimPrefix(lighthouse.URL, "http://"),
		strings.TrimPrefix(broken.URL, "http://"),
		"127.0.0.1",
		"http://",
	} {
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

func TestFormatClient(t *testing.T) {
	tests := []struct {
		name string
		in   clientResult
		want string
	}{
		{
			name: "with suffix",
			in:   clientResult{Version: ethereum.ParseClientVersion("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3")},
			want: "Geth 1.17.10 (stable)",
		},
		{
			name: "without suffix",
			in:   clientResult{Version: ethereum.ParseClientVersion("Prysm/v7.3.2-844d8b8 (linux amd64)")},
			want: "Prysm 7.3.2",
		},
		{
			name: "unknown client shows raw string",
			in:   clientResult{Version: ethereum.ParseClientVersion("SomeClient/v1.2.3")},
			want: `unrecognized: "SomeClient/v1.2.3"`,
		},
		{
			name: "error",
			in:   clientResult{Err: os.ErrDeadlineExceeded},
			want: "ERROR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatClient(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
