package forks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ropslabs/x3f/internal/software"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func TestEmbeddedScheduleIsValid(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Updated.Format(time.DateOnly); got != "2026-11-25" {
		t.Errorf("updated = %s", got)
	}

	// State on 2026-11-24 according to the EF blog (see SOURCES.md).
	tests := []struct {
		network    string
		lastActive string
		postponed  []string
	}{
		{"mainnet", "BPO2", nil},
		{"sepolia", "Glamsterdam", nil},
		{"hoodi", "BPO2", []string{"Glamsterdam"}},
	}
	for _, tt := range tests {
		n, ok := s.Networks[tt.network]
		if !ok {
			t.Fatalf("no %s", tt.network)
		}
		active := n.Active(now)
		if got := active[len(active)-1].Name; got != tt.lastActive {
			t.Errorf("%s: last active = %s, want %s", tt.network, got, tt.lastActive)
		}
		if next, ok := n.Next(now); ok {
			t.Errorf("%s: unexpected next fork %s", tt.network, next.Name)
		}
		var postponed []string
		for _, p := range n.Postponed() {
			postponed = append(postponed, p.Name)
		}
		if !slices.Equal(postponed, tt.postponed) {
			t.Errorf("%s: postponed = %v, want %v", tt.network, postponed, tt.postponed)
		}
	}

	// Spot checks against the announcements.
	mainnet := s.Networks["mainnet"]
	if got := mainnet.Forks[0].MinVersions[software.Geth].String(); got != "1.16.7" {
		t.Errorf("mainnet Fusaka geth minimum = %s", got)
	}
	hoodiGlam := s.Networks["hoodi"].Postponed()[0]
	if got := hoodiGlam.RemovedIn[software.Geth].String(); got != "1.17.10" {
		t.Errorf("hoodi Glamsterdam geth removed_in = %s", got)
	}
	if got := hoodiGlam.Time.Format(time.RFC3339); got != "2026-10-26T17:42:48Z" {
		t.Errorf("hoodi Glamsterdam former time = %s", got)
	}
	if n, ok := s.ForChainID(560048); !ok || n.Name != "hoodi" {
		t.Errorf("ForChainID(560048) = %s, %v", n.Name, ok)
	}
}

