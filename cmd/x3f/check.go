package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
)

// layerResult holds what we learned about one client (execution or
// consensus) of a node. S is the type of the sync status; Go generics let
// both layers share this struct and the code that fills it.
type layerResult[S any] struct {
	Version    ethereum.ClientVersion
	VersionErr error
	Sync       S
	SyncErr    error
	// SyncSkipped is set when the version request could not reach the
	// client at all (timeout, connection or DNS failure). The sync request
	// would fail the same way and wait for the same timeout, so it is not
	// sent. Errors the server returned do not skip it: some proxies block
	// single methods such as web3_clientVersion but allow eth_syncing.
	SyncSkipped bool
}

// nodeResult holds the results for one configured node. It deliberately has
// no URL fields: everything that is printed comes from here, so endpoint
// URLs (which may contain API keys) cannot end up in the output.
type nodeResult struct {
	Name      string
	Execution layerResult[ethereum.ExecutionSyncStatus]
	Consensus layerResult[ethereum.ConsensusSyncStatus]
}

// checkNodes queries all nodes concurrently and returns one result per node,
// in config order. Each request gets its own timeout.
func checkNodes(ctx context.Context, client *ethereum.Client, nodes []config.Node, timeout time.Duration) []nodeResult {
	results := make([]nodeResult, len(nodes))

	// A WaitGroup waits for a set of goroutines to finish. wg.Go starts f
	// in a new goroutine and tracks it; wg.Wait blocks until all are done.
	var wg sync.WaitGroup
	for i, n := range nodes {
		results[i].Name = n.Name
		// Since Go 1.22 every loop iteration has its own i and n, so the
		// goroutines below capture this iteration's values.
		//
		// Execution and consensus side run in parallel. The two goroutines
		// write to different fields of results[i]; distinct fields are
		// distinct memory, so no mutex is needed.
		wg.Go(func() {
			results[i].Execution = queryLayer(ctx, timeout, n.ExecutionURL,
				client.ExecutionVersion, client.ExecutionSync)
		})
		wg.Go(func() {
			results[i].Consensus = queryLayer(ctx, timeout, n.BeaconURL,
				client.ConsensusVersion, client.ConsensusSync)
		})
	}
	wg.Wait()
	return results
}

// queryLayer asks one client for its version and then its sync status.
// The sync request is skipped only if the client was unreachable.
// fetchVersion and fetchSync are method values such as
// client.ExecutionVersion: functions bound to their receiver, which can be
// passed around like any other function.
func queryLayer[S any](ctx context.Context, timeout time.Duration, endpoint string,
	fetchVersion func(context.Context, string) (string, error),
	fetchSync func(context.Context, string) (S, error)) layerResult[S] {

	var r layerResult[S]

	raw, err := withTimeout(ctx, timeout, endpoint, fetchVersion)
	if err != nil {
		r.VersionErr = err
		if ethereum.IsTransportError(err) {
			r.SyncSkipped = true
			return r
		}
	} else {
		r.Version = ethereum.ParseClientVersion(raw)
	}
	r.Sync, r.SyncErr = withTimeout(ctx, timeout, endpoint, fetchSync)
	return r
}

// withTimeout runs fetch with its own timeout. T is a type parameter: the
// same function works for fetchers returning a string or a sync status.
func withTimeout[T any](ctx context.Context, timeout time.Duration, endpoint string,
	fetch func(context.Context, string) (T, error)) (T, error) {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	// cancel releases the timer as soon as the request is done.
	defer cancel()
	return fetch(ctx, endpoint)
}

// printResults writes the results as an aligned table, followed by a list of
// errors. It reports how many requests failed.
func printResults(w io.Writer, results []nodeResult) (failed int, err error) {
	// tabwriter aligns tab-separated columns. Arguments: minwidth, tabwidth,
	// padding, padchar, flags.
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "NODE\tEXECUTION\tEL SYNC\tCONSENSUS\tCL SYNC")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.Name,
			formatVersion(r.Execution.Version, r.Execution.VersionErr),
			formatSync(r.Execution.SyncSkipped, r.Execution.SyncErr, func() string { return formatExecutionSync(r.Execution.Sync) }),
			formatVersion(r.Consensus.Version, r.Consensus.VersionErr),
			formatSync(r.Consensus.SyncSkipped, r.Consensus.SyncErr, func() string { return formatConsensusSync(r.Consensus.Sync) }),
		)
	}
	if err := tw.Flush(); err != nil {
		return 0, err
	}

	var lines []string
	add := func(node, what string, err error) {
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %s %s: %v", node, what, err))
		}
	}
	for _, r := range results {
		add(r.Name, "execution", r.Execution.VersionErr)
		add(r.Name, "execution sync", r.Execution.SyncErr)
		add(r.Name, "consensus", r.Consensus.VersionErr)
		add(r.Name, "consensus sync", r.Consensus.SyncErr)
	}
	if len(lines) > 0 {
		fmt.Fprintln(w, "\nErrors:")
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}
	return len(lines), nil
}

// formatVersion renders a version cell, e.g. "Geth 1.17.10 (stable)".
func formatVersion(v ethereum.ClientVersion, err error) string {
	if err != nil {
		return "ERROR"
	}
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

// formatSync handles the cases shared by both sync columns; format is only
// called when there is a status to show.
func formatSync(skipped bool, err error, format func() string) string {
	switch {
	case skipped:
		return "-"
	case err != nil:
		return "ERROR"
	default:
		return format()
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

// plural formats a count with a singular or plural noun: "1 block", "2 blocks".
func plural(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
