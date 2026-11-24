package ethereum

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionSync(t *testing.T) {
	tests := []struct {
		fixture      string
		want         ExecutionSyncStatus
		wantIndexing bool
		wantBehind   uint64
		wantBehindOK bool
	}{
		{
			fixture: "syncing-false.json",
			want:    ExecutionSyncStatus{},
			// Not syncing: BlocksBehind has no target, Indexing is false.
		},
		{
			fixture:      "syncing-spec-example.json",
			want:         ExecutionSyncStatus{Syncing: true, StartingBlock: 0x0, CurrentBlock: 0x1518, HighestBlock: 0x9567a3},
			wantBehind:   0x9567a3 - 0x1518,
			wantBehindOK: true,
		},
		{
			fixture:      "syncing-geth.json",
			want:         ExecutionSyncStatus{Syncing: true, StartingBlock: 0x0, CurrentBlock: 0x1518, HighestBlock: 0x9567a3},
			wantBehind:   0x9567a3 - 0x1518,
			wantBehindOK: true,
		},
		{
			fixture: "live/syncing-false-flashbots-mainnet.json",
			want:    ExecutionSyncStatus{},
		},
		{
			fixture: "syncing-geth-indexing.json",
			want: ExecutionSyncStatus{
				Syncing: true, CurrentBlock: 0x9567a3, HighestBlock: 0x9567a3,
				TxIndexRemainingBlocks: 0x1518,
			},
			wantIndexing: true,
			wantBehind:   0,
			wantBehindOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			srv := rpcServer(t, "eth_syncing", readFixture(t, filepath.Join("execution", tt.fixture)))

			got, err := NewClient(nil).ExecutionSync(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
			if got.Indexing() != tt.wantIndexing {
				t.Errorf("Indexing() = %v, want %v", got.Indexing(), tt.wantIndexing)
			}
			if tt.want.Syncing {
				behind, ok := got.BlocksBehind()
				if behind != tt.wantBehind || ok != tt.wantBehindOK {
					t.Errorf("BlocksBehind() = %d, %v; want %d, %v", behind, ok, tt.wantBehind, tt.wantBehindOK)
				}
			}
		})
	}
}

// Edge cases of the derived values; the inputs are synthetic.
func TestExecutionSyncStatusEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		in           ExecutionSyncStatus
		wantBehind   uint64
		wantOK       bool
		wantIndexing bool
	}{
		{
			name:   "target unknown (highestBlock 0)",
			in:     ExecutionSyncStatus{Syncing: true, CurrentBlock: 100},
			wantOK: false,
		},
		{
			name:   "nothing known yet",
			in:     ExecutionSyncStatus{Syncing: true},
			wantOK: false,
		},
		{
			name:       "current ahead of highest is 0, not an underflow",
			in:         ExecutionSyncStatus{Syncing: true, CurrentBlock: 200, HighestBlock: 100},
			wantBehind: 0,
			wantOK:     true,
		},
		{
			name:       "one block behind",
			in:         ExecutionSyncStatus{Syncing: true, CurrentBlock: 99, HighestBlock: 100},
			wantBehind: 1,
			wantOK:     true,
		},
		{
			name:       "index remaining but blocks missing is syncing, not indexing",
			in:         ExecutionSyncStatus{Syncing: true, CurrentBlock: 50, HighestBlock: 100, StateIndexRemaining: 5},
			wantBehind: 50,
			wantOK:     true,
		},
		{
			name:         "trienode index only",
			in:           ExecutionSyncStatus{Syncing: true, CurrentBlock: 100, HighestBlock: 100, TrienodeIndexRemaining: 1},
			wantBehind:   0,
			wantOK:       true,
			wantIndexing: true,
		},
		{
			name:         "index fields while not syncing are ignored",
			in:           ExecutionSyncStatus{Syncing: false, TxIndexRemainingBlocks: 1},
			wantOK:       false,
			wantIndexing: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behind, ok := tt.in.BlocksBehind()
			if behind != tt.wantBehind || ok != tt.wantOK {
				t.Errorf("BlocksBehind() = %d, %v; want %d, %v", behind, ok, tt.wantBehind, tt.wantOK)
			}
			if got := tt.in.Indexing(); got != tt.wantIndexing {
				t.Errorf("Indexing() = %v, want %v", got, tt.wantIndexing)
			}
		})
	}
}

func TestExecutionSyncErrors(t *testing.T) {
	// Synthetic malformed responses.
	tests := []struct {
		name    string
		result  string
		wantErr string
	}{
		{"null result", `null`, "neither result nor error"},
		{"string result", `"syncing"`, "neither false nor an object"},
		{"quantity without 0x", `{"currentBlock":"1518","highestBlock":"0x9567a3"}`, "no 0x prefix"},
		{"invalid hex digits", `{"currentBlock":"0xZZ","highestBlock":"0x9567a3"}`, "invalid hex quantity"},
		{"empty quantity", `{"currentBlock":"0x","highestBlock":"0x9567a3"}`, "no 0x prefix"},
		{"quantity as number", `{"currentBlock":5400,"highestBlock":"0x9567a3"}`, "must be a hex string"},
		{"quantity overflows uint64", `{"currentBlock":"0x10000000000000000"}`, "invalid hex quantity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"result":` + tt.result + `}`
			srv := rpcServer(t, "eth_syncing", []byte(body))

			_, err := NewClient(nil).ExecutionSync(context.Background(), srv.URL)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if !strings.HasPrefix(err.Error(), "eth_syncing: ") {
				t.Errorf("error %q should name the method", err)
			}
		})
	}
}

