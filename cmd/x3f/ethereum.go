package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/network"
	"github.com/ropslabs/x3f/internal/software"
	"github.com/ropslabs/x3f/internal/status"
)

// The Ethereum node type: an execution client and a consensus client.

// layerResult holds what we learned about one client (execution or
// consensus) of a node. S is the type of the sync status and F the type of
// the fork configuration; Go generics let both layers share this struct
// and the code that fills it.
type layerResult[S, F any] struct {
	Version    software.Reported
	VersionErr error
	Sync       S
	SyncErr    error
	ChainID    uint64
	ChainIDErr error
	// ForkConfig is the fork schedule the client reports; nil if the
	// request failed (ForkConfigErr) or was skipped.
	ForkConfig    *F
	ForkConfigErr error
	// Unreachable is set when the version request could not reach the
	// client at all (timeout, connection or DNS failure). The sync and
	// chain ID requests would fail the same way and wait for the same
	// timeout, so they are not sent. Errors the server returned do not
	// skip them: some proxies block single methods such as
	// web3_clientVersion but allow eth_syncing.
	Unreachable bool
}

// nodeResult holds the results for one configured node. It deliberately has
// no URL fields: everything that is printed comes from here, so endpoint
// URLs (which may contain API keys) cannot end up in the output.
type nodeResult struct {
	Name string
	// Network is the expected network from the config ("" if not set).
	Network string
	// Ignore is the node's own ignore_advisories list (without the
	// global entries).
	Ignore    []config.AdvisoryIgnore
	Execution layerResult[ethereum.ExecutionSyncStatus, ethereum.ExecutionForkConfig]
	Consensus layerResult[ethereum.ConsensusSyncStatus, ethereum.ConsensusForkConfig]
}

// ethereumNode checks an Ethereum node: an execution client and a
// consensus client.
type ethereumNode struct {
	cfg    config.Node
	client *ethereum.Client
	r      nodeResult
}

func (n *ethereumNode) name() string { return n.cfg.Name }
func (n *ethereumNode) typ() string  { return config.TypeEthereum }

func (n *ethereumNode) columns() []string {
	return []string{"NETWORK", "EXECUTION", "EL SYNC", "CONSENSUS", "CL SYNC", "FORK", "ADVISORIES"}
}

func (n *ethereumNode) software() []installed {
	return []installed{
		{component: "execution", version: n.r.Execution.Version},
		{component: "consensus", version: n.r.Consensus.Version},
	}
}

// query asks both clients; each request gets its own timeout.
func (n *ethereumNode) query(ctx context.Context, timeout time.Duration) {
	cfg, client := n.cfg, n.client
	n.r = nodeResult{Name: cfg.Name, Network: cfg.Network, Ignore: cfg.IgnoreAdvisories}
	// Execution and consensus side run in parallel. The two goroutines
	// write to different fields of n.r; distinct fields are distinct
	// memory, so no mutex is needed.
	var wg sync.WaitGroup
	wg.Go(func() {
		n.r.Execution = queryLayer(ctx, timeout, cfg.ExecutionURL,
			client.ExecutionVersion, client.ExecutionSync, client.ExecutionChainID, client.ExecutionForkConfig)
	})
	wg.Go(func() {
		n.r.Consensus = queryLayer(ctx, timeout, cfg.BeaconURL,
			client.ConsensusVersion, client.ConsensusSync, client.ConsensusChainID, client.ConsensusForkConfig)
	})
	wg.Wait()
}

