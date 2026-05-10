package janitor

import (
	"context"
	"testing"
	"time"
	"github.com/izenberk/shard-link/internal/storage"
)

func TestJanitor_EvictionImmunity(t *testing.T) {
	// 1. Setup Vessel in memory
	v, _ := storage.NewVessel(":memory:")
	defer v.Close()

	ctx := context.Background()

	// 2. Setup Janitor (max 3 shards)
	jan := NewJanitor(v, 1*time.Second, 3)
	t.Log("Initialized Janitor with limit: 3 shards")

	// 3. Insert Test Data
	// Shard 1: Core (IMMUNE)
	v.SaveShard(ctx, storage.Shard{ID: "core-1", Category: "core", Content: "Ego"})
	t.Log("Injected Core Shard (Immune by Category)")

	// Shard 2: Memory (IMMUNE by Bond)
	v.SaveShard(ctx, storage.Shard{ID: "bonded-1", Category: "memory", Content: "Deep Thought"})
	v.CreateBond(storage.ShardBond{FromID: "core-1", ToID: "bonded-1", Weight: 0.9})
	t.Log("Injected Bonded Shard (Immune by Weight > 0.85)")

	// Shard 3: Memory (Protected - It's not the "worst" because we'll add a worse one)
	v.SaveShard(ctx, storage.Shard{ID: "memory-1", Category: "memory", Content: "Active Memory"})
	// We give it a weak bond (0.5). It is NOT immune, but it is "less orphan" than Shard 4.
	v.CreateBond(storage.ShardBond{FromID: "core-1", ToID: "memory-1", Weight: 0.5})
	t.Log("Injected Weakly-Bonded Shard (Protected from Orphan status)")

	// Shard 4: Orphan (TARGET for Archive - Added to force the overage)
	v.SaveShard(ctx, storage.Shard{ID: "orphan-1", Category: "memory", Content: "Temp Data"})
	t.Log("Injected Orphan Shard (The target for Archive)")

	// 4. Trigger Cleanup
	t.Log("Triggering Janitor cleanup...")
	jan.performCleanup(ctx)

	// 5. Verify Results
	count, _ := v.GetCount(ctx)
	t.Logf("Active Shard Count after cleanup: %d", count)
	if count != 3 {
		t.Errorf("Expected 3 shards remaining, got %d", count)
	}

	// Verify the orphan is gone from active storage
	results, _ := v.FindResonant(ctx, nil, 10) // nil vector just gets everything
	for _, s := range results {
		if s.ID == "orphan-1" {
			t.Error("Orphan-1 was NOT evicted!")
		}
	}
	t.Log("Verification Complete: Orphan-1 was successfully faded to the Basement.")
}