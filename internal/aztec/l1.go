package aztec

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Reading the Aztec contracts on L1 (Ethereum), read-only with eth_call.
// The contract interfaces are those of aztec-packages v6.0.0 (commit
// 0219f11d81), l1-contracts/src/governance/interfaces/IRegistry.sol and
// core/interfaces/IRollup.sol.

// Function selectors: the first 4 bytes of the Keccak-256 hash of the
// function signature. Go's standard library has no Keccak-256, so they are
// constants here; TestSelectors checks them against recorded web3_sha3
// answers of an Ethereum node (testdata/live/l1).
const (
	// selGetCanonicalRollup is Registry.getCanonicalRollup().
	selGetCanonicalRollup = "0x971b0e6b"
	// selGetVersion is Rollup.getVersion().
	selGetVersion = "0x0d8e6e2c"
)

// addressRe matches an Ethereum address in hex.
var addressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

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
	word, err := c.call(ctx, endpoint, registry, selGetCanonicalRollup)
	if err != nil {
		return "", fmt.Errorf("getCanonicalRollup: %w", err)
	}
	// An address is the low 20 bytes of the word; the high 12 must be 0.
	if strings.Trim(word[:24], "0") != "" {
		return "", errors.New("getCanonicalRollup: result is not an address")
	}
	return "0x" + word[24:], nil
}

// RollupVersion returns the version of the rollup at rollup, the number
// getNodeInfo reports as rollupVersion.
func (c *Client) RollupVersion(ctx context.Context, endpoint, rollup string) (uint64, error) {
	word, err := c.call(ctx, endpoint, rollup, selGetVersion)
	if err != nil {
		return 0, fmt.Errorf("getVersion: %w", err)
	}
	n, _ := new(big.Int).SetString(word, 16)
	if !n.IsUint64() {
		return 0, errors.New("getVersion: version does not fit into 64 bits")
	}
	return n.Uint64(), nil
}

// call sends an eth_call without arguments to the contract at to and returns
// the first 32-byte word of the result as 64 lowercase hex digits.
func (c *Client) call(ctx context.Context, endpoint, to, selector string) (string, error) {
	if !addressRe.MatchString(to) {
		return "", fmt.Errorf("invalid contract address %q", to)
	}
	var raw string
	call := map[string]string{"to": strings.ToLower(to), "data": selector}
	if err := c.json.Call(ctx, endpoint, "eth_call", &raw, call, "latest"); err != nil {
		return "", err
	}
	b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	switch {
	case !strings.HasPrefix(raw, "0x") || err != nil:
		return "", errors.New("result is not hex")
	case len(b) == 0:
		// eth_call to an address without code returns "0x".
		return "", errors.New("empty result: no contract at this address?")
	case len(b) < 32:
		return "", fmt.Errorf("result has %d bytes, want 32", len(b))
	}
	return hex.EncodeToString(b[:32]), nil
}
