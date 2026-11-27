package status

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/software"
)

// recorded loads a recorded GitHub list from the releases package (see
// internal/releases/testdata/SOURCES.md); sub is "" for releases or
// "advisories".
func recorded[T any](t *testing.T, sub, file string) []T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "releases", "testdata", "github", sub, file))
	if err != nil {
		t.Fatal(err)
	}
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAssessAdvisoriesRecorded(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	data := func(t *testing.T, repo string) (*releases.AdvisoryResult, *releases.Result) {
		advs := recorded[releases.Advisory](t, "advisories", repo+".json")
		rels := recorded[releases.Release](t, "", repo+".json")
		return &releases.AdvisoryResult{Advisories: advs, FetchedAt: now},
			&releases.Result{Releases: rels, FetchedAt: now}
	}

	tests := []struct {
		name  string
		raw   string
		repo  string
		level Level
		cell  string
		// want are substrings of the findings, in this order.
		want []string
		// notes is the number of findings with level OK.
		notes int
	}{
		{
			name: "geth 1.17.10 up to date", raw: "Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3",
			repo: "ethereum_go-ethereum", level: OK, cell: "none known",
		},
		{
			// <= 1.16.7 (2x: high, medium), <= 1.16.8 (high, medium),
			// < 1.17.0 (medium): fixed in 1.16.8, 1.16.9 and 1.17.0.
			name: "geth 1.16.7", raw: "Geth/v1.16.7-stable-aaaaaaaa/linux-amd64/go1.25.3",
			repo: "ethereum_go-ethereum", level: Crit, cell: "5 (high)",
			want: []string{
				"Geth 1.16.7 is affected by 5 published advisories (2 high, 3 medium), fixed in 1.17.0 or later: ",
				`GHSA-2gjw-fg97-vg3r (high, CVE-2026-26314: "DoS via malicious p2p message"), `,
				`GHSA-mr7q-c9w9-wh4h (high, CVE-2026-22862: "DoS via malicious p2p message"), `,
				`GHSA-m6j8-rg6r-7mv8 (medium, CVE-2026-26315: "Improper ECIES Public Key Validation in RLPx Handshake") and 2 more; `,
				"https://github.com/ethereum/go-ethereum/security/advisories",
			},
		},
		{
			// Four advisories "<= 26.10.2" (1 medium, 3 low), fixed in
			// 26.11.0; the open range ">= 24.5.2" is bounded by its fix
			// 26.8.1. Plus the "all" advisory without a fix as a note.
			name: "besu 26.10.2", raw: "besu/v26.10.2/linux-x86_64/openjdk-java-21",
			repo: "besu-eth_besu", level: Warn, cell: "4 (medium)", notes: 1,
			want: []string{
				"Besu 26.10.2 is affected by 4 published advisories (1 medium, 3 low), fixed in 26.11.0 or later: ",
				`GHSA-mp5j-q856-vvx7 (medium: "Malformed peer receipts poison`,
				"and 1 more; https://github.com/besu-eth/besu/security/advisories",
				`cannot tell whether Besu 26.10.2 is affected by GHSA-v5gq-r843-8f73 (medium: "SEC-50 RLPx AES CTR keystream reusage #1315. (Case for private`,
				`(affected versions "all", patched "")`,
			},
		},
		{
			name: "besu 26.11.2 up to date", raw: "besu/v26.11.2/linux-x86_64/openjdk-java-21",
			repo: "besu-eth_besu", level: OK, cell: "none known", notes: 1,
		},
		{
			// < 8.3.0: 1 high, 2 medium, 1 low.
			name: "lighthouse 8.2.1", raw: "Lighthouse/v8.2.1-57c2abc/x86_64-linux",
			repo: "sigp_lighthouse", level: Crit, cell: "4 (high)",
			want: []string{
				"Lighthouse 8.2.1 is affected by 4 published advisories (1 high, 2 medium, 1 low), fixed in 8.3.0 or later: ",
				`GHSA-rvxr-393q-q925 (high: "Memory exhaustion when serving data column requests")`,
			},
		},
		{
			// Only "low" advisories: yellow, even without a grace period.
			name: "erigon 3.3.0", raw: "erigon/3.3.0-8a3f3b2c/linux-amd64/go1.27.4",
			repo: "erigontech_erigon", level: Warn, cell: "1 (low)",
			want: []string{"Erigon 3.3.0 is affected by 1 published advisory (1 low), fixed in 3.4.0 or later: GHSA-4c6q-hpx8-75wm"},
		},
		{
			name: "prysm has no advisories", raw: "Prysm/v7.3.2-844d8b8 (linux amd64)",
			repo: "OffchainLabs_prysm", level: OK, cell: "none known",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advs, rels := data(t, tt.repo)
			r := AssessAdvisories(AdvisoryInput{Version: ethereum.ParseClientVersion(tt.raw), Advisories: advs, Releases: rels, Now: now})
			total := 0
			for _, n := range r.Counts {
				total += n
			}
			if len(r.Counts) != 5 || (total == 0) != (tt.cell == "none known") {
				t.Errorf("counts %v for cell %q", r.Counts, tt.cell)
			}
			if r.Level != tt.level || r.Cell != tt.cell {
				t.Errorf("level %v, cell %q; want %v, %q", r.Level, r.Cell, tt.level, tt.cell)
			}
			all, notes := "", 0
			for _, f := range r.Findings {
				all += f.Message + "\n"
				if f.Level == OK {
					notes++
				}
			}
			if notes != tt.notes {
				t.Errorf("%d notes, want %d", notes, tt.notes)
			}
			rest := all
			for _, w := range tt.want {
				i := strings.Index(rest, w)
				if i < 0 {
					t.Fatalf("findings lack (in order) %q\nfindings:\n%s", w, all)
				}
				rest = rest[i+len(w):]
			}
		})
	}
}

