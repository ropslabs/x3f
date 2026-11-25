package status

import (
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
	future := synthetic(t, fmt.Sprintf(`      - {name: Later, status: postponed, epoch: %d, timestamp: %d, scheduled_in: {geth: 2.0.0}, removed_in: {geth: 2.0.1}, sources: [https://example.org/synthetic]}
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
