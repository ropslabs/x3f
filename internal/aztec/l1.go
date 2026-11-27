package aztec

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/ropslabs/x3f/internal/httpjson"
)

// Reading the Aztec contracts on L1 (Ethereum), read-only with eth_call.
// The contract interfaces are those of aztec-packages v6.0.0 (commit
// 0219f11d81), l1-contracts/src/governance/interfaces/IRegistry.sol,
// core/interfaces/IRollup.sol and core/interfaces/IValidatorSelection.sol.

// Function selectors: the first 4 bytes of the Keccak-256 hash of the
// function signature. Go's standard library has no Keccak-256, so they are
// constants here; TestSelectors checks them against recorded web3_sha3
// answers of an Ethereum node (testdata/live/l1).
const (
	// selGetCanonicalRollup is Registry.getCanonicalRollup().
	selGetCanonicalRollup = "0x971b0e6b"
	// selGetVersion is Rollup.getVersion().
	selGetVersion = "0x0d8e6e2c"
	// selGetTips is Rollup.getTips(): the pending and the proven
	// checkpoint number.
	selGetTips = "0x88e4a073"
	// selGetCheckpoint is Rollup.getCheckpoint(uint256).
	selGetCheckpoint = "0x20fc4881"
	// selGetSlotAt is Rollup.getSlotAt(uint256).
	selGetSlotAt = "0xb9d09163"
	// selGetSlotDuration is Rollup.getSlotDuration().
	selGetSlotDuration = "0xc4014c12"
	// selGetEpochDuration is Rollup.getEpochDuration().
	selGetEpochDuration = "0x5d3ea8f1"
	// errUnavailableCheckpoint is the selector of the revert
	// Rollup__UnavailableTempCheckpointLog(uint256,uint256,uint256)
	// (core/libraries/Errors.sol).
	errUnavailableCheckpoint = "0x8711786b"
)

// ErrCheckpointUnavailable means a checkpoint is no longer stored on L1:
// the rollup keeps only the newest epochDuration × (proof submission
// epochs + 1) + 1 checkpoints (core/libraries/rollup/STFLib.sol), 65 on
// mainnet and testnet.
var ErrCheckpointUnavailable = errors.New("checkpoint is no longer stored on L1")

// addressRe matches an Ethereum address in hex.
var addressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// RollupTips are the newest checkpoint numbers the rollup on L1 knows.
type RollupTips struct {
	// Pending is the newest checkpoint published to L1.
	Pending uint64
	// Proven is the newest checkpoint with a proof on L1.
	Proven uint64
}

// L1Time returns the timestamp of the newest block of the L1 endpoint.
func (c *Client) L1Time(ctx context.Context, endpoint string) (uint64, error) {
	var blk struct {
		Timestamp string `json:"timestamp"`
	}
	if err := c.json.Call(ctx, endpoint, "eth_getBlockByNumber", &blk, "latest", false); err != nil {
		return 0, fmt.Errorf("eth_getBlockByNumber: %w", err)
	}
	ts, err := hexUint64(blk.Timestamp)
	if err != nil {
		return 0, fmt.Errorf("eth_getBlockByNumber: timestamp: %w", err)
	}
	return ts, nil
}

// Tips returns the pending and proven checkpoint numbers of the rollup.
func (c *Client) Tips(ctx context.Context, endpoint, rollup string) (RollupTips, error) {
	words, err := c.call(ctx, endpoint, rollup, selGetTips, 2)
	if err != nil {
		return RollupTips{}, fmt.Errorf("getTips: %w", err)
	}
	var t RollupTips
	if t.Pending, err = wordUint64(words[0]); err == nil {
		t.Proven, err = wordUint64(words[1])
	}
	if err != nil {
		return RollupTips{}, fmt.Errorf("getTips: %w", err)
	}
	return t, nil
}

// CheckpointSlot returns the slot of checkpoint n. It returns an error
// wrapping ErrCheckpointUnavailable if the rollup no longer stores it.
func (c *Client) CheckpointSlot(ctx context.Context, endpoint, rollup string, n uint64) (uint64, error) {
	// CheckpointLog has only fixed-size fields; slotNumber is the 7th
	// word (core/libraries/compressed-data/CheckpointLog.sol).
	words, err := c.call(ctx, endpoint, rollup, selGetCheckpoint, 7, n)
	if data, ok := revertData(err); ok && strings.HasPrefix(data, errUnavailableCheckpoint) {
		return 0, fmt.Errorf("getCheckpoint(%d): %w", n, ErrCheckpointUnavailable)
	}
	if err != nil {
		return 0, fmt.Errorf("getCheckpoint(%d): %w", n, err)
	}
	slot, err := wordUint64(words[6])
	if err != nil {
		return 0, fmt.Errorf("getCheckpoint(%d): %w", n, err)
	}
	return slot, nil
}

// SlotAt returns the slot of the rollup at the given Unix time.
func (c *Client) SlotAt(ctx context.Context, endpoint, rollup string, ts uint64) (uint64, error) {
	return c.uintCall(ctx, endpoint, rollup, "getSlotAt", selGetSlotAt, ts)
}

