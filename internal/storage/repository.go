package storage

import "context"

// Repository is the contract for the "Vessel" storage engine.
// This allows us to swap SQLite for PostgreSQL seamlessly.
type Repository interface {
	SaveShard(ctx context.Context, s Shard) error
	FindResonant(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, error)
	FindText(ctx context.Context, query string, limit int, shouldTouch bool) ([]Shard, error)
	FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error)
	GetAllShards(ctx context.Context) ([]Shard, error)
	GetArchivedShards(ctx context.Context) ([]Shard, error)
	GetShardByID(ctx context.Context, id string) (Shard, error)
	GetCoreShards(ctx context.Context) ([]Shard, error)
	ArchiveShard(ctx context.Context, id string) error
	GetCount(ctx context.Context) (int, error)
	GetEvictionCandidates(ctx context.Context, limit int) ([]string, error)
	
	// Bonds & Relationships
	SaveBond(ctx context.Context, b ShardBond) error
	GetAllBonds(ctx context.Context) ([]ShardBond, error)

	// Mesh Intelligence
	SearchGraph(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, []ShardBond, error)
	GetGraphData(ctx context.Context) ([]Shard, []ShardBond, error)
	CalculateCommunities(ctx context.Context) (int, error)
	SyncBonds(ctx context.Context, threshold float64) (int, error)
	ReinforceShards(ctx context.Context, ids []string) error

	// Storage Hygiene
	Optimize(ctx context.Context) error

	Close() error
}
