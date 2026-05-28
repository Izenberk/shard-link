package hygiene

import (
	"context"
	"fmt"
	"log"
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
	log.Printf("[Hygiene] Service ignited. Interval: %v", h.interval)
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Hygiene] Shutting down...")
			return
		case <-ticker.C:
			h.performHygiene(ctx)
		}
	}
}

func (h *HygieneWorker) performHygiene(ctx context.Context) {
	log.Println("[Hygiene] Starting maintenance cycle...")
	h.logActivity("Hygiene: maintenance cycle started", "system", "")

	if h.pgVessel != nil {
		if err := h.pgVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] Postgres: %v", err)
			h.logActivity(fmt.Sprintf("Hygiene: Postgres VACUUM failed — %v", err), "error", "")
		} else {
			log.Println("[Hygiene] Postgres: VACUUM ANALYZE complete.")
			h.logActivity("Hygiene: Postgres VACUUM complete", "system", "")
		}
	}

	if h.graphVessel != nil {
		if err := h.graphVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] Neo4j: %v", err)
			h.logActivity(fmt.Sprintf("Hygiene: Neo4j optimization failed — %v", err), "error", "")
		} else {
			log.Println("[Hygiene] Neo4j: Index integrity verified.")
			h.logActivity("Hygiene: Neo4j index integrity verified", "system", "")
		}
	}

	if h.localVessel != nil {
		if err := h.localVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] SQLite: %v", err)
			h.logActivity(fmt.Sprintf("Hygiene: SQLite VACUUM failed — %v", err), "error", "")
		} else {
			log.Println("[Hygiene] SQLite: VACUUM complete.")
			h.logActivity("Hygiene: SQLite VACUUM complete", "system", "")
		}
	}

	log.Println("[Hygiene] Maintenance cycle complete.")
	h.logActivity("Hygiene: maintenance cycle complete", "system", "")
}