func TestAssessAdvisoriesFixNotReleased(t *testing.T) {
	// The release data is from before Lighthouse 8.3.0: the advisories
	// name a fix that is not out yet, so even "high" is only yellow.
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	advs := recorded[releases.Advisory](t, "advisories", "sigp_lighthouse.json")
	var before []releases.Release
	for _, r := range recorded[releases.Release](t, "", "sigp_lighthouse.json") {
		if v, ok := releases.ParseVersion(r.Tag); ok && releases.Compare(software.Lighthouse, v, releases.Version{Major: 8, Minor: 3}) < 0 {
			before = append(before, r)
		}
	}
	r := AssessAdvisories(AdvisoryInput{
		Version:    ethereum.ParseClientVersion("Lighthouse/v8.2.1-57c2abc/x86_64-linux"),
		Advisories: &releases.AdvisoryResult{Advisories: advs, FetchedAt: now},
		Releases:   &releases.Result{Releases: before, FetchedAt: now},
		Now:        now,
	})
	// All four still count in the cell; only the high one is yellow, the
	// medium and low ones without a fix are a note.
	if r.Level != Warn || r.Cell != "4 (high)" || len(r.Findings) != 2 {
		t.Fatalf("level %v, cell %q, %d findings; want WARN, \"4 (high)\", 2", r.Level, r.Cell, len(r.Findings))
	}
	want := []struct {
		level  Level
		prefix string
	}{
		{Warn, "Lighthouse 8.2.1 is affected by 1 published advisory without a released fix (1 high): no fix released yet, check the advisory for mitigations: "},
		{OK, "Lighthouse 8.2.1 is affected by 3 published advisories without a released fix (2 medium, 1 low): no fix released yet, check the advisory for mitigations: "},
	}
	for i, w := range want {
		if f := r.Findings[i]; f.Level != w.level || !strings.HasPrefix(f.Message, w.prefix) {
			t.Errorf("finding %d = %v %q\nwant %v, prefix %q", i, f.Level, f.Message, w.level, w.prefix)
		}
	}
}

