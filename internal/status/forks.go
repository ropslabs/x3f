package status

import (
	"fmt"
	"maps"
	"slices"
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
	// The fork schedule as the node reports it (eth_config and
	// /eth/v1/config/spec). nil if not available: then the *Err field
	// says why, or the layer was unreachable (both nil).
	ExecutionConfig    *ethereum.ExecutionForkConfig
	ExecutionConfigErr error
	ConsensusConfig    *ethereum.ConsensusForkConfig
	ConsensusConfigErr error
	Now                time.Time
	// CriticalWithin: a client that is not ready turns red this long
	// before the fork (if a ready release exists).
	CriticalWithin time.Duration
}

// ForkResult is the assessment plus the text for the FORK column.
type ForkResult struct {
	Assessment
	Cell string
}

// AssessForks checks fork readiness per layer. If the node reports its fork
// schedule, that answer decides; otherwise the client version is compared
// with the releases listed in forks.yaml (fallback, with a note).
//
// Node answer:
//
//	red:    the node is not on the latest active fork; it schedules a
//	        postponed fork at its former time; it schedules a fork that
//	        forks.yaml does not know within CriticalWithin (or already
//	        activated one); not ready for the next fork within
//	        CriticalWithin and a ready release exists
//	yellow: a fork unknown to forks.yaml further ahead (forks.yaml may be
//	        outdated); not ready for the next fork further ahead or no
//	        ready release known
//
// Version fallback:
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
	c := &forkCheck{in: in, n: n, r: &r}
	c.next, c.hasNext = n.Next(in.Now)

	for _, l := range []layer{
		{short: "EL", name: "execution", v: in.Execution},
		{short: "CL", name: "consensus", v: in.Consensus},
	} {
		switch {
		case l.short == "EL" && in.ExecutionConfig != nil:
			c.executionNode(l, *in.ExecutionConfig)
		case l.short == "CL" && in.ConsensusConfig != nil:
			c.consensusNode(l, *in.ConsensusConfig)
		default:
			err := in.ExecutionConfigErr
			if l.short == "CL" {
				err = in.ConsensusConfigErr
			}
			if err != nil {
				r.add(OK, "fork check of the %s client by version only: %v", l.name, err)
			}
			c.byVersion(l)
		}
	}

	switch {
	case c.cellProblem != "":
		r.Cell = c.cellProblem
	case c.hasNext && len(c.nextParts) == 0:
		r.Cell = fmt.Sprintf("%s in %s: ready", c.next.Name, days(c.next.Time.Sub(in.Now)))
	case c.hasNext:
		r.Cell = fmt.Sprintf("%s in %s: %s", c.next.Name, days(c.next.Time.Sub(in.Now)), strings.Join(c.nextParts, ", "))
	default:
		r.Cell = "none scheduled"
	}
	return r
}

// layer is one side of a node.
type layer struct {
	short, name string // "EL", "execution"
	v           ethereum.ClientVersion
}

// subject names the client in messages: "Geth 1.17.9" if the version is
// known, otherwise "the execution client".
func (l layer) subject() string {
	if l.v.Client != ethereum.Unknown && l.v.HasVersion() {
		return fmt.Sprintf("%s %s", l.v.Client, l.v.Version)
	}
	return "the " + l.name + " client"
}

// forkCheck collects findings and the FORK cell for one node.
type forkCheck struct {
	in      ForkInput
	n       forks.Network
	r       *ForkResult
	next    forks.Fork
	hasNext bool
	// cellProblem is the first problem found; it replaces the readiness
	// text in the FORK column.
	cellProblem string
	nextParts   []string // per-layer readiness for the next fork
}

func (c *forkCheck) problem(format string, a ...any) {
	if c.cellProblem == "" {
		c.cellProblem = fmt.Sprintf(format, a...)
	}
}

