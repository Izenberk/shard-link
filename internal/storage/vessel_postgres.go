package storage

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresVessel implements the Repository interface using PostgreSQL + pgvector.
type PostgresVessel struct {
	pool *pgxpool.Pool
}

// NewPostgresVessel connects to the PostgreSQL instance.
func NewPostgresVessel(ctx context.Context, connStr string) (*PostgresVessel, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresVessel{pool: pool}, nil
}

func (v *PostgresVessel) Close() error {
	v.pool.Close()
	return nil
}

func (v *PostgresVessel) SaveShard(ctx context.Context, s Shard) error {
	const query = `
		INSERT INTO shards (id, category, content, vector, metadata, source_type, source_ref, confidence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(id) DO UPDATE SET
			category = EXCLUDED.category,
			content = EXCLUDED.content,
			vector = EXCLUDED.vector,
			metadata = EXCLUDED.metadata,
			source_type = EXCLUDED.source_type,
			source_ref = EXCLUDED.source_ref,
			confidence = EXCLUDED.confidence,
			last_used = CURRENT_TIMESTAMP;
	`
	_, err := v.pool.Exec(ctx, query, s.ID, s.Category, s.Content, formatVector(s.Vector), s.Metadata, s.SourceType, s.SourceRef, s.Confidence)
	return err
}

