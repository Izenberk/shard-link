package janitor

import (
	"context"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// Scorer defines how we calculate the "Importance" of a memory.
type Scorer interface {
	Score(shard storage.Shard) float64
}

// Janitor handles background size management.
type Janitor struct {
	vessel 		*storage.Vessel
	interval 	time.Duration
	maxSize 	int		// Maximum number if shards before eviction starts
}

func NewJanitor(v *storage.Vessel, interval time.Duration, maxSize int) *Janitor {
	return &Janitor {
		vessel: v,
		interval: interval,
		maxSize: maxSize,
	}
}

func (j *Janitor) Run(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return		// Context cancelled, stop the Janitor
		case <-ticker.C:
			j.performCleanup()
		}
	}
}

func (j *Janitor) performCleanup() {
	count, err := j.vessel.GetCount()
	if err != nil {
		// In a real app, we would log this to a structured logger
		return
	}

	if count <= j.maxSize {
		return		// Vessel is within safe limits
	}
	overage := count - j.maxSize
	candidates, err := j.vessel.GetEvictionCandidates(overage)
	if err != nil {
		return
	}

	for _, id := range candidates {
		_ = j.vessel.ArchiveShard(id)
	}
}