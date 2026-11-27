package aztec

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

// Reading Aztec governance on L1, read-only. Interfaces of aztec-packages
// v6.0.0 (commit 0219f11d81): l1-contracts/src/governance/Governance.sol,
// interfaces/IGovernance.sol, interfaces/IPayload.sol,
// libraries/ProposalLib.sol and interfaces/IRegistry.sol.

const (
	// selProposalCount is Governance.proposalCount().
	selProposalCount = "0xda35c664"
	// selGetProposalState is Governance.getProposalState(uint256).
	selGetProposalState = "0x9080936f"
	// selGetProposal is Governance.getProposal(uint256).
	selGetProposal = "0xc7f758a8"
	// selGetActions is IPayload.getActions().
	selGetActions = "0x504d080b"
	// selGetURI is IPayload.getURI().
	selGetURI = "0x7754305c"
	// selAddRollup is Registry.addRollup(address): the action of a
	// payload that registers a new rollup, which becomes canonical.
	selAddRollup = "0x59264f00"
)

// ProposalState is the state of a governance proposal (enum ProposalState
// in IGovernance.sol, in that order).
type ProposalState uint8

const (
	ProposalPending ProposalState = iota
	ProposalActive
	ProposalQueued
	ProposalExecutable
	ProposalRejected
	ProposalExecuted
	ProposalDroppable
	ProposalDropped
	ProposalExpired
)

var proposalStateNames = []string{"pending", "active", "queued", "executable", "rejected", "executed", "droppable", "dropped", "expired"}

func (s ProposalState) String() string {
	if int(s) < len(proposalStateNames) {
		return proposalStateNames[s]
	}
	return fmt.Sprintf("state %d", s)
}

// Open reports whether the proposal may still be executed or dropped:
// pending, active, queued, executable or droppable. The other states are
// final.
func (s ProposalState) Open() bool {
	switch s {
	case ProposalPending, ProposalActive, ProposalQueued, ProposalExecutable, ProposalDroppable:
		return true
	}
	return false
}

// Proposal is the part of a governance proposal that x3f uses.
type Proposal struct {
	// Payload is the address of the payload contract, lowercase.
	Payload string
	// Creation is when the proposal was made; the durations are its
	// configuration at that time (ProposalConfiguration).
	Creation                                                 time.Time
	VotingDelay, VotingDuration, ExecutionDelay, GracePeriod time.Duration
}

// VotingEnds is the end of the voting phase (ProposalLib.activeThrough).
func (p Proposal) VotingEnds() time.Time {
	return p.Creation.Add(p.VotingDelay + p.VotingDuration)
}

// ExecutableFrom is the earliest time an accepted proposal can be
// executed: the state is executable once the time is after queuedThrough
// (creation + votingDelay + votingDuration + executionDelay).
func (p Proposal) ExecutableFrom() time.Time {
	return p.VotingEnds().Add(p.ExecutionDelay + time.Second)
}

// ExecutableUntil is the end of the grace period; after it an accepted
// proposal that was not executed expires.
func (p Proposal) ExecutableUntil() time.Time {
	return p.VotingEnds().Add(p.ExecutionDelay + p.GracePeriod)
}

// Action is one call a payload makes when its proposal is executed.
type Action struct {
	// Target is the called contract, lowercase.
	Target string
	// Data is the call data: selector and arguments.
	Data []byte
}

// ProposalCount returns the number of proposals; their IDs are 0 to
// count-1.
func (c *Client) ProposalCount(ctx context.Context, endpoint, governance string) (uint64, error) {
	return c.uintCall(ctx, endpoint, governance, "proposalCount", selProposalCount)
}

// ProposalState returns the state of a proposal.
func (c *Client) ProposalState(ctx context.Context, endpoint, governance string, id uint64) (ProposalState, error) {
	n, err := c.uintCall(ctx, endpoint, governance, fmt.Sprintf("getProposalState(%d)", id), selGetProposalState, id)
	if err != nil {
		return 0, err
	}
	if n >= uint64(len(proposalStateNames)) {
		return 0, fmt.Errorf("getProposalState(%d): unknown state %d", id, n)
	}
	return ProposalState(n), nil
}

// Proposal returns a proposal.
func (c *Client) Proposal(ctx context.Context, endpoint, governance string, id uint64) (Proposal, error) {
	// Proposal has only fixed-size fields: ProposalConfiguration (7
	// words: votingDelay, votingDuration, executionDelay, gracePeriod,
	// quorum, requiredYeaMargin, minimumVotes), cachedState, payload,
	// proposer, creation and Ballot (2 words).
	words, err := c.call(ctx, endpoint, governance, selGetProposal, 13, id)
	if err != nil {
		return Proposal{}, fmt.Errorf("getProposal(%d): %w", id, err)
	}
	var nums [5]uint64
	for i, w := range []int{0, 1, 2, 3, 10} {
		if nums[i], err = wordUint64(words[w]); err != nil || nums[i] > 1<<40 {
			return Proposal{}, fmt.Errorf("getProposal(%d): word %d is not a time", id, w)
		}
	}
	if strings.Trim(words[8][:24], "0") != "" {
		return Proposal{}, fmt.Errorf("getProposal(%d): payload is not an address", id)
	}
	sec := func(n uint64) time.Duration { return time.Duration(n) * time.Second }
	return Proposal{
		Payload:     "0x" + words[8][24:],
		Creation:    time.Unix(int64(nums[4]), 0).UTC(),
		VotingDelay: sec(nums[0]), VotingDuration: sec(nums[1]),
		ExecutionDelay: sec(nums[2]), GracePeriod: sec(nums[3]),
	}, nil
}

