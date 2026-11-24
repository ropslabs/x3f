// Package network knows the Ethereum networks x3f supports and maps chain
// IDs to them.
package network

import "fmt"

// Network is a public Ethereum network.
type Network struct {
	Name    string
	ChainID uint64
}

// Known networks. Chain IDs from go-ethereum params/chainspecs/*.json and
// Lighthouse built_in_network_configs/*/config.yaml (DEPOSIT_CHAIN_ID),
// checked against live endpoints on 2026-11-24; see
// internal/ethereum/testdata/SOURCES.md. Holesky is deprecated (since
// September 2025, per ethereum.org) and not included.
var known = []Network{
	{Name: "mainnet", ChainID: 1},
	{Name: "sepolia", ChainID: 11155111},
	{Name: "hoodi", ChainID: 560048},
}

// ByChainID returns the network with the given chain ID.
func ByChainID(id uint64) (Network, bool) {
	for _, n := range known {
		if n.ChainID == id {
			return n, true
		}
	}
	return Network{}, false
}

// ByName returns the network with the given name (lowercase).
func ByName(name string) (Network, bool) {
	for _, n := range known {
		if n.Name == name {
			return n, true
		}
	}
	return Network{}, false
}

// Names returns the names of all known networks, e.g. for error messages.
func Names() []string {
	names := make([]string, len(known))
	for i, n := range known {
		names[i] = n.Name
	}
	return names
}

// Describe names a chain ID for humans: "hoodi" or "chain 12345".
func Describe(id uint64) string {
	if n, ok := ByChainID(id); ok {
		return n.Name
	}
	return fmt.Sprintf("chain %d", id)
}
