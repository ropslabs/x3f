// Package aztec queries Aztec nodes (read-only) over their JSON-RPC API.
//
// Endpoint URLs are treated as secrets: requests go through
// internal/httpjson, whose errors never contain the URL or any part of it.
package aztec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/software"
)

// Client queries Aztec nodes over HTTP.
//
// The zero value is not usable; create one with NewClient.
type Client struct {
	json *httpjson.Client
}

// NewClient returns a Client that uses httpClient for requests. Pass nil to
// use a default client. Timeouts are controlled per call through the
// context; redirects are not followed (see httpjson.NewClient).
func NewClient(httpClient *http.Client) *Client {
	return &Client{json: httpjson.NewClient(httpClient)}
}

// NodeInfo is the part of the getNodeInfo answer that x3f uses.
type NodeInfo struct {
	// NodeVersion is the release of the node software, e.g. "6.0.0".
	NodeVersion string `json:"nodeVersion"`
	// L1ChainID is the chain ID of the L1 (Ethereum) network.
	L1ChainID uint64 `json:"l1ChainId"`
	// RollupVersion identifies the rollup the node follows; the L1
	// registry lists the versions.
	RollupVersion uint64 `json:"rollupVersion"`
	// L1Contracts are the addresses of the node's L1 contracts.
	L1Contracts L1Contracts `json:"l1ContractAddresses"`
}

// L1Contracts are the L1 contract addresses from getNodeInfo that x3f uses.
type L1Contracts struct {
	Rollup             string `json:"rollupAddress"`
	Registry           string `json:"registryAddress"`
	Governance         string `json:"governanceAddress"`
	GovernanceProposer string `json:"governanceProposerAddress"`
}

// methodNotFound is the JSON-RPC 2.0 error code for an unknown method.
const methodNotFound = -32601

// callNode calls a method of the node API. Aztec nodes serve it as aztec_*
// since v5 and as node_* before (and still, for compatibility, in v5 and
// v6); if the node does not know the aztec_ method, the node_ one is tried.
func (c *Client) callNode(ctx context.Context, endpoint, method string, out any) error {
	err := c.json.Call(ctx, endpoint, "aztec_"+method, out)
	if code, ok := httpjson.RPCErrorCode(err); ok && code == methodNotFound {
		err = c.json.Call(ctx, endpoint, "node_"+method, out)
	}
	return err
}

// NodeInfo calls getNodeInfo.
func (c *Client) NodeInfo(ctx context.Context, endpoint string) (NodeInfo, error) {
	var info NodeInfo
	if err := c.callNode(ctx, endpoint, "getNodeInfo", &info); err != nil {
		return NodeInfo{}, fmt.Errorf("getNodeInfo: %w", err)
	}
	if info.NodeVersion == "" {
		return NodeInfo{}, errors.New("getNodeInfo: response has no nodeVersion")
	}
	return info, nil
}

// versionRe matches a node version such as "6.0.0" or "6.1.0-rc.2" (an
// optional "v" is accepted). Group 1 is the numeric part, group 2 the
// suffix without its leading "-".
var versionRe = regexp.MustCompile(`^[vV]?(\d+\.\d+\.\d+)(?:-(.+))?$`)

// ParseNodeVersion turns the nodeVersion of getNodeInfo into a
// software.Reported. If it does not look like a version, Version is empty
// and Raw keeps what the node said.
func ParseNodeVersion(raw string) software.Reported {
	v := software.Reported{Raw: raw, Name: software.Aztec}
	if m := versionRe.FindStringSubmatch(raw); m != nil {
		v.Version, v.Suffix = m[1], m[2]
	}
	return v
}

// ChainTips are the checkpoint numbers a node has reached, from its
// getChainTips answer: the newest checkpoint it has seen on L1
// (checkpointed) and the newest proven one.
type ChainTips struct {
	Checkpointed uint64
	Proven       uint64
}

// ChainTips calls getChainTips.
func (c *Client) ChainTips(ctx context.Context, endpoint string) (ChainTips, error) {
	// Each tip has a block and a checkpoint; x3f compares checkpoints,
	// which is what L1 knows. Pointers tell a missing number from 0.
	type tip struct {
		Checkpoint struct {
			Number *uint64 `json:"number"`
		} `json:"checkpoint"`
	}
	var resp struct {
		Checkpointed tip `json:"checkpointed"`
		Proven       tip `json:"proven"`
	}
	if err := c.callNode(ctx, endpoint, "getChainTips", &resp); err != nil {
		return ChainTips{}, fmt.Errorf("getChainTips: %w", err)
	}
	if resp.Checkpointed.Checkpoint.Number == nil || resp.Proven.Checkpoint.Number == nil {
		return ChainTips{}, errors.New("getChainTips: response lacks the checkpoint numbers")
	}
	return ChainTips{Checkpointed: *resp.Checkpointed.Checkpoint.Number, Proven: *resp.Proven.Checkpoint.Number}, nil
}
