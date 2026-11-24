package config

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
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
		{Name: "holesky-1", ExecutionURL: "https://holesky.example.internal:8545", BeaconURL: "https://holesky.example.internal:5052"},
	}
	if len(cfg.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d", len(cfg.Nodes), len(want))
	}
	for i := range want {
		if cfg.Nodes[i] != want[i] {
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
			name:    "invalid yaml",
			yaml:    "nodes: [\n",
			wantErr: []string{"parse:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))

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
