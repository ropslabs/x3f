package notify

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/status"
)

var t0 = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

// node builds a node from check levels, e.g. node("a", "fork", status.Crit).
// The node level is the highest check level.
func node(name string, checks ...any) Node {
	n := Node{Name: name}
	for i := 0; i < len(checks); i += 2 {
		l := checks[i+1].(status.Level)
		a := status.Assessment{Level: l}
		if l != status.OK {
			a.Findings = []status.Finding{{Level: l, Message: fmt.Sprintf("%s is %s", checks[i], l)}}
		}
		n.Checks = append(n.Checks, Check{Name: checks[i].(string), Assessment: a})
		n.Level = max(n.Level, l)
	}
	return n
}

// describe renders changes compactly, e.g. "a Changed OK->CRIT".
func describe(changes []Change) []string {
	var out []string
	for _, c := range changes {
		switch c.Kind {
		case Changed:
			out = append(out, fmt.Sprintf("%s Changed %s->%s", c.Node.Name, c.Was, c.Node.Level))
		case Worse:
			out = append(out, fmt.Sprintf("%s Worse %s", c.Node.Name, strings.Join(c.Worse, ",")))
		case Reminder:
			out = append(out, c.Node.Name+" Reminder")
		}
	}
	return out
}

// step is one run: when, which nodes, what should be sent, and whether
// the message is delivered.
type step struct {
	after  time.Duration // since t0
	nodes  []Node
	want   []string
	failed bool
}

func runSteps(t *testing.T, o Options, steps []step) State {
	t.Helper()
	var s State
	for i, st := range steps {
		o.Now = t0.Add(st.after)
		changes, next := Evaluate(s, st.nodes, o)
		if got := describe(changes); !slices.Equal(got, st.want) {
			t.Errorf("step %d (+%v): got %q, want %q", i, st.after, got, st.want)
		}
		if st.failed {
			next = Undelivered(s, next, changes)
		}
		s = next
	}
	return s
}

func TestEvaluate(t *testing.T) {
	const ok, warn, crit = status.OK, status.Warn, status.Crit
	h := time.Hour
	warnMode := Options{MinLevel: warn, RemindEvery: 24 * h}
	critMode := Options{MinLevel: crit, RemindEvery: 24 * h}

	tests := []struct {
		name  string
		o     Options
		steps []step
	}{
		{
			name: "first run: everything not OK is reported once",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "sync", ok), node("b", "sync", warn), node("c", "fork", crit)},
					[]string{"b Changed OK->WARN", "c Changed OK->CRIT"}, false},
				{h, []Node{node("a", "sync", ok), node("b", "sync", warn), node("c", "fork", crit)}, nil, false},
			},
		},
		{
			name: "worse and back to good, min_level warn",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "sync", warn)}, []string{"a Changed OK->WARN"}, false},
				{h, []Node{node("a", "sync", crit)}, []string{"a Changed WARN->CRIT"}, false},
				{2 * h, []Node{node("a", "sync", warn)}, []string{"a Changed CRIT->WARN"}, false},
				{3 * h, []Node{node("a", "sync", ok)}, []string{"a Changed WARN->OK"}, false},
			},
		},
		{
			name: "min_level crit: OK <-> WARN is silent, leaving CRIT is not",
			o:    critMode,
			steps: []step{
				{0, []Node{node("a", "sync", warn)}, nil, false},
				{h, []Node{node("a", "sync", crit)}, []string{"a Changed WARN->CRIT"}, false},
				{2 * h, []Node{node("a", "sync", warn)}, []string{"a Changed CRIT->WARN"}, false},
				{3 * h, []Node{node("a", "sync", ok)}, nil, false},
				{4 * h, []Node{node("a", "sync", crit)}, []string{"a Changed OK->CRIT"}, false},
				{5 * h, []Node{node("a", "sync", ok)}, []string{"a Changed CRIT->OK"}, false},
			},
		},
		{
			name: "a second cause while CRIT",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "fork", crit, "sync", ok)}, []string{"a Changed OK->CRIT"}, false},
				{h, []Node{node("a", "fork", crit, "sync", warn)}, []string{"a Worse sync"}, false},
				{2 * h, []Node{node("a", "fork", crit, "sync", crit)}, []string{"a Worse sync"}, false},
				// Better is not worth a message while the node stays CRIT.
				{3 * h, []Node{node("a", "fork", crit, "sync", ok)}, nil, false},
				{4 * h, []Node{node("a", "fork", ok, "sync", crit)}, []string{"a Worse sync"}, false},
			},
		},
		{
			name: "a second cause while CRIT, min_level crit: only CRIT counts",
			o:    critMode,
			steps: []step{
				{0, []Node{node("a", "fork", crit, "sync", ok)}, []string{"a Changed OK->CRIT"}, false},
				{h, []Node{node("a", "fork", crit, "sync", warn)}, nil, false},
				{2 * h, []Node{node("a", "fork", crit, "sync", crit)}, []string{"a Worse sync"}, false},
			},
		},
		{
			name: "reminder once a day while CRIT",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "fork", crit)}, []string{"a Changed OK->CRIT"}, false},
				{23 * h, []Node{node("a", "fork", crit)}, nil, false},
				{24 * h, []Node{node("a", "fork", crit)}, []string{"a Reminder"}, false},
				{30 * h, []Node{node("a", "fork", crit, "sync", crit)}, []string{"a Worse sync"}, false},
				// The next reminder counts from the last message.
				{48 * h, []Node{node("a", "fork", crit, "sync", crit)}, nil, false},
				{54 * h, []Node{node("a", "fork", crit, "sync", crit)}, []string{"a Reminder"}, false},
			},
		},
		{
			name: "no reminders for WARN or when disabled",
			o:    Options{MinLevel: warn},
			steps: []step{
				{0, []Node{node("a", "fork", crit), node("b", "sync", warn)}, []string{"a Changed OK->CRIT", "b Changed OK->WARN"}, false},
				{100 * h, []Node{node("a", "fork", crit), node("b", "sync", warn)}, nil, false},
			},
		},
		{
			name: "undelivered changes are sent again",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "fork", crit), node("b", "sync", ok)}, []string{"a Changed OK->CRIT"}, true},
				{h, []Node{node("a", "fork", crit), node("b", "sync", warn)}, []string{"a Changed OK->CRIT", "b Changed OK->WARN"}, true},
				{2 * h, []Node{node("a", "fork", crit), node("b", "sync", warn)}, []string{"a Changed OK->CRIT", "b Changed OK->WARN"}, false},
				{3 * h, []Node{node("a", "fork", crit), node("b", "sync", warn)}, nil, false},
			},
		},
		{
			name: "a problem that ended before delivery is not reported",
			o:    warnMode,
			steps: []step{
				{0, []Node{node("a", "sync", crit)}, []string{"a Changed OK->CRIT"}, true},
				{h, []Node{node("a", "sync", ok)}, nil, false},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSteps(t, tt.o, tt.steps)
		})
	}
}

