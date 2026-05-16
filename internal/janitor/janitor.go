package janitor

import (
	"context"
	"log"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// Scorer defines how we calculate the "Importance" of a memory.
type Scorer interface {
	Score(shard storage.Shard) float64
}

// Janitor handles background size management.
type Janitor struct {
	vessel 		storage.Repository
	interval 	time.Duration
	maxSize 	int		// Maximum number if shards before eviction starts
}

func NewJanitor(v storage.Repository, interval time.Duration, maxSize int) *Janitor {
	return &Janitor {
		vessel: v,
		interval: interval,
		maxSize: maxSize,
	}
}

func (j *Janitor) Run(ctx context.Context) {
	log.Printf("[Janitor] Service started. Interval: %v, MaxSize: %d", j.interval, j.maxSize)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Janitor] Service shutting down...")
			return		// Context cancelled, stop the Janitor
		case <-ticker.C:
			j.performCleanup(ctx)
		}
	}
}

func (j *Janitor) performCleanup(ctx context.Context) {
	log.Println("[Janitor] Starting cleanup cycle...")
	count, err := j.vessel.GetCount(ctx)
	if err != nil {
		log.Printf("[Janitor ERROR] Failed to get shard count: %v", err)
		return
	}

	if count <= j.maxSize {
		log.Printf("[Janitor] Vessel within limits (%d/%d). No action needed.", count, j.maxSize)
		return		// Vessel is within safe limits
	}

	overage := count - j.maxSize
	log.Printf("[Janitor] Shard overage detected (+%d). Identifying eviction candidates...", overage)

	candidates, err := j.vessel.GetEvictionCandidates(ctx, overage)
	if err != nil {
		log.Printf("[Janitor ERROR] Failed to get eviction candidates: %v", err)
		return
	}

	for _, id := range candidates {
		log.Printf("[Janitor] Evicting shard: %s", id)
		_ = j.vessel.ArchiveShard(ctx, id)
	}

	if len(candidates) > 0 {
		log.Printf("[Janitor] Evicted %d shards. Running storage optimization...", len(candidates))
		_ = j.vessel.Optimize(ctx)
	}
	log.Println("[Janitor] Cleanup cycle complete.")
}