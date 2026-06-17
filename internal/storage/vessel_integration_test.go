package storage

import (
	"context"
	"testing"
	"time"
)

// TestSaveAndRetrieve verifies that SaveShard persists all fields and FindText
// returns the shard with the correct ID, category, and content.
func TestSaveAndRetrieve(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()

	want := Shard{
		ID:       "integ-save-1",
		Category: "memory",
		Content:  "integration test unique phrase alpha",
	}
	if err := v.SaveShard(ctx, want); err != nil {
		t.Fatalf("SaveShard: %v", err)
	}

	results, err := v.FindText(ctx, "unique phrase alpha", 10, false)
	if err != nil {
		t.Fatalf("FindText: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("FindText returned 0 results — shard was not persisted")
	}

	got := results[0]
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.Category != want.Category {
		t.Errorf("Category: got %q, want %q", got.Category, want.Category)
	}
	if got.Content != want.Content {
		t.Errorf("Content: got %q, want %q", got.Content, want.Content)
	}
}

// TestTouchUpdatesLastUsed verifies that FindText with shouldTouch=true calls
// ReinforceShards — updating the shard's last_used to approximately now.
func TestTouchUpdatesLastUsed(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()
	stale := time.Now().Add(-24 * time.Hour)

	if err := v.SaveShard(ctx, Shard{
		ID:       "integ-touch-1",
		Category: "memory",
		Content:  "integ touch content bravo",
		LastUsed: stale,
	}); err != nil {
		t.Fatalf("SaveShard: %v", err)
	}

	// shouldTouch=true triggers ReinforceShards internally
	_, err = v.FindText(ctx, "integ touch content bravo", 10, true)
	if err != nil {
		t.Fatalf("FindText: %v", err)
	}

	updated, err := v.GetShardByID(ctx, "integ-touch-1")
	if err != nil {
		t.Fatalf("GetShardByID: %v", err)
	}

	// last_used must have moved forward from the stale timestamp
	if !updated.LastUsed.After(stale) {
		t.Errorf("LastUsed not updated: got %v, original stale was %v", updated.LastUsed, stale)
	}
}

// TestReinforceShards_BatchUpdate verifies that a single ReinforceShards call
// updates last_used for all provided IDs in one batch.
func TestReinforceShards_BatchUpdate(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()
	stale := time.Now().Add(-48 * time.Hour)

	ids := []string{"batch-1", "batch-2"}
	for _, id := range ids {
		if err := v.SaveShard(ctx, Shard{
			ID:       id,
			Category: "memory",
			Content:  "batch reinforce test " + id,
			LastUsed: stale,
		}); err != nil {
			t.Fatalf("SaveShard %s: %v", id, err)
		}
	}

	if err := v.ReinforceShards(ctx, ids); err != nil {
		t.Fatalf("ReinforceShards: %v", err)
	}

	for _, id := range ids {
		got, err := v.GetShardByID(ctx, id)
		if err != nil {
			t.Fatalf("GetShardByID %s: %v", id, err)
		}
		if !got.LastUsed.After(stale) {
			t.Errorf("%s: LastUsed not updated — got %v, stale was %v", id, got.LastUsed, stale)
		}
	}
}

// TestArchiveShard verifies the two-step archive contract:
// the shard disappears from FindResonant (active mesh) and appears in GetArchivedShards.
func TestArchiveShard(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()

	if err := v.SaveShard(ctx, Shard{
		ID:       "integ-archive-1",
		Category: "memory",
		Content:  "shard to be archived",
	}); err != nil {
		t.Fatalf("SaveShard: %v", err)
	}

	if err := v.ArchiveShard(ctx, "integ-archive-1"); err != nil {
		t.Fatalf("ArchiveShard: %v", err)
	}

	// Must be gone from active mesh
	active, _ := v.FindResonant(ctx, nil, 10, false)
	for _, s := range active {
		if s.ID == "integ-archive-1" {
			t.Error("integ-archive-1 still in active mesh after ArchiveShard")
		}
	}

	// Must appear in archive
	archived, err := v.GetArchivedShards(ctx)
	if err != nil {
		t.Fatalf("GetArchivedShards: %v", err)
	}
	found := false
	for _, s := range archived {
		if s.ID == "integ-archive-1" {
			found = true
		}
	}
	if !found {
		t.Error("integ-archive-1 not in GetArchivedShards — archive step missing")
	}
}

// TestGetEvictionCandidates inserts 5 memory shards — 2 stale (30d), 3 fresh.
// GetEvictionCandidates(2) must return the 2 stale IDs (lowest survival scores).
func TestGetEvictionCandidates(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()
	stale := time.Now().Add(-30 * 24 * time.Hour)

	staleIDs := []string{"evict-stale-1", "evict-stale-2"}
	freshIDs := []string{"evict-fresh-1", "evict-fresh-2", "evict-fresh-3"}

	for _, id := range staleIDs {
		v.SaveShard(ctx, Shard{ID: id, Category: "memory", Content: "stale data " + id, LastUsed: stale})
	}
	for _, id := range freshIDs {
		v.SaveShard(ctx, Shard{ID: id, Category: "memory", Content: "fresh data " + id})
	}

	candidates, err := v.GetEvictionCandidates(ctx, 2)
	if err != nil {
		t.Fatalf("GetEvictionCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	staleSet := map[string]bool{"evict-stale-1": true, "evict-stale-2": true}
	for _, id := range candidates {
		if !staleSet[id] {
			t.Errorf("candidate %q is not a stale shard — wrong eviction target", id)
		}
	}
}

// TestCoreShardNeverEvicted verifies that category=core shards are always excluded
// from GetEvictionCandidates output, even when they are the oldest shards in the mesh.
func TestCoreShardNeverEvicted(t *testing.T) {
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("NewVessel: %v", err)
	}
	defer v.Close()

	ctx := context.Background()
	ancient := time.Now().Add(-365 * 24 * time.Hour) // 1 year old

	// Core shards are ancient — would be evicted first if not protected.
	for i := 1; i <= 3; i++ {
		v.SaveShard(ctx, Shard{
			ID:       "core-ancient-" + string(rune('0'+i)),
			Category: "core",
			Content:  "identity anchor",
			LastUsed: ancient,
		})
	}
	// Memory shards are also stale but not as old.
	v.SaveShard(ctx, Shard{ID: "mem-stale-1", Category: "memory", Content: "stale memory", LastUsed: time.Now().Add(-30 * 24 * time.Hour)})
	v.SaveShard(ctx, Shard{ID: "mem-stale-2", Category: "memory", Content: "stale memory", LastUsed: time.Now().Add(-30 * 24 * time.Hour)})

	candidates, err := v.GetEvictionCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("GetEvictionCandidates: %v", err)
	}

	for _, id := range candidates {
		// Any core-ancient-* ID in the result is a bug.
		for i := 1; i <= 3; i++ {
			coreID := "core-ancient-" + string(rune('0'+i))
			if id == coreID {
				t.Errorf("core shard %q appeared in eviction candidates — core shards must be eviction-immune", id)
			}
		}
	}
}
