package storage

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ncruces/go-sqlite3"
)

//go:embed schema.sql
var schema string

// Add the Pool
var vectorPool = sync.Pool{
	New: func() any {
		// We pre-allocate 1536 slots (the standard dimension for OpenAI/Gemini)
		return make([]float32, 1536)
	},
}

type Vessel struct {
	conn *sqlite3.Conn
}

// NewVessel opens the database at the given path and initializes the Shard-Link schema.
func NewVessel(path string) (*Vessel, error) {
	// 1. Open the direct connection
	conn, err := sqlite3.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open vessel: %w", err)
	}

	v := &Vessel{conn: conn}

	// 1. Register vec_version
	err = conn.CreateFunction("vec_version", 0, sqlite3.DETERMINISTIC, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
		ctx.ResultText("shard-link-go-v0.1.0")
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reg vec_version: %w", err)
	}

	// 2. Register vec_distance_cosine (The heart of Shard-Link)
	err = conn.CreateFunction("vec_distance_cosine", 2, sqlite3.DETERMINISTIC, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
		v1 := decodeVector(arg[0].RawBlob())
		v2 := decodeVector(arg[1].RawBlob())

		defer func() {
			if v1 != nil && cap(v1) == 1536 {
				vectorPool.Put(v1[:1536])
			}
			if v2 != nil && cap(v2) == 1536 {
				vectorPool.Put(v2[:1536])
			}
		}()

		if v1 == nil || v2 == nil || len(v1) != len(v2) {
			ctx.ResultFloat(2.0)
			return
		}

		ctx.ResultFloat(1.0 - cosineSimilarity(v1, v2))
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reg vec_distance: %w", err)
	}

	// 3. Initialize Schema
	if err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return v, nil
}

// SaveShard persists a fragment or updates it if the ID already exists.
func (v *Vessel) SaveShard(ctx context.Context, s Shard) error {
	const query = `
		INSERT INTO shards (id, category, content, vector, metadata)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			category=excluded.category,
			content=excluded.content,
			vector=excluded.vector,
			metadata=excluded.metadata,
			last_used=CURRENT_TIMESTAMP;
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("prepare save: %w", err)
	}
	defer stmt.Close()

	stmt.BindText(1, s.ID)
	stmt.BindText(2, s.Category)
	stmt.BindText(3, s.Content)
	stmt.BindBlob(4, s.Vector)
	stmt.BindBlob(5, s.Metadata)

	if err := stmt.Exec(); err != nil {
		return fmt.Errorf("exec save: %w", err)
	}
	return nil
}

// FindResonant searches for shards closest to the query vector.
func (v *Vessel) FindResonant(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	const query = `
		SELECT id, category, content, vector, metadata, last_used, created_at
		FROM shards
		ORDER BY vec_distance_cosine(vector, ?) ASC
		LIMIT ?;
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("prepare search: %w", err)
	}
	defer stmt.Close()

	stmt.BindBlob(1, queryVector)
	stmt.BindInt(2, limit)

	var shards []Shard
	for stmt.Step() {
		shards = append(shards, Shard{
			ID:       stmt.ColumnText(0),
			Category: stmt.ColumnText(1),
			Content:  stmt.ColumnText(2),
			Vector:   stmt.ColumnBlob(3, nil),
			Metadata: stmt.ColumnBlob(4, nil),
		})
	}

	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("scan results: %w", err)
	}

	return shards, nil
}

func (v *Vessel) FindText(ctx context.Context, query string, limit int) ([]Shard, error) {
	const sqlQuery = `
		SELECT id, category, content, vector, metadata, last_used, created_at
		FROM shards
		WHERE content LIKE ?
		ORDER BY last_used DESC
		LIMIT ?;
	`
	stmt, _, err := v.conn.Prepare(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("prepare text search: %w", err)
	}
	defer stmt.Close()

	stmt.BindText(1, "%"+query+"%")
	stmt.BindInt(2, limit)

	var shards []Shard
	for stmt.Step() {
		shards = append(shards, Shard{
			ID:       stmt.ColumnText(0),
			Category: stmt.ColumnText(1),
			Content:  stmt.ColumnText(2),
			Vector:   stmt.ColumnBlob(3, nil),
			Metadata: stmt.ColumnBlob(4, nil),
		})
	}

	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("scan text results: %w", err)
	}

	return shards, nil
}