func TestConsensusSync(t *testing.T) {
	// ptr returns a pointer to b. Go has no &true, so a tiny helper is
	// the usual way to get pointers to literals.
	ptr := func(b bool) *bool { return &b }

	tests := []struct {
		fixture string
		want    ConsensusSyncStatus
	}{
		{
			fixture: "syncing-spec-example.json",
			want:    ConsensusSyncStatus{HeadSlot: 1, SyncDistance: 1, IsSyncing: true, IsOptimistic: ptr(true), ELOffline: ptr(true)},
		},
		{
			fixture: "syncing-synced.json",
			want:    ConsensusSyncStatus{HeadSlot: 1, SyncDistance: 0, IsSyncing: false, IsOptimistic: ptr(false), ELOffline: ptr(false)},
		},
		{
			// Recorded live; fields in a different order than the spec.
			fixture: "live/syncing-lighthouse-publicnode-mainnet.json",
			want:    ConsensusSyncStatus{HeadSlot: 15727077, SyncDistance: 0, IsSyncing: false, IsOptimistic: ptr(true), ELOffline: ptr(false)},
		},
		{
			fixture: "live/syncing-nimbus-unstable-mainnet.json",
			want:    ConsensusSyncStatus{HeadSlot: 15727077, SyncDistance: 0, IsSyncing: false, IsOptimistic: ptr(true), ELOffline: ptr(true)},
		},
		{
			fixture: "syncing-nimbus-pre-bellatrix.json",
			want:    ConsensusSyncStatus{HeadSlot: 1, SyncDistance: 0, IsSyncing: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			body := readFixture(t, filepath.Join("consensus", tt.fixture))
			srv := beaconServer(t, "/eth/v1/node/syncing", body)

			got, err := NewClient(nil).ConsensusSync(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Structs with pointer fields cannot be compared with ==
			// meaningfully (it compares addresses), so compare by hand.
			if got.HeadSlot != tt.want.HeadSlot || got.SyncDistance != tt.want.SyncDistance || got.IsSyncing != tt.want.IsSyncing {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if !equalBoolPtr(got.IsOptimistic, tt.want.IsOptimistic) {
				t.Errorf("IsOptimistic = %s, want %s", fmtBoolPtr(got.IsOptimistic), fmtBoolPtr(tt.want.IsOptimistic))
			}
			if !equalBoolPtr(got.ELOffline, tt.want.ELOffline) {
				t.Errorf("ELOffline = %s, want %s", fmtBoolPtr(got.ELOffline), fmtBoolPtr(tt.want.ELOffline))
			}
		})
	}
}

func TestConsensusSyncLenientNumbers(t *testing.T) {
	// Synthetic: plain JSON numbers instead of the spec's decimal strings.
	body := `{"data":{"head_slot":12,"sync_distance":3,"is_syncing":true}}`
	srv := beaconServer(t, "/eth/v1/node/syncing", []byte(body))

	got, err := NewClient(nil).ConsensusSync(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HeadSlot != 12 || got.SyncDistance != 3 || !got.IsSyncing {
		t.Errorf("got %+v", got)
	}
}

func TestConsensusSyncErrors(t *testing.T) {
	// Synthetic malformed responses.
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"no data", `{}`, "lacks head_slot"},
		{"missing is_syncing", `{"data":{"head_slot":"1","sync_distance":"0"}}`, "lacks head_slot"},
		{"missing sync_distance", `{"data":{"head_slot":"1","is_syncing":false}}`, "lacks head_slot"},
		{"invalid number", `{"data":{"head_slot":"abc","sync_distance":"0","is_syncing":false}}`, "invalid Uint64"},
		{"negative number", `{"data":{"head_slot":"-1","sync_distance":"0","is_syncing":false}}`, "invalid Uint64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := beaconServer(t, "/eth/v1/node/syncing", []byte(tt.body))

			_, err := NewClient(nil).ConsensusSync(context.Background(), srv.URL)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// The sync calls use the same request code as the version calls; this
// checks that their errors are redacted as well.
func TestSyncErrorsDoNotLeakURL(t *testing.T) {
	const key = "syncsecret0123456789"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Synthetic: echo the request URL in a JSON-RPC error / bad body.
		if r.Method == http.MethodPost {
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"bad key `+r.URL.String()+`"}}`)
			return
		}
		io.WriteString(w, `{"data":{"head_slot":"`+r.URL.String()+`","sync_distance":"0","is_syncing":false}}`)
	}))
	defer srv.Close()
	endpoint := srv.URL + "/v3/" + key + "?apikey=" + key

	c := NewClient(nil)
	_, errEL := c.ExecutionSync(context.Background(), endpoint)
	_, errCL := c.ConsensusSync(context.Background(), endpoint)
	for _, err := range []error{errEL, errCL} {
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("error leaks URL: %v", err)
		}
	}
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fmtBoolPtr(b *bool) string {
	if b == nil {
		return "<nil>"
	}
	if *b {
		return "true"
	}
	return "false"
}
