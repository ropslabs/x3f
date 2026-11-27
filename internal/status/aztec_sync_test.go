package status

import (
	"testing"
	"time"
)

// syncBase is the recorded mainnet snapshot (internal/aztec/testdata):
// 72-second slots, 32-slot epochs, pending 36762 at slot 57983, proven
// 36734 at slot 57951.
var syncBase = AztecSyncInput{
	SlotDuration: 72, EpochDuration: 32,
	L1Pending: 36762, L1PendingSlot: 57983, L1Proven: 36734, L1ProvenSlot: 57951,
	NodeCheckpointed: 36762, NodeProven: 36734,
}

func TestAssessAztecSyncThresholds(t *testing.T) {
	tests := []struct {
		behind    uint64 // slots between the node's checkpoint and L1's
		wantLevel Level
		wantCell  string
	}{
		{0, OK, "synced"},
		{SyncWarnSlots, OK, "synced"},
		{SyncWarnSlots + 1, Warn, "3.6 min behind"},
		{SyncCritSlots, Warn, "9.6 min behind"},
		{SyncCritSlots + 1, Crit, "10.8 min behind"},
		{80, Crit, "1.6 h behind"},
	}
	for _, tt := range tests {
		in := syncBase
		in.NodeCheckpointed = in.L1Pending - 1
		in.NodeCheckpointedSlot = in.L1PendingSlot - tt.behind
		a, cell := AssessAztecSync(in)
		if a.Level != tt.wantLevel || cell != tt.wantCell {
			t.Errorf("%d slots behind: level %v, cell %q; want %v, %q", tt.behind, a.Level, cell, tt.wantLevel, tt.wantCell)
		}
	}
	// A node ahead of x3f's L1 read is in sync.
	in := syncBase
	in.NodeCheckpointed = in.L1Pending + 1
	if a, cell := AssessAztecSync(in); a.Level != OK || cell != "synced" {
		t.Errorf("ahead: level %v, cell %q", a.Level, cell)
	}
}

func TestAssessAztecProvenThresholds(t *testing.T) {
	tests := []struct {
		behind    uint64
		gone      bool
		wantLevel Level
	}{
		{0, false, OK},
		{1, false, Warn},
		{32, false, Warn}, // one epoch: one proof not seen yet
		{33, false, Crit}, // more than an epoch: proofs missed
		{0, true, Crit},   // no longer stored on L1
	}
	for _, tt := range tests {
		in := syncBase
		if tt.behind > 0 || tt.gone {
			in.NodeProven = in.L1Proven - 1
			in.NodeProvenSlot = in.L1ProvenSlot - tt.behind
			in.NodeProvenGone = tt.gone
		}
		if a := AssessAztecProven(in); a.Level != tt.wantLevel {
			t.Errorf("%d slots behind (gone %v): level %v, want %v", tt.behind, tt.gone, a.Level, tt.wantLevel)
		}
	}
}

func TestAssessNetworkProgress(t *testing.T) {
	tests := []struct {
		since     uint64 // slots without a new checkpoint
		wantOK    bool
		wantLevel Level
	}{
		{0, false, OK},
		{SyncCritSlots, false, OK},
		{SyncCritSlots + 1, true, OK}, // a note
		{32, true, OK},
		{33, true, Warn}, // more than an epoch
	}
	for _, tt := range tests {
		in := NetworkProgressInput{Network: "testnet", CurrentSlot: 43290 + tt.since, L1Pending: 42991, L1PendingSlot: 43290,
			SlotDuration: 72, EpochDuration: 32}
		f, ok := AssessNetworkProgress(in)
		if ok != tt.wantOK || f.Level != tt.wantLevel {
			t.Errorf("%d slots: finding %v (%v), want %v at %v", tt.since, f, ok, tt.wantOK, tt.wantLevel)
		}
		if got, want := in.CheckpointAge(), time.Duration(tt.since*72)*time.Second; got != want {
			t.Errorf("%d slots: age %v, want %v", tt.since, got, want)
		}
	}
	// The newest L1 block may be older than x3f's view of the checkpoint.
	in := NetworkProgressInput{Network: "testnet", CurrentSlot: 10, L1PendingSlot: 11, SlotDuration: 72, EpochDuration: 32}
	if _, ok := AssessNetworkProgress(in); ok || in.CheckpointAge() != 0 {
		t.Errorf("checkpoint after the current slot: ok %v, age %v", ok, in.CheckpointAge())
	}
}
