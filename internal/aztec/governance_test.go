package aztec

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func govFixture(t *testing.T, name, net string) []byte {
	t.Helper()
	return readFixture(t, "live/l1/governance/"+name+"-publicnode-"+net+".json")
}

func TestGovernanceSelectors(t *testing.T) {
	for name, sel := range map[string]string{
		"proposalCount": selProposalCount, "getProposalState": selGetProposalState, "getProposal": selGetProposal,
		"getActions": selGetActions, "getURI": selGetURI, "addRollup": selAddRollup,
	} {
		var resp struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(govFixture(t, "web3_sha3-"+name, "mainnet"), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Result) != 66 || resp.Result[:10] != sel {
			t.Errorf("%s: hash %s, selector constant %s", name, resp.Result, sel)
		}
	}
}

func TestGovernanceRecorded(t *testing.T) {
	// Recorded at 14:10 UTC; addresses from the getNodeInfo answers, the
	// payloads from the recorded getProposal answers.
	tests := []struct {
		net, governance, registry string
		count                     uint64
		states                    map[uint64]ProposalState
		id                        uint64
		payload                   string
		creation                  string
		delays                    [4]time.Duration
		executableFrom            string
		rollup                    string // registered by the payload; "" for none
		uri                       string
	}{
		{
			net: "mainnet", governance: "0x1102471eb3378fee427121c9efcea452e4b6b75e", registry: "0x35b22e09ee0390539439e24f06da43d83f90e298",
			count: 7, states: map[uint64]ProposalState{0: ProposalExecuted, 5: ProposalExecuted, 6: ProposalExecuted},
			id: 5, payload: "0xe8d663a8b355415f331a54d694f36685346b5433", creation: "2026-10-14T04:41:35Z",
			delays:         [4]time.Duration{72 * time.Hour, 7 * 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour},
			executableFrom: "2026-10-26T04:41:36Z",
			rollup:         "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f", uri: "V6UpgradePayload",
		},
		{
			net: "mainnet", governance: "0x1102471eb3378fee427121c9efcea452e4b6b75e", registry: "0x35b22e09ee0390539439e24f06da43d83f90e298",
			count: 7, states: map[uint64]ProposalState{6: ProposalExecuted},
			id: 6, payload: "0x340d8f93c874f59d0e49400cb7a9aff31eed5e71", creation: "2026-11-06T09:02:35Z",
			delays:         [4]time.Duration{72 * time.Hour, 7 * 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour},
			executableFrom: "2026-11-18T09:02:36Z",
			uri:            "ZeroRegistryRewardsPayload",
		},
		{
			net: "sepolia", governance: "0xcaf7447721447b22cd0076ac7c63877c3afd329f", registry: "0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba",
			count: 9, states: map[uint64]ProposalState{0: ProposalRejected, 8: ProposalExecuted},
			id: 8, payload: "0x5e9d4e98a9258951fcbbe644d6cbf122845f93be", creation: "2026-10-22T11:35:36Z",
			// Testnet: minutes, not days.
			delays:         [4]time.Duration{2 * time.Minute, 6 * time.Minute, 2 * time.Minute, 24 * time.Hour},
			executableFrom: "2026-10-22T11:45:37Z",
			rollup:         "0x1f435a7f2da358caf887a470faeee7d5d5f4efba", uri: "V6UpgradePayload",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s #%d", tt.net, tt.id), func(t *testing.T) {
			f := func(name string) []byte { return govFixture(t, name, tt.net) }
			replies := map[string][]byte{
				tt.governance + " " + selProposalCount:             f("eth_call-proposalCount"),
				tt.governance + " " + selGetProposal + word(tt.id): f(fmt.Sprintf("eth_call-getProposal-%d", tt.id)),
				tt.payload + " " + selGetActions:                   f(fmt.Sprintf("eth_call-getActions-%d", tt.id)),
				tt.payload + " " + selGetURI:                       f(fmt.Sprintf("eth_call-getURI-%d", tt.id)),
			}
			for id := range tt.states {
				replies[tt.governance+" "+selGetProposalState+word(id)] = f(fmt.Sprintf("eth_call-getProposalState-%d", id))
			}
			srv := mockL1(t, replies)
			c, ctx := NewClient(nil), context.Background()
			if n, err := c.ProposalCount(ctx, srv.URL, tt.governance); err != nil || n != tt.count {
				t.Errorf("ProposalCount = %d, %v; want %d", n, err, tt.count)
			}
			for id, want := range tt.states {
				if s, err := c.ProposalState(ctx, srv.URL, tt.governance, id); err != nil || s != want {
					t.Errorf("ProposalState(%d) = %v, %v; want %v", id, s, err, want)
				}
			}
			p, err := c.Proposal(ctx, srv.URL, tt.governance, tt.id)
			if err != nil {
				t.Fatal(err)
			}
			if p.Payload != tt.payload || p.Creation.Format(time.RFC3339) != tt.creation ||
				[4]time.Duration{p.VotingDelay, p.VotingDuration, p.ExecutionDelay, p.GracePeriod} != tt.delays {
				t.Errorf("Proposal = %+v", p)
			}
			if got := p.ExecutableFrom().Format(time.RFC3339); got != tt.executableFrom {
				t.Errorf("ExecutableFrom = %s, want %s", got, tt.executableFrom)
			}
			actions, err := c.PayloadActions(ctx, srv.URL, p.Payload)
			if err != nil {
				t.Fatal(err)
			}
			rollup, ok := RegisteredRollup(actions, tt.registry)
			if rollup != tt.rollup || ok != (tt.rollup != "") {
				t.Errorf("RegisteredRollup = %q, %v; want %q", rollup, ok, tt.rollup)
			}
			if uri, err := c.PayloadURI(ctx, srv.URL, p.Payload); err != nil || uri != tt.uri {
				t.Errorf("PayloadURI = %q, %v; want %q", uri, err, tt.uri)
			}
		})
	}
}

