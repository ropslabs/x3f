// Package metrics writes metrics in the Prometheus text format to a file
// for the node_exporter textfile collector.
//
// Format: https://prometheus.io/docs/instrumenting/exposition_formats/
// Collector: https://github.com/prometheus/node_exporter#textfile-collector
package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Family is one metric with all its samples. All x3f metrics are gauges.
type Family struct {
	Name    string
	Help    string
	Samples []Sample
}

// Sample is one time series: its labels and its value.
type Sample struct {
	Labels []Label
	Value  float64
}

// Label is a label name and value. Values must have low cardinality: no
// free text such as findings or error messages.
type Label struct {
	Name, Value string
}

// Render returns the families in the text format, in the given order.
// Families without samples are left out. There are no timestamps: the
// textfile collector rejects files that contain them.
func Render(families []Family) []byte {
	var b strings.Builder
	for _, f := range families {
		if len(f.Samples) == 0 {
			continue
		}
		fmt.Fprintf(&b, "# HELP %s %s\n", f.Name, escapeHelp(f.Help))
		fmt.Fprintf(&b, "# TYPE %s gauge\n", f.Name)
		for _, s := range f.Samples {
			b.WriteString(f.Name)
			if len(s.Labels) > 0 {
				b.WriteByte('{')
				for i, l := range s.Labels {
					if i > 0 {
						b.WriteByte(',')
					}
					fmt.Fprintf(&b, "%s=\"%s\"", l.Name, escapeLabel(l.Value))
				}
				b.WriteByte('}')
			}
			b.WriteByte(' ')
			b.WriteString(strconv.FormatFloat(s.Value, 'f', -1, 64))
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// escapeLabel escapes a label value: "the backslash (\), double-quote ("),
// and line feed (\n) characters have to be escaped as \\, \", and \n".
var escapeLabel = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace

// escapeHelp escapes a HELP text: "backslash and line-feed characters have
// to be escaped as \\ and \n".
var escapeHelp = strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace

// WriteFile writes data to path atomically: into a temporary file in the
// same directory, then renamed over path. The temporary name does not end
// in ".prom", so the collector never reads a half-written file. The file
// is readable by everyone (0644): node_exporter usually runs as another
// user. The directory must exist.
func WriteFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), tempPattern(path))
	if err != nil {
		return fmt.Errorf("write metrics: %w", err)
	}
	_, err = f.Write(data)
	if err == nil {
		// CreateTemp creates the file with mode 0600.
		err = f.Chmod(0o644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("write metrics: %w", err)
	}
	return nil
}

// tempPattern is the os.CreateTemp pattern for path: "x3f.prom.tmp-*",
// where CreateTemp replaces the * with a random number.
func tempPattern(path string) string {
	return filepath.Base(path) + ".tmp-*"
}
