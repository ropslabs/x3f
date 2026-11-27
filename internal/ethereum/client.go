// Package ethereum queries Ethereum execution and consensus clients
// (read-only) and parses the version strings they report.
//
// Endpoint URLs are treated as secrets: RPC providers often put API keys into
// the path, query or subdomain. Requests go through internal/httpjson, whose
// errors never contain the URL or any part of it.
package ethereum

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ropslabs/x3f/internal/httpjson"
)

// Client queries execution and consensus clients over HTTP.
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

// ExecutionVersion calls web3_clientVersion on an execution client's JSON-RPC
// endpoint and returns the raw version string.
func (c *Client) ExecutionVersion(ctx context.Context, endpoint string) (string, error) {
	var version string
	if err := c.callRPC(ctx, endpoint, "web3_clientVersion", &version); err != nil {
		return "", fmt.Errorf("web3_clientVersion: %w", err)
	}
	return version, nil
}

// ConsensusVersion calls GET /eth/v1/node/version on a consensus client's
// beacon API and returns the raw version string.
func (c *Client) ConsensusVersion(ctx context.Context, endpoint string) (string, error) {
	var resp struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := c.getBeacon(ctx, endpoint, "eth/v1/node/version", &resp); err != nil {
		return "", fmt.Errorf("/eth/v1/node/version: %w", err)
	}
	if resp.Data.Version == "" {
		return "", errors.New("/eth/v1/node/version: response has no data.version")
	}
	return resp.Data.Version, nil
}

func (c *Client) callRPC(ctx context.Context, endpoint, method string, result any) error {
	return c.json.Call(ctx, endpoint, method, result)
}

func (c *Client) getBeacon(ctx context.Context, endpoint, path string, out any) error {
	return c.json.Get(ctx, endpoint, path, out)
}
