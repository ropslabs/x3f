package releases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ropslabs/x3f/internal/software"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		// Spellings from the recorded advisories (testdata/SOURCES.md).
		{"< 1.13.15", true},
		{"<0.35.0", true},
		{"<= 26.10.2", true},
		{"< v7.0.0-beta.5", true},
		{">= 1.14.0, < 1.14.13", true},
		{">= v1.9.4, < 1.9.20", true},
		{">=v3.1.0 and  <v3.4.0", true},
		{"21.10.0,21.10.1", true},
		{"22.4.0-RC2,22.4.0,22.4.1,22.4.2,22.4.3,22.4.4,22.7.0,22.7.0-RC1,22.7.0-RC2,22.7.0-RC3,22.7.0-RC4", true},
		{">= 24.5.2", true},
		{"all", true},
		{"", true},
		// Not understood.
		{"< 1.2", false},
		{"~1.2.3", false},
		{">= 1.0.0, 1.2.0", false}, // comparator mixed with a plain version
		{"1.0.0 or 2.0.0", false},
		{"< 1.0.0,", false},
		{"every version", false},
	}
	for _, tt := range tests {
		if _, ok := ParseRange(tt.in); ok != tt.ok {
			t.Errorf("ParseRange(%q) ok = %v, want %v", tt.in, ok, tt.ok)
		}
	}
}

func TestParseFixes(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		ok   bool
	}{
		{"26.8.1", []string{"26.8.1"}, true},
		{">= 1.16.9", []string{"1.16.9"}, true},
		{">=8.3.0", []string{"8.3.0"}, true},
		{">= v1.9.18", []string{"1.9.18"}, true},
		{"v7.0.0-beta.5", []string{"7.0.0-beta.5"}, true},
		{"1.13.15, 1.14.2", []string{"1.13.15", "1.14.2"}, true},
		{"", nil, true},
		{"none", nil, false},
		{"< 2.0.0", nil, false},
	}
	for _, tt := range tests {
		got, ok := parseFixes(tt.in)
		var gotS []string
		for _, v := range got {
			gotS = append(gotS, v.String())
		}
		if ok != tt.ok || len(gotS) != len(tt.want) {
			t.Errorf("parseFixes(%q) = %v, %v; want %v, %v", tt.in, gotS, ok, tt.want, tt.ok)
			continue
		}
		for i := range gotS {
			if gotS[i] != tt.want[i] {
				t.Errorf("parseFixes(%q) = %v, want %v", tt.in, gotS, tt.want)
			}
		}
	}
}

// recordedAdvisory returns one advisory from the recorded fixtures.
func recordedAdvisory(t *testing.T, repo software.Repo, ghsa string) Advisory {
	t.Helper()
	var advs []Advisory
	if err := json.Unmarshal(advisoryFixture(t, repo), &advs); err != nil {
		t.Fatal(err)
	}
	for _, a := range advs {
		if a.GHSAID == ghsa {
			return a
		}
	}
	t.Fatalf("%s not in the %s fixture", ghsa, repo)
	return Advisory{}
}

