package releases

import (
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
)

// Repo is a GitHub repository.
type Repo struct {
	Owner, Name string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// repos maps each client to its GitHub repository. Paths were checked
// against the GitHub API on 2026-11-24. Several have moved; GitHub still
// redirects the old paths, but we use the current ones:
//
//	hyperledger/besu     -> besu-eth/besu
//	prysmaticlabs/prysm  -> OffchainLabs/prysm
//	Consensys/teku       -> Consensys-Incorporated/teku
//	ledgerwatch/erigon   -> erigontech/erigon
var repos = map[ethereum.ClientName]Repo{
	ethereum.Geth:       {"ethereum", "go-ethereum"},
	ethereum.Reth:       {"paradigmxyz", "reth"},
	ethereum.Nethermind: {"NethermindEth", "nethermind"},
	ethereum.Erigon:     {"erigontech", "erigon"},
	ethereum.Besu:       {"besu-eth", "besu"},
	ethereum.Lighthouse: {"sigp", "lighthouse"},
	ethereum.Prysm:      {"OffchainLabs", "prysm"},
	ethereum.Teku:       {"Consensys-Incorporated", "teku"},
	ethereum.Nimbus:     {"status-im", "nimbus-eth2"},
	ethereum.Lodestar:   {"ChainSafe", "lodestar"},
	ethereum.Grandine:   {"grandinetech", "grandine"},
}

// RepoFor returns the GitHub repository of a client.
func RepoFor(c ethereum.ClientName) (Repo, bool) {
	r, ok := repos[c]
	return r, ok
}

// Release is one GitHub release, reduced to the fields x3f uses.
type Release struct {
	Tag         string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
}

// stable reports whether r counts as a stable release of client: not a
// draft, not flagged as pre-release, a parseable version tag and a release
// suffix. Both checks are needed because the GitHub flag is unreliable in
// both directions (e.g. reth v1.0.0-rc.2 is not flagged, besu 24.5.0 is).
func stable(client ethereum.ClientName, r Release) (Version, bool) {
	if r.Draft || r.Prerelease {
		return Version{}, false
	}
	v, ok := ParseVersion(r.Tag)
	if !ok || ClassifySuffix(client, v.Suffix) != KindRelease {
		return Version{}, false
	}
	return v, true
}

// LatestStable returns the highest stable version. It uses the version
// number, not the publication date, so a backport to an older release line
// published later does not count as "latest".
func LatestStable(client ethereum.ClientName, rels []Release) (Version, bool) {
	var latest Version
	found := false
	for _, r := range rels {
		v, ok := stable(client, r)
		if ok && (!found || Compare(client, v, latest) > 0) {
			latest, found = v, true
		}
	}
	return latest, found
}

// NewestKnown returns the highest version among all releases that are not
// drafts, including pre-releases and any suffix. A running version above
// this is not any known release: a proxy's made-up version, a custom build,
// or a release newer than the data.
func NewestKnown(client ethereum.ClientName, rels []Release) (Version, bool) {
	var newest Version
	found := false
	for _, r := range rels {
		if r.Draft {
			continue
		}
		v, ok := ParseVersion(r.Tag)
		if ok && (!found || Compare(client, v, newest) > 0) {
			newest, found = v, true
		}
	}
	return newest, found
}

// NewerThanAll reports whether running is newer than every known release.
func NewerThanAll(client ethereum.ClientName, rels []Release, running Version) bool {
	newest, ok := NewestKnown(client, rels)
	return ok && Compare(client, running, newest) > 0
}

// FirstMissing returns the earliest published stable release that is newer
// than running: the moment the node started to be outdated. ok is false if
// no newer stable release exists.
func FirstMissing(client ethereum.ClientName, rels []Release, running Version) (Release, bool) {
	var first Release
	found := false
	for _, r := range rels {
		v, ok := stable(client, r)
		if !ok || Compare(client, v, running) <= 0 {
			continue
		}
		if !found || r.PublishedAt.Before(first.PublishedAt) {
			first, found = r, true
		}
	}
	return first, found
}