// notReady reports a layer that, per the node's own answer, is not ready
// for the next fork. Red within CriticalWithin, but only if a ready
// release is known; otherwise yellow.
func (c *forkCheck) notReady(l layer, why string) {
	until := c.next.Time.Sub(c.in.Now)
	min, haveRelease := releases.Version{}, false
	if l.v.Client != ethereum.Unknown {
		min, haveRelease = minVersionFor(c.n, c.next, l.v.Client)
	}
	running, parsed := releases.ParseVersion(l.v.Version)
	running.Suffix = l.v.Suffix
	recentEnough := haveRelease && parsed && releases.Compare(l.v.Client, running, min) >= 0
	level := Warn
	if haveRelease && until <= c.in.CriticalWithin {
		level = Crit
	}
	msg := fmt.Sprintf("%s is not ready for %s on %s (%s, in %s): %s",
		l.subject(), c.next.Name, c.n.Name, c.next.Time.Format(time.DateOnly), days(until), why)
	switch {
	case recentEnough:
		// The release supports the fork, but this node does not schedule
		// it: a configuration problem, e.g. an override or custom genesis.
		msg += fmt.Sprintf("; the release supports it (minimum %s), so check the node's chain configuration", min)
	case haveRelease:
		msg += fmt.Sprintf("; needs %s or later", min)
	case l.v.Client == ethereum.Unknown:
		msg += "; client not recognized, so it is unknown whether a ready release exists"
	default:
		msg += fmt.Sprintf("; no %s release announced yet (waiting for client release)", l.v.Client)
	}
	c.r.add(level, "%s", msg)
	c.nextParts = append(c.nextParts, l.short+" not ready")
}

// unknownFork reports a fork the node schedules that forks.yaml does not
// list. That happens right after a new announcement, before x3f is
// updated, so it is yellow; red if it is due within CriticalWithin or has
// already passed.
func (c *forkCheck) unknownFork(l layer, what string, at time.Time) {
	until := at.Sub(c.in.Now)
	level, when := Warn, fmt.Sprintf("in %s", days(until))
	if until <= c.in.CriticalWithin {
		level = Crit
	}
	if until <= 0 {
		when = "already passed"
	}
	c.r.add(level, "%s schedules %s at %s (%s) that forks.yaml (updated %s) does not list: forks.yaml may be outdated (update x3f or pass -forks), otherwise the node runs a wrong chain configuration",
		l.subject(), what, at.Format("2006-01-02 15:04 UTC"), when, c.in.Schedule.Updated.Format(time.DateOnly))
	c.problem("%s schedules unknown fork", l.short)
}

// oldSchedule reports that the node still schedules a postponed fork.
func (c *forkCheck) oldSchedule(l layer, p forks.Fork, detail string) {
	msg := fmt.Sprintf("%s still schedules the postponed %s at its former time (%s, %s)",
		l.subject(), p.Name, p.Time.Format("2006-01-02 15:04 UTC"), detail)
	if fixed, ok := p.RemovedIn[l.v.Client]; ok {
		msg += fmt.Sprintf("; update to %s or later", fixed)
	}
	if !p.Time.After(c.in.Now) {
		msg += "; the former activation time has passed, so the node has most likely left the chain"
	}
	c.r.add(Crit, "%s", msg)
	c.problem("%s has old %s schedule", l.short, p.Name)
}

// executionNode checks the eth_config answer.
func (c *forkCheck) executionNode(l layer, cfg ethereum.ExecutionForkConfig) {
	n, now := c.n, c.in.Now
	byTime := func(ts int64, postponed bool) (forks.Fork, bool) {
		for _, f := range n.Forks {
			if f.Postponed == postponed && f.Time.Unix() == ts {
				return f, true
			}
		}
		return forks.Fork{}, false
	}
	at := func(ts int64) time.Time { return time.Unix(ts, 0).UTC() }

	// The fork in effect must be the latest active one.
	if active := n.Active(now); len(active) > 0 {
		want := active[len(active)-1]
		cur := cfg.Current.ActivationTime
		if cur != want.Time.Unix() {
			if p, ok := byTime(cur, true); ok {
				c.oldSchedule(l, p, "eth_config current")
			} else if f, ok := byTime(cur, false); ok || cur < want.Time.Unix() {
				on := "a fork activated at " + at(cur).Format("2006-01-02 15:04 UTC")
				if ok {
					on = f.Name
				}
				c.r.add(Crit, "%s is still on %s (eth_config), but %s is active since %s: it cannot follow %s",
					l.subject(), on, want.Name, want.Time.Format(time.DateOnly), n.Name)
				c.problem("%s not on %s", l.short, want.Name)
			} else {
				c.unknownFork(l, "a fork", at(cur))
			}
		}
	}

	// Future forks the node knows: next and last (often the same).
	var future []int64
	for _, a := range []*ethereum.ForkActivation{cfg.Next, cfg.Last} {
		if a != nil && !slices.Contains(future, a.ActivationTime) {
			future = append(future, a.ActivationTime)
		}
	}
	for _, ts := range future {
		if _, ok := byTime(ts, false); ok {
			continue
		}
		if p, ok := byTime(ts, true); ok {
			c.oldSchedule(l, p, "eth_config")
			continue
		}
		c.unknownFork(l, "a fork", at(ts))
	}

	if c.hasNext {
		switch {
		case cfg.Next == nil:
			c.notReady(l, "eth_config shows no next fork")
		case cfg.Next.ActivationTime != c.next.Time.Unix():
			c.notReady(l, fmt.Sprintf("eth_config shows the next fork at %s, not at %s",
				at(cfg.Next.ActivationTime).Format("2006-01-02 15:04 UTC"), c.next.Time.Format("2006-01-02 15:04 UTC")))
		}
	}
}

