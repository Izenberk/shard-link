package storage

import (
	"context"
	"sync/atomic"
	"time"
)

// dirtyShardCount tracks how many shards have been saved since the last synthesis.
// lastSynthesisNano stores the unix-nano timestamp of the last synthesis run.
// Atomic — zero-cost coordination between SaveShard (writer) and Synthesizer (reader).
var dirtyShardCount atomic.Int64
var lastSynthesisNano atomic.Int64

// Baseline the deferral clock at process start so the 24h ceiling is not
// tripped immediately by a zero-value timestamp (time.Since(zero) is ~56 years).
func init() {
	lastSynthesisNano.Store(time.Now().UnixNano())
}

// MarkShardDirty increments the dirty counter (replaces the old bool MarkMeshDirty).
func MarkShardDirty() { dirtyShardCount.Add(1) }

// DirtyShardCount returns the number of shards saved since the last synthesis.
func DirtyShardCount() int64 { return dirtyShardCount.Load() }

// IsMeshDirty is kept as a derived helper for any existing callers.
func IsMeshDirty() bool { return dirtyShardCount.Load() > 0 }

// ConsumeDirtyShards subtracts the observed count (NOT Store(0)) and stamps the
// clock. Subtracting preserves any shards saved *during* a synthesis run, which
// can take minutes for the LLM calls.
func ConsumeDirtyShards(n int64) {
	dirtyShardCount.Add(-n)
	lastSynthesisNano.Store(time.Now().UnixNano())
}

// LastSynthesisTime returns the timestamp of the last synthesis run.
func LastSynthesisTime() time.Time { return time.Unix(0, lastSynthesisNano.Load()) }

// LogFunc is a callback for broadcasting system activity.
type LogFunc func(msg string, category string, shardID string)

var GlobalLogger LogFunc

// ShardActivity represents a persistent log entry.
type ShardActivity struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	ShardID   string `json:"shard_id"`
}

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
	UseCount    int
	BondCount   int

	// Cognitive Science Fields (v4.0)
	Salience         float64     // LLM-scored importance [0.1, 1.0]; default 0.5
	RetrievalHistory []time.Time // Rolling window of last 20 retrieval timestamps
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