func TestParseRejectsMistakes(t *testing.T) {
	// valid is a minimal correct document; each case breaks one thing.
	// Mainnet genesis 1606824023 + 411392 * 384 = 1764798551.
	const valid = `updated: 2026-11-24
networks:
  mainnet:
    chain_id: 1
    genesis_time: 1606824023
    forks:
      - name: Fusaka
        execution: Osaka
        consensus: Fulu
        epoch: 411392
        timestamp: 1764798551
        min_versions: {geth: 1.16.7}
        sources: [https://blog.ethereum.org/2025/11/06/fusaka-mainnet-announcement]
`
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	tests := []struct {
		name, from, to, wantErr string
	}{
		{"typo in timestamp", "timestamp: 1764798551", "timestamp: 1764798552", "does not match epoch 411392 (want 1764798551)"},
		{"unknown network", "  mainnet:", "  holesky:", `network "holesky" is not one of`},
		{"wrong chain id", "chain_id: 1", "chain_id: 2", "chain_id 2, want 1"},
		{"unknown client", "{geth: 1.16.7}", "{erigonx: 1.0.0}", `unknown client "erigonx"`},
		{"invalid version", "{geth: 1.16.7}", "{geth: latest}", `invalid version "latest"`},
		{"missing source", "        sources: [https://blog.ethereum.org/2025/11/06/fusaka-mainnet-announcement]\n", "", "at least one source is required"},
		{"source not https", "https://blog", "http://blog", "is not an https URL"},
		{"unknown field", "        min_versions:", "        min_version:", "field min_version not found"},
		{"bad type", "        epoch:", "        type: hard\n        epoch:", `type "hard"`},
		{"bad status", "        epoch:", "        status: cancelled\n        epoch:", `status "cancelled"`},
		{"removed_in without postponed", "        epoch:", "        removed_in: {geth: 1.0.0}\n        epoch:", "only apply to postponed forks"},
		{"bad date", "updated: 2026-11-24", "updated: yesterday", "updated: want a date"},
		{"missing layer name", "        consensus: Fulu\n", "", "execution and consensus fork names are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(valid, tt.from) {
				t.Fatalf("test bug: %q not in document", tt.from)
			}
			_, err := Parse([]byte(strings.Replace(valid, tt.from, tt.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRejectsWrongOrder(t *testing.T) {
	_, err := Parse([]byte(`updated: 2026-11-24
networks:
  mainnet:
    chain_id: 1
    genesis_time: 1606824023
    forks:
      - {name: BPO1, type: bpo, epoch: 412672, timestamp: 1765290071, sources: [https://a.example]}
      - {name: Fusaka, execution: Osaka, consensus: Fulu, epoch: 411392, timestamp: 1764798551, sources: [https://a.example]}
`))
	if err == nil || !strings.Contains(err.Error(), "must be in order of activation") {
		t.Errorf("error = %v", err)
	}
}

// TestCrossCheckEthClients compares forks.yaml with a snapshot of the
// eth-clients network configs (testdata/eth-clients, see SOURCES.md).
// eth-clients is a cross-check only: a failure here means "look at the
// official sources again", never "copy the value over".
//
// To refresh the snapshot, download metadata/config.yaml and the "config"
// section of metadata/genesis.json from eth-clients/{mainnet,sepolia,hoodi}
// and update the commit hashes in SOURCES.md.
func TestCrossCheckEthClients(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for name, n := range s.Networks {
		t.Run(name, func(t *testing.T) {
			el, cl := ethClientsSchedule(t, name)
			oldest := n.Forks[0]

			want := map[string]bool{} // "EL ts" / "CL epoch" entries forks.yaml implies
			for _, f := range n.Forks {
				elKey, clKey := keysFor(f)
				if f.Postponed {
					if ts, ok := el[elKey]; ok && ts == f.Time.Unix() {
						t.Errorf("eth-clients still schedules postponed %s at %d (%s)", f.Name, ts, elKey)
					}
					if ep, ok := cl[clKey]; ok && ep == f.Epoch {
						t.Errorf("eth-clients still schedules postponed %s at epoch %d (%s)", f.Name, ep, clKey)
					}
					continue
				}
				if ts, ok := el[elKey]; !ok || ts != f.Time.Unix() {
					t.Errorf("%s: forks.yaml has EL timestamp %d, eth-clients %s = %d (present: %v)", f.Name, f.Time.Unix(), elKey, ts, ok)
				}
				if ep, ok := cl[clKey]; !ok || ep != f.Epoch {
					t.Errorf("%s: forks.yaml has CL epoch %d, eth-clients %s = %d (present: %v)", f.Name, f.Epoch, clKey, ep, ok)
				}
				want["EL "+elKey] = true
				want["CL "+clKey] = true
			}
			// Forks in eth-clients that forks.yaml does not know (from the
			// oldest fork listed in forks.yaml on).
			for k, ts := range el {
				if ts >= oldest.Time.Unix() && !want["EL "+k] {
					t.Errorf("eth-clients schedules %s = %d, missing in forks.yaml", k, ts)
				}
			}
			for k, ep := range cl {
				if ep >= oldest.Epoch && !want["CL "+k] {
					t.Errorf("eth-clients schedules %s = epoch %d, missing in forks.yaml", k, ep)
				}
			}
		})
	}
}

// keysFor returns the eth-clients keys of a fork: e.g. "osakaTime" and
// "FULU_FORK_EPOCH", or "bpo1Time" and "BLOB_SCHEDULE[...]" for BPOs.
func keysFor(f Fork) (elKey, clKey string) {
	if f.BPO {
		return strings.ToLower(f.Name) + "Time", "BLOB_SCHEDULE " + strings.ToLower(f.Name)
	}
	return strings.ToLower(f.Execution) + "Time", strings.ToUpper(f.Consensus) + "_FORK_EPOCH"
}

// ethClientsSchedule reads the snapshot: EL fork times (key -> unix time)
// and CL fork epochs (key -> epoch; BPOs as "BLOB_SCHEDULE bpoN" in order).
// Far-future epochs mean "not scheduled" and are left out.
func ethClientsSchedule(t *testing.T, network string) (el map[string]int64, cl map[string]uint64) {
	t.Helper()
	dir := filepath.Join("testdata", "eth-clients")

	var genesis struct {
		Config map[string]any `json:"config"`
	}
	data, err := os.ReadFile(filepath.Join(dir, network+"-genesis-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &genesis); err != nil {
		t.Fatal(err)
	}
	el = map[string]int64{}
	for k, v := range genesis.Config {
		if f, ok := v.(float64); ok && strings.HasSuffix(k, "Time") {
			el[k] = int64(f)
		}
	}

	var config struct {
		Forks        map[string]any `yaml:",inline"`
		BlobSchedule []struct {
			Epoch uint64 `yaml:"EPOCH"`
		} `yaml:"BLOB_SCHEDULE"`
	}
	data, err = os.ReadFile(filepath.Join(dir, network+"-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	const farFuture = 18446744073709551615
	cl = map[string]uint64{}
	for k, v := range config.Forks {
		if !strings.HasSuffix(k, "_FORK_EPOCH") {
			continue
		}
		var ep uint64
		if _, err := fmt.Sscan(fmt.Sprint(v), &ep); err == nil && ep != farFuture {
			cl[k] = ep
		}
	}
	for i, b := range config.BlobSchedule {
		cl[fmt.Sprintf("BLOB_SCHEDULE bpo%d", i+1)] = b.Epoch
	}
	return el, cl
}
