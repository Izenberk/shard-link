package storage

import (
	"context"
	"fmt"
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
		INSERT INTO shards (id, category, content, vector, metadata)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(id) DO UPDATE SET
			category = EXCLUDED.category,
			content = EXCLUDED.content,
			vector = EXCLUDED.vector,
			metadata = EXCLUDED.metadata,
			last_used = CURRENT_TIMESTAMP;
	`
	_, err := v.pool.Exec(ctx, query, s.ID, s.Category, s.Content, formatVector(s.Vector), s.Metadata)
	return err
}

func (v *PostgresVessel) FindResonant(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	const query = `
		SELECT id, category, content, vector, metadata
		FROM shards
		WHERE vector IS NOT NULL
		ORDER BY vector <=> $1
		LIMIT $2;
	`
	rows, err := v.pool.Query(ctx, query, formatVector(queryVector), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres search failed: %w", err)
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		var vecStr *string // Use pointer to handle NULLs from DB
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata); err != nil {
			return nil, err
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
}

func (v *PostgresVessel) FindText(ctx context.Context, query string, limit int) ([]Shard, error) {
	const sql = `
		SELECT id, category, content, vector, metadata
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
		var vecStr *string // Use pointer to handle NULLs from DB
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata); err != nil {
			return nil, err
		}
		shards = append(shards, s)
	}
	return shards, rows.Err()
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
		INSERT INTO shards_archive (id, category, content, vector, metadata)
		SELECT id, category, content, vector, metadata FROM shards WHERE id = $1
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
	const query = `SELECT id, category, content, vector, metadata FROM shards`
	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shards []Shard
	for rows.Next() {
		var s Shard
		var vecStr *string // Use pointer to handle NULLs from DB
		if err := rows.Scan(&s.ID, &s.Category, &s.Content, &vecStr, &s.Metadata); err != nil {
			return nil, err
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
		s = append(s, fmt.Sprintf("%f", f))
	}
	res := "[" + strings.Join(s, ",") + "]"
	return &res
}
