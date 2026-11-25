package main

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/metrics"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/status"
)

// Metrics for the node_exporter textfile collector. Labels only carry
// values from a small, known set (node names from the config, check and
// client names, parsed version numbers, fork names from forks.yaml), never
// free text such as findings, titles or error messages.

// checkUnknown is the value of x3f_check_status for a check that could not
// run, like UNKNOWN (3) in the Nagios plugin convention.
const checkUnknown = 3

// severityLabels is the order of the severity label in x3f_advisories.
var severityLabels = []string{"critical", "high", "medium", "low", "none"}

// buildMetrics returns all metric families of a run.
func buildMetrics(results []nodeResult, rows []row, rel map[releases.Repo]releaseData, schedule *forks.Schedule, success bool, now time.Time) []metrics.Family {
	nodeStatus := metrics.Family{Name: "x3f_node_status", Help: "Status of a node: 0 OK, 1 WARN, 2 CRIT."}
	checkStatus := metrics.Family{Name: "x3f_check_status", Help: "Status of one check of a node: 0 OK, 1 WARN, 2 CRIT, 3 unknown (the check could not run)."}
	advisories := metrics.Family{Name: "x3f_advisories", Help: "Published security advisories that affect a client and are not ignored, by severity."}
	for _, r := range rows {
		node := metrics.Label{Name: "node", Value: r.node}
		nodeStatus.Samples = append(nodeStatus.Samples, metrics.Sample{Labels: []metrics.Label{node}, Value: float64(r.level)})
		for _, f := range r.findings {
			value := float64(f.Level)
			if f.notRun {
				value = checkUnknown
			}
			checkStatus.Samples = append(checkStatus.Samples, metrics.Sample{
				Labels: []metrics.Label{node, {Name: "check", Value: strings.ReplaceAll(f.name, " ", "_")}},
				Value:  value,
			})
		}
		for _, comp := range []string{"execution", "consensus"} {
			counts, ok := r.advisories[comp]
			if !ok {
				continue
			}
			for _, sev := range severityLabels {
				advisories.Samples = append(advisories.Samples, metrics.Sample{
					Labels: []metrics.Label{node, {Name: "component", Value: comp}, {Name: "severity", Value: sev}},
					Value:  float64(counts[sev]),
				})
			}
		}
	}

	clientInfo := metrics.Family{Name: "x3f_client_info", Help: "Client and version a node runs; build is release, prerelease, dev or unknown."}
	latestInfo := metrics.Family{Name: "x3f_client_latest_info", Help: "Highest stable release of a client known from GitHub."}
	var latestDone []ethereum.ClientName
	var networks []forks.Network
	for _, r := range results {
		components := []struct {
			name string
			v    ethereum.ClientVersion
		}{{"execution", r.Execution.Version}, {"consensus", r.Consensus.Version}}
		for _, c := range components {
			v := c.v
			running, ok := releases.ParseVersion(v.Version)
			if v.Client == ethereum.Unknown || !v.HasVersion() || !ok {
				continue
			}
			client := strings.ToLower(v.Client.String())
			clientInfo.Samples = append(clientInfo.Samples, metrics.Sample{Labels: []metrics.Label{
				{Name: "node", Value: r.Name}, {Name: "component", Value: c.name}, {Name: "client", Value: client},
				{Name: "version", Value: plainVersion(running)}, {Name: "build", Value: buildLabel(releases.ClassifySuffix(v.Client, v.Suffix))},
			}, Value: 1})

			if repo, ok := releases.RepoFor(v.Client); ok && rel[repo].result != nil && !slices.Contains(latestDone, v.Client) {
				latestDone = append(latestDone, v.Client)
				if latest, ok := releases.LatestStable(v.Client, rel[repo].result.Releases); ok {
					latestInfo.Samples = append(latestInfo.Samples, metrics.Sample{Labels: []metrics.Label{
						{Name: "client", Value: client}, {Name: "version", Value: plainVersion(latest)},
					}, Value: 1})
				}
			}
		}
		if id, ok := detectedChain(r); ok && schedule != nil {
			if n, ok := schedule.ForChainID(id); ok && !slices.ContainsFunc(networks, func(x forks.Network) bool { return x.Name == n.Name }) {
				networks = append(networks, n)
			}
		}
	}

	nextFork := metrics.Family{Name: "x3f_next_fork_timestamp_seconds", Help: "Activation time of the next scheduled fork of a network the nodes are on (Unix time)."}
	for _, n := range networks {
		if f, ok := n.Next(now); ok {
			nextFork.Samples = append(nextFork.Samples, metrics.Sample{Labels: []metrics.Label{
				{Name: "network", Value: n.Name}, {Name: "fork", Value: f.Name},
			}, Value: float64(f.Time.Unix())})
		}
	}

	successValue := 0.0
	if success {
		successValue = 1
	}
	return []metrics.Family{
		nodeStatus, checkStatus, clientInfo, latestInfo, advisories, nextFork,
		{Name: "x3f_build_info", Help: "Version of x3f.", Samples: []metrics.Sample{{Labels: []metrics.Label{
			{Name: "version", Value: buildVersion()}, {Name: "commit", Value: buildCommit()}, {Name: "goversion", Value: runtime.Version()},
		}, Value: 1}}},
		{Name: "x3f_last_run_timestamp_seconds", Help: "When the last run of x3f ended (Unix time).", Samples: []metrics.Sample{{Value: float64(now.Unix())}}},
		{Name: "x3f_last_run_success", Help: "1 if x3f could do its own work in the last run (GitHub data, notification, state file), 0 otherwise.", Samples: []metrics.Sample{{Value: successValue}}},
	}
}

// githubComplete reports whether release and advisory data is available
// for every detected client: fresh, or cached and younger than the 7 days
// the status rules accept.
func githubComplete(rel map[releases.Repo]releaseData, adv map[releases.Repo]advisoryData, now time.Time) bool {
	usable := func(refreshErr error, fetchedAt time.Time) bool {
		return refreshErr == nil || now.Sub(fetchedAt) <= status.MaxReleaseDataAge
	}
	for _, d := range rel {
		if d.result == nil || !usable(d.result.RefreshErr, d.result.FetchedAt) {
			return false
		}
	}
	for _, d := range adv {
		if d.result == nil || !usable(d.result.RefreshErr, d.result.FetchedAt) {
			return false
		}
	}
	return true
}

// plainVersion is a version without suffix, e.g. "1.17.10". Suffixes can
// contain commit hashes or arbitrary text and stay out of labels.
func plainVersion(v releases.Version) string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func buildLabel(k releases.SuffixKind) string {
	switch k {
	case releases.KindRelease:
		return "release"
	case releases.KindPreRelease:
		return "prerelease"
	case releases.KindDevBuild:
		return "dev"
	default:
		return "unknown"
	}
}
