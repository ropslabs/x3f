package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	got := string(Render([]Family{
		{Name: "x3f_node_status", Help: "Status of a node: 0 OK, 1 WARN, 2 CRIT.", Samples: []Sample{
			{Labels: []Label{{"node", "mainnet-1"}}, Value: 0},
			{Labels: []Label{{"node", "mainnet-2"}}, Value: 2},
		}},
		{Name: "x3f_empty", Help: "left out", Samples: nil},
		{Name: "x3f_last_run_timestamp_seconds", Help: "When the last run ended.", Samples: []Sample{
			{Value: 1795600000},
		}},
	}))
	want := `# HELP x3f_node_status Status of a node: 0 OK, 1 WARN, 2 CRIT.
# TYPE x3f_node_status gauge
x3f_node_status{node="mainnet-1"} 0
x3f_node_status{node="mainnet-2"} 2
# HELP x3f_last_run_timestamp_seconds When the last run ended.
# TYPE x3f_last_run_timestamp_seconds gauge
x3f_last_run_timestamp_seconds 1795600000
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestRenderEscapes follows the escaping example of the exposition format
// documentation.
func TestRenderEscapes(t *testing.T) {
	got := string(Render([]Family{{Name: "m", Help: "a \\ b\nc", Samples: []Sample{
		{Labels: []Label{{"node", "C:\\DIR\\FILE.TXT"}, {"other", "Cannot find file:\n\"FILE.TXT\""}}, Value: 1},
	}}}))
	want := `# HELP m a \\ b\nc
# TYPE m gauge
m{node="C:\\DIR\\FILE.TXT",other="Cannot find file:\n\"FILE.TXT\""} 1
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x3f.prom")
	if err := WriteFile(path, []byte("a 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("a 2\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "a 2\n" {
		t.Errorf("content %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, %v; want 0644", info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestTempName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x3f.prom")
	f, err := os.CreateTemp(filepath.Dir(path), tempPattern(path))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	// The collector only reads files ending in ".prom".
	if name := filepath.Base(f.Name()); strings.HasSuffix(name, ".prom") || !strings.HasPrefix(name, "x3f.prom.tmp-") {
		t.Errorf("temporary file %q", name)
	}
}

func TestWriteFileFailure(t *testing.T) {
	// The rename must fail: path is a non-empty directory.
	dir := t.TempDir()
	path := filepath.Join(dir, "x3f.prom")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("a 1\n")); err == nil {
		t.Fatal("rename over a non-empty directory succeeded")
	}
	// The temporary file is removed again.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "x3f.prom" {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestWriteFileMissingDir(t *testing.T) {
	err := WriteFile(filepath.Join(t.TempDir(), "missing", "x3f.prom"), []byte("a 1\n"))
	if err == nil || !strings.HasPrefix(err.Error(), "write metrics: ") {
		t.Errorf("error = %v", err)
	}
}