func TestEvaluateSinceAndRemovedNodes(t *testing.T) {
	o := Options{MinLevel: status.Warn, Now: t0}
	changes, s := Evaluate(State{}, []Node{node("a", "fork", status.Crit), node("gone", "sync", status.OK)}, o)
	s = Undelivered(State{}, s, changes)

	// Two hours later the message is delivered: Since is still the first
	// time the problem was seen.
	o.Now = t0.Add(2 * time.Hour)
	changes, s = Evaluate(s, []Node{node("a", "fork", status.Crit)}, o)
	if len(changes) != 1 || !changes[0].Since.Equal(t0) {
		t.Fatalf("changes = %+v, want one with Since %v", changes, t0)
	}
	if _, ok := s.Nodes["gone"]; ok || len(s.Nodes) != 1 {
		t.Errorf("state keeps removed nodes: %+v", s.Nodes)
	}
	want := NodeState{Level: status.Crit, Since: t0, Told: Told{
		Level: status.Crit, Checks: map[string]status.Level{"fork": status.Crit}, At: o.Now}}
	if !reflect.DeepEqual(s.Nodes["a"], want) {
		t.Errorf("state = %+v, want %+v", s.Nodes["a"], want)
	}

	// A new level starts a new Since.
	o.Now = t0.Add(3 * time.Hour)
	changes, _ = Evaluate(s, []Node{node("a", "fork", status.Warn)}, o)
	if len(changes) != 1 || !changes[0].Since.Equal(o.Now) {
		t.Errorf("changes = %+v, want one with Since %v", changes, o.Now)
	}
}

func TestStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	s, err := LoadState(path)
	if err != nil || len(s.Nodes) != 0 {
		t.Fatalf("missing file: %+v, %v", s, err)
	}
	_, s = Evaluate(s, []Node{node("a", "fork", status.Crit, "sync", status.Warn)}, Options{MinLevel: status.Warn, Now: t0})
	if err := SaveState(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("round trip:\n got  %+v\n want %+v", got, s)
	}

	// Readable JSON, levels as words, only the owner may read it.
	data := readFile(t, path)
	for _, want := range []string{`"version": 1`, `"level": "CRIT"`, `"sync": "WARN"`} {
		if !strings.Contains(data, want) {
			t.Errorf("state file lacks %s:\n%s", want, data)
		}
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}

	writeFile(t, path, `{"version": 2, "nodes": {}}`)
	if _, err := LoadState(path); err == nil || !strings.Contains(err.Error(), "version 2, want 1") {
		t.Errorf("other version: %v", err)
	}
	writeFile(t, path, `{"version": 1, "nodes": {"a": {"level": "RED"}}}`)
	if _, err := LoadState(path); err == nil || !strings.Contains(err.Error(), `unknown level "RED"`) {
		t.Errorf("bad level: %v", err)
	}
}