func TestPayloadActionsRevert(t *testing.T) {
	// Recorded: getActions of mainnet proposal #1 reverts; its URI reads.
	const payload = "0x9b8bc4b0c079d2b3db923e51db63fbb1f03d5b12"
	srv := mockL1(t, map[string][]byte{
		payload + " " + selGetActions: govFixture(t, "eth_call-getActions-1", "mainnet"),
		payload + " " + selGetURI:     govFixture(t, "eth_call-getURI-1", "mainnet"),
	})
	c, ctx := NewClient(nil), context.Background()
	if _, err := c.PayloadActions(ctx, srv.URL, payload); err == nil || !strings.HasPrefix(err.Error(), "getActions: JSON-RPC error 3: execution reverted") {
		t.Errorf("error = %v", err)
	}
	if uri, err := c.PayloadURI(ctx, srv.URL, payload); err != nil || uri != "https://github.com/AztecProtocol/ignition-contracts/" {
		t.Errorf("PayloadURI = %q, %v", uri, err)
	}
}

func TestProposalStateNames(t *testing.T) {
	open := map[ProposalState]bool{ProposalPending: true, ProposalActive: true, ProposalQueued: true, ProposalExecutable: true, ProposalDroppable: true}
	for s := ProposalPending; s <= ProposalExpired; s++ {
		if s.Open() != open[s] {
			t.Errorf("%v.Open() = %v", s, s.Open())
		}
	}
	if ProposalQueued.String() != "queued" || ProposalState(9).String() != "state 9" {
		t.Error("names")
	}
	srv := mockL1(t, map[string][]byte{
		"0x1102471eb3378fee427121c9efcea452e4b6b75e " + selGetProposalState + word(0): []byte(`{"jsonrpc":"2.0","id":1,"result":"0x` + word(9) + `"}`),
	})
	if _, err := NewClient(nil).ProposalState(context.Background(), srv.URL, "0x1102471eb3378fee427121c9efcea452e4b6b75e", 0); err == nil {
		t.Error("state 9 accepted")
	}
}

