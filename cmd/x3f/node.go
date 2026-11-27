package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ropslabs/x3f/internal/aztec"
	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/software"
	"github.com/ropslabs/x3f/internal/status"
)

// checker checks one configured node. Every node type implements it, and
// newChecker picks the implementation for the type in the config. Types are
// compiled in, not loaded as plugins. The run loop, the report, the
// notifications and the metrics only use this interface, so a new type
// does not change them.
//
// An interface in Go is a set of methods; any type that has these methods
// satisfies it, without declaring so.
type checker interface {
	// name is the node name from the config.
	name() string
	// typ is the node type from the config, e.g. "ethereum".
	typ() string
	// columns are the table headings after STATUS and NODE. They are the
	// same for every node of a type; each row has one cell per column.
	columns() []string
	// query asks the node for everything the checks need. It is called
	// once, concurrently with the other nodes.
	query(ctx context.Context, timeout time.Duration)
	// software lists what query found on the node, in a fixed order.
	// Releases and advisories are fetched for it.
	software() []installed
	// assess evaluates the result of query.
	assess(up upstream, p assessParams) row
}

// linker is implemented by checkers whose row depends on other nodes, such
// as an Aztec node on its L1 node. link completes the node's own row from
// the rows of the other nodes, by node name, after every node is assessed.
// Links go one level deep: a linked row is not itself linked to.
type linker interface {
	link(own *row, rows map[string]row)
}

// installed is one piece of software on a node, e.g. its execution client.
type installed struct {
	// component is its role on the node, e.g. "execution". It is used as
	// a metric label.
	component string
	// version is what the node reported. Its Name is Unknown if the
	// software was not recognized or could not be queried.
	version software.Reported
}

// upstream holds the data fetched from the software's repositories.
type upstream struct {
	rel map[software.Repo]releaseData
	adv map[software.Repo]advisoryData
}

// versionInput is the input of status.AssessVersion for a version a node
// reported (err is the error of the request).
func (up upstream) versionInput(v software.Reported, err error, p assessParams) status.VersionInput {
	in := status.VersionInput{Version: v, Err: err, Now: p.now, Grace: p.grace}
	if repo, ok := software.ReleaseRepo(v.Name); ok {
		d := up.rel[repo]
		in.Releases, in.ReleasesErr = d.result, d.err
	}
	return in
}

// advisoryInput is the input of status.AssessAdvisories, with the merged
// ignore list of the node. Whether a fix is released is checked against
// the software's own releases: those are what an operator can install.
func (up upstream) advisoryInput(v software.Reported, err error, ignore []config.AdvisoryIgnore, p assessParams) status.AdvisoryInput {
	in := status.AdvisoryInput{Version: v, Err: err, Ignore: ignore, Now: p.now}
	if repo, ok := software.AdvisoryRepo(v.Name); ok {
		d := up.adv[repo]
		in.Advisories, in.AdvisoriesErr = d.result, d.err
	}
	if repo, ok := software.ReleaseRepo(v.Name); ok {
		in.Releases = up.rel[repo].result
	}
	return in
}

// clients are the protocol clients of the node types. Tests replace them
// to control the network.
type clients struct {
	ethereum *ethereum.Client
	aztec    *aztec.Client
}

// newChecker returns the checker for a node; nodes is the whole config,
// for references between nodes. config.Validate has already rejected
// unknown types and references.
func newChecker(n config.Node, nodes []config.Node, c clients) checker {
	switch n.Type {
	case config.TypeEthereum:
		return &ethereumNode{cfg: n, client: c.ethereum}
	case config.TypeAztec:
		return newAztecNode(n, nodes, c.aztec)
	}
	// Unreachable after validation; a panic shows a missing case at once.
	panic(fmt.Sprintf("node %q: no checker for type %q", n.Name, n.Type))
}

// queryAll queries all nodes concurrently.
func queryAll(ctx context.Context, nodes []checker, timeout time.Duration) {
	// A WaitGroup waits for a set of goroutines to finish. wg.Go starts f
	// in a new goroutine and tracks it; wg.Wait blocks until all are done.
	var wg sync.WaitGroup
	for _, n := range nodes {
		// Each checker writes only to itself, so no mutex is needed.
		wg.Go(func() { n.query(ctx, timeout) })
	}
	wg.Wait()
}

// allInstalled returns the software of all nodes, node by node.
func allInstalled(nodes []checker) []installed {
	var all []installed
	for _, n := range nodes {
		all = append(all, n.software()...)
	}
	return all
}