func TestAdvisoryCheckRecorded(t *testing.T) {
	var (
		lighthouse = software.Repo{Owner: "sigp", Name: "lighthouse"}
		erigon     = software.Repo{Owner: "erigontech", Name: "erigon"}
		lodestar   = software.Repo{Owner: "ChainSafe", Name: "lodestar"}
		teku       = software.Repo{Owner: "Consensys-Incorporated", Name: "teku"}
	)
	tests := []struct {
		name    string
		client  software.Name
		repo    software.Repo
		ghsa    string
		running string
		want    Match
		fix     string // "" means no fix
	}{
		// "<= 1.16.8", patched ">= 1.16.9"
		{"geth at bound", software.Geth, geth, "GHSA-m6j8-rg6r-7mv8", "1.16.8", Affected, "1.16.9"},
		{"geth fixed", software.Geth, geth, "GHSA-m6j8-rg6r-7mv8", "1.16.9", NotAffected, ""},
		{"geth dev build of the fix", software.Geth, geth, "GHSA-m6j8-rg6r-7mv8", "1.16.9-unstable", NotAffected, ""},
		// ">= 1.14.0, < 1.14.13"
		{"geth below range", software.Geth, geth, "GHSA-q26p-9cq4-7fc2", "1.13.15", NotAffected, ""},
		{"geth range start", software.Geth, geth, "GHSA-q26p-9cq4-7fc2", "1.14.0", Affected, "1.14.13"},
		{"geth range end", software.Geth, geth, "GHSA-q26p-9cq4-7fc2", "1.14.13", NotAffected, ""},
		// ">= v1.9.4, < 1.9.20"
		{"geth v prefix", software.Geth, geth, "GHSA-xw37-57qp-9mm4", "1.9.4", Affected, "1.9.20"},
		// ">= 24.5.2", patched "26.8.1": bounded by the fix.
		{"besu open range below", software.Besu, besu, "GHSA-p4h2-gvh4-pv6j", "24.5.1", NotAffected, ""},
		{"besu open range start", software.Besu, besu, "GHSA-p4h2-gvh4-pv6j", "24.5.2", Affected, "26.8.1"},
		{"besu open range before fix", software.Besu, besu, "GHSA-p4h2-gvh4-pv6j", "26.8.0", Affected, "26.8.1"},
		{"besu open range fixed", software.Besu, besu, "GHSA-p4h2-gvh4-pv6j", "26.8.1", NotAffected, ""},
		{"besu open range newer", software.Besu, besu, "GHSA-p4h2-gvh4-pv6j", "26.10.2", NotAffected, ""},
		// List "22.4.0-RC2,22.4.0,…,22.7.0-RC4", patched "22.7.1"
		{"besu list rc", software.Besu, besu, "GHSA-4456-w38r-m53x", "22.4.0-RC2", Affected, "22.7.1"},
		{"besu list release", software.Besu, besu, "GHSA-4456-w38r-m53x", "22.7.0", Affected, "22.7.1"},
		{"besu not listed", software.Besu, besu, "GHSA-4456-w38r-m53x", "22.5.0", NotAffected, ""},
		{"besu list fixed", software.Besu, besu, "GHSA-4456-w38r-m53x", "22.7.1", NotAffected, ""},
		// "all", no patched version.
		{"besu all without fix", software.Besu, besu, "GHSA-v5gq-r843-8f73", "26.11.0", Unclear, ""},
		// "< v7.0.0-beta.5", patched "v7.0.0-beta.5"
		{"lighthouse pre-release", software.Lighthouse, lighthouse, "GHSA-wm9c-xvqq-5c28", "7.0.0-beta.4", Affected, "7.0.0-beta.5"},
		{"lighthouse release", software.Lighthouse, lighthouse, "GHSA-wm9c-xvqq-5c28", "7.0.0", NotAffected, ""},
		// "<8.3.0", patched ">=8.3.0"
		{"lighthouse recent", software.Lighthouse, lighthouse, "GHSA-rvxr-393q-q925", "8.2.1", Affected, "8.3.0"},
		// ">=v3.1.0 and  <v3.4.0"
		{"erigon before", software.Erigon, erigon, "GHSA-4c6q-hpx8-75wm", "3.0.5", NotAffected, ""},
		{"erigon start", software.Erigon, erigon, "GHSA-4c6q-hpx8-75wm", "3.1.0", Affected, "3.4.0"},
		{"erigon inside", software.Erigon, erigon, "GHSA-4c6q-hpx8-75wm", "3.3.9", Affected, "3.4.0"},
		{"erigon fixed", software.Erigon, erigon, "GHSA-4c6q-hpx8-75wm", "3.4.0", NotAffected, ""},
		// "< 1.24.0", patched "1.25.0": the range decides, the fix is named.
		{"lodestar affected", software.Lodestar, lodestar, "GHSA-m9c9-mc2h-9wjw", "1.23.1", Affected, "1.25.0"},
		{"lodestar not affected", software.Lodestar, lodestar, "GHSA-m9c9-mc2h-9wjw", "1.24.0", NotAffected, ""},
		// "<21.12.1"
		{"teku log4j", software.Teku, teku, "GHSA-mwfw-vm54-g3p7", "21.12.0", Affected, "21.12.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, ok := ParseVersion(tt.running)
			if !ok {
				t.Fatalf("bad test version %q", tt.running)
			}
			m, fix, hasFix := recordedAdvisory(t, tt.repo, tt.ghsa).Check(tt.client, running)
			gotFix := ""
			if hasFix {
				gotFix = fix.String()
			}
			if m != tt.want || gotFix != tt.fix {
				t.Errorf("Check(%s) = %v, fix %q; want %v, fix %q", tt.running, m, gotFix, tt.want, tt.fix)
			}
		})
	}
}