// SlotDuration returns the duration of a slot in seconds.
func (c *Client) SlotDuration(ctx context.Context, endpoint, rollup string) (uint64, error) {
	return c.uintCall(ctx, endpoint, rollup, "getSlotDuration", selGetSlotDuration)
}

// EpochDuration returns the number of slots in an epoch.
func (c *Client) EpochDuration(ctx context.Context, endpoint, rollup string) (uint64, error) {
	return c.uintCall(ctx, endpoint, rollup, "getEpochDuration", selGetEpochDuration)
}

// uintCall calls a function that returns one unsigned number.
func (c *Client) uintCall(ctx context.Context, endpoint, to, name, selector string, args ...uint64) (uint64, error) {
	words, err := c.call(ctx, endpoint, to, selector, 1, args...)
	if err == nil {
		var n uint64
		if n, err = wordUint64(words[0]); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("%s: %w", name, err)
}

// L1ChainID returns the chain ID of the L1 endpoint (eth_chainId).
func (c *Client) L1ChainID(ctx context.Context, endpoint string) (uint64, error) {
	var raw string
	if err := c.json.Call(ctx, endpoint, "eth_chainId", &raw); err != nil {
		return 0, fmt.Errorf("eth_chainId: %w", err)
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(raw, "0x"), 16)
	if !strings.HasPrefix(raw, "0x") || !ok || !n.IsUint64() {
		return 0, fmt.Errorf("eth_chainId: invalid chain ID %q", raw)
	}
	return n.Uint64(), nil
}

// CanonicalRollup returns the address of the canonical rollup, the one the
// network currently runs, from the registry at registry. The address is
// returned in lowercase.
func (c *Client) CanonicalRollup(ctx context.Context, endpoint, registry string) (string, error) {
	words, err := c.call(ctx, endpoint, registry, selGetCanonicalRollup, 1)
	if err != nil {
		return "", fmt.Errorf("getCanonicalRollup: %w", err)
	}
	word := words[0]
	// An address is the low 20 bytes of the word; the high 12 must be 0.
	if strings.Trim(word[:24], "0") != "" {
		return "", errors.New("getCanonicalRollup: result is not an address")
	}
	return "0x" + word[24:], nil
}

// RollupVersion returns the version of the rollup at rollup, the number
// getNodeInfo reports as rollupVersion.
func (c *Client) RollupVersion(ctx context.Context, endpoint, rollup string) (uint64, error) {
	words, err := c.call(ctx, endpoint, rollup, selGetVersion, 1)
	if err != nil {
		return 0, fmt.Errorf("getVersion: %w", err)
	}
	n, err := wordUint64(words[0])
	if err != nil {
		return 0, fmt.Errorf("getVersion: version %w", err)
	}
	return n, nil
}

// wordUint64 converts a 32-byte word in hex to a uint64.
func wordUint64(word string) (uint64, error) {
	n, ok := new(big.Int).SetString(word, 16)
	if !ok || !n.IsUint64() {
		return 0, errors.New("does not fit into 64 bits")
	}
	return n.Uint64(), nil
}

// hexUint64 converts a JSON-RPC quantity such as "0x5f5e100" to a uint64.
func hexUint64(s string) (uint64, error) {
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !strings.HasPrefix(s, "0x") || !ok || !n.IsUint64() {
		return 0, fmt.Errorf("invalid quantity %q", s)
	}
	return n.Uint64(), nil
}

// revertData returns the revert data of a failed eth_call, if err is one.
func revertData(err error) (string, bool) {
	var re *httpjson.RPCError
	if !errors.As(err, &re) || re.Data == "" {
		return "", false
	}
	return re.Data, true
}

// call sends an eth_call to the contract at to, with unsigned numbers as
// arguments, and returns the first words 32-byte words of the result, each
// as 64 lowercase hex digits.
func (c *Client) call(ctx context.Context, endpoint, to, selector string, words int, args ...uint64) ([]string, error) {
	if !addressRe.MatchString(to) {
		return nil, fmt.Errorf("invalid contract address %q", to)
	}
	data := selector
	for _, a := range args {
		data += fmt.Sprintf("%064x", a)
	}
	var raw string
	call := map[string]string{"to": strings.ToLower(to), "data": data}
	if err := c.json.Call(ctx, endpoint, "eth_call", &raw, call, "latest"); err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	switch {
	case !strings.HasPrefix(raw, "0x") || err != nil:
		return nil, errors.New("result is not hex")
	case len(b) == 0:
		// eth_call to an address without code returns "0x".
		return nil, errors.New("empty result: no contract at this address?")
	case len(b) < 32*words:
		return nil, fmt.Errorf("result has %d bytes, want %d", len(b), 32*words)
	}
	out := make([]string, words)
	for i := range out {
		out[i] = hex.EncodeToString(b[32*i : 32*(i+1)])
	}
	return out, nil
}