func (v *Vessel) GetCoreShards(ctx context.Context) ([]Shard, error) {
	const query = `SELECT id, content FROM shards WHERE category = 'core'`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var shards []Shard
	for stmt.Step() {
		shards = append(shards, Shard{
			ID:      stmt.ColumnText(0),
			Content: stmt.ColumnText(1),
		})
	}
	return shards, stmt.Err()
}

func (v *Vessel) ArchiveShard(ctx context.Context, id string) error {
	const query = `
		INSERT OR REPLACE INTO shards_archive (id, category, content, vector, metadata)
		SELECT id, category, content, vector, metadata FROM shards WHERE id = ?;
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	stmt.BindText(1, id)

	if err := stmt.Exec(); err != nil {
		return err
	}

	return v.DeleteShard(id)
}

func (v *Vessel) GetAllShards(ctx context.Context) ([]Shard, error) {
	const query = `SELECT id, category, content, vector, metadata, last_used, created_at FROM shards`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var shards []Shard
	for stmt.Step() {
		lu, _ := time.Parse(time.RFC3339, stmt.ColumnText(5))
		ca, _ := time.Parse(time.RFC3339, stmt.ColumnText(6))

		shards = append(shards, Shard{
			ID:        stmt.ColumnText(0),
			Category:  stmt.ColumnText(1),
			Content:   stmt.ColumnText(2),
			Vector:    stmt.ColumnBlob(3, nil),
			Metadata:  stmt.ColumnBlob(4, nil),
			LastUsed:  lu,
			CreatedAt: ca,
		})
	}
	return shards, stmt.Err()
}

func (v *Vessel) GetCount(ctx context.Context) (int, error) {
	stmt, _, err := v.conn.Prepare("SELECT COUNT(*) FROM shards")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	if stmt.Step() {
		return stmt.ColumnInt(0), nil
	}
	return 0, stmt.Err()
}

func (v *Vessel) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	const query = `
		SELECT id FROM shards
		WHERE category != 'core'
			AND id NOT IN (SELECT to_id FROM shard_bonds WHERE weight > 0.85)
		ORDER BY
			last_used ASC,
			(SELECT COUNT(*) FROM shard_bonds WHERE from_id = shards.id OR to_id = shards.id) ASC
		LIMIT ?;
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	stmt.BindInt(1, limit)

	var ids []string
	for stmt.Step() {
		ids = append(ids, stmt.ColumnText(0))
	}
	return ids, stmt.Err()
}

func (v *Vessel) DeleteShard(id string) error {
	stmt, _, err := v.conn.Prepare("DELETE FROM shards WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	stmt.BindText(1, id)
	return stmt.Exec()
}

// SaveBond manually links two shards.
func (v *Vessel) SaveBond(ctx context.Context, b ShardBond) error {
	const query = `INSERT OR REPLACE INTO shard_bonds (from_id, to_id, weight) VALUES (?, ?, ?)`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	stmt.BindText(1, b.FromID)
	stmt.BindText(2, b.ToID)
	stmt.BindFloat(3, b.Weight)

	return stmt.Exec()
}

func (v *Vessel) GetAllBonds(ctx context.Context) ([]ShardBond, error) {
	const query = `SELECT from_id, to_id, weight FROM shard_bonds`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var bonds []ShardBond
	for stmt.Step() {
		bonds = append(bonds, ShardBond{
			FromID: stmt.ColumnText(0),
			ToID:   stmt.ColumnText(1),
			Weight: stmt.ColumnFloat(2),
		})
	}
	return bonds, stmt.Err()
}

func (v *Vessel) SearchGraph(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	// SQLite doesn't support complex graph traversal easily, so we fallback to vector search
	return v.FindResonant(ctx, queryVector, limit)
}

func (v *Vessel) GetGraphData(ctx context.Context) ([]Shard, []ShardBond, error) {
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

func (v *Vessel) CalculateCommunities(ctx context.Context) (int, error) {
	// SQLite doesn't support Louvain/Leiden natively.
	// For now, we return 0 communities.
	return 0, nil
}

func (v *Vessel) Close() error {
	return v.conn.Close()
}

// --- Resonance Math Helpers ---

func decodeVector(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	limit := len(b) / 4
	var v []float32
	if limit <= 1536 {
		v = vectorPool.Get().([]float32)
		v = v[:limit]
	} else {
		v = make([]float32, limit)
	}
	for i := 0; i < limit; i++ {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}

func putFloat32(b []byte, f float32) {
	binary.LittleEndian.PutUint32(b, math.Float32bits(f))
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		valA := float64(a[i])
		valB := float64(b[i])
		dot += valA * valB
		normA += valA * valA
		normB += valB * valB
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
