package config

import (
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// go test runs with the package directory as working directory, so
	// relative paths to testdata/ work.
	cfg, err := Load(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	want := []Node{
		{Name: "mainnet-1", ExecutionURL: "http://localhost:8545", BeaconURL: "http://localhost:5052"},
		{Name: "hoodi-1", ExecutionURL: "https://hoodi.example.internal:8545", BeaconURL: "https://hoodi.example.internal:5052"},
	}
	if len(cfg.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d", len(cfg.Nodes), len(want))
	}
	for i := range want {
		// reflect.DeepEqual compares structs that contain slices, which
		// == cannot.
		if !reflect.DeepEqual(cfg.Nodes[i], want[i]) {
			t.Errorf("nodes[%d] = %+v, want %+v", i, cfg.Nodes[i], want[i])
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "does-not-exist.yaml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want an fs.ErrNotExist error", err)
	}
}

// TestParse is table-driven: each entry is one case, and the loop below runs
// them all as named subtests (go test -run 'TestParse/duplicate' runs one).
func TestParse(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		// wantErr lists substrings the error must contain; empty means
		// the config must be valid.
		wantErr []string
	}{
		{
			name: "valid",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
`,
		},
		{
			name:    "empty document",
			yaml:    "",
			wantErr: []string{"config is empty"},
		},
		{
			name:    "no nodes",
			yaml:    "nodes: []\n",
			wantErr: []string{"no nodes configured"},
		},
		{
			name: "missing name",
			yaml: `
nodes:
  - execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
`,
			wantErr: []string{"nodes[0]: name is required"},
		},
		{
			name: "duplicate name",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
  - name: a
    execution_url: http://localhost:8546
    beacon_url: http://localhost:5053
`,
			wantErr: []string{`node "a": duplicate name`},
		},
		{
			name: "missing urls",
			yaml: `
nodes:
  - name: a
`,
			wantErr: []string{"execution_url: is required", "beacon_url: is required"},
		},
		{
			name: "wrong scheme",
			yaml: `
nodes:
  - name: a
    execution_url: ws://localhost:8546
    beacon_url: http://localhost:5052
`,
			wantErr: []string{`execution_url: scheme must be http or https, got "ws"`},
		},
		{
			name: "missing host",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: "http://"
`,
			wantErr: []string{"beacon_url: host is missing"},
		},
		{
			name: "unparsable url",
			yaml: `
nodes:
  - name: a
    execution_url: "http://local host:8545"
    beacon_url: http://localhost:5052
`,
			wantErr: []string{"execution_url: invalid URL"},
		},
		{
			// Provider URLs carry API keys; the error must not repeat them.
			// The loop below checks that "secret" never appears in errors.
			name: "unparsable url with key is not echoed",
			yaml: `
nodes:
  - name: a
    execution_url: "https://rpc host.example/v3/secret-key-123"
    beacon_url: "https://user:secret-pw@host.example/%zz/secret"
`,
			wantErr: []string{"execution_url: invalid URL", "beacon_url: invalid URL"},
		},
		{
			name: "known network",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    network: hoodi
`,
		},
		{
			// Holesky is deprecated and not supported.
			name: "unknown network",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    network: holesky
`,
			wantErr: []string{`node "a": network "holesky" is not one of mainnet, sepolia, hoodi`},
		},
		{
			name: "unknown network in other capitalisation",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    network: Holesky
`,
			wantErr: []string{`network "Holesky" is not one of mainnet, sepolia, hoodi`},
		},
		{
			name: "unknown field",
			yaml: `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_ulr: http://localhost:5052
`,
			wantErr: []string{"beacon_ulr"},
		},
		{
			name: "ignore lists",
			yaml: `
ignore_advisories:
  - {id: GHSA-v5gq-r843-8f73, reason: "no QBFT", expires: 2027-03-01}
  - {id: GHSA-4h4f-925g-8h9p, reason: "no EIP-7702 traffic"}
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    ignore_advisories:
      - {id: GHSA-v5gq-r843-8f73, reason: "other expiry", expires: "2027-06-01"}
`,
		},
		{
			name: "ignore list mistakes",
			yaml: `
ignore_advisories:
  - {id: CVE-2026-26314, reason: x}
  - {id: GHSA-v5gq-r843-8f73}
  - {id: ghsa-v5gq-r843-8f73, reason: "  "}
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    ignore_advisories:
      - {id: GHSA-4h4f-925g-8h9p-x, reason: typo}
`,
			wantErr: []string{
				`ignore_advisories[0]: id "CVE-2026-26314" is not a GitHub advisory ID`,
				"ignore_advisories GHSA-v5gq-r843-8f73: reason is required",
				"ignore_advisories GHSA-v5gq-r843-8f73: listed twice",
				`node "a": ignore_advisories[0]: id "GHSA-4h4f-925g-8h9p-x" is not`,
			},
		},
		{
			name: "ignore entry with a time of day",
			yaml: `
ignore_advisories:
  - {id: GHSA-v5gq-r843-8f73, reason: x, expires: 2027-03-01T12:00:00Z}
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
`,
			wantErr: []string{`line 3: "2027-03-01T12:00:00Z" is not a date (YYYY-MM-DD)`},
		},
		{
			name: "ignore entry with an unknown field",
			yaml: `
ignore_advisories:
  - {id: GHSA-v5gq-r843-8f73, reason: x, expiry: 2027-03-01}
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
`,
			wantErr: []string{"field expiry not found"},
		},
		{
			name:    "invalid yaml",
			yaml:    "nodes: [\n",
			wantErr: []string{"parse:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Errorf("error leaks URL contents: %v", err)
			}

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestNetworkNameIsNormalized(t *testing.T) {
	for _, in := range []string{"mainnet", "Mainnet", "MAINNET", " mainnet "} {
		cfg, err := Parse([]byte(`
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    network: "` + in + `"
`))
		if err != nil {
			t.Errorf("%q: unexpected error: %v", in, err)
			continue
		}
		if got := cfg.Nodes[0].Network; got != "mainnet" {
			t.Errorf("%q normalized to %q, want %q", in, got, "mainnet")
		}
	}
}

func TestIgnoreAdvisories(t *testing.T) {
	cfg, err := Parse([]byte(`
ignore_advisories:
  - {id: ghsa-V5GQ-r843-8f73, reason: "no QBFT", expires: 2027-03-01}
  - {id: GHSA-4h4f-925g-8h9p, reason: global}
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
    ignore_advisories:
      - {id: GHSA-4h4f-925g-8h9p, reason: node}
`))
	if err != nil {
		t.Fatal(err)
	}
	g := cfg.IgnoreAdvisories
	if g[0].ID != "GHSA-v5gq-r843-8f73" {
		t.Errorf("ID normalized to %q", g[0].ID)
	}
	if want := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC); !g[0].Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v", g[0].Expires, want)
	}
	if !g[1].Expires.IsZero() {
		t.Errorf("Expires = %v, want zero", g[1].Expires)
	}

	// The node's entry replaces the global one with the same ID.
	merged := MergeIgnores(g, cfg.Nodes[0].IgnoreAdvisories)
	var got []string
	for _, e := range merged {
		got = append(got, e.ID+" "+e.Reason)
	}
	want := []string{"GHSA-4h4f-925g-8h9p node", "GHSA-v5gq-r843-8f73 no QBFT"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %q, want %q", got, want)
	}

	for _, tt := range []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2027, 2, 28, 23, 59, 59, 0, time.UTC), false},
		{time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), true},
		{time.Date(2027, 3, 1, 0, 30, 0, 0, time.FixedZone("CET", 3600)), false}, // 23:30 UTC the day before
	} {
		if got := g[0].Expired(tt.at); got != tt.want {
			t.Errorf("Expired(%v) = %v, want %v", tt.at, got, tt.want)
		}
	}
	if g[1].Expired(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("entry without expires expired")
	}
}
