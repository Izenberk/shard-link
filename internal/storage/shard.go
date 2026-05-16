package storage

import (
	"context"
	"time"
)

// Shard is the atomic unit of long-term memory.
type Shard struct {
	ID          string
	Category    string // 'core', 'session', 'memory'
	Content     string
	Vector      []byte // Encoded float32s
	Metadata    []byte // JSONB

	// Phase 9: Source Provenance
	SourceType string  // 'manual', 'github', 'chat', 'web_scrape'
	SourceRef  string  // URI, File Path, or ID
	Confidence float64 // 0.0 - 1.0 Reliability Score

	CommunityID int64   // GraphRAG Community Mapping
	PageRank    float64 // Semantic Centrality Score
	LastUsed    time.Time
	CreatedAt   time.Time
}

// ShardBond represents a semantic link between fragments.
type ShardBond struct {
	FromID string  `json:"from_id"`
	ToID   string  `json:"to_id"`
	Weight float64 `json:"weight"`
}

// Embedder defines the contract for turning text into high-dimensional vectors.
// This abstraction allows us to swap between Cloud APIs and Local models.
type Embedder interface {
	// Embed generates a vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dimension returns the size of the vector produced by this embedder.
	Dimension() int
}
