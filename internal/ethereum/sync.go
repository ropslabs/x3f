package ethereum

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ExecutionSyncStatus is the result of eth_syncing.
type ExecutionSyncStatus struct {
	// Syncing is false when the client returned false ("not syncing").
	Syncing bool

	// Block numbers from the standard response. Zero if not reported.
	StartingBlock uint64
	CurrentBlock  uint64
	HighestBlock  uint64

	// Geth-specific counters (internal/ethapi/api.go). Geth keeps reporting
	// "syncing" while any of these is non-zero, even when all blocks are
	// there. Other clients do not send them, so they stay zero.
	TxIndexRemainingBlocks uint64
	StateIndexRemaining    uint64
	TrienodeIndexRemaining uint64
}

// Indexing reports whether all blocks are there and the client is only
// building its indexes (a Geth state that is less severe than syncing).
func (s ExecutionSyncStatus) Indexing() bool {
	indexRemaining := s.TxIndexRemainingBlocks > 0 || s.StateIndexRemaining > 0 || s.TrienodeIndexRemaining > 0
	return s.Syncing && s.CurrentBlock >= s.HighestBlock && indexRemaining
}

// BlocksBehind returns how many blocks the client still needs. ok is false
// if the target is unknown (HighestBlock is 0, e.g. right after start).
// If CurrentBlock is ahead of HighestBlock (the target can lag behind), the
// result is 0 instead of an unsigned underflow.
func (s ExecutionSyncStatus) BlocksBehind() (behind uint64, ok bool) {
	if s.HighestBlock == 0 {
		return 0, false
	}
	if s.CurrentBlock >= s.HighestBlock {
		return 0, true
	}
	return s.HighestBlock - s.CurrentBlock, true
}

// ConsensusSyncStatus is the result of GET /eth/v1/node/syncing.
type ConsensusSyncStatus struct {
	HeadSlot     uint64
	SyncDistance uint64
	IsSyncing    bool
	// IsOptimistic and ELOffline are pointers because they can be missing:
	// Nimbus omits them before the Bellatrix / Capella forks. nil means
	// "not reported", which is different from false.
	IsOptimistic *bool
	ELOffline    *bool
}

// ExecutionSync calls eth_syncing on an execution client.
func (c *Client) ExecutionSync(ctx context.Context, endpoint string) (ExecutionSyncStatus, error) {
	var raw json.RawMessage
	if err := c.callRPC(ctx, endpoint, "eth_syncing", &raw); err != nil {
		return ExecutionSyncStatus{}, fmt.Errorf("eth_syncing: %w", err)
	}
	s, err := parseExecutionSync(raw)
	if err != nil {
		return ExecutionSyncStatus{}, fmt.Errorf("eth_syncing: %w", err)
	}
	return s, nil
}

func parseExecutionSync(raw json.RawMessage) (ExecutionSyncStatus, error) {
	// The result is either a boolean or an object. Look at the first
	// non-space byte to tell them apart.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] != '{' {
		var syncing bool
		if err := json.Unmarshal(trimmed, &syncing); err != nil {
			return ExecutionSyncStatus{}, errors.New("result is neither false nor an object")
		}
		// The spec only allows false here. Treat true as "syncing without
		// details" rather than failing.
		return ExecutionSyncStatus{Syncing: syncing}, nil
	}

	var obj struct {
		StartingBlock          hexUint64 `json:"startingBlock"`
		CurrentBlock           hexUint64 `json:"currentBlock"`
		HighestBlock           hexUint64 `json:"highestBlock"`
		TxIndexRemainingBlocks hexUint64 `json:"txIndexRemainingBlocks"`
		StateIndexRemaining    hexUint64 `json:"stateIndexRemaining"`
		TrienodeIndexRemaining hexUint64 `json:"trienodeIndexRemaining"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return ExecutionSyncStatus{}, fmt.Errorf("decode result: %w", err)
	}
	return ExecutionSyncStatus{
		Syncing:                true,
		StartingBlock:          uint64(obj.StartingBlock),
		CurrentBlock:           uint64(obj.CurrentBlock),
		HighestBlock:           uint64(obj.HighestBlock),
		TxIndexRemainingBlocks: uint64(obj.TxIndexRemainingBlocks),
		StateIndexRemaining:    uint64(obj.StateIndexRemaining),
		TrienodeIndexRemaining: uint64(obj.TrienodeIndexRemaining),
	}, nil
}

// ConsensusSync calls GET /eth/v1/node/syncing on a consensus client.
func (c *Client) ConsensusSync(ctx context.Context, endpoint string) (ConsensusSyncStatus, error) {
	var resp struct {
		Data *struct {
			HeadSlot     *decUint64 `json:"head_slot"`
			SyncDistance *decUint64 `json:"sync_distance"`
			IsSyncing    *bool      `json:"is_syncing"`
			IsOptimistic *bool      `json:"is_optimistic"`
			ELOffline    *bool      `json:"el_offline"`
		} `json:"data"`
	}
	if err := c.getBeacon(ctx, endpoint, "eth/v1/node/syncing", &resp); err != nil {
		return ConsensusSyncStatus{}, fmt.Errorf("/eth/v1/node/syncing: %w", err)
	}

	d := resp.Data
	// The spec marks these three as required. Missing ones would silently
	// read as 0/false and could make a syncing node look synced.
	if d == nil || d.HeadSlot == nil || d.SyncDistance == nil || d.IsSyncing == nil {
		return ConsensusSyncStatus{}, errors.New("/eth/v1/node/syncing: response lacks head_slot, sync_distance or is_syncing")
	}
	return ConsensusSyncStatus{
		HeadSlot:     uint64(*d.HeadSlot),
		SyncDistance: uint64(*d.SyncDistance),
		IsSyncing:    *d.IsSyncing,
		IsOptimistic: d.IsOptimistic,
		ELOffline:    d.ELOffline,
	}, nil
}

// Error messages in this file never quote data from the server: like HTTP
// error pages, a response could contain things that do not belong in logs.

// hexUint64 decodes a JSON-RPC quantity such as "0x1518".
//
// A type with an UnmarshalJSON method takes over its own decoding: the json
// package calls it instead of the default logic. The pointer receiver lets
// the method change the value it is called on.
type hexUint64 uint64

func (h *hexUint64) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New("quantity must be a hex string")
	}
	digits, ok := strings.CutPrefix(s, "0x")
	if !ok || digits == "" {
		return errors.New("quantity has no 0x prefix")
	}
	n, err := strconv.ParseUint(digits, 16, 64)
	if err != nil {
		return errors.New("invalid hex quantity")
	}
	*h = hexUint64(n)
	return nil
}

// decUint64 decodes a beacon API Uint64, which the spec encodes as a decimal
// string ("12345"). A plain JSON number is accepted too, to be lenient with
// clients that deviate from the spec.
type decUint64 uint64

func (d *decUint64) UnmarshalJSON(data []byte) error {
	s := string(data)
	if unquoted, err := strconv.Unquote(s); err == nil {
		s = unquoted
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return errors.New("invalid Uint64")
	}
	*d = decUint64(n)
	return nil
}
