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
		{Name: "mainnet-1", Type: TypeEthereum, ExecutionURL: "http://localhost:8545", BeaconURL: "http://localhost:5052"},
		{Name: "hoodi-1", Type: TypeEthereum, ExecutionURL: "https://hoodi.example.internal:8545", BeaconURL: "https://hoodi.example.internal:5052"},
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

func TestTelegram(t *testing.T) {
	parse := func(telegram string) (*Config, error) {
		return Parse([]byte(telegram + `
nodes:
  - name: a
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
`))
	}

	cfg, err := parse("")
	if err != nil || cfg.Telegram != nil {
		t.Fatalf("no telegram section: %+v, %v", cfg.Telegram, err)
	}

	// An unquoted number is a YAML integer; it still decodes into the
	// string field.
	cfg, err = parse("telegram:\n  chat_id: -1001234567890\n")
	if err != nil {
		t.Fatal(err)
	}
	tg := cfg.Telegram
	if tg.ChatID != "-1001234567890" || tg.MinLevel != "warn" || tg.Remind() != 24*time.Hour {
		t.Errorf("defaults: %+v, remind %v", tg, tg.Remind())
	}

	cfg, err = parse("telegram: {chat_id: \"@x3f_alerts\", min_level: CRIT, remind_every: 0}\n")
	if err != nil {
		t.Fatal(err)
	}
	if tg := cfg.Telegram; tg.ChatID != "@x3f_alerts" || tg.MinLevel != "crit" || tg.Remind() != 0 {
		t.Errorf("explicit: %+v, remind %v", tg, tg.Remind())
	}
	cfg, err = parse("telegram: {chat_id: 42, remind_every: 6h30m}\n")
	if err != nil || cfg.Telegram.Remind() != 6*time.Hour+30*time.Minute {
		t.Errorf("remind_every 6h30m: %v", err)
	}

	for _, tt := range []struct{ yaml, want string }{
		{"telegram: {}", "telegram: chat_id must be a numeric chat ID or @channelname"},
		{"telegram: {chat_id: 1, min_level: info}", `telegram: min_level "info" is not warn or crit`},
		{"telegram: {chat_id: 1, remind_every: 24}", `"24" is not a duration such as 24h, 90m or 0`},
		{"telegram: {chat_id: 1, remind_every: -1h}", `"-1h" is not a duration`},
		// The bot token belongs in X3F_TELEGRAM_BOT_TOKEN.
		{"telegram: {chat_id: 1, bot_token: x}", "field bot_token not found"},
	} {
		_, err := parse(tt.yaml + "\n")
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", tt.yaml, err, tt.want)
		}
	}

	// A token pasted as chat_id is not echoed.
	const token = "123456789:AAsecret-token-not-real" // gitleaks:allow (test dummy)
	_, err = parse("telegram: {chat_id: \"" + token + "\"}\n")
	if err == nil || strings.Contains(err.Error(), "AAsecret") {
		t.Errorf("error = %v", err)
	}
}