func TestAssessAdvisoriesRules(t *testing.T) {
	// Go values, not API responses: combinations the recorded data does
	// not contain.
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	geth := ethereum.ParseClientVersion("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3")
	adv := func(id, severity, rng, patched string) releases.Advisory {
		return releases.Advisory{GHSAID: id, Severity: severity, Summary: "title of " + id, PublishedAt: now,
			Vulnerabilities: []releases.AdvisoryVulnerability{{VulnerableVersionRange: rng, PatchedVersions: patched}}}
	}
	result := func(advs ...releases.Advisory) *releases.AdvisoryResult {
		return &releases.AdvisoryResult{Advisories: advs, FetchedAt: now}
	}

	tests := []struct {
		name  string
		in    AdvisoryInput
		level Level
		cell  string
		want  []string
	}{
		{
			name:  "critical without fix is yellow",
			in:    AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-a", "critical", ">= 1.0.0", ""))},
			level: Warn, cell: "1 (critical)",
			want: []string{"1 published advisory without a released fix (1 critical): no fix released yet, check the advisory for mitigations: GHSA-a (critical: \"title of GHSA-a\")"},
		},
		{
			name:  "low without fix is a note",
			in:    AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-g", "low", ">= 1.0.0", ""))},
			level: OK, cell: "1 (low)",
			want: []string{"1 published advisory without a released fix (1 low): no fix released yet, check the advisory for mitigations: GHSA-g (low: \"title of GHSA-g\")"},
		},
		{
			name:  "missing severity without fix stays yellow",
			in:    AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-h", "", ">= 1.0.0", ""))},
			level: Warn, cell: "1",
			want: []string{"1 published advisory without a released fix (1 no severity): no fix released yet"},
		},
		{
			name:  "missing severity is yellow",
			in:    AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-b", "", "< 1.18.0", "1.18.0"))},
			level: Warn, cell: "1",
			want: []string{"1 published advisory (1 no severity), fixed in 1.18.0 or later: GHSA-b (no severity: "},
		},
		{
			name: "fixed and unfixed: two findings",
			in: AdvisoryInput{Version: geth, Advisories: result(
				adv("GHSA-c", "high", "< 1.18.0", "1.18.0"), adv("GHSA-d", "low", ">= 1.0.0", ""))},
			level: Crit, cell: "2 (high)",
			want: []string{"1 published advisory (1 high), fixed in 1.18.0 or later: GHSA-c", "without a released fix (1 low)"},
		},
		{
			name:  "without release data the named fix counts",
			in:    AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-e", "high", "< 1.18.0", "1.18.0"))},
			level: Crit, cell: "1 (high)",
		},
		{
			name: "fix not in the release data",
			in: AdvisoryInput{Version: geth, Advisories: result(adv("GHSA-f", "high", "< 1.18.0", "1.18.0")),
				Releases: &releases.Result{Releases: []releases.Release{{Tag: "v1.17.10"}}}},
			level: Warn, cell: "1 (high)",
			want: []string{"no fix released yet"},
		},
		{
			name:  "advisories unknown",
			in:    AdvisoryInput{Version: geth, AdvisoriesErr: errors.New("GitHub security advisories of ethereum/go-ethereum: timeout")},
			level: Warn, cell: "?",
			want: []string{"advisories unknown: GitHub security advisories of ethereum/go-ethereum: timeout"},
		},
		{
			name: "GitHub down, data 3 days old",
			in: AdvisoryInput{Version: geth, Advisories: &releases.AdvisoryResult{
				FetchedAt: now.Add(-3 * 24 * time.Hour), RefreshErr: errors.New("HTTP 503")}},
			level: OK, cell: "none known",
			want: []string{"GitHub not reachable, using advisory data from 2026-11-22: HTTP 503"},
		},
		{
			name: "GitHub down, data 8 days old",
			in: AdvisoryInput{Version: geth, Advisories: &releases.AdvisoryResult{
				FetchedAt: now.Add(-8 * 24 * time.Hour), RefreshErr: errors.New("HTTP 503")}},
			level: Warn, cell: "none known",
			want: []string{"advisory data is 8 days old, GitHub not reachable: HTTP 503"},
		},
		{
			name:  "version request failed: not assessed",
			in:    AdvisoryInput{Version: geth, Err: errors.New("boom"), Advisories: result(adv("GHSA-g", "high", "< 2.0.0", "2.0.0"))},
			level: OK, cell: "",
		},
		{
			name:  "unknown client: not assessed",
			in:    AdvisoryInput{Version: ethereum.ParseClientVersion("MEVblocker"), Advisories: result(adv("GHSA-h", "high", "< 2.0.0", "2.0.0"))},
			level: OK, cell: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.Now = now
			r := AssessAdvisories(tt.in)
			if r.Level != tt.level || r.Cell != tt.cell {
				t.Errorf("level %v, cell %q; want %v, %q (findings %+v)", r.Level, r.Cell, tt.level, tt.cell, r.Findings)
			}
			for _, w := range tt.want {
				if !hasFinding(r.Assessment, w) {
					t.Errorf("no finding contains %q; findings %+v", w, r.Findings)
				}
			}
		})
	}
}

