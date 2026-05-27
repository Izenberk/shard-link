package mcp

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// SessionCentroid tracks the rolling vector centroid for a single MCP session.
// Updated via EMA on each retrieval — recent results weigh more than old ones.
type SessionCentroid struct {
	Vector     []float32
	QueryCount int
	LastUsed   time.Time
}

// WorkingMemory provides per-session cognitive biasing for search queries.
// It maintains a rolling centroid per MCP session that drifts toward
// recently retrieved content — mimicking associative priming in human memory.
type WorkingMemory struct {
	mu       sync.RWMutex
	sessions map[string]*SessionCentroid
	ttl      time.Duration
}

// NewWorkingMemory creates a WorkingMemory store with the given session TTL.
func NewWorkingMemory(ttl time.Duration) *WorkingMemory {
	return &WorkingMemory{
		sessions: make(map[string]*SessionCentroid),
		ttl:      ttl,
	}
}

// Bias blends the raw query vector with the session's accumulated centroid.
// Formula: biased = λ·query + (1-λ)·centroid
// Returns the original query unchanged if no centroid exists yet (first query).
func (wm *WorkingMemory) Bias(sessionID string, queryVec []float32, lambda float64) []float32 {
	wm.mu.RLock()
	sc, exists := wm.sessions[sessionID]
	wm.mu.RUnlock()

	if !exists || sc.Vector == nil {
		return queryVec
	}

	// Guard: dimension mismatch — return unbiased rather than panic
	if len(queryVec) != len(sc.Vector) {
		return queryVec
	}

	biased := make([]float32, len(queryVec))
	for i := range queryVec {
		biased[i] = float32(lambda)*queryVec[i] + float32(1.0-lambda)*sc.Vector[i]
	}

	return biased
}

// Update recalculates the session centroid from newly retrieved shards.
// Uses Exponential Moving Average: centroid = α·mean(result_vectors) + (1-α)·old_centroid
// α = 0.3 gives recent retrievals more weight without discarding accumulated context.
func (wm *WorkingMemory) Update(sessionID string, shards []storage.Shard) {
	if len(shards) == 0 {
		return
	}

	// Decode all shard vectors and compute their mean
	var vectors [][]float32
	for _, s := range shards {
		v := storage.DecodeVector(s.Vector)
		if v != nil {
			vectors = append(vectors, v)
		}
	}

	if len(vectors) == 0 {
		return
	}

	// Compute mean of result vectors
	dim := len(vectors[0])
	mean := make([]float32, dim)
	for _, v := range vectors {
		if len(v) != dim {
			continue // Skip dimension mismatches
		}
		for i := range v {
			mean[i] += v[i]
		}
	}
	count := float32(len(vectors))
	for i := range mean {
		mean[i] /= count
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	sc, exists := wm.sessions[sessionID]
	if !exists || sc.Vector == nil {
		// First query in this session — seed the centroid directly
		wm.sessions[sessionID] = &SessionCentroid{
			Vector:     mean,
			QueryCount: 1,
			LastUsed:   time.Now(),
		}
		log.Printf("[MCP] Working Memory: session %s centroid seeded (1 query)", sessionID)
		return
	}

	// EMA blend: centroid = α·mean + (1-α)·old_centroid
	const alpha = 0.3
	if len(sc.Vector) == dim {
		for i := range sc.Vector {
			sc.Vector[i] = float32(alpha)*mean[i] + float32(1.0-alpha)*sc.Vector[i]
		}
	} else {
		// Dimension changed (shouldn't happen, but guard gracefully)
		sc.Vector = mean
	}

	sc.QueryCount++
	sc.LastUsed = time.Now()
	log.Printf("[MCP] Working Memory: session %s centroid updated (%d queries)", sessionID, sc.QueryCount)
}

// StartCleanup launches a background goroutine that sweeps expired sessions every 5 minutes.
// Exits when the context is cancelled.
func (wm *WorkingMemory) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wm.mu.Lock()
				now := time.Now()
				cleaned := 0
				for id, sc := range wm.sessions {
					if now.Sub(sc.LastUsed) > wm.ttl {
						delete(wm.sessions, id)
						cleaned++
					}
				}
				wm.mu.Unlock()

				if cleaned > 0 {
					log.Printf("[WorkingMemory] Cleaned %d expired sessions", cleaned)
				}
			}
		}
	}()
}
