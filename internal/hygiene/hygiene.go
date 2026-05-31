package hygiene

import (
	"context"
	"log/slog"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

type HygieneWorker struct {
	graphVessel *storage.VesselGraph
	pgVessel    *storage.PostgresVessel
	localVessel *storage.Vessel
	interval    time.Duration
	logger      storage.LogFunc
}

func NewHygieneWorker(
	g *storage.VesselGraph,
	pg *storage.PostgresVessel,
	lv *storage.Vessel,
	interval time.Duration,
	logger storage.LogFunc,
) *HygieneWorker {
	return &HygieneWorker{
		graphVessel: g,
		pgVessel:    pg,
		localVessel: lv,
		interval:    interval,
		logger:      logger,
	}
}

func (h *HygieneWorker) logActivity(msg, category, shardID string) {
	if h.logger != nil {
		h.logger(msg, category, shardID)
	}
}

func (h *HygieneWorker) Run(ctx context.Context) {
	slog.Info("hygiene worker started", "interval", h.interval)
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("hygiene worker shutting down")
			return
		case <-ticker.C:
			h.performHygiene(ctx)
		}
	}
}

func (h *HygieneWorker) performHygiene(ctx context.Context) {
	slog.Debug("hygiene maintenance cycle starting")
	h.logActivity("Hygiene: maintenance cycle started", "system", "")

	if h.pgVessel != nil {
		if err := h.pgVessel.Optimize(ctx); err != nil {
			slog.Error("hygiene postgres optimization failed", "error", err)
			h.logActivity("Hygiene: Postgres VACUUM failed", "error", "")
		} else {
			slog.Debug("hygiene postgres VACUUM ANALYZE complete")
			h.logActivity("Hygiene: Postgres VACUUM complete", "system", "")
		}
	}

	if h.graphVessel != nil {
		if err := h.graphVessel.Optimize(ctx); err != nil {
			slog.Error("hygiene neo4j optimization failed", "error", err)
			h.logActivity("Hygiene: Neo4j optimization failed", "error", "")
		} else {
			slog.Debug("hygiene neo4j index integrity verified")
			h.logActivity("Hygiene: Neo4j index integrity verified", "system", "")
		}
	}

	if h.localVessel != nil {
		if err := h.localVessel.Optimize(ctx); err != nil {
			slog.Error("hygiene sqlite optimization failed", "error", err)
			h.logActivity("Hygiene: SQLite VACUUM failed", "error", "")
		} else {
			slog.Debug("hygiene sqlite VACUUM complete")
			h.logActivity("Hygiene: SQLite VACUUM complete", "system", "")
		}
	}

	slog.Debug("hygiene maintenance cycle complete")
	h.logActivity("Hygiene: maintenance cycle complete", "system", "")
}
