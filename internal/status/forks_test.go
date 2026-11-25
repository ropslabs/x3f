package status

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/releases"
)

var forkNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// cv builds a ClientVersion from a version string in the client's real
// format (see internal/ethereum/testdata/SOURCES.md); the versions
// themselves are chosen for the test.
func cv(raw string) ethereum.ClientVersion { return ethereum.ParseClientVersion(raw) }

func TestAssessForksBuiltInSchedule(t *testing.T) {
	s, err := forks.Default()
	if err != nil {
		t.Fatal(err)
	}
	const mainnet, sepolia, hoodi = 1, 11155111, 560048
	lighthouse := cv("Lighthouse/v8.3.3-57c2abc/x86_64-linux")

	tests := []struct {
		name      string
		chain     uint64
		el, cl    ethereum.ClientVersion
		wantLevel Level
		wantCell  string
		wantMsgs  []string
	}{
		{
			name: "mainnet, current clients", chain: mainnet,
			el: cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), cl: lighthouse,
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			name: "mainnet, exactly the Fusaka minimum", chain: mainnet,
			el: cv("Geth/v1.16.7-stable-aaaaaaaa/linux-amd64/go1.25.3"), cl: lighthouse,
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			name: "mainnet, below the Fusaka minimum", chain: mainnet,
			el: cv("Geth/v1.16.6-stable-aaaaaaaa/linux-amd64/go1.25.3"), cl: lighthouse,
			wantLevel: Crit, wantCell: "EL below Fusaka minimum",
			wantMsgs: []string{"Geth 1.16.6 is older than the minimum release for Fusaka (1.16.7): it cannot follow mainnet"},
		},
		{
			// Grandine's stable 2.x line does not support Glamsterdam; the
			// Sepolia announcement lists 3.0.0-rc.0.
			name: "sepolia, grandine stable line", chain: sepolia,
			el: cv("reth/v2.11.0-b9abbcc/x86_64-unknown-linux-gnu"), cl: cv("Grandine/2.0.6-70a5c7ea/x86_64-linux"),
			wantLevel: Crit, wantCell: "CL below Glamsterdam minimum",
			wantMsgs: []string{"Grandine 2.0.6 is older than the minimum release for Glamsterdam (3.0.0-rc.0): it cannot follow sepolia"},
		},
		{
			name: "sepolia, pre-release minimum met", chain: sepolia,
			el: cv("reth/v2.11.0-b9abbcc/x86_64-unknown-linux-gnu"), cl: cv("Lighthouse/v8.3.0-rc.0-67da032/x86_64-linux"),
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			name: "hoodi, geth with the postponed schedule", chain: hoodi,
			el: cv("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), cl: cv("Lodestar/v1.51.0/7923280"),
			wantLevel: Crit, wantCell: "EL has old Glamsterdam schedule",
			wantMsgs: []string{"Geth 1.17.9 still contains the postponed Glamsterdam schedule (2026-10-26 17:42 UTC); update to 1.17.10 or later; the former activation time has passed"},
		},
		{
			name: "hoodi, geth before the schedule was added", chain: hoodi,
			el: cv("Geth/v1.17.8-stable-aaaaaaaa/linux-amd64/go1.27.3"), cl: cv("Lodestar/v1.51.0/7923280"),
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			// rc.1 contained the schedule, rc.2 removed it: needs
			// pre-release ordering.
			name: "hoodi, grandine rc.1", chain: hoodi,
			el: cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), cl: cv("Grandine/3.0.0-rc.1-aaaaaaaa/x86_64-linux"),
			wantLevel: Crit, wantCell: "CL has old Glamsterdam schedule",
			wantMsgs: []string{"Grandine 3.0.0 still contains the postponed Glamsterdam schedule"},
		},
		{
			name: "hoodi, grandine rc.2", chain: hoodi,
			el: cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), cl: cv("Grandine/3.0.0-rc.2-aaaaaaaa/x86_64-linux"),
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			// The Nimbus codename suffix must not interfere.
			name: "hoodi, nimbus 26.10.1", chain: hoodi,
			el: cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"), cl: cv("Nimbus/v26.10.1-aaaaaa-stateofus"),
			wantLevel: Crit, wantCell: "CL has old Glamsterdam schedule",
			wantMsgs: []string{"Nimbus 26.10.1 still contains the postponed Glamsterdam schedule"},
		},
		{
			name: "unknown network", chain: 1337,
			el: cv("Geth/v1.0.0-stable/x"), wantLevel: OK, wantCell: "-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessForks(ForkInput{
				Schedule: s, ChainID: tt.chain, HaveChain: true,
				Execution: tt.el, Consensus: tt.cl, Now: forkNow, CriticalWithin: 14 * 24 * time.Hour,
			})
			if r.Cell != tt.wantCell {
				t.Errorf("cell = %q, want %q", r.Cell, tt.wantCell)
			}
			checkAssessment(t, r.Assessment, tt.wantLevel, tt.wantMsgs)
		})
	}

	if r := AssessForks(ForkInput{Schedule: s, HaveChain: false, Now: forkNow}); r.Cell != "-" || r.Level != OK {
		t.Errorf("no network: %+v", r)
	}
}

