package network

import "testing"

func TestLookup(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   uint64
	}{{"mainnet", 1}, {"sepolia", 11155111}, {"hoodi", 560048}} {
		if n, ok := ByChainID(tt.id); !ok || n.Name != tt.name {
			t.Errorf("ByChainID(%d) = %+v, %v", tt.id, n, ok)
		}
		if n, ok := ByName(tt.name); !ok || n.ChainID != tt.id {
			t.Errorf("ByName(%q) = %+v, %v", tt.name, n, ok)
		}
	}
	// Holesky (17000) is deprecated and deliberately unknown.
	if _, ok := ByChainID(17000); ok {
		t.Error("holesky must not be known")
	}
	if _, ok := ByName("holesky"); ok {
		t.Error("holesky must not be known")
	}
	if got := Describe(560048); got != "hoodi" {
		t.Errorf("Describe(560048) = %q", got)
	}
	if got := Describe(1337); got != "chain 1337" {
		t.Errorf("Describe(1337) = %q", got)
	}
}
