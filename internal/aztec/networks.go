package aztec

import (
	"slices"
	"strings"
)

// Network is a public Aztec network. Its L1 chain and its registry on that
// chain identify it: the registry lists every rollup the network ever ran.
type Network struct {
	// Name is the name used by the Aztec CLI and in x3f.yaml. The docs
	// call mainnet "Alpha"; CLI and configuration values stay "mainnet"
	// and "testnet" (docs/docs/networks.md in aztec-packages).
	Name string
	// L1ChainID is the chain ID of the L1 (Ethereum) network.
	L1ChainID uint64
	// Registry is the address of the registry contract on L1, lowercase.
	Registry string
}

// networks are the public Aztec networks, from network_config.json of
// AztecProtocol/networks (commit 7182c14b, the file the node itself loads
// for --network); TestNetworksMatchNetworkConfig checks them against a
// copy in testdata/networks.
var networks = []Network{
	{Name: "mainnet", L1ChainID: 1, Registry: "0x35b22e09ee0390539439e24f06da43d83f90e298"},
	{Name: "testnet", L1ChainID: 11155111, Registry: "0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba"},
}

// NetworkByName returns the network with the given name (lowercase).
func NetworkByName(name string) (Network, bool) {
	i := slices.IndexFunc(networks, func(n Network) bool { return n.Name == name })
	if i < 0 {
		return Network{}, false
	}
	return networks[i], true
}

// NetworkNames returns the names of all known networks, e.g. for error
// messages.
func NetworkNames() []string {
	names := make([]string, len(networks))
	for i, n := range networks {
		names[i] = n.Name
	}
	return names
}

// IdentifyNetwork returns the network with this L1 chain ID and registry
// address (in any case), as a node reports them in getNodeInfo. Both must
// match: a registry address alone could exist on another chain.
func IdentifyNetwork(l1ChainID uint64, registry string) (Network, bool) {
	registry = strings.ToLower(registry)
	i := slices.IndexFunc(networks, func(n Network) bool {
		return n.L1ChainID == l1ChainID && n.Registry == registry
	})
	if i < 0 {
		return Network{}, false
	}
	return networks[i], true
}
