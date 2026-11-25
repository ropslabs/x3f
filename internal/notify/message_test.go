package notify

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/status"
)

func TestFormat(t *testing.T) {
	crit := Node{Name: "mainnet-2", Level: status.Crit, Checks: []Check{
		{"execution", status.Assessment{Level: status.Crit, Findings: []status.Finding{
			{Level: status.Crit, Message: "Besu 23.1.1 is outdated"},
			{Level: status.OK, Message: "a note that stays in the report"},
		}}},
		{"fork", status.Assessment{Level: status.Warn, Findings: []status.Finding{
			{Level: status.Warn, Message: "forks.yaml may be outdated"},
		}}},
	}}
	ok := Node{Name: "mainnet-1", Level: status.OK, Checks: []Check{{"sync", status.Assessment{}}}}
	now := t0.Add(2 * time.Hour)

	got := Format([]Change{
		{Kind: Changed, Node: crit, Was: status.OK, Since: t0},
		{Kind: Changed, Node: ok, Was: status.Warn, Since: now},
		{Kind: Worse, Node: crit, Worse: []string{"fork"}},
		{Kind: Reminder, Node: crit, Since: t0},
	}, now, MaxMessageLength)
	want := `🔴 <b>mainnet-2</b>: CRIT (was OK, since 2026-11-25 12:00 UTC)
• CRIT execution: Besu 23.1.1 is outdated
• WARN fork: forks.yaml may be outdated

✅ <b>mainnet-1</b>: OK (was WARN)

🔴 <b>mainnet-2</b>: still CRIT, worse: fork
• CRIT execution: Besu 23.1.1 is outdated
• WARN fork: forks.yaml may be outdated

🔴 <b>mainnet-2</b>: still CRIT (since 2026-11-25 12:00 UTC)
• CRIT execution: Besu 23.1.1 is outdated
• WARN fork: forks.yaml may be outdated`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatEscapes: node names come from the config and findings may
// quote text from GitHub; neither may produce HTML tags.
func TestFormatEscapes(t *testing.T) {
	n := Node{Name: `<b>evil</b> & "co"`, Level: status.Warn, Checks: []Check{
		{"execution <i>advisories</i>", status.Assessment{Level: status.Warn, Findings: []status.Finding{
			{Level: status.Warn, Message: `GHSA-x (medium: "<a href='x'>click</a> &amp;")`},
		}}},
	}}
	got := Format([]Change{{Kind: Worse, Node: n, Worse: []string{"<u>sync</u>"}}}, t0, MaxMessageLength)
	want := `🟡 <b>&lt;b&gt;evil&lt;/b&gt; &amp; &#34;co&#34;</b>: still WARN, worse: &lt;u&gt;sync&lt;/u&gt;
• WARN execution &lt;i&gt;advisories&lt;/i&gt;: GHSA-x (medium: &#34;&lt;a href=&#39;x&#39;&gt;click&lt;/a&gt; &amp;amp;&#34;)`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatShortens(t *testing.T) {
	long := strings.Repeat("x", 1000)
	var changes []Change
	for range 30 {
		changes = append(changes, Change{Kind: Reminder, Since: t0, Node: Node{Name: "n", Level: status.Crit, Checks: []Check{
			{"fork", status.Assessment{Level: status.Crit, Findings: []status.Finding{{Level: status.Crit, Message: long}}}},
		}}})
	}
	got := Format(changes, t0, MaxMessageLength)
	if length(got) > MaxMessageLength {
		t.Errorf("length %d > %d", length(got), MaxMessageLength)
	}
	if !strings.HasSuffix(got, "\n… (shortened, run x3f for the full report)") {
		t.Errorf("no tail:\n%s", got[len(got)-200:])
	}
	// Each finding is cut to maxFindingLength characters.
	first := strings.Split(got, "\n")[1]
	if want := "• CRIT fork: " + strings.Repeat("x", maxFindingLength-1) + "…"; first != want {
		t.Errorf("finding line has %d characters, want %d", length(first), length(want))
	}

	// A short message is not shortened, even if it uses the limit fully.
	one := Format(changes[:1], t0, MaxMessageLength)
	if Format(changes[:1], t0, length(one)) != one {
		t.Error("message of exactly the limit was shortened")
	}
}

func TestLength(t *testing.T) {
	for s, want := range map[string]int{"abc": 3, "ä": 1, "•": 1, "🔴": 2, "✅": 1} {
		if got := length(s); got != want {
			t.Errorf("length(%q) = %d, want %d", s, got, want)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
