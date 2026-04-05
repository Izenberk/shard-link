package storage

import (
	"time"
)

// Shard is the atomic unit of long-term memory.
type Shard struct {
	ID			string
	Category	string		// 'core', 'session', 'memory'
	Content		string
	Vector		[]byte		// Encoded float32s
	Metadata	[]byte		// JSONB
	LastUsed	time.Time
	CreatedAt	time.Time
}

// ShardBond represents a semantic link between fragments.
type ShardBond struct {
	FromID	string
	ToID	string
	Weight	float64
}