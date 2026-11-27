package aztec

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// TestNetworksMatchNetworkConfig compares the built-in networks with the
// copy of network_config.json in testdata/networks (see SOURCES.md).
func TestNetworksMatchNetworkConfig(t *testing.T) {
	var cfg map[string]struct {
		L1ChainID uint64 `json:"l1ChainId"`
		Registry  string `json:"registryAddress"`
	}
	if err := json.Unmarshal(readFixture(t, "networks/network_config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	if got, want := NetworkNames(), slices.Sorted(maps.Keys(cfg)); !slices.Equal(slices.Sorted(slices.Values(got)), want) {
		t.Errorf("networks %v, network_config.json has %v", got, want)
	}
	for name, c := range cfg {
		n, ok := NetworkByName(name)
		if !ok || n.L1ChainID != c.L1ChainID || n.Registry != strings.ToLower(c.Registry) {
			t.Errorf("%s: built in %+v, network_config.json %+v", name, n, c)
		}
	}
}

func TestIdentifyNetwork(t *testing.T) {
	tests := []struct {
		name     string
		chainID  uint64
		registry string
		want     string // "" means unknown
	}{
		// Values of the recorded getNodeInfo answers.
		{"mainnet", 1, "0x35b22e09ee0390539439e24f06da43d83f90e298", "mainnet"},
		{"testnet", 11155111, "0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba", "testnet"},
		// The docs write checksummed (mixed-case) addresses.
		{"mixed case", 1, "0x35b22e09Ee0390539439E24f06Da43D83f90e298", "mainnet"},
		{"registry on another chain", 11155111, "0x35b22e09ee0390539439e24f06da43d83f90e298", ""},
		{"unknown registry", 1, "0x0000000000000000000000000000000000000001", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := IdentifyNetwork(tt.chainID, tt.registry)
			if got := n.Name; ok != (tt.want != "") || got != tt.want {
				t.Errorf("IdentifyNetwork = %q, %v; want %q", got, ok, tt.want)
			}
		})
	}
	if _, ok := NetworkByName("alpha"); ok {
		t.Error(`"alpha" is the docs name, not a configuration value`)
	}
}
