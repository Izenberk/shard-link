package storage

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"github.com/ncruces/go-sqlite3"
)

//go:embed schema.sql
var schema string

// Add the Pool
var vectorPool = sync.Pool{
	New: func() any {
		// We pre-allocate 3072 slots (Production Gemini Standard)
		return make([]float32, 3072)
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
		ctx.ResultText("shard-link-go-v0.2.0")
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
			if v1 != nil && cap(v1) == 3072 {
				vectorPool.Put(v1[:3072])
			}
			if v2 != nil && cap(v2) == 3072 {
				vectorPool.Put(v2[:3072])
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
		INSERT INTO shards (id, category, content, vector, metadata, source_type, source_ref, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			category=excluded.category,
			content=excluded.content,
			vector=excluded.vector,
			metadata=excluded.metadata,
			source_type=excluded.source_type,
			source_ref=excluded.source_ref,
			confidence=excluded.confidence,
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
	stmt.BindText(6, s.SourceType)
	stmt.BindText(7, s.SourceRef)
	stmt.BindFloat(8, s.Confidence)

	if err := stmt.Exec(); err != nil {
		return fmt.Errorf("exec save: %w", err)
	}
	log.Printf("[Vessel-SQLite] Shard Saved: %s (Category: %s)", s.ID, s.Category)
	return nil
}

// FindResonant searches for shards closest to the query vector and updates their last_used timestamp.
func (v *Vessel) FindResonant(ctx context.Context, queryVector []byte, limit int) ([]Shard, error) {
	// 1. Find the IDs first
	const selectQuery = `
		SELECT id
		FROM shards
		ORDER BY vec_distance_cosine(vector, ?) ASC
		LIMIT ?;
	`
	stmt, _, err := v.conn.Prepare(selectQuery)
	if err != nil {
		return nil, fmt.Errorf("prepare search select: %w", err)
	}
	defer stmt.Close()

	stmt.BindBlob(1, queryVector)
	stmt.BindInt(2, limit)

	var ids []string
	for stmt.Step() {
		ids = append(ids, stmt.ColumnText(0))
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// 2. Update last_used and return full shard data
	var shards []Shard
	for _, id := range ids {
		const updateQuery = `
			UPDATE shards 
			SET last_used = CURRENT_TIMESTAMP 
			WHERE id = ? 
			RETURNING id, category, content, vector, metadata, source_type, source_ref, confidence, created_at, last_used;
		`
		uStmt, _, err := v.conn.Prepare(updateQuery)
		if err != nil {
			return nil, fmt.Errorf("prepare search update: %w", err)
		}
		uStmt.BindText(1, id)
		
		if uStmt.Step() {
			shards = append(shards, Shard{
				ID:         uStmt.ColumnText(0),
				Category:   uStmt.ColumnText(1),
				Content:    uStmt.ColumnText(2),
				Vector:     uStmt.ColumnBlob(3, nil),
				Metadata:   uStmt.ColumnBlob(4, nil),
				SourceType: uStmt.ColumnText(5),
				SourceRef:  uStmt.ColumnText(6),
				Confidence: uStmt.ColumnFloat(7),
				CreatedAt:  parseTime(uStmt.ColumnText(8)),
				LastUsed:   parseTime(uStmt.ColumnText(9)),
			})
		}
		uStmt.Close()
	}

	return shards, nil
}

func parseTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05", s)
	return t
}

func (v *Vessel) FindText(ctx context.Context, query string, limit int) ([]Shard, error) {
	const sqlQuery = `
		SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, last_used, created_at
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
			ID:         stmt.ColumnText(0),
			Category:   stmt.ColumnText(1),
			Content:    stmt.ColumnText(2),
			Vector:     stmt.ColumnBlob(3, nil),
			Metadata:   stmt.ColumnBlob(4, nil),
			SourceType: stmt.ColumnText(5),
			SourceRef:  stmt.ColumnText(6),
			Confidence: stmt.ColumnFloat(7),
		})
	}

	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("scan text results: %w", err)
	}

	return shards, nil
}

// FindHybrid performs Reciprocal Rank Fusion (RRF) on vector and text search results.
func (v *Vessel) FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error) {
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
		INSERT OR REPLACE INTO shards_archive (id, category, content, vector, metadata, source_type, source_ref, confidence)
		SELECT id, category, content, vector, metadata, source_type, source_ref, confidence FROM shards WHERE id = ?;
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
	const query = `SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, last_used, created_at FROM shards`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var shards []Shard
	for stmt.Step() {
		lu, _ := time.Parse(time.RFC3339, stmt.ColumnText(8))
		ca, _ := time.Parse(time.RFC3339, stmt.ColumnText(9))

		shards = append(shards, Shard{
			ID:         stmt.ColumnText(0),
			Category:   stmt.ColumnText(1),
			Content:    stmt.ColumnText(2),
			Vector:     stmt.ColumnBlob(3, nil),
			Metadata:   stmt.ColumnBlob(4, nil),
			SourceType: stmt.ColumnText(5),
			SourceRef:  stmt.ColumnText(6),
			Confidence: stmt.ColumnFloat(7),
			LastUsed:   lu,
			CreatedAt:  ca,
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
	thresholdStr := os.Getenv("JANITOR_RESONANCE_THRESHOLD")
	if thresholdStr == "" {
		thresholdStr = "0.70"
	}

	// Protection Logic: Exclude 'core' shards and shards with high resonance to core
	const query = `
		SELECT s.id FROM shards s
		WHERE s.category != 'core'
			AND s.id NOT IN (SELECT to_id FROM shard_bonds WHERE weight > ?)
			AND NOT EXISTS (
				SELECT 1 FROM shards core
				WHERE core.category = 'core'
					AND (1.0 - vec_distance_cosine(s.vector, core.vector)) > ?
			)
		ORDER BY
			s.last_used ASC,
			(SELECT COUNT(*) FROM shard_bonds WHERE from_id = s.id OR to_id = s.id) ASC
		LIMIT ?;
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	stmt.BindText(1, thresholdStr)
	stmt.BindText(2, thresholdStr)
	stmt.BindInt(3, limit)

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

func (v *Vessel) SearchGraph(ctx context.Context, queryVector []byte, limit int) ([]Shard, []ShardBond, error) {
	// SQLite fallback to vector search
	shards, err := v.FindResonant(ctx, queryVector, limit)
	return shards, nil, err
}

func (v *Vessel) SyncBonds(ctx context.Context, threshold float64) (int, error) {
	// SQLite doesn't support autonomous graph linking yet
	return 0, nil
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
	return 0, nil
}

// Optimize runs maintenance tasks to reclaim space and update statistics
func (v *Vessel) Optimize(ctx context.Context) error {
	err := v.conn.Exec("PRAGMA optimize;")
	if err != nil {
		return fmt.Errorf("pragma optimize failed: %w", err)
	}

	err = v.conn.Exec("VACUUM;")
	if err != nil {
		return fmt.Errorf("vacuum failed: %w", err)
	}

	return nil
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
	if limit <= 3072 {
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