func TestRegisteredRollup(t *testing.T) {
	const registry = "0x35b22e09ee0390539439e24f06da43d83f90e298"
	data := func(sel, arg string) []byte { b, _ := hex.DecodeString(strings.TrimPrefix(sel, "0x") + arg); return b }
	rollup := strings.Repeat("0", 24) + "e37a2663cc9c119ba41572c40fab0bbb23a5402f"
	tests := []struct {
		name    string
		actions []Action
		want    string
	}{
		{"addRollup on the registry", []Action{{Target: registry, Data: data(selAddRollup, rollup)}}, "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f"},
		{"mixed-case target", []Action{{Target: strings.ToUpper(registry), Data: data(selAddRollup, rollup)}}, "0xe37a2663cc9c119ba41572c40fab0bbb23a5402f"},
		// GSE has an addRollup with the same selector (see the recorded
		// V6 payload); only the registry's counts.
		{"addRollup on another contract", []Action{{Target: "0xa92ecfd0e70c9cd5e5cd76c50af0f7da93567a4f", Data: data(selAddRollup, rollup)}}, ""},
		{"other function on the registry", []Action{{Target: registry, Data: data("0x497cd426", rollup)}}, ""},
		{"argument not an address", []Action{{Target: registry, Data: data(selAddRollup, "ff"+rollup[2:])}}, ""},
		{"short data", []Action{{Target: registry, Data: data(selAddRollup, "")}}, ""},
		{"none", nil, ""},
	}
	for _, tt := range tests {
		got, ok := RegisteredRollup(tt.actions, registry)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("%s: %q, %v; want %q", tt.name, got, ok, tt.want)
		}
	}
}

// TestDecodeMalformed feeds malformed ABI data: errors, never a panic.
func TestDecodeMalformed(t *testing.T) {
	w := func(ns ...uint64) []byte {
		var b []byte
		for _, n := range ns {
			b = append(b, make([]byte, 24)...)
			b = append(b, byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		}
		return b
	}
	huge := append([]byte{0xff}, make([]byte, 31)...)
	for name, b := range map[string][]byte{
		"empty":               {},
		"offset past the end": w(0x1000),
		"huge offset":         huge,
		"count too large":     w(32, 65),
		"count past the end":  w(32, 2),
		// Malformed as actions, but a valid one-byte string.
		"tuple offset past end": w(32, 1, 0x1000),
		"bytes length past end": w(32, 1, 32, 0, 64, 0x1000),
	} {
		if _, err := decodeActions(b); err == nil {
			t.Errorf("decodeActions(%s): no error", name)
		}
	}
	for name, b := range map[string][]byte{
		"empty":               {},
		"offset past the end": w(0x1000),
		"huge offset":         huge,
		"length past the end": w(32, 2),
		"length huge":         append(w(32), huge...),
	} {
		if _, err := decodeString(b); err == nil {
			t.Errorf("decodeString(%s): no error", name)
		}
	}
	// 65 well-formed actions, all pointing to the same empty call: only
	// the limit rejects them.
	many := w(32, maxActions+1)
	for range maxActions + 1 {
		many = append(many, w((maxActions+1)*32)...)
	}
	many = append(many, w(0, 64, 0)...)
	if _, err := decodeActions(many); err == nil || err.Error() != "invalid number of actions" {
		t.Errorf("%d actions: error %v", maxActions+1, err)
	}
	if a, err := decodeActions(append(w(32, 1, 32), w(0, 64, 0)...)); err != nil || len(a) != 1 {
		t.Errorf("one empty action: %v, %v", a, err)
	}

	// A string that is not UTF-8, and one longer than maxURI.
	if _, err := decodeString(append(w(32, 1), append([]byte{0xff}, make([]byte, 31)...)...)); err == nil {
		t.Error("non-UTF-8 string accepted")
	}
	if _, err := decodeString(append(w(32, maxURI+1), make([]byte, maxURI+32)...)); err == nil {
		t.Error("overlong string accepted")
	}
}

// FuzzDecodeActions checks that no input makes the decoders panic. The seeds
// are the recorded answers; "go test -fuzz" explores further.
func FuzzDecodeActions(f *testing.F) {
	for _, name := range []string{"eth_call-getActions-5-publicnode-mainnet.json", "eth_call-getActions-6-publicnode-mainnet.json",
		"eth_call-getURI-5-publicnode-mainnet.json", "eth_call-getActions-8-publicnode-sepolia.json"} {
		var resp struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(readFixture(f, "live/l1/governance/"+name), &resp); err != nil {
			f.Fatal(err)
		}
		b, _ := hex.DecodeString(strings.TrimPrefix(resp.Result, "0x"))
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		decodeActions(b)
		decodeString(b)
	})
}