func TestAssessAdvisoriesCleansText(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	summary := "evil\x1b[2J\x1b[31m title\nwith‮newline " + strings.Repeat("x", 100)
	r := AssessAdvisories(AdvisoryInput{
		Version: ethereum.ParseClientVersion("Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3"),
		Advisories: &releases.AdvisoryResult{FetchedAt: now, Advisories: []releases.Advisory{{
			GHSAID: "GHSA-\x1bx", Severity: "high", Summary: summary,
			Vulnerabilities: []releases.AdvisoryVulnerability{{VulnerableVersionRange: "< 2.0.0", PatchedVersions: "2.0.0"}},
		}}},
		Now: now,
	})
	if len(r.Findings) != 1 {
		t.Fatalf("findings %+v", r.Findings)
	}
	msg := r.Findings[0].Message
	for _, bad := range []string{"\x1b", "\n", "‮", `\x1b`, `\n`, `‮`} {
		if strings.Contains(msg, bad) {
			t.Errorf("finding contains %q: %q", bad, msg)
		}
	}
	if !strings.Contains(msg, `"evil [2J [31m title with newline xxx`) || !strings.Contains(msg, `x…")`) {
		t.Errorf("finding = %q", msg)
	}
}

// TestAssessAdvisoriesIgnore uses the recorded Besu advisories. Besu
// 26.10.2 is affected by GHSA-mp5j-q856-vvx7 (medium) and three low ones
// (GHSA-2vfg-f2fr-2jrv, GHSA-pvgm-f647-f7vc, GHSA-4c5c-6mr5-vxrh);
// GHSA-v5gq-r843-8f73 ("all", no fix) is unclear.
func TestAssessAdvisoriesIgnore(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	advs := &releases.AdvisoryResult{Advisories: recorded[releases.Advisory](t, "advisories", "besu-eth_besu.json"), FetchedAt: now}
	rels := &releases.Result{Releases: recorded[releases.Release](t, "", "besu-eth_besu.json"), FetchedAt: now}
	day := func(s string) config.Date {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return config.Date{Time: d}
	}
	entry := func(id, reason, expires string) config.AdvisoryIgnore {
		e := config.AdvisoryIgnore{ID: id, Reason: reason}
		if expires != "" {
			e.Expires = day(expires)
		}
		return e
	}

	tests := []struct {
		name   string
		ignore []config.AdvisoryIgnore
		level  Level
		cell   string
		// want are the findings, each as a substring, in this order.
		want []string
	}{
		{
			name: "medium and the unclear one ignored",
			ignore: []config.AdvisoryIgnore{
				entry("GHSA-v5gq-r843-8f73", "we do not run private transactions", "2027-03-01"),
				entry("GHSA-mp5j-q856-vvx7", "no snap sync", ""),
			},
			level: Warn, cell: "3 (low)",
			want: []string{
				"Besu 26.10.2 is affected by 3 published advisories (3 low), fixed in 26.11.0 or later",
				`2 ignored advisories for Besu 26.10.2 (ignore_advisories): GHSA-mp5j-q856-vvx7 (medium: "Malformed peer receipts poison Besu's synced receipt store and break …"), ` +
					`reason "no snap sync"; GHSA-v5gq-r843-8f73 (medium: "SEC-50 RLPx AES CTR keystream reusage #1315. (Case for private networ…"), ` +
					`reason "we do not run private transactions", until 2027-03-01`,
			},
		},
		{
			name: "all ignored",
			ignore: []config.AdvisoryIgnore{
				entry("GHSA-v5gq-r843-8f73", "a", ""), entry("GHSA-mp5j-q856-vvx7", "b", ""),
				entry("GHSA-2vfg-f2fr-2jrv", "c", ""), entry("GHSA-pvgm-f647-f7vc", "d", ""),
				entry("GHSA-4c5c-6mr5-vxrh", "e", ""),
			},
			level: OK, cell: "none known",
			want: []string{"5 ignored advisories for Besu 26.10.2"},
		},
		{
			// The entry expires at 00:00 UTC on its date; now is noon.
			name:   "expired entry counts again",
			ignore: []config.AdvisoryIgnore{entry("GHSA-mp5j-q856-vvx7", "no snap sync", "2026-11-25")},
			level:  Warn, cell: "4 (medium)",
			want: []string{
				"Besu 26.10.2 is affected by 4 published advisories (1 medium, 3 low)",
				`the ignore_advisories entry for GHSA-mp5j-q856-vvx7 expired on 2026-11-25, the advisory counts again (reason was "no snap sync")`,
				"cannot tell whether Besu 26.10.2 is affected by GHSA-v5gq-r843-8f73",
			},
		},
		{
			name:   "expires tomorrow: still ignored",
			ignore: []config.AdvisoryIgnore{entry("GHSA-mp5j-q856-vvx7", "no snap sync", "2026-11-26")},
			level:  Warn, cell: "3 (low)",
			want: []string{
				"(3 low)",
				"1 ignored advisory for Besu 26.10.2 (ignore_advisories): GHSA-mp5j-q856-vvx7",
				"cannot tell whether",
			},
		},
		{
			// Fixed in 26.8.1, so 26.10.2 is not affected: nothing to say.
			name:   "ignored but not affecting",
			ignore: []config.AdvisoryIgnore{entry("GHSA-m886-r6ch-pjvr", "no QBFT", "")},
			level:  Warn, cell: "4 (medium)",
			want: []string{"(1 medium, 3 low)", "cannot tell whether"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := AssessAdvisories(AdvisoryInput{
				Version:    ethereum.ParseClientVersion("besu/v26.10.2/linux-x86_64/openjdk-java-21"),
				Advisories: advs, Releases: rels, Ignore: tt.ignore, Now: now,
			})
			if r.Level != tt.level || r.Cell != tt.cell {
				t.Errorf("level %v, cell %q; want %v, %q", r.Level, r.Cell, tt.level, tt.cell)
			}
			if len(r.Findings) != len(tt.want) {
				t.Fatalf("%d findings, want %d: %+v", len(r.Findings), len(tt.want), r.Findings)
			}
			for i, f := range r.Findings {
				if !strings.Contains(f.Message, tt.want[i]) {
					t.Errorf("finding %d = %q, want it to contain %q", i, f.Message, tt.want[i])
				}
			}
		})
	}
}

