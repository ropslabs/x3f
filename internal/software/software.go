// Package software identifies the software x3f checks: its name, its
// upstream repository and what its version suffixes mean. It belongs to no
// chain: the Ethereum clients are entries here, and other node software
// gets entries of its own.
package software

// Name identifies a piece of software, e.g. an Ethereum client.
//
// It is a named string type: the compiler keeps it apart from plain strings,
// so a Name cannot be mixed up with, say, a node name by accident.
type Name string

// Known software. The values are lowercase and stable; use String for display.
const (
	Unknown Name = "unknown"

	// Execution clients.
	Geth       Name = "geth"
	Reth       Name = "reth"
	Nethermind Name = "nethermind"
	Erigon     Name = "erigon"
	Besu       Name = "besu"
	Ethrex     Name = "ethrex"

	// Consensus clients.
	Lighthouse Name = "lighthouse"
	Prysm      Name = "prysm"
	Teku       Name = "teku"
	Nimbus     Name = "nimbus"
	Lodestar   Name = "lodestar"
	Grandine   Name = "grandine"

	// Aztec node software (full node, sequencer, prover), released from
	// the aztec-packages monorepo.
	Aztec Name = "aztec"
)

var displayNames = map[Name]string{
	Unknown:    "unknown",
	Geth:       "Geth",
	Reth:       "Reth",
	Nethermind: "Nethermind",
	Erigon:     "Erigon",
	Besu:       "Besu",
	Ethrex:     "Ethrex",
	Lighthouse: "Lighthouse",
	Prysm:      "Prysm",
	Teku:       "Teku",
	Nimbus:     "Nimbus",
	Lodestar:   "Lodestar",
	Grandine:   "Grandine",
	Aztec:      "Aztec",
}

// String returns the display name. Implementing String() makes fmt use it
// automatically for %s and %v (it satisfies the fmt.Stringer interface).
func (c Name) String() string {
	if s, ok := displayNames[c]; ok {
		return s
	}
	return string(c)
}

// Reported is the version a node reports for a piece of software, parsed
// into its parts. Each node type parses its own format, e.g.
// ethereum.ParseClientVersion for "Geth/v1.17.10-stable-1df30b18/...".
type Reported struct {
	// Raw is the unmodified string the node reported.
	Raw string
	// Name is the detected implementation, or Unknown.
	Name Name
	// Version is the numeric MAJOR.MINOR.PATCH part without a "v" prefix,
	// e.g. "1.17.10". Empty if no version could be found.
	Version string
	// Suffix holds everything after the numeric version that is not the
	// commit, joined with "-", e.g. "stable", "unstable-20261120", "rc.1",
	// "dev", "stateofus". It is kept verbatim: whether a suffix means
	// "pre-release" depends on the software and is decided by
	// ClassifySuffix.
	Suffix string
	// Commit is the abbreviated git commit hash, if the string contains one.
	Commit string
}

// HasVersion reports whether a numeric version was found.
func (v Reported) HasVersion() bool { return v.Version != "" }

// Repo is a GitHub repository.
type Repo struct {
	Owner, Name string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// repos maps each piece of software to its GitHub repository. Paths were
// checked against the GitHub API on 2026-11-24 (aztec-packages on
// 2026-11-27). Several have moved; GitHub still
// redirects the old paths, but we use the current ones:
//
//	hyperledger/besu     -> besu-eth/besu
//	prysmaticlabs/prysm  -> OffchainLabs/prysm
//	Consensys/teku       -> Consensys-Incorporated/teku
//	ledgerwatch/erigon   -> erigontech/erigon
var repos = map[Name]Repo{
	Geth:       {"ethereum", "go-ethereum"},
	Reth:       {"paradigmxyz", "reth"},
	Nethermind: {"NethermindEth", "nethermind"},
	Erigon:     {"erigontech", "erigon"},
	Besu:       {"besu-eth", "besu"},
	Ethrex:     {"lambdaclass", "ethrex"},
	Lighthouse: {"sigp", "lighthouse"},
	Prysm:      {"OffchainLabs", "prysm"},
	Teku:       {"Consensys-Incorporated", "teku"},
	Nimbus:     {"status-im", "nimbus-eth2"},
	Lodestar:   {"ChainSafe", "lodestar"},
	Grandine:   {"grandinetech", "grandine"},
	Aztec:      {"AztecProtocol", "aztec-packages"},
}

// RepoFor returns the GitHub repository of a piece of software.
func RepoFor(c Name) (Repo, bool) {
	r, ok := repos[c]
	return r, ok
}