// consensusNode checks the fork epochs from /eth/v1/config/spec.
func (c *forkCheck) consensusNode(l layer, cfg ethereum.ConsensusForkConfig) {
	n, now := c.n, c.in.Now
	if len(n.Forks) == 0 {
		return
	}
	oldest := n.Forks[0].Epoch

	// state describes how the node schedules fork f: "" if as in
	// forks.yaml, otherwise why not. epoch is the node's epoch for f's
	// consensus fork name (if it has one).
	state := func(f forks.Fork) (why string, epoch uint64, scheduled bool) {
		if f.BPO {
			if slices.Contains(cfg.BlobSchedule, f.Epoch) {
				return "", f.Epoch, true
			}
			return fmt.Sprintf("BLOB_SCHEDULE has no entry for epoch %d", f.Epoch), 0, false
		}
		key := strings.ToUpper(f.Consensus)
		ep, ok := cfg.Epochs[key]
		switch {
		case !ok:
			return fmt.Sprintf("the client does not know %s (no %s_FORK_EPOCH)", f.Consensus, key), 0, false
		case ep == ethereum.FarFutureEpoch:
			return fmt.Sprintf("%s is not scheduled (%s_FORK_EPOCH is far future)", f.Consensus, key), 0, false
		case ep != f.Epoch:
			return fmt.Sprintf("%s_FORK_EPOCH is %d, not %d", key, ep, f.Epoch), ep, true
		}
		return "", ep, true
	}

	// Active forks must all be in place; report the oldest one missing.
	for _, f := range n.Active(now) {
		if why, _, _ := state(f); why != "" {
			c.r.add(Crit, "%s is not on %s (%s), which is active since %s: it cannot follow %s",
				l.subject(), f.Name, why, f.Time.Format(time.DateOnly), n.Name)
			c.problem("%s not on %s", l.short, f.Name)
			break
		}
	}

	// Postponed forks must not be scheduled at their former epoch. A
	// different real epoch means a new date that forks.yaml lacks.
	for _, p := range n.Postponed() {
		why, ep, scheduled := state(p)
		switch {
		case why == "":
			c.oldSchedule(l, p, fmt.Sprintf("epoch %d", p.Epoch))
		case scheduled && ep >= oldest:
			c.unknownFork(l, fmt.Sprintf("%s at epoch %d", p.Consensus, ep), n.EpochTime(ep))
		}
	}

	// Later scheduled forks (after the next one) at a different epoch.
	for _, f := range n.Forks {
		if f.Postponed || !f.Time.After(now) || (c.hasNext && f.Name == c.next.Name) {
			continue
		}
		if why, ep, scheduled := state(f); why != "" && scheduled {
			c.unknownFork(l, fmt.Sprintf("%s at epoch %d", f.Consensus, ep), n.EpochTime(ep))
		}
	}

	// Forks the node schedules that forks.yaml does not know at all.
	known := map[string]bool{}
	knownEpochs := map[uint64]bool{}
	for _, f := range n.Forks {
		if !f.BPO {
			known[strings.ToUpper(f.Consensus)] = true
		}
		if !f.Postponed {
			knownEpochs[f.Epoch] = true
		}
	}
	// maps.Keys yields the keys in random order; sorted for stable output.
	for _, name := range slices.Sorted(maps.Keys(cfg.Epochs)) {
		ep := cfg.Epochs[name]
		knownEpochs[ep] = true // BLOB_SCHEDULE entries at hard forks are no forks of their own
		if ep == ethereum.FarFutureEpoch || ep < oldest || known[name] {
			continue
		}
		c.unknownFork(l, fmt.Sprintf("%s at epoch %d", titleCase(name), ep), n.EpochTime(ep))
	}
	for _, ep := range cfg.BlobSchedule {
		if ep >= oldest && !knownEpochs[ep] {
			c.unknownFork(l, fmt.Sprintf("a blob parameter change at epoch %d", ep), n.EpochTime(ep))
		}
	}

	if c.hasNext {
		if why, _, _ := state(c.next); why != "" {
			c.notReady(l, why)
		}
	}
}