func TestAssessIgnoreList(t *testing.T) {
	besu := recorded[releases.Advisory](t, "advisories", "besu-eth_besu.json")
	geth := recorded[releases.Advisory](t, "advisories", "ethereum_go-ethereum.json")
	entries := []config.AdvisoryIgnore{
		{ID: "GHSA-v5gq-r843-8f73", Reason: "known Besu advisory"},
		{ID: "GHSA-2gjw-fg97-vg3r", Reason: "known Geth advisory"},
		{ID: "GHSA-v5gq-r843-8f37", Reason: "typo\x1b[31m"},
	}
	r := AssessIgnoreList(entries, [][]releases.Advisory{besu, geth}, []string{"Besu", "Geth"}, false)
	if r.Level != OK || len(r.Findings) != 1 {
		t.Fatalf("got %+v, want one note", r)
	}
	want := `GHSA-v5gq-r843-8f37 is not a published advisory of Besu or Geth; check the ID for a typo (reason "typo [31m")`
	if r.Findings[0].Message != want {
		t.Errorf("got  %q\nwant %q", r.Findings[0].Message, want)
	}

	// A global entry may be correct, for a client no node runs.
	r = AssessIgnoreList(entries, [][]releases.Advisory{besu, geth}, []string{"Besu", "Geth"}, true)
	want = `GHSA-v5gq-r843-8f37 not found in the advisories of the clients in use (Besu, Geth): a typo, or for a client none of your nodes runs? (reason "typo [31m")`
	if len(r.Findings) != 1 || r.Findings[0].Message != want {
		t.Errorf("global: got %+v\nwant %q", r.Findings, want)
	}

	// Without the Geth list, the Geth advisory looks unknown; that is why
	// callers only pass complete data.
	r = AssessIgnoreList(entries, [][]releases.Advisory{besu}, []string{"Besu"}, false)
	if len(r.Findings) != 2 {
		t.Errorf("got %+v, want two notes", r.Findings)
	}
}

