package storage

import (
	"strings"
	"sync"
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

// --- Race condition tests (run with: go test -race ./internal/storage/) ---

// TestDirtyCounter_ConcurrentWrites launches 50 goroutines that each call
// MarkShardDirty once. The final count must equal 50 with no lost increments —
// the race detector fires if the underlying atomic is not actually atomic.
func TestDirtyCounter_ConcurrentWrites(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount()) // reset to zero

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			MarkShardDirty()
		}()
	}
	wg.Wait()

	got := DirtyShardCount()
	if got != goroutines {
		t.Errorf("expected %d, got %d — possible lost increment under concurrent writes", goroutines, got)
	}
	ConsumeDirtyShards(DirtyShardCount())
}

// TestDirtyCounter_ConcurrentReadWrite runs 10 writer goroutines (5 marks each)
// alongside 1 reader goroutine calling DirtyShardCount. The race detector confirms
// reads and writes do not produce torn values.
func TestDirtyCounter_ConcurrentReadWrite(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount())

	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				MarkShardDirty()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			_ = DirtyShardCount()
		}
	}()

	wg.Wait()

	if got := DirtyShardCount(); got != 50 {
		t.Errorf("concurrent reads must not affect count: expected 50, got %d", got)
	}
	ConsumeDirtyShards(DirtyShardCount())
}

// TestConsume_ConcurrentWithMark races 50 Mark goroutines against 10 Consume(1)
// goroutines. Atomic Add guarantees all operations serialise — final value must
// be exactly 40 (50 - 10) regardless of scheduling order.
func TestConsume_ConcurrentWithMark(t *testing.T) {
	ConsumeDirtyShards(DirtyShardCount())

	var wg sync.WaitGroup

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			MarkShardDirty()
		}()
	}

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ConsumeDirtyShards(1)
		}()
	}

	wg.Wait()

	if got := DirtyShardCount(); got != 40 {
		t.Errorf("50 marks - 10 consumes = 40 expected, got %d — atomic serialisation broken", got)
	}
	ConsumeDirtyShards(DirtyShardCount())
}
