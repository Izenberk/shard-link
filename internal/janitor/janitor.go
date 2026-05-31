package janitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/izenberk/shard-link/internal/metrics"
	"github.com/izenberk/shard-link/internal/storage"
)

// Scorer defines how we calculate the "Importance" of a memory.
type Scorer interface {
	Score(shard storage.Shard) float64
}

// Janitor handles background size management.
type Janitor struct {
	vessel   storage.Repository
	interval time.Duration
	maxSize  int // Maximum number of shards before eviction starts
	logger   storage.LogFunc
}

func NewJanitor(v storage.Repository, interval time.Duration, maxSize int, logger storage.LogFunc) *Janitor {
	return &Janitor{
		vessel:   v,
		interval: interval,
		maxSize:  maxSize,
		logger:   logger,
	}
}

func (j *Janitor) logActivity(msg, category, shardID string) {
	if j.logger != nil {
		j.logger(msg, category, shardID)
	}
}

func (j *Janitor) Run(ctx context.Context) {
	slog.Info("janitor started", "interval", j.interval, "max_size", j.maxSize)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("janitor shutting down")
			return		// Context cancelled, stop the Janitor
		case <-ticker.C:
			j.performCleanup(ctx)
		}
	}
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
		_ = j.vessel.ArchiveShard(ctx, id)
		metrics.JanitorEvictionsTotal.Add(1)
	}

	if len(candidates) > 0 {
		slog.Info("janitor eviction complete — running optimization", "evicted", len(candidates))
		_ = j.vessel.Optimize(ctx)
	}
	metrics.JanitorCycleLatency.Observe(time.Since(start))
	slog.Debug("janitor cleanup cycle complete")
	j.logActivity("Janitor cycle complete", "system", "")
}
