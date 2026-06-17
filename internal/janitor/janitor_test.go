package janitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// mockArchiver satisfies the Archiver interface and records every shard it receives.
type mockArchiver struct {
	mu    sync.Mutex
	saved []storage.Shard
}

func (m *mockArchiver) SaveArchivedShard(_ context.Context, s storage.Shard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, s)
	return nil
}

// TestJanitor_EvictionImmunity verifies the three-tier protection stack:
//  1. category=core  → hard SQL exclusion (never a candidate)
//  2. higher survival → fresh shards score above the stale orphan
//  3. lowest survival → stale orphan evicted first
func TestJanitor_EvictionImmunity(t *testing.T) {
	v, _ := storage.NewVessel(":memory:")
	defer v.Close()

	ctx := context.Background()
	jan := NewJanitor(v, nil, 1*time.Second, 3, nil)

	stale := time.Now().Add(-30 * 24 * time.Hour)

	// Tier 1: excluded by SQL WHERE — never reaches the scorer.
	v.SaveShard(ctx, storage.Shard{ID: "core-1", Category: "core", Content: "Ego"})

	// Tier 2: fresh memory shards — higher survival score than the stale orphan.
	v.SaveShard(ctx, storage.Shard{ID: "bonded-1", Category: "memory", Content: "Deep Thought"})
	v.SaveBond(ctx, storage.ShardBond{FromID: "core-1", ToID: "bonded-1", Weight: 0.9})

	v.SaveShard(ctx, storage.Shard{ID: "memory-1", Category: "memory", Content: "Active Memory"})
	v.SaveBond(ctx, storage.ShardBond{FromID: "core-1", ToID: "memory-1", Weight: 0.5})

	// Tier 3: 30-day-stale orphan — lowest survival score, the only eviction target.
	v.SaveShard(ctx, storage.Shard{ID: "orphan-1", Category: "memory", Content: "Temp Data", LastUsed: stale})

	jan.performCleanup(ctx)

	count, _ := v.GetCount(ctx)
	if count != 3 {
		t.Errorf("expected 3 shards remaining, got %d", count)
	}

	// Confirm orphan-1 is gone from active storage.
	results, _ := v.FindResonant(ctx, nil, 10, false)
	for _, s := range results {
		if s.ID == "orphan-1" {
			t.Error("orphan-1 was NOT evicted — stale orphan should be the first eviction target")
		}
	}
}

// TestJanitor_AtExactLimit verifies that the Janitor does NOT evict when the mesh
// is exactly at capacity (count == maxSize). Eviction only triggers on overage.
func TestJanitor_AtExactLimit(t *testing.T) {
	v, _ := storage.NewVessel(":memory:")
	defer v.Close()

	ctx := context.Background()
	jan := NewJanitor(v, nil, 1*time.Second, 3, nil)

	v.SaveShard(ctx, storage.Shard{ID: "core-1", Category: "core", Content: "Identity"})
	v.SaveShard(ctx, storage.Shard{ID: "mem-1", Category: "memory", Content: "Fact A"})
	v.SaveShard(ctx, storage.Shard{ID: "mem-2", Category: "memory", Content: "Fact B"})

	jan.performCleanup(ctx)

	count, _ := v.GetCount(ctx)
	if count != 3 {
		t.Errorf("expected 3 shards at exact limit — Janitor should not evict, got %d", count)
	}
}

// TestJanitor_CommSummarySurvives verifies that community digest shards (comm-summary-*)
// are never evicted, even when they are the oldest and lowest-scoring non-core shards.
func TestJanitor_CommSummarySurvives(t *testing.T) {
	v, _ := storage.NewVessel(":memory:")
	defer v.Close()

	ctx := context.Background()
	jan := NewJanitor(v, nil, 1*time.Second, 3, nil)

	veryOld := time.Now().Add(-60 * 24 * time.Hour)
	stale := time.Now().Add(-30 * 24 * time.Hour)

	v.SaveShard(ctx, storage.Shard{ID: "core-1", Category: "core", Content: "Identity"})
	// Community digest is the oldest shard — it would be evicted without protection.
	v.SaveShard(ctx, storage.Shard{ID: "comm-summary-abc", Category: "community", Content: "Cluster A digest", LastUsed: veryOld})
	v.SaveShard(ctx, storage.Shard{ID: "mem-fresh", Category: "memory", Content: "Recent fact"})
	// This memory shard is the actual eviction target (oldest non-protected shard).
	v.SaveShard(ctx, storage.Shard{ID: "mem-stale", Category: "memory", Content: "Old fact", LastUsed: stale})

	jan.performCleanup(ctx)

	count, _ := v.GetCount(ctx)
	if count != 3 {
		t.Errorf("expected 3 shards after eviction, got %d", count)
	}

	// The community digest must still be present.
	results, _ := v.FindResonant(ctx, nil, 10, false)
	found := false
	for _, s := range results {
		if s.ID == "comm-summary-abc" {
			found = true
		}
	}
	if !found {
		t.Error("comm-summary-abc was evicted — community shards must be eviction-immune")
	}
}

// TestJanitor_WhiteDwarfArchivedBeforeRemoval verifies the two-step eviction sequence:
// Step 1 — SaveArchivedShard (Postgres White Dwarf backup), category mutated to "archived"
// Step 2 — ArchiveShard (remove from active living mesh)
// Both steps must fire; the shard must reach the archiver before being deleted from storage.
func TestJanitor_WhiteDwarfArchivedBeforeRemoval(t *testing.T) {
	v, _ := storage.NewVessel(":memory:")
	defer v.Close()

	ctx := context.Background()
	arch := &mockArchiver{}
	jan := NewJanitor(v, arch, 1*time.Second, 2, nil)

	stale := time.Now().Add(-30 * 24 * time.Hour)

	v.SaveShard(ctx, storage.Shard{ID: "core-1", Category: "core", Content: "Identity"})
	v.SaveShard(ctx, storage.Shard{ID: "mem-stale", Category: "memory", Content: "Old fact", LastUsed: stale})
	v.SaveShard(ctx, storage.Shard{ID: "mem-fresh", Category: "memory", Content: "New fact"})

	jan.performCleanup(ctx)

	// Step 1: White Dwarf must have received exactly one shard.
	arch.mu.Lock()
	defer arch.mu.Unlock()

	if len(arch.saved) != 1 {
		t.Fatalf("expected 1 White Dwarf archival call, got %d", len(arch.saved))
	}

	got := arch.saved[0]
	if got.ID != "mem-stale" {
		t.Errorf("expected mem-stale archived to White Dwarf, got %s", got.ID)
	}
	// evictShard mutates category to "archived" before handing off to the archiver.
	if got.Category != "archived" {
		t.Errorf("expected category 'archived' on White Dwarf shard, got %q", got.Category)
	}

	// Step 2: shard must be gone from active storage.
	results, _ := v.FindResonant(ctx, nil, 10, false)
	for _, s := range results {
		if s.ID == "mem-stale" {
			t.Error("mem-stale still in active storage — ArchiveShard should have removed it from the living mesh")
		}
	}
}
