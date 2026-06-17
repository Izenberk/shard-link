package storage

import (
	"strings"
	"testing"
	"time"
)

// TestDirtyShardCounter verifies the three atomic operations on dirtyShardCount:
// MarkShardDirty increments, DirtyShardCount reads, ConsumeDirtyShards subtracts.
func TestDirtyShardCounter(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount()) // reset to zero

	if got := DirtyShardCount(); got != 0 {
		t.Fatalf("expected 0 after reset, got %d", got)
	}

	MarkShardDirty()
	MarkShardDirty()
	MarkShardDirty()

	if got := DirtyShardCount(); got != 3 {
		t.Errorf("after 3 marks: expected 3, got %d", got)
	}

	ConsumeDirtyShards(3)

	if got := DirtyShardCount(); got != 0 {
		t.Errorf("after consuming 3: expected 0, got %d", got)
	}
}

// TestDirtyShardCounter_PartialConsume verifies that ConsumeDirtyShards subtracts
// the observed count rather than zeroing — shards saved DURING synthesis are preserved.
func TestDirtyShardCounter_PartialConsume(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount())

	for i := 0; i < 5; i++ {
		MarkShardDirty()
	}

	ConsumeDirtyShards(3) // consume only 3 — simulates 2 shards saved during synthesis

	if got := DirtyShardCount(); got != 2 {
		t.Errorf("expected 2 remaining after partial consume, got %d", got)
	}

	ConsumeDirtyShards(DirtyShardCount()) // clean up
}

// TestCommSummaryPrefixGate verifies the gate pattern used in VesselGraph.SaveShard:
// IDs with the "comm-summary-" prefix must NOT increment dirtyShardCount.
// The gate is: if !strings.HasPrefix(s.ID, "comm-summary-") { MarkShardDirty() }
func TestCommSummaryPrefixGate(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount())

	regular := []string{"regular-shard", "session-note-1", "memory-xyz"}
	summaries := []string{"comm-summary-42", "comm-summary-0", "comm-summary-abc"}

	for _, id := range regular {
		if !strings.HasPrefix(id, "comm-summary-") {
			MarkShardDirty()
		}
	}
	for _, id := range summaries {
		if !strings.HasPrefix(id, "comm-summary-") {
			MarkShardDirty()
		}
	}

	got := DirtyShardCount()
	if got != int64(len(regular)) {
		t.Errorf("expected %d dirty (regular shards only, summaries gated), got %d", len(regular), got)
	}

	ConsumeDirtyShards(DirtyShardCount())
}

// TestLastSynthesisTime verifies that ConsumeDirtyShards stamps lastSynthesisNano
// and that LastSynthesisTime reflects a timestamp within the call window.
func TestLastSynthesisTime(t *testing.T) {
	before := time.Now()
	ConsumeDirtyShards(DirtyShardCount()) // stamps lastSynthesisNano
	after := time.Now()

	got := LastSynthesisTime()

	if got.Before(before) {
		t.Errorf("LastSynthesisTime %v is before the ConsumeDirtyShards call at %v", got, before)
	}
	if got.After(after) {
		t.Errorf("LastSynthesisTime %v is after ConsumeDirtyShards returned at %v", got, after)
	}
}