func TestAdvisoryPackages(t *testing.T) {
	aztec := software.Repo{Owner: "AztecProtocol", Name: "aztec-packages"}
	tests := []struct {
		name    string
		repo    software.Repo
		ghsa    string
		running string
		m       Match
		want    []string
	}{
		// Three entries ">= 6.0.0-nightly.20260721, < 7.0.0-0" without a
		// fix; the entries for 4.x and 7.0.0 nightlies do not match.
		{"affected", aztec, "GHSA-73xc-j27q-wg75", "6.0.0", Affected,
			[]string{"@aztec/bb.js", "@aztec-foundation/bb.js", "barretenberg-rs"}},
		// "@aztec/bb.js" and "barretenberg-rs" each have three entries
		// for 4.x and 5.x; 5.2.0 matches "< 5.2.1" once per package.
		{"each package once", aztec, "GHSA-2hc8-mq24-76v2", "5.2.0", Affected,
			[]string{"@aztec/bb.js", "barretenberg-rs"}},
		{"not affected", aztec, "GHSA-2hc8-mq24-76v2", "6.0.0", Affected, nil},
		// One entry with a free-text range; its package name contains a
		// space, as GitHub has it.
		{"unclear", aztec, "GHSA-778c-2h5c-8r96", "6.0.0", Unclear, []string{"@aztec/archiver @aztec/stdlib"}},
		{"other match", aztec, "GHSA-73xc-j27q-wg75", "6.0.0", Unclear, nil},
		// "<= 1.16.8": the entry without a package name is left out.
		{"entry without a name", geth, "GHSA-m6j8-rg6r-7mv8", "1.16.8", Affected, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, ok := ParseVersion(tt.running)
			if !ok {
				t.Fatalf("bad test version %q", tt.running)
			}
			got := recordedAdvisory(t, tt.repo, tt.ghsa).Packages(software.Aztec, running, tt.m)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Packages(%s, %v) = %q, want %q", tt.running, tt.m, got, tt.want)
			}
		})
	}
}

