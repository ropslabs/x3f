package ethereum

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionChainID(t *testing.T) {
	tests := []struct {
		fixture string
		want    uint64
	}{
		{"chainid-spec-example.json", 1},
		{"live/chainid-publicnode-mainnet.json", 1},
		{"live/chainid-publicnode-sepolia.json", 11155111}, // 0xaa36a7
		{"live/chainid-publicnode-hoodi.json", 560048},     // 0x88bb0
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := rpcServer(t, "eth_chainId", readFixture(t, filepath.Join("execution", tt.fixture)))
			got, err := NewClient(nil).ExecutionChainID(context.Background(), srv.URL)
			if err != nil || got != tt.want {
				t.Errorf("got %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestConsensusChainID(t *testing.T) {
	tests := []struct {
		fixture string
		want    uint64
	}{
		{"deposit-contract-spec-example.json", 1},
		{"live/deposit-contract-publicnode-mainnet.json", 1},
		{"live/deposit-contract-publicnode-sepolia.json", 11155111},
		{"live/deposit-contract-lodestar-hoodi.json", 560048},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := beaconServer(t, "/eth/v1/config/deposit_contract", readFixture(t, filepath.Join("consensus", tt.fixture)))
			got, err := NewClient(nil).ConsensusChainID(context.Background(), srv.URL)
			if err != nil || got != tt.want {
				t.Errorf("got %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestChainIDErrors(t *testing.T) {
	// Synthetic malformed responses.
	t.Run("execution: no hex", func(t *testing.T) {
		srv := rpcServer(t, "eth_chainId", []byte(`{"jsonrpc":"2.0","id":1,"result":"1"}`))
		_, err := NewClient(nil).ExecutionChainID(context.Background(), srv.URL)
		if err == nil || !strings.Contains(err.Error(), "eth_chainId: decode result") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("consensus: chain_id missing", func(t *testing.T) {
		srv := beaconServer(t, "/eth/v1/config/deposit_contract", []byte(`{"data":{"address":"0x00"}}`))
		_, err := NewClient(nil).ConsensusChainID(context.Background(), srv.URL)
		if err == nil || !strings.Contains(err.Error(), "no data.chain_id") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("consensus: chain_id not a number", func(t *testing.T) {
		srv := beaconServer(t, "/eth/v1/config/deposit_contract", []byte(`{"data":{"chain_id":"one"}}`))
		_, err := NewClient(nil).ConsensusChainID(context.Background(), srv.URL)
		if err == nil || !strings.Contains(err.Error(), "invalid Uint64") {
			t.Errorf("error = %v", err)
		}
	})
}
