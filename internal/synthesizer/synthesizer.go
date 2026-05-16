package synthesizer

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// Synthesizer handles autonomous relational linking.
type Synthesizer struct {
	vessel    storage.Repository
	interval  time.Duration
	threshold float64
}

func NewSynthesizer(v storage.Repository, interval time.Duration) *Synthesizer {
	tStr := os.Getenv("MESH_LINK_THRESHOLD")
	threshold, err := strconv.ParseFloat(tStr, 64)
	if err != nil {
		threshold = 0.70
	}

	return &Synthesizer{
		vessel:    v,
		interval:  interval,
		threshold: threshold,
	}
}

func (s *Synthesizer) Run(ctx context.Context) {
	log.Printf("[Synthesizer] Service ignited. Interval: %v, Threshold: %.2f", s.interval, s.threshold)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Synthesizer] Service shutting down...")
			return
		case <-ticker.C:
			s.performSynthesis(ctx)
		}
	}
}

func (s *Synthesizer) performSynthesis(ctx context.Context) {
	log.Println("[Synthesizer] Analyzing Knowledge Mesh for new semantic bonds...")
	
	count, err := s.vessel.SyncBonds(ctx, s.threshold)
	if err != nil {
		log.Printf("[Synthesizer ERROR] Failed to sync bonds: %v", err)
		return
	}

	if count > 0 {
		log.Printf("[Synthesizer] Aha! Established %d new semantic bonds autonomously.", count)
		
		// After linking, it's a good idea to refresh communities
		log.Println("[Synthesizer] Refreshing Knowledge Neighborhoods (Louvain)...")
		_, _ = s.vessel.CalculateCommunities(ctx)
	} else {
		log.Println("[Synthesizer] No new relationships identified in this cycle.")
	}
}