func TestAdvisoryCheckEdgeCases(t *testing.T) {
	// Go values, not API responses: combinations the recorded data does
	// not contain.
	entry := func(rng, patched string) AdvisoryVulnerability {
		return AdvisoryVulnerability{VulnerableVersionRange: rng, PatchedVersions: patched}
	}
	tests := []struct {
		name    string
		entries []AdvisoryVulnerability
		running string
		want    Match
		fix     string
	}{
		{"open range without fix", []AdvisoryVulnerability{entry(">= 1.0.0", "")}, "2.0.0", Affected, ""},
		{"upper bound without fix", []AdvisoryVulnerability{entry("< 3.0.0", "")}, "2.0.0", Affected, ""},
		{"all with fix", []AdvisoryVulnerability{entry("all", "2.1.0")}, "2.0.0", Affected, "2.1.0"},
		{"all with fix, fixed", []AdvisoryVulnerability{entry("all", "2.1.0")}, "2.1.0", NotAffected, ""},
		{"backport: fix in running's line", []AdvisoryVulnerability{entry("< 1.14.2", "1.13.15, 1.14.2")}, "1.13.10", Affected, "1.13.15"},
		{"backport: older line's fix is below running", []AdvisoryVulnerability{entry("< 1.14.2", "1.13.15, 1.14.2")}, "1.14.0", Affected, "1.14.2"},
		{"two entries: highest fix", []AdvisoryVulnerability{entry("< 1.5.0", "1.5.0"), entry("< 1.6.0", "1.6.0")}, "1.4.0", Affected, "1.6.0"},
		{"two entries: one without fix", []AdvisoryVulnerability{entry("< 1.5.0", "1.5.0"), entry("< 2.0.0", "")}, "1.4.0", Affected, ""},
		{"unclear entry, other affected", []AdvisoryVulnerability{entry("~1.2.3", ""), entry("< 1.5.0", "1.5.0")}, "1.4.0", Affected, "1.5.0"},
		{"unclear entry, other not affected", []AdvisoryVulnerability{entry("~1.2.3", ""), entry("< 1.0.0", "1.0.0")}, "1.4.0", Unclear, ""},
		{"patched not understood", []AdvisoryVulnerability{entry("< 1.5.0", "soon")}, "1.4.0", Unclear, ""},
		{"no entries", nil, "1.4.0", NotAffected, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, _ := ParseVersion(tt.running)
			m, fix, hasFix := Advisory{Vulnerabilities: tt.entries}.Check(software.Geth, running)
			gotFix := ""
			if hasFix {
				gotFix = fix.String()
			}
			if m != tt.want || gotFix != tt.fix {
				t.Errorf("got %v, fix %q; want %v, fix %q", m, gotFix, tt.want, tt.fix)
			}
		})
	}
}

// TestRecordedRangesAreUnderstood makes sure every range and patched
// version in the recorded advisories is understood, except the free-text
// entry of Aztec's GHSA-778c-2h5c-8r96 (range and patched version). The
// advisories x3f cannot decide are Besu's "all" without a fix and that
// Aztec advisory.
func TestRecordedRangesAreUnderstood(t *testing.T) {
	freeText := "current Ignition mainnet release (v4 tag)"
	files, err := filepath.Glob(filepath.Join("testdata", "github", "advisories", "*.json"))
	if err != nil || len(files) != 13 {
		t.Fatalf("found %d fixture files, err %v", len(files), err)
	}
	var unclear []string
	total := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var advs []Advisory
		if err := json.Unmarshal(data, &advs); err != nil {
			t.Fatal(err)
		}
		for _, a := range advs {
			total++
			for _, e := range a.Vulnerabilities {
				if _, ok := ParseRange(e.VulnerableVersionRange); ok == (e.VulnerableVersionRange == freeText) {
					t.Errorf("%s: range %q not understood", a.GHSAID, e.VulnerableVersionRange)
				}
				if _, ok := parseFixes(e.PatchedVersions); !ok && e.VulnerableVersionRange != freeText {
					t.Errorf("%s: patched %q not understood", a.GHSAID, e.PatchedVersions)
				}
			}
			if m, _, _ := a.Check(software.Geth, Version{Major: 1}); m == Unclear {
				unclear = append(unclear, a.GHSAID)
			}
		}
	}
	if total != 77 {
		t.Errorf("checked %d advisories, want 77", total)
	}
	slices.Sort(unclear)
	if want := []string{"GHSA-778c-2h5c-8r96", "GHSA-v5gq-r843-8f73"}; !slices.Equal(unclear, want) {
		t.Errorf("unclear advisories = %v, want %v", unclear, want)
	}
}
