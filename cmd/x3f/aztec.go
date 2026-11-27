package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ropslabs/x3f/internal/aztec"
	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/software"
	"github.com/ropslabs/x3f/internal/status"
)

// The Aztec node type: one piece of software, the node itself, and its L1
// (Ethereum), through which x3f reads the Aztec contracts.

// aztecNode checks an Aztec node. Like nodeResult, it keeps no URL that is
// printed: the node URL stays in cfg and the L1 URL in l1Endpoint, and
// both are only passed to the client.
type aztecNode struct {
	cfg    config.Node
	client *aztec.Client
	// l1Endpoint is the URL x3f reads L1 through: the execution_url of
	// the Ethereum node named in l1, or l1_url. Never printed.
	l1Endpoint string
	// l1Label names the L1 endpoint in messages, e.g. `L1 node "eth-1"`.
	l1Label string

	// info is the getNodeInfo answer; zero if the request failed.
	info aztec.NodeInfo
	// version is what the node reported; zero if the request failed.
	version software.Reported
	// err is the error of the getNodeInfo request.
	err error
	// rollup holds the L1 reads; only set if getNodeInfo worked.
	rollup status.RollupInput
}

// newAztecNode returns the checker of an Aztec node. nodes is the whole
// config, to find the Ethereum node that l1 names; config.Validate has
// checked that it exists.
func newAztecNode(n config.Node, nodes []config.Node, client *aztec.Client) *aztecNode {
	a := &aztecNode{cfg: n, client: client, l1Endpoint: n.L1URL, l1Label: "the l1_url endpoint"}
	if n.L1 != "" {
		a.l1Label = fmt.Sprintf("L1 node %q", n.L1)
		for _, e := range nodes {
			// config.Validate compares names without surrounding spaces.
			if e.Type == config.TypeEthereum && strings.TrimSpace(e.Name) == n.L1 {
				a.l1Endpoint = e.ExecutionURL
			}
		}
	}
	return a
}

func (n *aztecNode) name() string { return n.cfg.Name }
func (n *aztecNode) typ() string  { return config.TypeAztec }

func (n *aztecNode) columns() []string {
	return []string{"NETWORK", "VERSION", "ROLLUP", "ADVISORIES"}
}

func (n *aztecNode) software() []installed {
	return []installed{{component: "node", version: n.version}}
}

// query asks the node for its version and contracts, then reads the
// node's registry on L1. Each request gets its own timeout.
func (n *aztecNode) query(ctx context.Context, timeout time.Duration) {
	info, err := withTimeout(ctx, timeout, n.cfg.URL, n.client.NodeInfo)
	if err != nil {
		n.err = err
		return
	}
	n.info = info
	n.version = aztec.ParseNodeVersion(info.NodeVersion)

	r := status.RollupInput{
		NodeRollup: info.L1Contracts.Rollup, NodeVersion: info.RollupVersion, NodeL1ChainID: info.L1ChainID,
		L1: n.l1Label,
	}
	defer func() { n.rollup = r }()
	r.L1ChainID, r.L1ChainErr = withTimeout(ctx, timeout, n.l1Endpoint, n.client.L1ChainID)
	if r.L1ChainErr != nil || r.L1ChainID != info.L1ChainID {
		// The registry of the node's network is not on this chain.
		return
	}
	// The method values below need extra arguments; closures fit them to
	// the signature withTimeout expects.
	r.Canonical, r.CanonicalErr = withTimeout(ctx, timeout, n.l1Endpoint, func(ctx context.Context, ep string) (string, error) {
		return n.client.CanonicalRollup(ctx, ep, info.L1Contracts.Registry)
	})
	if r.CanonicalErr != nil {
		return
	}
	r.CanonicalVersion, r.CanonicalVersionErr = withTimeout(ctx, timeout, n.l1Endpoint, func(ctx context.Context, ep string) (uint64, error) {
		return n.client.RollupVersion(ctx, ep, r.Canonical)
	})
}

// assess applies the network and rollup rules of Aztec nodes and the same
// version and advisory rules as for Ethereum clients.
func (n *aztecNode) assess(up upstream, p assessParams) row {
	vi := up.versionInput(n.version, n.err, p)
	ignore := config.MergeIgnores(p.ignore, n.cfg.IgnoreAdvisories)
	adv := status.AssessAdvisories(up.advisoryInput(n.version, n.err, ignore, p))
	ignoreCheck, ignoreRan := checkIgnoreList(n.cfg.IgnoreAdvisories, n.software(), up.adv, false)
	advCell := adv.Cell
	if advCell == "" {
		advCell = "-"
	}

	// Without getNodeInfo there is nothing to compare; the version check
	// already reports why.
	var netCheck, rollupCheck status.Assessment
	netCell, rollupCell := "-", "-"
	rollupNotRun := true
	if n.err == nil {
		netCheck, netCell = status.AssessAztecNetwork(status.AztecNetworkInput{
			Expected: n.cfg.Network, L1ChainID: n.info.L1ChainID, Registry: n.info.L1Contracts.Registry,
		})
		rollupCheck, rollupCell, rollupNotRun = status.AssessRollup(n.rollup)
	}

	rw := row{
		cells: []string{netCell, formatVersion(n.version, n.err, vi), rollupCell, advCell},
		findings: []layerFindings{
			{"network", netCheck, n.err != nil},
			{"version", status.AssessVersion(vi), false},
			{"rollup", rollupCheck, rollupNotRun},
			// An empty cell means the node was not assessed.
			{"advisories", adv.Assessment, adv.Cell == ""},
			{"ignore_advisories", ignoreCheck, len(n.cfg.IgnoreAdvisories) > 0 && !ignoreRan},
		},
	}
	if adv.Counts != nil {
		rw.advisories = []componentCounts{{"node", adv.Counts}}
	}
	return rw
}
