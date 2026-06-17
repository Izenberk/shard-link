package synthesizer

import (
	"context"
	"testing"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// countingVessel wraps a real *storage.Vessel and counts SyncBonds calls.
// All other methods delegate to the embedded Vessel unchanged.
type countingVessel struct {
	*storage.Vessel
	syncBondsCalled int
}

func (c *countingVessel) SyncBonds(ctx context.Context, threshold float64) (int, error) {
	c.syncBondsCalled++
	return c.Vessel.SyncBonds(ctx, threshold)
}

// newTestVessel returns a real in-memory SQLite vessel wrapped for call counting.
func newTestVessel(t *testing.T) *countingVessel {
	t.Helper()
	v, err := storage.NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	t.Cleanup(func() { v.Close() })
	return &countingVessel{Vessel: v}
}

// TestIntegGate_BelowThreshold verifies that with a real SQLite vessel, the
// synthesizer skips SyncBonds when dirty < min and deferral has not elapsed.
func TestIntegGate_BelowThreshold(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount()) // reset + stamp now

	for i := 0; i < 3; i++ {
		storage.MarkShardDirty() // 3 < threshold of 5
	}
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	cv := newTestVessel(t)
	syn := &Synthesizer{
		vessel:         cv,
		minDirtyShards: 5,
		maxDeferral:    24 * time.Hour,
	}

	syn.performSynthesis(context.Background())

	if cv.syncBondsCalled != 0 {
		t.Errorf("expected SyncBonds not called (dirty=3 < min=5, not deferred), got %d calls", cv.syncBondsCalled)
	}
}

// TestIntegGate_ThresholdReached verifies that synthesis fires through a real vessel
// when dirty >= minDirtyShards. SyncBonds on SQLite is a no-op stub — the test
// confirms the wiring reaches it.
func TestIntegGate_ThresholdReached(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount())

	for i := 0; i < 5; i++ {
		storage.MarkShardDirty() // exactly at threshold
	}
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	cv := newTestVessel(t)
	syn := &Synthesizer{
		vessel:         cv,
		minDirtyShards: 5,
		maxDeferral:    24 * time.Hour,
	}

	syn.performSynthesis(context.Background())

	if cv.syncBondsCalled != 1 {
		t.Errorf("expected SyncBonds called once (dirty=5 >= min=5), got %d", cv.syncBondsCalled)
	}
}

// TestIntegGate_DeferralForced verifies that synthesis fires through a real vessel
// when maxDeferral is exceeded, even with dirty < threshold.
func TestIntegGate_DeferralForced(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount())
	storage.MarkShardDirty() // 1 < threshold of 5
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	cv := newTestVessel(t)
	syn := &Synthesizer{
		vessel:         cv,
		minDirtyShards: 5,
		maxDeferral:    0, // zero duration: always exceeded
	}

	syn.performSynthesis(context.Background())

	if cv.syncBondsCalled != 1 {
		t.Errorf("expected SyncBonds called once (max deferral exceeded), got %d", cv.syncBondsCalled)
	}
}

// TestCommSummaryNotReArming verifies that saving a comm-summary shard to the
// SQLite Vessel does NOT increment dirtyShardCount. The SQLite save path never
// calls MarkShardDirty — that gate lives in VesselGraph for the Neo4j path.
func TestCommSummaryNotReArming(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount()) // ensure counter starts at 0
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	cv := newTestVessel(t)
	ctx := context.Background()

	// Saving a community digest shard must not arm the synthesizer.
	err := cv.SaveShard(ctx, storage.Shard{
		ID:       "comm-summary-integration-42",
		Category: "community",
		Content:  "Cluster 42 digest: patterns detected in session shards.",
	})
	if err != nil {
		t.Fatalf("SaveShard: %v", err)
	}

	if got := storage.DirtyShardCount(); got != 0 {
		t.Errorf("expected dirtyShardCount=0 after comm-summary save, got %d — SQLite path must not arm synthesizer", got)
	}
}
