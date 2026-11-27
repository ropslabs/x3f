package main

import (
	"context"
	"time"

	"github.com/ropslabs/x3f/internal/aztec"
	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/software"
	"github.com/ropslabs/x3f/internal/status"
)

// The Aztec node type: one piece of software, the node itself. Its L1
// (cfg.L1 or cfg.L1URL) is validated by config, but not used yet.

// aztecNode checks an Aztec node. Like nodeResult, it has no URL field of
// its own: the URL stays in cfg and is only passed to the client.
type aztecNode struct {
	cfg    config.Node
	client *aztec.Client
	// version is what the node reported; zero if the request failed.
	version software.Reported
	// err is the error of the getNodeInfo request.
	err error
}

func (n *aztecNode) name() string { return n.cfg.Name }
func (n *aztecNode) typ() string  { return config.TypeAztec }

func (n *aztecNode) columns() []string {
	return []string{"VERSION", "ADVISORIES"}
}

func (n *aztecNode) software() []installed {
	return []installed{{component: "node", version: n.version}}
}

// query asks the node for its version.
func (n *aztecNode) query(ctx context.Context, timeout time.Duration) {
	info, err := withTimeout(ctx, timeout, n.cfg.URL, n.client.NodeInfo)
	if err != nil {
		n.err = err
		return
	}
	n.version = aztec.ParseNodeVersion(info.NodeVersion)
}

// assess applies the same version and advisory rules as for Ethereum
// clients.
func (n *aztecNode) assess(up upstream, p assessParams) row {
	vi := up.versionInput(n.version, n.err, p)
	ignore := config.MergeIgnores(p.ignore, n.cfg.IgnoreAdvisories)
	adv := status.AssessAdvisories(up.advisoryInput(n.version, n.err, ignore, p))
	ignoreCheck, ignoreRan := checkIgnoreList(n.cfg.IgnoreAdvisories, n.software(), up.adv, false)
	advCell := adv.Cell
	if advCell == "" {
		advCell = "-"
	}
	rw := row{
		cells: []string{formatVersion(n.version, n.err, vi), advCell},
		findings: []layerFindings{
			{"version", status.AssessVersion(vi), false},
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
