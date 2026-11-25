package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/releases"
)

// MaxForkDataAge is how old forks.yaml may be before x3f adds a note that
// newer announcements might be missing.
const MaxForkDataAge = 30 * 24 * time.Hour

// ForkInput describes one node for the fork readiness check.
type ForkInput struct {
	Schedule *forks.Schedule
	// ChainID is the detected network; HaveChain is false if detection
	// failed on both sides.
	ChainID   uint64
	HaveChain bool
	// Versions of both clients; zero values if unknown or unreachable.
	Execution ethereum.ClientVersion
	Consensus ethereum.ClientVersion
	Now       time.Time
	// CriticalWithin: a client that is not ready turns red this long
	// before the fork (if a ready release exists).
	CriticalWithin time.Duration
}

// ForkResult is the assessment plus the text for the FORK column.
type ForkResult struct {
	Assessment
	Cell string
}

// AssessForks checks fork readiness by comparing client versions with the
// releases listed in forks.yaml (the version fallback; build step 4c adds
// asking the node itself).
//
//	red:    a client is older than the minimum release of an active fork
//	        (it cannot follow the chain); a client release still contains
//	        a postponed fork's former schedule; a client is not ready for
//	        the next fork within CriticalWithin and a ready release exists
//	yellow: not ready, but more than CriticalWithin before the fork; no
//	        ready release announced yet ("waiting for client release");
//	        client not recognized while a fork is scheduled
func AssessForks(in ForkInput) ForkResult {
	r := ForkResult{Cell: "-"}
	if in.Schedule == nil || !in.HaveChain {
		return r
	}
	n, ok := in.Schedule.ForChainID(in.ChainID)
	if !ok {
		return r // unknown network: already noted by AssessNetwork
	}

	next, hasNext := n.Next(in.Now)
	var cellProblem string
	var nextParts []string // per-layer readiness for the next fork

	for _, layer := range []struct {
		short, name string
		v           ethereum.ClientVersion
	}{{"EL", "execution", in.Execution}, {"CL", "consensus", in.Consensus}} {
		v := layer.v
		running, ok := releases.ParseVersion(v.Version)
		if v.Client == ethereum.Unknown || !v.HasVersion() || !ok {
			if hasNext && v.Raw != "" {
				r.add(Warn, "%s client not recognized: readiness for %s unknown", layer.name, next.Name)
				nextParts = append(nextParts, layer.short+" unknown")
			}
			continue
		}
		running.Suffix = v.Suffix
		name := fmt.Sprintf("%s %s", v.Client, v.Version)

		// a) Below the minimum of the latest active fork that lists this
		// client: the node cannot follow the chain.
		active := n.Active(in.Now)
		for i := len(active) - 1; i >= 0; i-- {
			min, ok := active[i].MinVersions[v.Client]
			if !ok {
				continue
			}
			if releases.Compare(v.Client, running, min) < 0 {
				r.add(Crit, "%s is older than the minimum release for %s (%s): it cannot follow %s",
					name, active[i].Name, min, n.Name)
				cellProblem = fmt.Sprintf("%s below %s minimum", layer.short, active[i].Name)
			}
			break
		}

		// b) Still contains the former schedule of a postponed fork.
		for _, p := range n.Postponed() {
			from, ok1 := p.ScheduledIn[v.Client]
			fixed, ok2 := p.RemovedIn[v.Client]
			if !ok1 || !ok2 || releases.Compare(v.Client, running, from) < 0 || releases.Compare(v.Client, running, fixed) >= 0 {
				continue
			}
			msg := fmt.Sprintf("%s still contains the postponed %s schedule (%s); update to %s or later",
				name, p.Name, p.Time.Format("2006-01-02 15:04 UTC"), fixed)
			if !p.Time.After(in.Now) {
				msg += "; the former activation time has passed, so the node has most likely left the chain"
			}
			r.add(Crit, "%s", msg)
			if cellProblem == "" {
				cellProblem = fmt.Sprintf("%s has old %s schedule", layer.short, p.Name)
			}
		}

		// c) Readiness for the next fork.
		if !hasNext {
			continue
		}
		until := next.Time.Sub(in.Now)
		min, ok := minVersionFor(n, next, v.Client)
		switch {
		case !ok:
			r.add(Warn, "%s on %s in %s: no %s release announced yet (waiting for client release)",
				next.Name, n.Name, days(until), v.Client)
			nextParts = append(nextParts, layer.short+" waiting for release")
		case releases.Compare(v.Client, running, min) < 0:
			level := Warn
			if until <= in.CriticalWithin {
				level = Crit
			}
			r.add(level, "%s is not ready for %s on %s (%s, in %s): needs %s or later",
				name, next.Name, n.Name, next.Time.Format(time.DateOnly), days(until), min)
			nextParts = append(nextParts, layer.short+" not ready")
		}
	}

	switch {
	case cellProblem != "":
		r.Cell = cellProblem
	case hasNext && len(nextParts) == 0:
		r.Cell = fmt.Sprintf("%s in %s: ready", next.Name, days(next.Time.Sub(in.Now)))
	case hasNext:
		r.Cell = fmt.Sprintf("%s in %s: %s", next.Name, days(next.Time.Sub(in.Now)), strings.Join(nextParts, ", "))
	default:
		r.Cell = "none scheduled"
	}
	return r
}

// minVersionFor returns the minimum release of client for fork f. A BPO
// fork has no release list of its own; its releases are those announced
// for the hard fork it belongs to (the latest earlier non-BPO fork).
func minVersionFor(n forks.Network, f forks.Fork, client ethereum.ClientName) (releases.Version, bool) {
	if !f.BPO {
		v, ok := f.MinVersions[client]
		return v, ok
	}
	for i := len(n.Forks) - 1; i >= 0; i-- {
		g := n.Forks[i]
		if g.Postponed || g.BPO || g.Time.After(f.Time) {
			continue
		}
		v, ok := g.MinVersions[client]
		return v, ok
	}
	return releases.Version{}, false
}

// ForkDataNote returns a note if the fork data is older than MaxForkDataAge.
func ForkDataNote(s *forks.Schedule, now time.Time) (Finding, bool) {
	if s == nil {
		return Finding{}, false
	}
	age := now.Sub(s.Updated)
	if age <= MaxForkDataAge {
		return Finding{}, false
	}
	return Finding{Level: OK, Message: fmt.Sprintf(
		"fork data last updated %s (%s ago); newer announcements may be missing: update x3f or pass -forks",
		s.Updated.Format(time.DateOnly), days(age))}, true
}