// maxActions and maxURI bound what x3f decodes from a payload: the data is
// chosen by whoever made the proposal.
const (
	maxActions = 64
	maxURI     = 2048
)

// PayloadActions returns the actions of a proposal's payload.
func (c *Client) PayloadActions(ctx context.Context, endpoint, payload string) ([]Action, error) {
	b, err := c.callRaw(ctx, endpoint, payload, selGetActions)
	if err != nil {
		return nil, fmt.Errorf("getActions: %w", err)
	}
	actions, err := decodeActions(b)
	if err != nil {
		return nil, fmt.Errorf("getActions: %w", err)
	}
	return actions, nil
}

// PayloadURI returns the description of a proposal's payload: free text
// chosen by whoever made the proposal, e.g. a link or a name.
func (c *Client) PayloadURI(ctx context.Context, endpoint, payload string) (string, error) {
	b, err := c.callRaw(ctx, endpoint, payload, selGetURI)
	if err != nil {
		return "", fmt.Errorf("getURI: %w", err)
	}
	uri, err := decodeString(b)
	if err != nil {
		return "", fmt.Errorf("getURI: %w", err)
	}
	return uri, nil
}

// RegisteredRollup returns the rollup that an addRollup action on the
// registry at registry registers, if the actions contain one.
func RegisteredRollup(actions []Action, registry string) (string, bool) {
	for _, a := range actions {
		if !strings.EqualFold(a.Target, registry) || len(a.Data) != 4+32 || "0x"+hex.EncodeToString(a.Data[:4]) != selAddRollup {
			continue
		}
		arg := a.Data[4:]
		if strings.Trim(hex.EncodeToString(arg[:12]), "0") == "" {
			return "0x" + hex.EncodeToString(arg[12:]), true
		}
	}
	return "", false
}

// decodeActions decodes the ABI encoding of Action[], a dynamic array of
// (address target, bytes data) tuples. Offsets are relative to the start
// of the array's elements and of each tuple. Every offset and length is
// checked against the data, so malformed data gives an error, not a panic.
func decodeActions(b []byte) ([]Action, error) {
	arr, err := offsetAt(b, 0)
	if err != nil {
		return nil, err
	}
	n, err := offsetAt(b, arr)
	if err != nil || n > maxActions {
		return nil, fmt.Errorf("invalid number of actions")
	}
	base := arr + 32
	actions := make([]Action, 0, n)
	for i := range n {
		rel, err := offsetAt(b, base+32*i)
		if err != nil {
			return nil, err
		}
		tuple := base + rel
		target, err := wordAt(b, tuple)
		if err != nil || strings.Trim(hex.EncodeToString(target[:12]), "0") != "" {
			return nil, fmt.Errorf("action %d: target is not an address", i)
		}
		dataRel, err := offsetAt(b, tuple+32)
		if err != nil {
			return nil, err
		}
		data, err := bytesAt(b, tuple+dataRel, len(b))
		if err != nil {
			return nil, fmt.Errorf("action %d: %w", i, err)
		}
		actions = append(actions, Action{Target: "0x" + hex.EncodeToString(target[12:]), Data: data})
	}
	return actions, nil
}

// decodeString decodes the ABI encoding of a returned string.
func decodeString(b []byte) (string, error) {
	off, err := offsetAt(b, 0)
	if err != nil {
		return "", err
	}
	s, err := bytesAt(b, off, maxURI)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(s) {
		return "", errors.New("string is not UTF-8")
	}
	return string(s), nil
}

// wordAt returns the 32-byte word at off.
func wordAt(b []byte, off int) ([]byte, error) {
	if off < 0 || off > len(b)-32 {
		return nil, errors.New("data too short")
	}
	return b[off : off+32], nil
}

// offsetAt reads the word at off as an offset or length, which must lie
// within the data.
func offsetAt(b []byte, off int) (int, error) {
	w, err := wordAt(b, off)
	if err != nil {
		return 0, err
	}
	n := new(big.Int).SetBytes(w)
	if !n.IsInt64() || n.Int64() > int64(len(b)) {
		return 0, errors.New("offset or length out of range")
	}
	return int(n.Int64()), nil
}

// bytesAt reads a length-prefixed byte string at off, at most limit bytes.
func bytesAt(b []byte, off, limit int) ([]byte, error) {
	n, err := offsetAt(b, off)
	if err != nil {
		return nil, err
	}
	if n > limit || off+32+n > len(b) {
		return nil, errors.New("bytes out of range")
	}
	return b[off+32 : off+32+n], nil
}