// titleCase turns "HEZE" into "Heze".
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return s[:1] + strings.ToLower(s[1:])
}

// byVersion is the fallback: it compares the client version with the
// releases listed in forks.yaml.
func (c *forkCheck) byVersion(l layer) {
	n, r, v := c.n, c.r, l.v
	running, ok := releases.ParseVersion(v.Version)
	if v.Client == ethereum.Unknown || !v.HasVersion() || !ok {
		if c.hasNext && v.Raw != "" {
			r.add(Warn, "%s client not recognized: readiness for %s unknown", l.name, c.next.Name)
			c.nextParts = append(c.nextParts, l.short+" unknown")
		}
		return
	}
	running.Suffix = v.Suffix
	name := l.subject()

	// a) Below the minimum of the latest active fork that lists this
	// client: the node cannot follow the chain.
	active := n.Active(c.in.Now)
	for i := len(active) - 1; i >= 0; i-- {
		min, ok := active[i].MinVersions[v.Client]
		if !ok {
			continue
		}
		if releases.Compare(v.Client, running, min) < 0 {
			r.add(Crit, "%s is older than the minimum release for %s (%s): it cannot follow %s",
				name, active[i].Name, min, n.Name)
			c.problem("%s below %s minimum", l.short, active[i].Name)
		}
		break
	}

	// b) Still contains the former schedule of a postponed fork: only
	// releases from scheduled_in up to (not including) removed_in.
	for _, p := range n.Postponed() {
		from, ok1 := p.ScheduledIn[v.Client]
		fixed, ok2 := p.RemovedIn[v.Client]
		if !ok1 || !ok2 || releases.Compare(v.Client, running, from) < 0 || releases.Compare(v.Client, running, fixed) >= 0 {
			continue
		}
		msg := fmt.Sprintf("%s still contains the postponed %s schedule (%s); update to %s or later",
			name, p.Name, p.Time.Format("2006-01-02 15:04 UTC"), fixed)
		if !p.Time.After(c.in.Now) {
			msg += "; the former activation time has passed, so the node has most likely left the chain"
		}
		r.add(Crit, "%s", msg)
		c.problem("%s has old %s schedule", l.short, p.Name)
	}

	// c) Readiness for the next fork.
	if !c.hasNext {
		return
	}
	until := c.next.Time.Sub(c.in.Now)
	min, ok := minVersionFor(n, c.next, v.Client)
	switch {
	case !ok:
		r.add(Warn, "%s on %s in %s: no %s release announced yet (waiting for client release)",
			c.next.Name, n.Name, days(until), v.Client)
		c.nextParts = append(c.nextParts, l.short+" waiting for release")
	case releases.Compare(v.Client, running, min) < 0:
		level := Warn
		if until <= c.in.CriticalWithin {
			level = Crit
		}
		r.add(level, "%s is not ready for %s on %s (%s, in %s): needs %s or later",
			name, c.next.Name, n.Name, c.next.Time.Format(time.DateOnly), days(until), min)
		c.nextParts = append(c.nextParts, l.short+" not ready")
	}
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
