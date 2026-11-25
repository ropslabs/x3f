package ethereum

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExecutionForkConfig(t *testing.T) {
	tests := []struct {
		fixture   string
		current   int64
		next      int64 // 0: no next fork
		last      int64
		currentID string
	}{
		// EIP-7910 example: Hoodi with Prague scheduled.
		{"eth-config-eip-example.json", 0, 1742999832, 1742999832, "0xbef71d30"},
		{"eth-config-eip-no-future.json", 0, 0, 0, "0xbef71d30"},
		// Live: current = BPO2 on mainnet and Hoodi, Glamsterdam on Sepolia.
		{"live/eth-config-publicnode-mainnet.json", 1767747671, 0, 0, "0x07c9462e"},
		{"live/eth-config-publicnode-hoodi.json", 1762955544, 0, 0, "0x23aa1351"},
		{"live/eth-config-publicnode-sepolia.json", 1791294816, 0, 0, "0x6c1d9423"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := rpcServer(t, "eth_config", readFixture(t, filepath.Join("execution", tt.fixture)))
			cfg, err := NewClient(nil).ExecutionForkConfig(context.Background(), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Current.ActivationTime != tt.current || cfg.Current.ForkID != tt.currentID {
				t.Errorf("current = %+v", cfg.Current)
			}
			check := func(name string, a *ForkActivation, want int64) {
				switch {
				case want == 0 && a != nil:
					t.Errorf("%s = %+v, want null", name, *a)
				case want != 0 && (a == nil || a.ActivationTime != want):
					t.Errorf("%s = %+v, want %d", name, a, want)
				}
			}
			check("next", cfg.Next, tt.next)
			check("last", cfg.Last, tt.last)
		})
	}
}

func TestExecutionForkConfigErrors(t *testing.T) {
	tests := []struct {
		name, body, wantErr string
	}{
		// Live: MEVblocker does not offer eth_config.
		{"method not available", string(readFixture(t, filepath.Join("execution", "live", "eth-config-mevblocker-mainnet.json"))), "eth_config: JSON-RPC error -32601"},
		// Synthetic malformed responses.
		{"no current", `{"jsonrpc":"2.0","id":1,"result":{"next":null,"last":null}}`, "no current configuration"},
		{"current without activationTime", `{"jsonrpc":"2.0","id":1,"result":{"current":{"forkId":"0x1"},"next":null,"last":null}}`, "without valid activationTime"},
		{"activationTime as string", `{"jsonrpc":"2.0","id":1,"result":{"current":{"activationTime":"0x0"},"next":null,"last":null}}`, "decode result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := rpcServer(t, "eth_config", []byte(tt.body))
			_, err := NewClient(nil).ExecutionForkConfig(context.Background(), srv.URL)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if IsTransportError(err) {
				t.Error("must not be a transport error: the fallback depends on it")
			}
		})
	}
}

func TestConsensusForkConfig(t *testing.T) {
	const ff = FarFutureEpoch
	tests := []struct {
		fixture string
		epochs  map[string]uint64 // subset that must match
		missing []string          // fork names the client does not know
		blobs   []uint64
	}{
		// beacon-APIs example: no fork epochs, BLOB_SCHEDULE for Deneb and Electra.
		{"config-spec-example.json", map[string]uint64{}, []string{"FULU"}, []uint64{269568, 364032}},
		{"live/spec-publicnode-mainnet.json", map[string]uint64{"FULU": 411392, "GLOAS": ff, "HEZE": ff}, nil, []uint64{412672, 419072}},
		{"live/spec-publicnode-sepolia.json", map[string]uint64{"FULU": 272640, "GLOAS": 353024, "HEZE": ff}, nil, []uint64{274176, 275712}},
		{"live/spec-lodestar-hoodi.json", map[string]uint64{"ELECTRA": 2048, "FULU": 50688, "GLOAS": ff}, nil, []uint64{52480, 54016}},
		// Nimbus has no HEZE_FORK_EPOCH: it does not know that fork yet.
		{"live/spec-nimbus-unstable-mainnet.json", map[string]uint64{"FULU": 411392, "GLOAS": ff}, []string{"HEZE"}, []uint64{412672, 419072}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := beaconServer(t, "/eth/v1/config/spec", readFixture(t, filepath.Join("consensus", tt.fixture)))
			cfg, err := NewClient(nil).ConsensusForkConfig(context.Background(), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.epochs {
				if got, ok := cfg.Epochs[name]; !ok || got != want {
					t.Errorf("%s = %d (present %v), want %d", name, got, ok, want)
				}
			}
			for _, name := range tt.missing {
				if _, ok := cfg.Epochs[name]; ok {
					t.Errorf("%s present, want missing", name)
				}
			}
			if !slices.Equal(cfg.BlobSchedule, tt.blobs) {
				t.Errorf("blob schedule = %v, want %v", cfg.BlobSchedule, tt.blobs)
			}
			for name := range maps.Keys(cfg.Epochs) {
				if strings.Contains(name, "_") || name != strings.ToUpper(name) {
					t.Errorf("unexpected fork name %q", name)
				}
			}
		})
	}
}

func TestConsensusForkConfigErrors(t *testing.T) {
	// Synthetic malformed responses.
	tests := []struct {
		name, body, wantErr string
	}{
		{"no data", `{}`, "response has no data"},
		{"bad epoch", `{"data":{"FULU_FORK_EPOCH":"soon"}}`, "FULU_FORK_EPOCH is not an epoch"},
		{"bad blob schedule", `{"data":{"BLOB_SCHEDULE":"none"}}`, "BLOB_SCHEDULE is not a list"},
		{"blob entry without epoch", `{"data":{"BLOB_SCHEDULE":[{"MAX_BLOBS_PER_BLOCK":"6"}]}}`, "entry without EPOCH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := beaconServer(t, "/eth/v1/config/spec", []byte(tt.body))
			_, err := NewClient(nil).ConsensusForkConfig(context.Background(), srv.URL)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
