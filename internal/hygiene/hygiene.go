package hygiene

import (
	"context"
	"log"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

type HygieneWorker struct {
	graphVessel *storage.VesselGraph
	pgVessel    *storage.PostgresVessel
	localVessel *storage.Vessel
	interval    time.Duration
}

func NewHygieneWorker(
	g *storage.VesselGraph,
	pg *storage.PostgresVessel,
	lv *storage.Vessel,
	interval time.Duration,
) *HygieneWorker {
	return &HygieneWorker{
		graphVessel: g,
		pgVessel:    pg,
		localVessel: lv,
		interval:    interval,
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

	if h.pgVessel != nil {
		if err := h.pgVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] Postgres: %v", err)
		} else {
			log.Println("[Hygiene] Postgres: VACUUM ANALYZE complete.")
		}
	}

	if h.graphVessel != nil {
		if err := h.graphVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] Neo4j: %v", err)
		} else {
			log.Println("[Hygiene] Neo4j: Index integrity verified.")
		}
	}

	if h.localVessel != nil {
		if err := h.localVessel.Optimize(ctx); err != nil {
			log.Printf("[Hygiene ERROR] SQLite: %v", err)
		} else {
			log.Println("[Hygiene] SQLite: VACUUM complete.")
		}
	}

	log.Println("[Hygiene] Maintenance cycle complete.")
}