// queryLayer asks one client for its version, then its sync status, chain
// ID and fork configuration. The later requests are skipped only if the
// client was unreachable.
// fetchVersion and fetchSync are method values such as
// client.ExecutionVersion: functions bound to their receiver, which can be
// passed around like any other function.
func queryLayer[S, F any](ctx context.Context, timeout time.Duration, endpoint string,
	fetchVersion func(context.Context, string) (string, error),
	fetchSync func(context.Context, string) (S, error),
	fetchChainID func(context.Context, string) (uint64, error),
	fetchForkConfig func(context.Context, string) (F, error)) layerResult[S, F] {

	var r layerResult[S, F]

	raw, err := withTimeout(ctx, timeout, endpoint, fetchVersion)
	if err != nil {
		r.VersionErr = err
		if httpjson.IsTransportError(err) {
			r.Unreachable = true
			return r
		}
	} else {
		r.Version = ethereum.ParseClientVersion(raw)
	}
	r.Sync, r.SyncErr = withTimeout(ctx, timeout, endpoint, fetchSync)
	r.ChainID, r.ChainIDErr = withTimeout(ctx, timeout, endpoint, fetchChainID)
	if cfg, err := withTimeout(ctx, timeout, endpoint, fetchForkConfig); err != nil {
		r.ForkConfigErr = err
	} else {
		r.ForkConfig = &cfg
	}
	return r
}

// assess evaluates the node's result.
func (n *ethereumNode) assess(up upstream, p assessParams) row {
	now := p.now
	r := n.r
	ev := up.versionInput(r.Execution.Version, r.Execution.VersionErr, p)
	cv := up.versionInput(r.Consensus.Version, r.Consensus.VersionErr, p)
	chainID, haveChain := detectedChain(r)
	fork := status.AssessForks(status.ForkInput{
		Schedule: p.schedule, ChainID: chainID, HaveChain: haveChain,
		Execution: r.Execution.Version, Consensus: r.Consensus.Version,
		ExecutionConfig: r.Execution.ForkConfig, ExecutionConfigErr: r.Execution.ForkConfigErr,
		ConsensusConfig: r.Consensus.ForkConfig, ConsensusConfigErr: r.Consensus.ForkConfigErr,
		Now: now, CriticalWithin: p.forkCriticalIn,
	})
	ignore := config.MergeIgnores(p.ignore, r.Ignore)
	ea := status.AssessAdvisories(up.advisoryInput(r.Execution.Version, r.Execution.VersionErr, ignore, p))
	ca := status.AssessAdvisories(up.advisoryInput(r.Consensus.Version, r.Consensus.VersionErr, ignore, p))
	ignoreCheck, ignoreRan := checkIgnoreList(r.Ignore, n.software(), up.adv, false)
	a := []layerFindings{
		{"network", status.AssessNetwork(status.NetworkInput{
			Expected:    r.Network,
			ExecutionID: r.Execution.ChainID, ExecutionErr: r.Execution.ChainIDErr, ExecutionSkipped: r.Execution.Unreachable,
			ConsensusID: r.Consensus.ChainID, ConsensusErr: r.Consensus.ChainIDErr, ConsensusSkipped: r.Consensus.Unreachable,
		}), r.Execution.Unreachable && r.Consensus.Unreachable},
		{"execution", status.AssessVersion(ev), false},
		{"execution sync", status.AssessExecutionSync(status.SyncInput{Skipped: r.Execution.Unreachable, Err: r.Execution.SyncErr}, r.Execution.Sync), r.Execution.Unreachable},
		{"consensus", status.AssessVersion(cv), false},
		{"consensus sync", status.AssessConsensusSync(status.SyncInput{Skipped: r.Consensus.Unreachable, Err: r.Consensus.SyncErr}, r.Consensus.Sync), r.Consensus.Unreachable},
		{"fork", fork.Assessment, fork.NotRun},
		// An empty cell means the client was not assessed.
		{"execution advisories", ea.Assessment, ea.Cell == ""},
		{"consensus advisories", ca.Assessment, ca.Cell == ""},
		// Without entries there is nothing to check, which is not
		// "could not run".
		{"ignore_advisories", ignoreCheck, len(r.Ignore) > 0 && !ignoreRan},
	}
	rw := row{
		cells: []string{
			formatNetwork(r),
			formatVersion(r.Execution.Version, r.Execution.VersionErr, ev),
			formatSync(r.Execution.Unreachable, r.Execution.SyncErr, func() string { return formatExecutionSync(r.Execution.Sync) }),
			formatVersion(r.Consensus.Version, r.Consensus.VersionErr, cv),
			formatSync(r.Consensus.Unreachable, r.Consensus.SyncErr, func() string { return formatConsensusSync(r.Consensus.Sync) }),
			fork.Cell,
			formatAdvisories(ea.Cell, ca.Cell),
		},
		findings: a,
	}
	for _, c := range []componentCounts{{"execution", ea.Counts}, {"consensus", ca.Counts}} {
		if c.counts != nil {
			rw.advisories = append(rw.advisories, c)
		}
	}
	if haveChain && p.schedule != nil {
		if net, ok := p.schedule.ForChainID(chainID); ok {
			if f, ok := net.Next(now); ok {
				rw.nextForks = []nextFork{{network: net.Name, fork: f.Name, at: f.Time}}
			}
		}
	}
	return rw
}