// synthetic builds a mainnet schedule (synthetic data) with an active fork
// "Past" (min geth 1.0.0), and further forks given as YAML list entries.
// Use epochAt to get a matching epoch/timestamp pair.
func synthetic(t *testing.T, more string) *forks.Schedule {
	t.Helper()
	pastEp, pastTs := epochAt(forkNow.Add(-30 * 24 * time.Hour))
	doc := fmt.Sprintf(`updated: 2026-11-24
networks:
  mainnet:
    chain_id: 1
    genesis_time: 1606824023
    forks:
      - {name: Past, execution: Osaka, consensus: Fulu, epoch: %d, timestamp: %d, min_versions: {geth: 1.0.0, lighthouse: 1.0.0}, sources: [https://example.org/synthetic]}
%s`, pastEp, pastTs, more)
	s, err := forks.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("synthetic schedule: %v\n%s", err, doc)
	}
	return s
}

// epochAt returns the mainnet epoch at or before tm and its timestamp.
func epochAt(tm time.Time) (uint64, int64) {
	const genesis = 1606824023
	ep := (tm.Unix() - genesis) / 384
	return uint64(ep), genesis + ep*384
}

func TestAssessForksNextFork(t *testing.T) {
	ep, ts := epochAt(forkNow.Add(5*24*time.Hour + time.Hour))
	bpo := synthetic(t, fmt.Sprintf(`      - {name: BPO9, type: bpo, epoch: %d, timestamp: %d, sources: [https://example.org/synthetic]}
`, ep, ts))
	future := synthetic(t, fmt.Sprintf(`      - {name: Later, execution: Amsterdam, consensus: Gloas, status: postponed, epoch: %d, timestamp: %d, scheduled_in: {geth: 2.0.0}, removed_in: {geth: 2.0.1}, sources: [https://example.org/synthetic]}
`, ep, ts))

	tests := []struct {
		name      string
		s         *forks.Schedule
		el, cl    ethereum.ClientVersion
		wantLevel Level
		wantCell  string
		wantMsgs  []string
	}{
		{
			// A BPO has no release list; the releases of the hard fork it
			// belongs to ("Past", geth 1.0.0) apply.
			name: "BPO uses the releases of its hard fork",
			s:    bpo, el: cv("Geth/v1.0.0-stable/x"), cl: cv("Lighthouse/v1.0.0-abc1234/x"),
			wantLevel: OK, wantCell: "BPO9 in 5 days: ready",
		},
		{
			name: "below the hard fork minimum: not ready for the BPO either",
			s:    bpo, el: cv("Geth/v0.9.0-stable/x"), cl: cv("Lighthouse/v1.0.0-abc1234/x"),
			wantLevel: Crit, wantCell: "EL below Past minimum",
			wantMsgs: []string{"Geth 0.9.0 is older than the minimum release for Past (1.0.0)", "Geth 0.9.0 is not ready for BPO9 on mainnet"},
		},
		{
			name: "unrecognized client while a fork is scheduled",
			s:    bpo, el: cv("MEVblocker"), cl: cv("Lighthouse/v1.0.0-abc1234/x"),
			wantLevel: Warn, wantCell: "BPO9 in 5 days: EL unknown",
			wantMsgs: []string{"execution client not recognized: readiness for BPO9 unknown"},
		},
		{
			// A postponed fork whose former date is still ahead: no
			// "left the chain" remark yet.
			name: "postponed schedule, former date still ahead",
			s:    future, el: cv("Geth/v2.0.0-stable/x"), cl: cv("Lighthouse/v1.0.0-abc1234/x"),
			wantLevel: Crit, wantCell: "EL has old Later schedule",
			wantMsgs: []string{"update to 2.0.1 or later"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessForks(ForkInput{
				Schedule: tt.s, ChainID: 1, HaveChain: true,
				Execution: tt.el, Consensus: tt.cl, Now: forkNow, CriticalWithin: 14 * 24 * time.Hour,
			})
			if r.Cell != tt.wantCell {
				t.Errorf("cell = %q, want %q", r.Cell, tt.wantCell)
			}
			checkAssessment(t, r.Assessment, tt.wantLevel, tt.wantMsgs)
			for _, f := range r.Findings {
				if strings.Contains(f.Message, "left the chain") && tt.s == future {
					t.Errorf("unexpected remark: %s", f.Message)
				}
			}
		})
	}
}