func TestNodeType(t *testing.T) {
	tests := []struct {
		name, typ string
		want      string // normalized type; empty: an error is expected
	}{
		{"default", "", TypeEthereum},
		{"explicit", "ethereum", TypeEthereum},
		{"capitalized", " Ethereum ", TypeEthereum},
		{"unknown", "solana", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "nodes:\n  - name: a\n    execution_url: http://localhost:8545\n    beacon_url: http://localhost:5052\n"
			if tt.typ != "" {
				yaml += "    type: \"" + tt.typ + "\"\n"
			}
			cfg, err := Parse([]byte(yaml))
			if tt.want == "" {
				if err == nil || !strings.Contains(err.Error(), `node "a": type "solana" is not one of ethereum`) {
					t.Errorf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Nodes[0].Type; got != tt.want {
				t.Errorf("type = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAztecNode(t *testing.T) {
	// Every case has the Ethereum node "eth" and the Aztec node "az";
	// node holds the fields of "az" after its name.
	const eth = "  - name: eth\n    execution_url: http://localhost:8545\n    beacon_url: http://localhost:5052\n"
	tests := []struct {
		name  string
		node  string
		extra string // more nodes
		// wantErr is a part of the error; empty: no error expected.
		wantErr           string
		wantL1, wantL1URL string
	}{
		{
			name:   "l1 names an Ethereum node",
			node:   "    type: Aztec\n    url: http://localhost:8080\n    l1: \" eth \"\n",
			wantL1: "eth",
		},
		{
			name:      "l1_url",
			node:      "    type: aztec\n    url: http://localhost:8080\n    l1_url: https://provider.example/v3/key\n",
			wantL1URL: "https://provider.example/v3/key",
		},
		{
			name:    "neither l1 nor l1_url",
			node:    "    type: aztec\n    url: http://localhost:8080\n",
			wantErr: `node "az": l1 (an Ethereum node of this config) or l1_url is required`,
		},
		{
			name:    "both l1 and l1_url",
			node:    "    type: aztec\n    url: http://localhost:8080\n    l1: eth\n    l1_url: https://provider.example\n",
			wantErr: `node "az": set only one of l1 and l1_url`,
		},
		{
			name:    "l1 is not a node",
			node:    "    type: aztec\n    url: http://localhost:8080\n    l1: eht\n",
			wantErr: `node "az": l1 "eht" is not a node of this config`,
		},
		{
			name:    "l1 is not an Ethereum node",
			node:    "    type: aztec\n    url: http://localhost:8080\n    l1: az2\n",
			extra:   "  - name: az2\n    type: aztec\n    url: http://localhost:8081\n    l1: eth\n",
			wantErr: `node "az": l1 "az2" is not an Ethereum node`,
		},
		{
			name:    "l1 is a URL",
			node:    "    type: aztec\n    url: http://localhost:8080\n    l1: https://provider.example/v3/secret-key\n",
			wantErr: `node "az": l1 is a node name; use l1_url for an endpoint URL`,
		},
		{
			name:    "url is required",
			node:    "    type: aztec\n    l1: eth\n",
			wantErr: `node "az": url: is required`,
		},
		{
			name:    "invalid url and l1_url",
			node:    "    type: aztec\n    url: \"https://host.example/%zz/secret\"\n    l1_url: \"ftp://secret@host.example\"\n",
			wantErr: `node "az": l1_url: scheme must be http or https, got "ftp"`,
		},
		{
			name: "Ethereum fields on an Aztec node",
			node: "    type: aztec\n    url: http://localhost:8080\n    l1: eth\n" +
				"    execution_url: http://localhost:8545\n    beacon_url: http://localhost:5052\n    network: mainnet\n",
			wantErr: `node "az": execution_url is not used by type aztec` + "\n" +
				`node "az": beacon_url is not used by type aztec` + "\n" +
				`node "az": network is not used by type aztec`,
		},
		{
			name: "Aztec fields on an Ethereum node",
			node: "    execution_url: http://localhost:8545\n    beacon_url: http://localhost:5052\n" +
				"    url: http://localhost:8080\n    l1: eth\n    l1_url: https://provider.example\n",
			wantErr: `node "az": url is not used by type ethereum` + "\n" +
				`node "az": l1 is not used by type ethereum` + "\n" +
				`node "az": l1_url is not used by type ethereum`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "nodes:\n" + eth + "  - name: az\n" + tt.node + tt.extra
			cfg, err := Parse([]byte(yaml))
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Errorf("error leaks URL contents: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			az := cfg.Nodes[1]
			if az.Type != TypeAztec || az.L1 != tt.wantL1 || az.L1URL != tt.wantL1URL {
				t.Errorf("type %q, l1 %q, l1_url %q; want aztec, %q, %q", az.Type, az.L1, az.L1URL, tt.wantL1, tt.wantL1URL)
			}
		})
	}
}