// detectedChain returns the node's chain ID for the fork check, preferring
// the execution side. If the two sides disagree, there is no single
// schedule to compare against: AssessNetwork already reports it as
// critical, and a fork check would only add follow-up errors.
func detectedChain(r nodeResult) (uint64, bool) {
	haveEL := !r.Execution.Unreachable && r.Execution.ChainIDErr == nil
	haveCL := !r.Consensus.Unreachable && r.Consensus.ChainIDErr == nil
	switch {
	case haveEL && haveCL && r.Execution.ChainID != r.Consensus.ChainID:
		return 0, false
	case haveEL:
		return r.Execution.ChainID, true
	case haveCL:
		return r.Consensus.ChainID, true
	}
	return 0, false
}

// formatAdvisories combines the advisory cells of both clients, e.g.
// "EL 5 (high)", "EL 1 (low), CL ?" or "none known". "-" means neither
// client could be assessed.
func formatAdvisories(el, cl string) string {
	var parts []string
	for _, p := range []struct{ layer, cell string }{{"EL", el}, {"CL", cl}} {
		if p.cell != "" && p.cell != "none known" {
			parts = append(parts, p.layer+" "+p.cell)
		}
	}
	switch {
	case len(parts) > 0:
		return strings.Join(parts, ", ")
	case el != "" || cl != "":
		return "none known"
	default:
		return "-"
	}
}

// formatNetwork renders the network cell: the network both clients are on,
// "EL x / CL y" if they disagree, the one that is known if only one side
// answered, "?" if detection failed and "-" if both are unreachable.
func formatNetwork(r nodeResult) string {
	el, cl := r.Execution, r.Consensus
	haveEL := !el.Unreachable && el.ChainIDErr == nil
	haveCL := !cl.Unreachable && cl.ChainIDErr == nil
	switch {
	case haveEL && haveCL && el.ChainID != cl.ChainID:
		return "EL " + network.Describe(el.ChainID) + " / CL " + network.Describe(cl.ChainID)
	case haveEL:
		return network.Describe(el.ChainID)
	case haveCL:
		return network.Describe(cl.ChainID)
	case el.Unreachable && cl.Unreachable:
		return "-"
	default:
		return "?"
	}
}

func formatExecutionSync(s ethereum.ExecutionSyncStatus) string {
	switch {
	case !s.Syncing:
		return "synced"
	case s.Indexing():
		return "indexing"
	}
	behind, ok := s.BlocksBehind()
	if !ok {
		return "syncing (target unknown)"
	}
	return fmt.Sprintf("syncing (%s behind)", plural(behind, "block"))
}

func formatConsensusSync(s ethereum.ConsensusSyncStatus) string {
	parts := []string{"synced"}
	if s.IsSyncing {
		parts[0] = fmt.Sprintf("syncing (%s behind)", plural(s.SyncDistance, "slot"))
	}
	if s.IsOptimistic != nil && *s.IsOptimistic {
		parts = append(parts, "optimistic")
	}
	if s.ELOffline != nil && *s.ELOffline {
		parts = append(parts, "EL offline")
	}
	return strings.Join(parts, ", ")
}