func TestForkDataNote(t *testing.T) {
	s := &forks.Schedule{Updated: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}
	if _, ok := ForkDataNote(s, s.Updated.Add(30*24*time.Hour)); ok {
		t.Error("30 days old: no note expected")
	}
	f, ok := ForkDataNote(s, s.Updated.Add(31*24*time.Hour))
	if !ok || f.Level != OK || !strings.Contains(f.Message, "fork data last updated 2026-10-25 (31 days ago)") {
		t.Errorf("31 days old: %+v, %v", f, ok)
	}
}

// TestAssessForksPostponedIsARange checks that the "old schedule" rule only
// applies from the release that added the postponed schedule (scheduled_in)
// up to, but not including, the release that removed it (removed_in).
// Releases from before the schedule never knew it and must not turn red.
func TestAssessForksPostponedIsARange(t *testing.T) {
	s, err := forks.Default()
	if err != nil {
		t.Fatal(err)
	}
	const hoodi = 560048
	lodestar := cv("Lodestar/v1.51.0/7923280")
	geth := cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3")

	// Explicit cases (version strings in the clients' real formats).
	tests := []struct {
		name      string
		el, cl    ethereum.ClientVersion
		wantLevel Level
	}{
		{"geth 1.17.5, never had the schedule", cv("Geth/v1.17.5-stable-aaaaaaaa/linux-amd64/go1.27.3"), lodestar, OK},
		{"geth 1.17.8, last release before", cv("Geth/v1.17.8-stable-aaaaaaaa/linux-amd64/go1.27.3"), lodestar, OK},
		{"geth 1.17.9, added the schedule", cv("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), lodestar, Crit},
		{"geth 1.17.10, removed it", cv("Geth/v1.17.10-stable-aaaaaaaa/linux-amd64/go1.27.3"), lodestar, OK},
		{"geth 1.18.0, later release", cv("Geth/v1.18.0-stable-aaaaaaaa/linux-amd64/go1.27.3"), lodestar, OK},
		{"grandine 3.0.0-rc.0, before (pre-release)", geth, cv("Grandine/3.0.0-rc.0-aaaaaaaa/x86_64-linux"), OK},
		{"grandine 3.0.0, after rc.2", geth, cv("Grandine/3.0.0-aaaaaaaa/x86_64-linux"), OK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessForks(ForkInput{
				Schedule: s, ChainID: hoodi, HaveChain: true,
				Execution: tt.el, Consensus: tt.cl, Now: forkNow, CriticalWithin: 14 * 24 * time.Hour,
			})
			if r.Level != tt.wantLevel {
				t.Errorf("level = %v, want %v; findings: %+v", r.Level, tt.wantLevel, r.Findings)
			}
		})
	}

	// Every client listed for Hoodi Glamsterdam, at the range boundaries.
	glam := s.Networks["hoodi"].Postponed()[0]
	for client, from := range glam.ScheduledIn {
		fixed, ok := glam.RemovedIn[client]
		if !ok {
			t.Errorf("%s: scheduled_in without removed_in", client)
			continue
		}
		cases := []struct {
			label   string
			v       releases.Version
			inRange bool
		}{
			{"scheduled_in", from, true},
			{"removed_in", fixed, false},
		}
		below, ok := before(from)
		if !ok {
			t.Errorf("%s: no version below %s constructed", client, from)
		} else {
			cases = append(cases, struct {
				label   string
				v       releases.Version
				inRange bool
			}{"before scheduled_in", below, false})
		}
		for _, c := range cases {
			v := ethereum.ClientVersion{
				Raw: "test", Client: client, Suffix: c.v.Suffix,
				Version: fmt.Sprintf("%d.%d.%d", c.v.Major, c.v.Minor, c.v.Patch),
			}
			in := ForkInput{Schedule: s, ChainID: hoodi, HaveChain: true, Now: forkNow, CriticalWithin: 14 * 24 * time.Hour}
			if isConsensus(client) {
				in.Execution, in.Consensus = geth, v
			} else {
				in.Execution, in.Consensus = v, lodestar
			}
			got := hasFinding(AssessForks(in).Assessment, "postponed Glamsterdam schedule")
			if got != c.inRange {
				t.Errorf("%s %s (%s): old-schedule finding = %v, want %v", client, c.v, c.label, got, c.inRange)
			}
		}
	}
}

// before returns a version just below v (test data): the previous patch
// release, a late patch of the previous minor (x.(y-1).99) for x.y.0, or
// for a pre-release "rc.N" the previous rc.
func before(v releases.Version) (releases.Version, bool) {
	switch {
	case v.Suffix == "" && v.Patch > 0:
		v.Patch--
		return v, true
	case v.Suffix == "" && v.Minor > 0:
		v.Minor, v.Patch = v.Minor-1, 99
		return v, true
	case strings.HasPrefix(v.Suffix, "rc."):
		var n int
		if _, err := fmt.Sscanf(v.Suffix, "rc.%d", &n); err == nil && n > 0 {
			v.Suffix = fmt.Sprintf("rc.%d", n-1)
			return v, true
		}
	}
	return v, false
}

func isConsensus(c ethereum.ClientName) bool {
	switch c {
	case ethereum.Lighthouse, ethereum.Prysm, ethereum.Teku, ethereum.Nimbus, ethereum.Lodestar, ethereum.Grandine:
		return true
	}
	return false
}

func hasFinding(a Assessment, substr string) bool {
	for _, f := range a.Findings {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}

// Node answers as recorded live on 2026-11-24
// (internal/ethereum/testdata/execution/live/eth-config-*.json and
// consensus/live/spec-*.json); variations are marked as synthetic.
const ff = ethereum.FarFutureEpoch

func elConfig(current int64, next ...int64) *ethereum.ExecutionForkConfig {
	cfg := &ethereum.ExecutionForkConfig{Current: ethereum.ForkActivation{ActivationTime: current}}
	if len(next) > 0 {
		cfg.Next = &ethereum.ForkActivation{ActivationTime: next[0]}
		cfg.Last = &ethereum.ForkActivation{ActivationTime: next[len(next)-1]}
	}
	return cfg
}

func clConfig(epochs map[string]uint64, blobs ...uint64) *ethereum.ConsensusForkConfig {
	return &ethereum.ConsensusForkConfig{Epochs: epochs, BlobSchedule: blobs}
}

func TestAssessForksNodeAnswer(t *testing.T) {
	s, err := forks.Default()
	if err != nil {
		t.Fatal(err)
	}
	const mainnet, hoodi = 1, 560048
	geth := cv("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3")
	lighthouse := cv("Lighthouse/v8.3.3-57c2abc/x86_64-linux")
	lodestar := cv("Lodestar/v1.51.0/7923280")
	// Live answers.
	mainnetEL := elConfig(1767747671) // BPO2
	hoodiEL := elConfig(1762955544)   // BPO2
	mainnetCL := clConfig(map[string]uint64{"ELECTRA": 364032, "FULU": 411392, "GLOAS": ff, "HEZE": ff}, 412672, 419072)
	nimbusCL := clConfig(map[string]uint64{"FULU": 411392, "GLOAS": ff}, 412672, 419072) // no HEZE key
	hoodiCL := clConfig(map[string]uint64{"ELECTRA": 2048, "FULU": 50688, "GLOAS": ff, "HEZE": ff}, 52480, 54016)
	hoodiEpochTime := func(ep uint64) int64 { return 1742213400 + int64(ep)*384 }

	tests := []struct {
		name      string
		chain     uint64
		el, cl    ethereum.ClientVersion
		elCfg     *ethereum.ExecutionForkConfig
		elErr     error
		clCfg     *ethereum.ConsensusForkConfig
		clErr     error
		wantLevel Level
		wantCell  string
		wantMsgs  []string
	}{
		{name: "mainnet live", chain: mainnet, el: geth, cl: lighthouse, elCfg: mainnetEL, clCfg: mainnetCL,
			wantLevel: OK, wantCell: "none scheduled"},
		{name: "nimbus does not know heze: fine while not scheduled", chain: mainnet, el: geth, cl: cv("Nimbus/v26.10.2-f64d64-stateofus"), elCfg: mainnetEL, clCfg: nimbusCL,
			wantLevel: OK, wantCell: "none scheduled"},
		{name: "hoodi live", chain: hoodi, el: geth, cl: lodestar, elCfg: hoodiEL, clCfg: hoodiCL,
			wantLevel: OK, wantCell: "none scheduled"},
		{
			// The node answer wins over the version range of 4b: a 1.17.9
			// build without the postponed schedule is fine.
			name: "node answer wins over the version rule", chain: hoodi,
			el: cv("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), cl: lodestar, elCfg: hoodiEL, clCfg: hoodiCL,
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			name: "fallback to the version rule, with a note", chain: hoodi,
			el: cv("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), cl: lodestar,
			elErr: errors.New("eth_config: JSON-RPC error -32601: The method eth_config does not exist/is not available"), clCfg: hoodiCL,
			wantLevel: Crit, wantCell: "EL has old Glamsterdam schedule",
			wantMsgs: []string{
				"fork check of the execution client by version only: eth_config: JSON-RPC error -32601",
				"Geth 1.17.9 still contains the postponed Glamsterdam schedule",
			},
		},
		{
			// Synthetic: the node activated the postponed fork.
			name: "EL activated the postponed fork", chain: hoodi, el: cv("Geth/v1.17.9-stable-aaaaaaaa/linux-amd64/go1.27.3"), cl: lodestar,
			elCfg: elConfig(1793036568), clCfg: hoodiCL,
			wantLevel: Crit, wantCell: "EL has old Glamsterdam schedule",
			wantMsgs: []string{"Geth 1.17.9 still schedules the postponed Glamsterdam at its former time (2026-10-26 17:42 UTC, eth_config current); update to 1.17.10 or later; the former activation time has passed"},
		},
		{
			// Synthetic: CL still has Gloas at the former epoch.
			name: "CL still schedules the postponed fork", chain: hoodi, el: geth, cl: lodestar, elCfg: hoodiEL,
			clCfg:     clConfig(map[string]uint64{"FULU": 50688, "GLOAS": 132352}, 52480, 54016),
			wantLevel: Crit, wantCell: "CL has old Glamsterdam schedule",
			wantMsgs: []string{"Lodestar 1.51.0 still schedules the postponed Glamsterdam at its former time (2026-10-26 17:42 UTC, epoch 132352); update to 1.50.1 or later; the former activation time has passed"},
		},
		{
			// Synthetic: Gloas at a new epoch that forks.yaml does not have
			// yet (e.g. a new date announced after the last update).
			name: "CL schedules the postponed fork at a new date", chain: hoodi, el: geth, cl: lodestar, elCfg: hoodiEL,
			clCfg:     clConfig(map[string]uint64{"FULU": 50688, "GLOAS": 150000}, 52480, 54016),
			wantLevel: Warn, wantCell: "CL schedules unknown fork",
			wantMsgs: []string{fmt.Sprintf("Lodestar 1.51.0 schedules Gloas at epoch 150000 at %s (in 49 days) that forks.yaml (updated 2026-11-24) does not list: forks.yaml may be outdated",
				time.Unix(hoodiEpochTime(150000), 0).UTC().Format("2006-01-02 15:04 UTC"))},
		},
		{
			// Synthetic: EL one fork behind.
			name: "EL still on BPO1", chain: mainnet, el: geth, cl: lighthouse, elCfg: elConfig(1765290071), clCfg: mainnetCL,
			wantLevel: Crit, wantCell: "EL not on BPO2",
			wantMsgs: []string{"Geth 1.17.10 is still on BPO1 (eth_config), but BPO2 is active since 2026-01-07: it cannot follow mainnet"},
		},
		{
			// Synthetic: Prague activation time (before forks.yaml starts).
			name: "EL on a fork before forks.yaml", chain: mainnet, el: geth, cl: lighthouse, elCfg: elConfig(1746612311), clCfg: mainnetCL,
			wantLevel: Crit, wantCell: "EL not on BPO2",
			wantMsgs: []string{"is still on a fork activated at 2025-05-07 10:05 UTC (eth_config), but BPO2 is active"},
		},
		{
			// Synthetic: CL without the BPO2 entry.
			name: "CL misses BPO2", chain: mainnet, el: geth, cl: lighthouse, elCfg: mainnetEL,
			clCfg:     clConfig(map[string]uint64{"FULU": 411392, "GLOAS": ff}, 412672),
			wantLevel: Crit, wantCell: "CL not on BPO2",
			wantMsgs: []string{"Lighthouse 8.3.3 is not on BPO2 (BLOB_SCHEDULE has no entry for epoch 419072), which is active since 2026-01-07: it cannot follow mainnet"},
		},
		{
			// Synthetic: a consensus client without Fulu.
			name: "CL before Fusaka", chain: mainnet, el: geth, cl: lighthouse, elCfg: mainnetEL,
			clCfg:     clConfig(map[string]uint64{"ELECTRA": 364032, "FULU": ff}),
			wantLevel: Crit, wantCell: "CL not on Fusaka",
			wantMsgs: []string{"is not on Fusaka (Fulu is not scheduled (FULU_FORK_EPOCH is far future))"},
		},
		{
			// Synthetic: entries for hard forks in BLOB_SCHEDULE (as in the
			// beacon-APIs example) are not forks of their own.
			name: "BLOB_SCHEDULE entries at hard forks", chain: mainnet, el: geth, cl: lighthouse, elCfg: mainnetEL,
			clCfg:     clConfig(map[string]uint64{"DENEB": 269568, "ELECTRA": 364032, "FULU": 411392, "GLOAS": ff}, 269568, 364032, 411392, 412672, 419072),
			wantLevel: OK, wantCell: "none scheduled",
		},
		{
			// Synthetic: unknown forks on both layers, far ahead and close.
			name: "unknown forks", chain: mainnet, el: geth, cl: lighthouse,
			elCfg:     elConfig(1767747671, forkNow.Add(40*24*time.Hour).Unix()),
			clCfg:     clConfig(map[string]uint64{"FULU": 411392, "GLOAS": ff, "HEZE": 500000}, 412672, 419072, 450000),
			wantLevel: Crit, wantCell: "EL schedules unknown fork",
			wantMsgs: []string{
				"Geth 1.17.10 schedules a fork at 2027-01-03 12:00 UTC (in 40 days) that forks.yaml (updated 2026-11-24) does not list",
				"Lighthouse 8.3.3 schedules Heze at epoch 500000",
				"Lighthouse 8.3.3 schedules a blob parameter change at epoch 450000 at 2026-05-24 12:00 UTC (already passed)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessForks(ForkInput{
				Schedule: s, ChainID: tt.chain, HaveChain: true,
				Execution: tt.el, Consensus: tt.cl,
				ExecutionConfig: tt.elCfg, ExecutionConfigErr: tt.elErr,
				ConsensusConfig: tt.clCfg, ConsensusConfigErr: tt.clErr,
				Now: forkNow, CriticalWithin: 14 * 24 * time.Hour,
			})
			if r.Cell != tt.wantCell {
				t.Errorf("cell = %q, want %q", r.Cell, tt.wantCell)
			}
			checkAssessment(t, r.Assessment, tt.wantLevel, tt.wantMsgs)
		})
	}
}

func TestAssessForksNodeNextFork(t *testing.T) {
	pastEp, pastTs := epochAt(forkNow.Add(-30 * 24 * time.Hour))
	ep, ts := epochAt(forkNow.Add(5*24*time.Hour + time.Hour))
	farEp, farTs := epochAt(forkNow.Add(20*24*time.Hour + time.Hour))
	hard := func(ep uint64, ts int64) *forks.Schedule {
		return synthetic(t, fmt.Sprintf(`      - {name: Next9, execution: Amsterdam, consensus: Gloas, epoch: %d, timestamp: %d, min_versions: {geth: 1.1.0, lighthouse: 1.1.0}, sources: [https://example.org/synthetic]}
`, ep, ts))
	}
	soon, far := hard(ep, ts), hard(farEp, farTs)
	bpo := synthetic(t, fmt.Sprintf(`      - {name: BPO9, type: bpo, epoch: %d, timestamp: %d, sources: [https://example.org/synthetic]}
`, ep, ts))

	geth, reth := cv("Geth/v1.0.0-stable/x"), cv("reth/v1.0.0-abc1234/x")
	lighthouse := cv("Lighthouse/v1.0.0-abc1234/x")
	elReady := elConfig(pastTs, ts)
	clReady := clConfig(map[string]uint64{"FULU": pastEp, "GLOAS": ep})
	clOld := clConfig(map[string]uint64{"FULU": pastEp, "GLOAS": ff})

	tests := []struct {
		name      string
		s         *forks.Schedule
		el        ethereum.ClientVersion
		elCfg     *ethereum.ExecutionForkConfig
		clCfg     *ethereum.ConsensusForkConfig
		wantLevel Level
		wantCell  string
		wantMsgs  []string
	}{
		{name: "both ready", s: soon, el: geth, elCfg: elReady, clCfg: clReady,
			wantLevel: OK, wantCell: "Next9 in 5 days: ready"},
		{name: "EL without next fork, release exists, close", s: soon, el: geth, elCfg: elConfig(pastTs), clCfg: clReady,
			wantLevel: Crit, wantCell: "Next9 in 5 days: EL not ready",
			wantMsgs: []string{"Geth 1.0.0 is not ready for Next9 on mainnet (" + time.Unix(ts, 0).UTC().Format(time.DateOnly) + ", in 5 days): eth_config shows no next fork; needs 1.1.0 or later"}},
		{name: "EL next at another time", s: soon, el: geth, elCfg: elConfig(pastTs, ts+384), clCfg: clReady,
			wantLevel: Crit, wantCell: "EL schedules unknown fork",
			wantMsgs: []string{"Geth 1.0.0 schedules a fork at", "eth_config shows the next fork at"}},
		{name: "far away: yellow", s: far, el: geth, elCfg: elConfig(pastTs), clCfg: clConfig(map[string]uint64{"FULU": pastEp, "GLOAS": farEp}),
			wantLevel: Warn, wantCell: "Next9 in 20 days: EL not ready",
			wantMsgs: []string{"eth_config shows no next fork; needs 1.1.0 or later"}},
		{name: "no ready release known: yellow", s: soon, el: reth, elCfg: elConfig(pastTs), clCfg: clReady,
			wantLevel: Warn, wantCell: "Next9 in 5 days: EL not ready",
			wantMsgs: []string{"no Reth release announced yet (waiting for client release)"}},
		{name: "client not recognized: yellow", s: soon, el: cv("MEVblocker"), elCfg: elConfig(pastTs), clCfg: clReady,
			wantLevel: Warn, wantCell: "Next9 in 5 days: EL not ready",
			wantMsgs: []string{"the execution client is not ready for Next9 on mainnet (2026-11-29, in 5 days): eth_config shows no next fork; client not recognized, so it is unknown whether a ready release exists"}},
		{name: "node answer works without a recognized version", s: soon, el: cv("MEVblocker"), elCfg: elReady, clCfg: clReady,
			wantLevel: OK, wantCell: "Next9 in 5 days: ready"},
		{name: "CL fork far future", s: soon, el: geth, elCfg: elReady, clCfg: clOld,
			wantLevel: Crit, wantCell: "Next9 in 5 days: CL not ready",
			wantMsgs: []string{"Lighthouse 1.0.0 is not ready for Next9 on mainnet (2026-11-29, in 5 days): Gloas is not scheduled (GLOAS_FORK_EPOCH is far future); needs 1.1.0 or later"}},
		{name: "CL does not know the fork", s: soon, el: geth, elCfg: elReady, clCfg: clConfig(map[string]uint64{"FULU": pastEp}),
			wantLevel: Crit, wantCell: "Next9 in 5 days: CL not ready",
			wantMsgs: []string{"the client does not know Gloas (no GLOAS_FORK_EPOCH)"}},
		{name: "CL fork at another epoch", s: soon, el: geth, elCfg: elReady, clCfg: clConfig(map[string]uint64{"FULU": pastEp, "GLOAS": ep + 1}),
			wantLevel: Crit, wantCell: "Next9 in 5 days: CL not ready",
			wantMsgs: []string{fmt.Sprintf("GLOAS_FORK_EPOCH is %d, not %d", ep+1, ep)}},
		{name: "BPO ready", s: bpo, el: geth, elCfg: elReady, clCfg: clConfig(map[string]uint64{"FULU": pastEp}, ep),
			wantLevel: OK, wantCell: "BPO9 in 5 days: ready"},
		{name: "BPO missing in blob schedule", s: bpo, el: geth, elCfg: elReady, clCfg: clConfig(map[string]uint64{"FULU": pastEp}),
			wantLevel: Crit, wantCell: "BPO9 in 5 days: CL not ready",
			// The BPO uses the releases of Past (1.0.0): the running release
			// supports it, so the node's configuration is the problem.
			wantMsgs: []string{fmt.Sprintf("BLOB_SCHEDULE has no entry for epoch %d; the release supports it (minimum 1.0.0), so check the node's chain configuration", ep)}},
		{name: "release supports the fork, node does not schedule it", s: soon, el: cv("Geth/v1.1.0-stable/x"), elCfg: elConfig(pastTs), clCfg: clReady,
			wantLevel: Crit, wantCell: "Next9 in 5 days: EL not ready",
			wantMsgs: []string{"Geth 1.1.0 is not ready for Next9 on mainnet (2026-11-29, in 5 days): eth_config shows no next fork; the release supports it (minimum 1.1.0), so check the node's chain configuration"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessForks(ForkInput{
				Schedule: tt.s, ChainID: 1, HaveChain: true,
				Execution: tt.el, Consensus: lighthouse,
				ExecutionConfig: tt.elCfg, ConsensusConfig: tt.clCfg,
				Now: forkNow, CriticalWithin: 14 * 24 * time.Hour,
			})
			if r.Cell != tt.wantCell {
				t.Errorf("cell = %q, want %q", r.Cell, tt.wantCell)
			}
			checkAssessment(t, r.Assessment, tt.wantLevel, tt.wantMsgs)
		})
	}
}
