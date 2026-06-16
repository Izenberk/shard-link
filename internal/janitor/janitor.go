package janitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/izenberk/shard-link/internal/metrics"
	"github.com/izenberk/shard-link/internal/storage"
)

// Archiver is the minimal contract for White Dwarf archival before Neo4j deletion.
// Keeping it local avoids importing the concrete PostgresVessel type.
type Archiver interface {
	SaveArchivedShard(ctx context.Context, s storage.Shard) error
}

// Scorer defines how we calculate the "Importance" of a memory.
type Scorer interface {
	Score(shard storage.Shard) float64
}

// Janitor handles background size management.
type Janitor struct {
	vessel         storage.Repository
	archivalVessel Archiver // nil when Postgres is not configured
	interval       time.Duration
	maxSize        int // Maximum number of shards before eviction starts
	logger         storage.LogFunc
}

func NewJanitor(v storage.Repository, archiver Archiver, interval time.Duration, maxSize int, logger storage.LogFunc) *Janitor {
	return &Janitor{
		vessel:         v,
		archivalVessel: archiver,
		interval:       interval,
		maxSize:        maxSize,
		logger:         logger,
	}
}

func (j *Janitor) logActivity(msg, category, shardID string) {
	if j.logger != nil {
		j.logger(msg, category, shardID)
	}
}

// evictShard archives to White Dwarf (Postgres) then removes from Neo4j.
// Matches the same two-step sequence as the manual EVICT_SHARD UI button.
func (j *Janitor) evictShard(ctx context.Context, id string) {
	if j.archivalVessel != nil {
		shard, err := j.vessel.GetShardByID(ctx, id)
		if err == nil {
			shard.Category = "archived"
			if err := j.archivalVessel.SaveArchivedShard(ctx, shard); err != nil {
				slog.Error("janitor: failed to archive shard to White Dwarf", "shard_id", id, "error", err)
			}
		} else {
			slog.Error("janitor: failed to fetch shard before archival", "shard_id", id, "error", err)
		}
	}
	_ = j.vessel.ArchiveShard(ctx, id)
	metrics.JanitorEvictionsTotal.Add(1)
}

func (j *Janitor) Run(ctx context.Context) {
	slog.Info("janitor started", "interval", j.interval, "max_size", j.maxSize)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("janitor shutting down")
			return
		case <-ticker.C:
			j.performCleanup(ctx)
		}
	}
}

// RunForced bypasses the capacity gate and evicts up to target low-survival shards immediately.
// target=0 defaults to 20. Returns the number of shards evicted.
func (j *Janitor) RunForced(ctx context.Context, target int) (int, error) {
	if target <= 0 {
		target = 20
	}
	j.logActivity("Manual Janitor cycle started", "system", "")

	candidates, err := j.vessel.GetEvictionCandidates(ctx, target)
	if err != nil {
		j.logActivity("Manual Janitor failed: "+err.Error(), "error", "")
		return 0, err
	}

	for _, id := range candidates {
		slog.Info("janitor (forced) evicting shard", "shard_id", id)
		j.logActivity("Janitor evicted: "+id, "evict", id)
		j.evictShard(ctx, id)
	}

	if len(candidates) > 0 {
		_ = j.vessel.Optimize(ctx)
	}

	j.logActivity(fmt.Sprintf("Manual Janitor complete — %d evicted", len(candidates)), "system", "")
	return len(candidates), nil
}

func (j *Janitor) performCleanup(ctx context.Context) {
	start := time.Now()
	slog.Debug("janitor cleanup cycle starting")
	count, err := j.vessel.GetCount(ctx)
	if err != nil {
		slog.Error("janitor failed to get shard count", "error", err)
		return
	}

	j.logActivity("Janitor cycle started", "system", "")

	if count <= j.maxSize {
		slog.Debug("janitor: mesh within limits", "count", count, "max", j.maxSize)
		j.logActivity("Janitor: mesh within limits", "system", "")
		return
	}

	overage := count - j.maxSize
	slog.Info("janitor overage detected", "overage", overage, "count", count, "max", j.maxSize)
	j.logActivity("Overage detected", "warn", "")

	candidates, err := j.vessel.GetEvictionCandidates(ctx, overage)
	if err != nil {
		slog.Error("janitor failed to get eviction candidates", "error", err)
		return
	}

	for _, id := range candidates {
		slog.Info("janitor evicting shard", "shard_id", id)
		j.logActivity("Janitor evicted: "+id, "evict", id)
		j.evictShard(ctx, id)
	}

	if len(candidates) > 0 {
		slog.Info("janitor eviction complete — running optimization", "evicted", len(candidates))
		_ = j.vessel.Optimize(ctx)
	}
	metrics.JanitorCycleLatency.Observe(time.Since(start))
	slog.Debug("janitor cleanup cycle complete")
	j.logActivity("Janitor cycle complete", "system", "")
}
