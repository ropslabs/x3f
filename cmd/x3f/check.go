package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
)

// clientResult is the outcome of querying one client of a node.
type clientResult struct {
	Version ethereum.ClientVersion
	Err     error
}

// nodeResult holds the results for one configured node. It deliberately has
// no URL fields: everything that is printed comes from here, so endpoint
// URLs (which may contain API keys) cannot end up in the output.
type nodeResult struct {
	Name      string
	Execution clientResult
	Consensus clientResult
}

// checkNodes queries all nodes concurrently and returns one result per node,
// in config order. Each request gets its own timeout.
func checkNodes(ctx context.Context, client *ethereum.Client, nodes []config.Node, timeout time.Duration) []nodeResult {
	results := make([]nodeResult, len(nodes))

	// A WaitGroup waits for a set of goroutines to finish. wg.Go starts f
	// in a new goroutine and tracks it; wg.Wait blocks until all are done.
	var wg sync.WaitGroup
	for i, n := range nodes {
		// Since Go 1.22 every loop iteration has its own i and n, so the
		// goroutine below captures this iteration's values.
		wg.Go(func() {
			// Each goroutine writes only to its own slice element, so no
			// mutex is needed.
			results[i] = nodeResult{
				Name:      n.Name,
				Execution: query(ctx, timeout, n.ExecutionURL, client.ExecutionVersion),
				Consensus: query(ctx, timeout, n.BeaconURL, client.ConsensusVersion),
			}
		})
	}
	wg.Wait()
	return results
}

// query runs one version request with a timeout and parses the result.
// fetch is a method value such as client.ExecutionVersion: a function bound
// to its receiver, which can be passed around like any other function.
func query(ctx context.Context, timeout time.Duration, endpoint string,
	fetch func(context.Context, string) (string, error)) clientResult {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	// cancel releases the timer as soon as the request is done.
	defer cancel()

	raw, err := fetch(ctx, endpoint)
	if err != nil {
		return clientResult{Err: err}
	}
	return clientResult{Version: ethereum.ParseClientVersion(raw)}
}

// printResults writes the results as an aligned table, followed by a list of
// errors. It reports how many requests failed.
func printResults(w io.Writer, results []nodeResult) (failed int, err error) {
	// tabwriter aligns tab-separated columns. Arguments: minwidth, tabwidth,
	// padding, padchar, flags.
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "NODE\tEXECUTION\tCONSENSUS")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, formatClient(r.Execution), formatClient(r.Consensus))
	}
	if err := tw.Flush(); err != nil {
		return 0, err
	}

	var lines []string
	for _, r := range results {
		if r.Execution.Err != nil {
			lines = append(lines, fmt.Sprintf("  %s execution: %v", r.Name, r.Execution.Err))
		}
		if r.Consensus.Err != nil {
			lines = append(lines, fmt.Sprintf("  %s consensus: %v", r.Name, r.Consensus.Err))
		}
	}
	if len(lines) > 0 {
		fmt.Fprintln(w, "\nErrors:")
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}
	return len(lines), nil
}

// formatClient renders one table cell, e.g. "Geth 1.17.10 (stable)".
func formatClient(r clientResult) string {
	if r.Err != nil {
		return "ERROR"
	}
	v := r.Version
	if v.Client == ethereum.Unknown || !v.HasVersion() {
		// Show what the client reported, so the user can still see it.
		return fmt.Sprintf("unrecognized: %q", v.Raw)
	}
	s := v.Client.String() + " " + v.Version
	if v.Suffix != "" {
		s += " (" + v.Suffix + ")"
	}
	return s
}
