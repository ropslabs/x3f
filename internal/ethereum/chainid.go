package ethereum

import (
	"context"
	"errors"
	"fmt"
)

// ExecutionChainID calls eth_chainId on an execution client.
func (c *Client) ExecutionChainID(ctx context.Context, endpoint string) (uint64, error) {
	var id hexUint64
	if err := c.callRPC(ctx, endpoint, "eth_chainId", &id); err != nil {
		return 0, fmt.Errorf("eth_chainId: %w", err)
	}
	return uint64(id), nil
}

// ConsensusChainID reads the chain ID of the execution chain a consensus
// client follows, from GET /eth/v1/config/deposit_contract.
//
// Only chain_id is used. The deposit contract address does not identify
// the network: Hoodi uses the same address as mainnet.
func (c *Client) ConsensusChainID(ctx context.Context, endpoint string) (uint64, error) {
	var resp struct {
		Data *struct {
			ChainID *decUint64 `json:"chain_id"`
		} `json:"data"`
	}
	if err := c.getBeacon(ctx, endpoint, "eth/v1/config/deposit_contract", &resp); err != nil {
		return 0, fmt.Errorf("/eth/v1/config/deposit_contract: %w", err)
	}
	if resp.Data == nil || resp.Data.ChainID == nil {
		return 0, errors.New("/eth/v1/config/deposit_contract: response has no data.chain_id")
	}
	return uint64(*resp.Data.ChainID), nil
}