func (v *PostgresVessel) FindResonant(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	// 1. Find the IDs of resonant shards
	const selectQuery = `
		SELECT id
		FROM shards
		WHERE vector IS NOT NULL
		ORDER BY vector <=> $1
		LIMIT $2;
	`
	rows, err := v.pool.Query(ctx, selectQuery, formatVector(queryVector), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres search select failed: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// 2. Update last_used and return full shard data
	const updateQuery = `
		UPDATE shards 
		SET last_used = CURRENT_TIMESTAMP
		WHERE id = ANY($1)
		RETURNING id, category, content, vector::text, metadata, source_type, source_ref, confidence, created_at, last_used;
	`
	rows, err = v.pool.Query(ctx, updateQuery, ids)
	if err != nil {
		return nil, fmt.Errorf("postgres search update failed: %w", err)
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		var vecStr *string 
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata, &s.SourceType, &s.SourceRef, &s.Confidence, &s.CreatedAt, &s.LastUsed); err != nil {
			return nil, err
		}
		if vecStr != nil {
			s.Vector = parseVector(*vecStr)
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
}

func (v *PostgresVessel) FindText(ctx context.Context, query string, limit int) ([]Shard, error) {
	const sql = `
		SELECT id, category, content, vector::text, metadata, source_type, source_ref, confidence
		FROM shards
		WHERE content ILIKE $1
		ORDER BY last_used DESC
		LIMIT $2;
	`
	rows, err := v.pool.Query(ctx, sql, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("postgres text search failed: %w", err)
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		var vecStr *string
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata, &s.SourceType, &s.SourceRef, &s.Confidence); err != nil {
			return nil, err
		}
		if vecStr != nil {
			s.Vector = parseVector(*vecStr)
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
}

// FindHybrid performs Reciprocal Rank Fusion (RRF) on vector and text search results.
func (v *PostgresVessel) FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error) {
	candidateLimit := limit * 2

	vectorResults, err := v.FindResonant(ctx, queryVector, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("vector search failed in hybrid: %w", err)
	}

	textResults, err := v.FindText(ctx, textQuery, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("text search failed in hybrid: %w", err)
	}

	return ReciprocalRankFusion(limit, 60.0, vectorResults, textResults), nil
}

func (v *PostgresVessel) GetCoreShards(ctx context.Context) ([]Shard, error) {
	const query = `SELECT id, content FROM shards WHERE category = 'core'`
	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		if err := rows.Scan(&s.ID, &s.Content); err != nil {
			return nil, err
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
}

func (v *PostgresVessel) ArchiveShard(ctx context.Context, id string) error {
	tx, err := v.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const moveQuery = `
		INSERT INTO shards_archive (id, category, content, vector, metadata, source_type, source_ref, confidence)
		SELECT id, category, content, vector, metadata, source_type, source_ref, confidence FROM shards WHERE id = $1
		ON CONFLICT(id) DO NOTHING;
	`
	if _, err := tx.Exec(ctx, moveQuery, id); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, "DELETE FROM shards WHERE id = $1", id); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (v *PostgresVessel) GetAllShards(ctx context.Context) ([]Shard, error) {
	const query = `SELECT id, category, content, vector::text, metadata, source_type, source_ref, confidence, last_used, created_at FROM shards`
	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		var vecStr *string 
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata, &s.SourceType, &s.SourceRef, &s.Confidence, &s.LastUsed, &s.CreatedAt); err != nil {
			return nil, err
		}
		if vecStr != nil {
			s.Vector = parseVector(*vecStr)
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
}

func (v *PostgresVessel) GetCount(ctx context.Context) (int, error) {
	var count int
	err := v.pool.QueryRow(ctx, "SELECT COUNT(*) FROM shards").Scan(&count)
	return count, err
}

func (v *PostgresVessel) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	const query = `
		SELECT s.id FROM shards s
		LEFT JOIN (
			SELECT from_id, COUNT(*) as link_count FROM shard_bonds GROUP BY from_id
		) b ON s.id = b.from_id
		WHERE s.category != 'core'
		ORDER BY 
			(COALESCE(b.link_count, 0) + 1) / (EXTRACT(EPOCH FROM (NOW() - s.last_used)) + 1) ASC
		LIMIT $1;
	`
	rows, err := v.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (v *PostgresVessel) SaveBond(ctx context.Context, b ShardBond) error {
	const query = `
		INSERT INTO shard_bonds (from_id, to_id, weight)
		VALUES ($1, $2, $3)
		ON CONFLICT(from_id, to_id) DO UPDATE SET weight = EXCLUDED.weight;
	`
	_, err := v.pool.Exec(ctx, query, b.FromID, b.ToID, b.Weight)
	return err
}

func (v *PostgresVessel) GetAllBonds(ctx context.Context) ([]ShardBond, error) {
	const query = `SELECT from_id, to_id, weight FROM shard_bonds`
	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bonds []ShardBond
	for rows.Next() {
		var b ShardBond
		if err := rows.Scan(&b.FromID, &b.ToID, &b.Weight); err != nil {
			return nil, err
		}
		bonds = append(bonds, b)
	}
	return bonds, rows.Err()
}

func (v *PostgresVessel) SearchGraph(ctx context.Context, queryVector []byte, limit int) ([]Shard, []ShardBond, error) {
	// Postgres fallback to vector search
	shards, err := v.FindResonant(ctx, queryVector, limit)
	return shards, nil, err
}

func (v *PostgresVessel) SyncBonds(ctx context.Context, threshold float64) (int, error) {
	// Postgres doesn't support autonomous graph linking yet
	return 0, nil
}

func (v *PostgresVessel) GetGraphData(ctx context.Context) ([]Shard, []ShardBond, error) {
	shards, err := v.GetAllShards(ctx)
	if err != nil {
		return nil, nil, err
	}
	bonds, err := v.GetAllBonds(ctx)
	if err != nil {
		return nil, nil, err
	}
	return shards, bonds, nil
}

func (v *PostgresVessel) CalculateCommunities(ctx context.Context) (int, error) {
	// Postgres fallback (no native Louvain)
	return 0, nil
}

// Optimize runs Postgres-specific maintenance routines
func (v *PostgresVessel) Optimize(ctx context.Context) error {
	// 1. Partial Index for Core Shards
	_, _ = v.pool.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_shards_category_core ON shards(category) WHERE category = 'core';")
	
	// 2. HNSW Tuning for pgvector. We assume the extension is enabled and vector column exists.
	// m=16, ef_construction=64 are good baseline parameters for 1536-D vectors.
	_, _ = v.pool.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_shards_vector ON shards USING hnsw (vector vector_cosine_ops) WITH (m = 16, ef_construction = 64);")

	// 3. VACUUM ANALYZE to reclaim dead tuples and update statistics for the query planner.
	// Note: VACUUM cannot run inside a transaction block, so we execute it directly on the pool.
	_, err := v.pool.Exec(ctx, "VACUUM ANALYZE shards;")
	if err != nil {
		return fmt.Errorf("postgres vacuum failed: %w", err)
	}
	return nil
}

func formatVector(v []byte) *string {
	if len(v) == 0 {
		return nil
	}
	floats := decodeVector(v)
	if len(floats) == 0 {
		return nil
	}
	var s []string
	for _, f := range floats {
		s = append(s, strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	res := "[" + strings.Join(s, ",") + "]"
	return &res
}

// parseVector converts a pgvector string "[0.1, 0.2, ...]" back to []byte (float32s)
func parseVector(s string) []byte {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	b := make([]byte, len(parts)*4)
	for i, p := range parts {
		f, _ := strconv.ParseFloat(strings.TrimSpace(p), 32)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(float32(f)))
	}
	return b
}
