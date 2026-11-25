package notify

import (
	"maps"
	"time"

	"github.com/ropslabs/x3f/internal/status"
)

// Notification rules (agreed for build step 6), with min_level as the
// threshold (WARN by default, or CRIT):
//
//	changed:  the node's level changed, and the new or the old level is at
//	          least min_level. With min_level CRIT that is: turned red, or
//	          stopped being red (all clear). OK <-> WARN is then silent.
//	worse:    the node stays CRIT, but a check got worse and is now at
//	          least min_level, so a second red cause does not go unnoticed.
//	reminder: the node stays CRIT and nothing else was said for
//	          RemindEvery (0: never).
//
// Only levels are compared, not finding texts: those change on every run
// ("8 days old") and would cause a message each time.

// Node is the current status of one node, as the report shows it.
type Node struct {
	Name   string
	Level  status.Level
	Checks []Check
}

// Check is one part of a node's assessment, e.g. "execution advisories".
// It is identified by its name, so new kinds of components need no change
// here.
type Check struct {
	Name string
	status.Assessment
}

// Options are the settings for Evaluate.
type Options struct {
	// MinLevel is status.Warn or status.Crit.
	MinLevel status.Level
	// RemindEvery is the reminder interval for a node that stays CRIT; 0
	// disables reminders.
	RemindEvery time.Duration
	Now         time.Time
}

// Kind is the reason for a message about a node.
type Kind int

const (
	Changed  Kind = iota // the level changed
	Worse                // still CRIT, but a check got worse
	Reminder             // still CRIT, nothing said for a while
)

// Change is one node worth a message.
type Change struct {
	Kind Kind
	Node Node
	// Was is the level of the last message (for Changed).
	Was status.Level
	// Since is when the node's current level was first seen.
	Since time.Time
	// Worse names the checks that got worse (for Worse).
	Worse []string
}

// Evaluate compares the nodes with the remembered state. It returns the
// changes worth a message and the next state, which assumes the message is
// delivered; if it is not, use Undelivered.
func Evaluate(prev State, nodes []Node, o Options) ([]Change, State) {
	next := State{Version: stateVersion, Nodes: make(map[string]NodeState, len(nodes))}
	var changes []Change
	for _, n := range nodes {
		p, seen := prev.Nodes[n.Name]
		ns := NodeState{Level: n.Level, Since: p.Since, Told: p.Told}
		if !seen || p.Level != n.Level {
			ns.Since = o.Now
		}
		checks := notOK(n.Checks)
		told := p.Told // zero value for a new node: OK, never told

		c := Change{Node: n, Was: told.Level, Since: ns.Since}
		send := false
		switch {
		case n.Level != told.Level:
			c.Kind = Changed
			send = n.Level >= o.MinLevel || told.Level >= o.MinLevel
		case n.Level == status.Crit:
			for _, ch := range n.Checks {
				if ch.Level > told.Checks[ch.Name] && ch.Level >= o.MinLevel {
					c.Worse = append(c.Worse, ch.Name)
				}
			}
			switch {
			case len(c.Worse) > 0:
				c.Kind, send = Worse, true
			case o.RemindEvery > 0 && o.Now.Sub(told.At) >= o.RemindEvery:
				c.Kind, send = Reminder, true
			}
		}

		// The baseline follows the node even without a message, so a
		// silent change (below min_level) is not reported later.
		ns.Told = Told{Level: n.Level, Checks: checks, At: told.At}
		if send {
			ns.Told.At = o.Now
			changes = append(changes, c)
		}
		next.Nodes[n.Name] = ns
	}
	return changes, next
}

// Undelivered returns next with the baseline of every changed node reset
// to prev, so that the next run reports the change again.
func Undelivered(prev, next State, changes []Change) State {
	out := State{Version: next.Version, Nodes: maps.Clone(next.Nodes)}
	for _, c := range changes {
		ns := out.Nodes[c.Node.Name]
		ns.Told = prev.Nodes[c.Node.Name].Told
		out.Nodes[c.Node.Name] = ns
	}
	return out
}

// notOK returns the level of every check that is not OK.
func notOK(checks []Check) map[string]status.Level {
	m := make(map[string]status.Level)
	for _, c := range checks {
		if c.Level != status.OK {
			m[c.Name] = c.Level
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
