package status

import (
	"fmt"
	"strings"

	"github.com/ropslabs/x3f/internal/aztec"
	"github.com/ropslabs/x3f/internal/network"
)

// Rules for Aztec nodes (agreed for step 3 of the Aztec module):
//
//	Network green:  a known network (and the configured one, if set)
//	        yellow: an unknown network (L1 chain or registry not known)
//	        red:    another network than configured
//	Rollup  green:  the node follows the canonical rollup of its registry
//	        yellow: not checked, with the reason: the L1 endpoint x3f reads
//	                is unreachable, fails or is on another chain than the
//	                node's L1 (the check is then reported as unknown)
//	        red:    the node follows another rollup than the canonical one,
//	                e.g. after a network upgrade

// AztecNetworkInput is what the network check of an Aztec node needs.
type AztecNetworkInput struct {
	// Expected is the network from the config; "" if not set.
	Expected string
	// L1ChainID and Registry are what the node reports in getNodeInfo.
	L1ChainID uint64
	Registry  string
}

// AssessAztecNetwork applies the network rules. cell is the network name,
// or "?" for an unknown network.
func AssessAztecNetwork(in AztecNetworkInput) (a Assessment, cell string) {
	n, known := aztec.IdentifyNetwork(in.L1ChainID, in.Registry)
	where := fmt.Sprintf("L1 %s, registry %s", network.Describe(in.L1ChainID), clean(in.Registry, 42))
	switch {
	case known && (in.Expected == "" || in.Expected == n.Name):
		return a, n.Name
	case known:
		a.add(Crit, "node is on %s, expected %s", n.Name, in.Expected)
		return a, n.Name
	case in.Expected != "":
		a.add(Crit, "node is on an unknown network (%s), expected %s", where, in.Expected)
	default:
		a.add(Warn, "unknown network (%s): not one of %s", where, strings.Join(aztec.NetworkNames(), ", "))
	}
	return a, "?"
}

// RollupInput is what the rollup check of an Aztec node needs. The L1
// fields are the results of x3f's own L1 reads.
type RollupInput struct {
	// NodeRollup, NodeVersion and NodeL1ChainID are what the node reports
	// in getNodeInfo.
	NodeRollup    string
	NodeVersion   uint64
	NodeL1ChainID uint64
	// L1 names the L1 endpoint for messages, e.g. `L1 node "eth-1"`; it
	// never contains a URL.
	L1 string
	// L1ChainID is the chain ID of the L1 endpoint, L1ChainErr the error
	// of that request.
	L1ChainID  uint64
	L1ChainErr error
	// Canonical is the canonical rollup of the node's registry,
	// CanonicalErr the error of that read. Neither is set if the L1
	// endpoint is on another chain.
	Canonical    string
	CanonicalErr error
	// CanonicalVersion is the version of the canonical rollup;
	// CanonicalVersionErr is set if it could not be read.
	CanonicalVersion    uint64
	CanonicalVersionErr error
}

// AssessRollup applies the rollup rules. cell describes the node's rollup;
// notRun is set if the check could not run (its finding says why).
func AssessRollup(in RollupInput) (a Assessment, cell string, notRun bool) {
	notChecked := fmt.Sprintf("%d (not checked)", in.NodeVersion)
	switch {
	case in.L1ChainErr != nil:
		a.add(Warn, "not checked: %s failed: %v", in.L1, in.L1ChainErr)
		return a, notChecked, true
	case in.L1ChainID != in.NodeL1ChainID:
		// Reading the registry on this chain would compare the node with
		// another network's contracts.
		a.add(Warn, "not checked: %s is on %s, but the node's L1 is %s",
			in.L1, network.Describe(in.L1ChainID), network.Describe(in.NodeL1ChainID))
		return a, notChecked, true
	case in.CanonicalErr != nil:
		a.add(Warn, "not checked: reading the registry through %s failed: %v", in.L1, in.CanonicalErr)
		return a, notChecked, true
	case strings.EqualFold(in.Canonical, in.NodeRollup):
		return a, fmt.Sprintf("%d (canonical)", in.NodeVersion), false
	}
	canonical := "?"
	if in.CanonicalVersionErr == nil {
		canonical = fmt.Sprint(in.CanonicalVersion)
	}
	a.add(Crit, "node follows rollup version %d (%s), but the canonical rollup is version %s (%s): the network has moved on, upgrade the node",
		in.NodeVersion, clean(in.NodeRollup, 42), canonical, in.Canonical)
	return a, fmt.Sprintf("%d, canonical %s", in.NodeVersion, canonical), false
}