func TestOrList(t *testing.T) {
	for _, tt := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"Besu"}, "Besu"},
		{[]string{"Besu", "Geth"}, "Besu or Geth"},
		{[]string{"Besu", "Geth", "Nimbus"}, "Besu, Geth or Nimbus"},
	} {
		if got := orList(tt.in); got != tt.want {
			t.Errorf("orList(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAssessAdvisoriesCounts(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	advs := &releases.AdvisoryResult{Advisories: recorded[releases.Advisory](t, "advisories", "ethereum_go-ethereum.json"), FetchedAt: now}
	v := ethereum.ParseClientVersion("Geth/v1.16.7-stable-aaaaaaaa/linux-amd64/go1.25.3")

	// Five advisories affect 1.16.7 (see TestAssessAdvisoriesRecorded);
	// one of them ignored.
	r := AssessAdvisories(AdvisoryInput{Version: v, Advisories: advs, Now: now,
		Ignore: []config.AdvisoryIgnore{{ID: "GHSA-2gjw-fg97-vg3r", Reason: "test"}}})
	want := map[string]int{"critical": 0, "high": 1, "medium": 3, "low": 0, "none": 0}
	if !maps.Equal(r.Counts, want) {
		t.Errorf("counts = %v, want %v", r.Counts, want)
	}

	r = AssessAdvisories(AdvisoryInput{Version: v, AdvisoriesErr: errors.New("down"), Now: now})
	if r.Counts != nil {
		t.Errorf("counts without data = %v, want nil", r.Counts)
	}
}

// GHSA-73xc-j27q-wg75 (medium) affects every Aztec 6.x release and names no
// fix for that line (recorded data, see internal/releases/testdata). A
// medium advisory without a fix is a note: the operator cannot update, so
// the node must not stay yellow. It still counts in the cell.
func TestAssessAdvisoriesMediumWithoutFixIsNote(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	advs := recorded[releases.Advisory](t, "advisories", "AztecProtocol_aztec-packages.json")
	for _, running := range []string{"6.0.0", "6.1.0"} {
		t.Run(running, func(t *testing.T) {
			r := AssessAdvisories(AdvisoryInput{
				Version:    software.Reported{Raw: running, Name: software.Aztec, Version: running},
				Advisories: &releases.AdvisoryResult{Advisories: advs, FetchedAt: now},
				Now:        now,
			})
			if r.Level != OK || r.Cell != "1 (medium)" || r.Counts["medium"] != 1 {
				t.Errorf("level %v, cell %q, counts %v; want OK, \"1 (medium)\", 1 medium", r.Level, r.Cell, r.Counts)
			}
			want := []string{
				"Aztec " + running + " is affected by 1 published advisory without a released fix (1 medium): no fix released yet, check the advisory for mitigations: GHSA-73xc-j27q-wg75 (medium: ",
				// The free-text range of GHSA-778c-2h5c-8r96.
				"cannot tell whether Aztec " + running + " is affected by GHSA-778c-2h5c-8r96",
			}
			if len(r.Findings) != len(want) {
				t.Fatalf("findings %v, want %d", r.Findings, len(want))
			}
			for i, w := range want {
				if f := r.Findings[i]; f.Level != OK || !strings.HasPrefix(f.Message, w) {
					t.Errorf("finding %d = %v %q\nwant NOTE, prefix %q", i, f.Level, f.Message, w)
				}
			}
		})
	}
}
