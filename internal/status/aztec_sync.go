package status

import (
	"fmt"
	"time"
)

// Sync rules for Aztec nodes (agreed for step 4 of the Aztec module). Like
// is compared with like: the newest checkpoint the node has seen on L1
// (checkpointed) with the rollup's pending checkpoint on L1, and the
// node's proven checkpoint with the rollup's proven one. The usual gap
// between proven and pending does not count. Lags are measured in slots
// between the checkpoints and shown as time (slot duration from L1).
//
//	Sync    green:  at most SyncWarnSlots behind
//	        yellow: more than SyncWarnSlots behind
//	        red:    more than SyncCritSlots behind, or so far behind that
//	                the node's checkpoint is no longer stored on L1
//	Proven  green:  the node knows the newest proof on L1
//	        yellow: at most one epoch behind (one proof not seen yet)
//	        red:    more than one epoch behind (proofs missed)
//	Network note:   no new checkpoint on L1 for more than SyncCritSlots;
//	        warning: for more than one epoch. A stalled network does not
//	                change the nodes' status: they cannot be more in sync
//	                than L1.
//
// Thresholds: on mainnet consecutive checkpoints are up to 2 slots apart,
// so a node one checkpoint behind can look 2 slots behind. Red after 8
// slots (9.6 minutes with 72-second slots) plus a 5-minute run interval
// stays below the inactivity slashing limits (testnet: 90 % of an epoch
// of 38.4 minutes; mainnet: 70 % in 2 consecutive epochs).
const (
	SyncWarnSlots = 2
	SyncCritSlots = 8
)

// AztecSyncInput is what the sync and proven checks need. x3f reads L1
// first and then the node, so a checkpoint that appears in between can
// only make the node look ahead, never behind.
type AztecSyncInput struct {
	// Err is set if a read failed; the checks then do not run.
	Err error
	// SlotDuration (seconds) and EpochDuration (slots) of the rollup.
	SlotDuration, EpochDuration uint64
	// L1Pending and L1Proven are the rollup's tips, with their slots.
	L1Pending, L1PendingSlot uint64
	L1Proven, L1ProvenSlot   uint64
	// NodeCheckpointed and NodeProven are the node's tips. Their slots
	// are only read if the node is behind; Gone is set if the rollup no
	// longer stores that checkpoint.
	NodeCheckpointed, NodeCheckpointedSlot uint64
	NodeCheckpointedGone                   bool
	NodeProven, NodeProvenSlot             uint64
	NodeProvenGone                         bool
}

// AssessAztecSync applies the sync rules. cell is "synced" or how far the
// node is behind.
func AssessAztecSync(in AztecSyncInput) (a Assessment, cell string) {
	if in.NodeCheckpointed >= in.L1Pending {
		return a, "synced"
	}
	if in.NodeCheckpointedGone {
		a.add(Crit, "node is at checkpoint %d, L1 at %d: so far behind that its checkpoint is no longer stored on L1 (more than %s)",
			in.NodeCheckpointed, in.L1Pending, slotsTime(2*in.EpochDuration, in.SlotDuration))
		return a, fmt.Sprintf("%d checkpoints behind", in.L1Pending-in.NodeCheckpointed)
	}
	behind := in.L1PendingSlot - min(in.NodeCheckpointedSlot, in.L1PendingSlot)
	if behind <= SyncWarnSlots {
		return a, "synced"
	}
	level := Warn
	if behind > SyncCritSlots {
		level = Crit
	}
	t := slotsTime(behind, in.SlotDuration)
	a.add(level, "node is %s behind L1: checkpoint %d, L1 at %d", t, in.NodeCheckpointed, in.L1Pending)
	return a, t + " behind"
}

// AssessAztecProven applies the proven rules.
func AssessAztecProven(in AztecSyncInput) Assessment {
	var a Assessment
	if in.NodeProven >= in.L1Proven {
		return a
	}
	behind := in.L1ProvenSlot - min(in.NodeProvenSlot, in.L1ProvenSlot)
	if in.NodeProvenGone || behind > in.EpochDuration {
		a.add(Crit, "node is missing proofs: proven checkpoint %d, L1 has proven %d (more than an epoch, %s)",
			in.NodeProven, in.L1Proven, slotsTime(in.EpochDuration, in.SlotDuration))
		return a
	}
	a.add(Warn, "node has not seen the newest proof yet: proven checkpoint %d, L1 has proven %d (%s)",
		in.NodeProven, in.L1Proven, slotsTime(behind, in.SlotDuration))
	return a
}

// NetworkProgressInput describes how long a network has been without a
// new checkpoint on L1.
type NetworkProgressInput struct {
	// Network is the network's name, e.g. "testnet".
	Network string
	// CurrentSlot is the rollup's slot at the time of the newest L1
	// block; L1Pending and L1PendingSlot are its newest checkpoint.
	CurrentSlot, L1Pending, L1PendingSlot uint64
	SlotDuration, EpochDuration           uint64
}

// CheckpointAge is how long the network has been without a new checkpoint.
func (in NetworkProgressInput) CheckpointAge() time.Duration {
	slots := in.CurrentSlot - min(in.L1PendingSlot, in.CurrentSlot)
	return time.Duration(slots*in.SlotDuration) * time.Second
}

// AssessNetworkProgress returns a finding about the network itself if it
// has been without a new checkpoint for long; ok is false otherwise.
func AssessNetworkProgress(in NetworkProgressInput) (f Finding, ok bool) {
	slots := in.CurrentSlot - min(in.L1PendingSlot, in.CurrentSlot)
	if slots <= SyncCritSlots {
		return Finding{}, false
	}
	level := OK
	if slots > in.EpochDuration {
		level = Warn
	}
	return Finding{Level: level, Message: fmt.Sprintf(
		"%s: no new checkpoint on L1 for %s (last: checkpoint %d at slot %d); Aztec nodes cannot be more in sync than that",
		in.Network, slotsTime(slots, in.SlotDuration), in.L1Pending, in.L1PendingSlot)}, true
}

// slotsTime renders a number of slots as time: "2.4 min", "1.3 h".
func slotsTime(slots, slotDuration uint64) string {
	d := time.Duration(slots*slotDuration) * time.Second
	if d >= 90*time.Minute {
		return fmt.Sprintf("%.1f h", d.Hours())
	}
	return fmt.Sprintf("%.1f min", d.Minutes())
}
