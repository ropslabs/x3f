package ethereum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// FarFutureEpoch is the epoch consensus clients use for forks they know
// but that are not scheduled (FAR_FUTURE_EPOCH = 2**64 - 1).
const FarFutureEpoch = math.MaxUint64

// ExecutionForkConfig is the fork part of an eth_config answer (EIP-7910).
// eth_config describes the current, the next and the last scheduled fork
// configuration; it has no fork names, only activation times.
type ExecutionForkConfig struct {
	Current ForkActivation
	// Next and Last are nil if the client has no future fork configured.
	// If only one future fork is configured, both are the same.
	Next, Last *ForkActivation
}

// ForkActivation is one configuration of eth_config.
type ForkActivation struct {
	// ActivationTime is the fork's Unix timestamp; 0 for genesis.
	ActivationTime int64
	ForkID         string
}

// ExecutionForkConfig calls eth_config on an execution client.
func (c *Client) ExecutionForkConfig(ctx context.Context, endpoint string) (ExecutionForkConfig, error) {
	type activation struct {
		// A pointer tells a missing field apart from 0 (genesis).
		ActivationTime *int64 `json:"activationTime"`
		ForkID         string `json:"forkId"`
	}
	var resp struct {
		Current *activation `json:"current"`
		Next    *activation `json:"next"`
		Last    *activation `json:"last"`
	}
	if err := c.callRPC(ctx, endpoint, "eth_config", &resp); err != nil {
		return ExecutionForkConfig{}, fmt.Errorf("eth_config: %w", err)
	}
	convert := func(a *activation) (*ForkActivation, error) {
		if a == nil {
			return nil, nil
		}
		if a.ActivationTime == nil || *a.ActivationTime < 0 {
			return nil, errors.New("eth_config: configuration without valid activationTime")
		}
		return &ForkActivation{ActivationTime: *a.ActivationTime, ForkID: a.ForkID}, nil
	}
	if resp.Current == nil {
		return ExecutionForkConfig{}, errors.New("eth_config: response has no current configuration")
	}
	var cfg ExecutionForkConfig
	cur, err := convert(resp.Current)
	if err != nil {
		return ExecutionForkConfig{}, err
	}
	cfg.Current = *cur
	if cfg.Next, err = convert(resp.Next); err != nil {
		return ExecutionForkConfig{}, err
	}
	if cfg.Last, err = convert(resp.Last); err != nil {
		return ExecutionForkConfig{}, err
	}
	return cfg, nil
}

// ConsensusForkConfig is the fork part of GET /eth/v1/config/spec.
type ConsensusForkConfig struct {
	// Epochs maps the fork name in upper case ("FULU", "GLOAS") to its
	// epoch, from the <NAME>_FORK_EPOCH keys. The spec lists the constants
	// of every fork the client knows, so a missing name means the client
	// does not know that fork. FarFutureEpoch means known, not scheduled.
	Epochs map[string]uint64
	// BlobSchedule holds the epochs of the BLOB_SCHEDULE entries, in the
	// order returned. Entries may also exist for hard forks (the official
	// example lists Deneb and Electra), not only for BPO forks.
	BlobSchedule []uint64
}

// ConsensusForkConfig reads the fork epochs and the blob schedule from
// GET /eth/v1/config/spec.
func (c *Client) ConsensusForkConfig(ctx context.Context, endpoint string) (ConsensusForkConfig, error) {
	var resp struct {
		// json.RawMessage: the values have different types (quoted
		// integers, hex strings, arrays); only a few are decoded.
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := c.getBeacon(ctx, endpoint, "eth/v1/config/spec", &resp); err != nil {
		return ConsensusForkConfig{}, fmt.Errorf("/eth/v1/config/spec: %w", err)
	}
	if resp.Data == nil {
		return ConsensusForkConfig{}, errors.New("/eth/v1/config/spec: response has no data")
	}
	cfg := ConsensusForkConfig{Epochs: map[string]uint64{}}
	for key, raw := range resp.Data {
		name, ok := strings.CutSuffix(key, "_FORK_EPOCH")
		if !ok || name == "" {
			continue
		}
		var ep decUint64
		if err := json.Unmarshal(raw, &ep); err != nil {
			// The key is ours, the value is not quoted in the error.
			return ConsensusForkConfig{}, fmt.Errorf("/eth/v1/config/spec: %s is not an epoch", key)
		}
		cfg.Epochs[name] = uint64(ep)
	}
	if raw, ok := resp.Data["BLOB_SCHEDULE"]; ok {
		var entries []struct {
			Epoch *decUint64 `json:"EPOCH"`
		}
		if err := json.Unmarshal(raw, &entries); err != nil {
			return ConsensusForkConfig{}, errors.New("/eth/v1/config/spec: BLOB_SCHEDULE is not a list of entries")
		}
		for _, e := range entries {
			if e.Epoch == nil {
				return ConsensusForkConfig{}, errors.New("/eth/v1/config/spec: BLOB_SCHEDULE entry without EPOCH")
			}
			cfg.BlobSchedule = append(cfg.BlobSchedule, uint64(*e.Epoch))
		}
	}
	return cfg, nil
}
