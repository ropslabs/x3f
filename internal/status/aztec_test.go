package status

import (
	"errors"
	"testing"
)

func TestAssessAztecNetwork(t *testing.T) {
	const mainnetRegistry = "0x35b22e09ee0390539439e24f06da43d83f90e298"
	tests := []struct {
		name      string
		in        AztecNetworkInput
		wantLevel Level
		wantCell  string
		wantMsg   string
	}{
		{"known", AztecNetworkInput{L1ChainID: 1, Registry: mainnetRegistry}, OK, "mainnet", ""},
		{"known and expected", AztecNetworkInput{Expected: "mainnet", L1ChainID: 1, Registry: mainnetRegistry}, OK, "mainnet", ""},
		{"other than expected", AztecNetworkInput{Expected: "testnet", L1ChainID: 1, Registry: mainnetRegistry}, Crit, "mainnet",
			"node is on mainnet, expected testnet"},
		{"unknown, expected", AztecNetworkInput{Expected: "mainnet", L1ChainID: 5, Registry: mainnetRegistry}, Crit, "?",
			"node is on an unknown network (L1 chain 5, registry " + mainnetRegistry + "), expected mainnet"},
		{"unknown", AztecNetworkInput{L1ChainID: 1, Registry: "0x0000000000000000000000000000000000000001"}, Warn, "?",
			"unknown network (L1 mainnet, registry 0x0000000000000000000000000000000000000001): not one of mainnet, testnet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, cell := AssessAztecNetwork(tt.in)
			if a.Level != tt.wantLevel || cell != tt.wantCell {
				t.Errorf("level %v, cell %q; want %v, %q", a.Level, cell, tt.wantLevel, tt.wantCell)
			}
			if msg := firstMessage(a); msg != tt.wantMsg {
				t.Errorf("message %q\nwant    %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestAssessRollup(t *testing.T) {
	const node = "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f"
	const other = "0x91ff8bbd8ebb07893010d50a48a1609e5ebd8e34"
	base := RollupInput{NodeRollup: node, NodeVersion: 2070860440, NodeL1ChainID: 1, L1: `L1 node "eth-1"`, L1ChainID: 1}
	with := func(f func(*RollupInput)) RollupInput { in := base; f(&in); return in }
	tests := []struct {
		name       string
		in         RollupInput
		wantLevel  Level
		wantCell   string
		wantNotRun bool
		wantMsg    string
	}{
		// The docs write checksummed (mixed-case) addresses.
		{"canonical, mixed case", with(func(in *RollupInput) { in.Canonical = "0xE37A2663Cc9c119BA41572C40fAb0bbb23a5402f" }),
			OK, "2070860440 (canonical)", false, ""},
		{"old rollup", with(func(in *RollupInput) {
			in.NodeRollup, in.NodeVersion, in.Canonical, in.CanonicalVersion = other, 4248422647, node, 2070860440
		}), Crit, "4248422647, canonical 2070860440", false,
			"node follows rollup version 4248422647 (" + other + "), but the canonical rollup is version 2070860440 (" + node + "): the network has moved on, upgrade the node"},
		{"old rollup, canonical version unknown", with(func(in *RollupInput) {
			in.NodeRollup, in.NodeVersion, in.Canonical, in.CanonicalVersionErr = other, 4248422647, node, errors.New("getVersion: boom")
		}), Crit, "4248422647, canonical ?", false,
			"node follows rollup version 4248422647 (" + other + "), but the canonical rollup is version ? (" + node + "): the network has moved on, upgrade the node"},
		{"L1 failed", with(func(in *RollupInput) { in.L1ChainErr = errors.New("eth_chainId: connection failed") }),
			Warn, "2070860440 (not checked)", true, `not checked: L1 node "eth-1" failed: eth_chainId: connection failed`},
		{"L1 on another chain", with(func(in *RollupInput) { in.L1ChainID = 11155111; in.Canonical = other }),
			Warn, "2070860440 (not checked)", true, `not checked: L1 node "eth-1" is on sepolia, but the node's L1 is mainnet`},
		{"registry read failed", with(func(in *RollupInput) { in.CanonicalErr = errors.New("getCanonicalRollup: execution reverted") }),
			Warn, "2070860440 (not checked)", true, `not checked: reading the registry through L1 node "eth-1" failed: getCanonicalRollup: execution reverted`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, cell, notRun := AssessRollup(tt.in)
			if a.Level != tt.wantLevel || cell != tt.wantCell || notRun != tt.wantNotRun {
				t.Errorf("level %v, cell %q, notRun %v; want %v, %q, %v", a.Level, cell, notRun, tt.wantLevel, tt.wantCell, tt.wantNotRun)
			}
			if msg := firstMessage(a); msg != tt.wantMsg {
				t.Errorf("message %q\nwant    %q", msg, tt.wantMsg)
			}
		})
	}
}

// firstMessage returns the message of the only finding, "" without any.
func firstMessage(a Assessment) string {
	if len(a.Findings) == 0 {
		return ""
	}
	return a.Findings[0].Message
}
