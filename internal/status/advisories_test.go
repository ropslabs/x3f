package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/releases"
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
				`cannot tell whether Besu 26.10.2 is affected by GHSA-v5gq-r843-8f73 (medium: "SEC-50 RLPx AES CTR keystream reusage #1315. (Case for private`,
				`(affected versions "all", patched "")`,
				"Besu 26.10.2 is affected by 4 published advisories (1 medium, 3 low), fixed in 26.11.0 or later: ",
				`GHSA-mp5j-q856-vvx7 (medium: "Malformed peer receipts poison`,
				"and 1 more; https://github.com/besu-eth/besu/security/advisories",
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
		if v, ok := releases.ParseVersion(r.Tag); ok && releases.Compare(ethereum.Lighthouse, v, releases.Version{Major: 8, Minor: 3}) < 0 {
			before = append(before, r)
		}
	}
	r := AssessAdvisories(AdvisoryInput{
		Version:    ethereum.ParseClientVersion("Lighthouse/v8.2.1-57c2abc/x86_64-linux"),
		Advisories: &releases.AdvisoryResult{Advisories: advs, FetchedAt: now},
		Releases:   &releases.Result{Releases: before, FetchedAt: now},
		Now:        now,
	})
	if r.Level != Warn || r.Cell != "4 (high)" || len(r.Findings) != 1 {
		t.Fatalf("level %v, cell %q, %d findings; want WARN, \"4 (high)\", 1", r.Level, r.Cell, len(r.Findings))
	}
	want := "Lighthouse 8.2.1 is affected by 4 published advisories without a released fix (1 high, 2 medium, 1 low): no fix released yet, check the advisory for mitigations: "
	if !strings.HasPrefix(r.Findings[0].Message, want) {
		t.Errorf("finding = %q\nwant prefix %q", r.Findings[0].Message, want)
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
