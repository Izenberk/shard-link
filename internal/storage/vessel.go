package storage

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ncruces/go-sqlite3"
)

//go:embed schema.sql
var schema string

// vectorDimension controls the pool allocation size.
// Set via SetVectorDimension() from main.go before any vessel creation.
var vectorDimension = 768

// SetVectorDimension configures the global vector dimension for pool allocations.
// Must be called before NewVessel() or any DecodeVector usage.
func SetVectorDimension(dim int) {
	vectorDimension = dim
}

var vectorPool = sync.Pool{
	New: func() any {
		return make([]float32, vectorDimension)
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
		v1 := DecodeVector(arg[0].RawBlob())
		v2 := DecodeVector(arg[1].RawBlob())

		defer func() {
			if v1 != nil && cap(v1) == vectorDimension {
				vectorPool.Put(v1[:vectorDimension])
			}
			if v2 != nil && cap(v2) == vectorDimension {
				vectorPool.Put(v2[:vectorDimension])
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
	slog.Debug("shard saved", "engine", "sqlite", "id", s.ID, "category", s.Category)
	return nil
}

// FindResonant searches for shards closest to the query vector and conditionally updates their last_used timestamp.
func (v *Vessel) FindResonant(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, error) {
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

	// 2. Conditionally update last_used and return full shard data
	var shards []Shard
	for _, id := range ids {
		var sql string
		if shouldTouch {
			sql = `
				UPDATE shards 
				SET last_used = CURRENT_TIMESTAMP 
				WHERE id = ? 
				RETURNING id, category, content, vector, metadata, source_type, source_ref, confidence, created_at, last_used;
			`
		} else {
			sql = `
				SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, created_at, last_used
				FROM shards 
				WHERE id = ?;
			`
		}

		uStmt, _, err := v.conn.Prepare(sql)
		if err != nil {
			return nil, fmt.Errorf("prepare search retrieval: %w", err)
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

func (v *Vessel) FindText(ctx context.Context, query string, limit int, shouldTouch bool) ([]Shard, error) {
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
	var ids []string
	for stmt.Step() {
		shard := Shard{
			ID:         stmt.ColumnText(0),
			Category:   stmt.ColumnText(1),
			Content:    stmt.ColumnText(2),
			Vector:     stmt.ColumnBlob(3, nil),
			Metadata:   stmt.ColumnBlob(4, nil),
			SourceType: stmt.ColumnText(5),
			SourceRef:  stmt.ColumnText(6),
			Confidence: stmt.ColumnFloat(7),
		}
		shards = append(shards, shard)
		ids = append(ids, shard.ID)
	}

	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("scan text results: %w", err)
	}

	if shouldTouch && len(ids) > 0 {
		_ = v.ReinforceShards(ctx, ids)
	}

	return shards, nil
}

// ReinforceShards manually updates usage metrics for a list of shard IDs.
func (v *Vessel) ReinforceShards(ctx context.Context, ids []string) error {
	for _, id := range ids {
		stmt, _, err := v.conn.Prepare("UPDATE shards SET last_used = CURRENT_TIMESTAMP WHERE id = ?")
		if err != nil {
			return err
		}
		stmt.BindText(1, id)
		_ = stmt.Exec()
		stmt.Close()
	}
	return nil
}


// FindHybrid performs Reciprocal Rank Fusion (RRF) on vector and text search results.
func (v *Vessel) FindHybrid(ctx context.Context, textQuery string, queryVector []byte, limit int) ([]Shard, error) {
	candidateLimit := limit * 2

	vectorResults, err := v.FindResonant(ctx, queryVector, candidateLimit, true)
	if err != nil {
		return nil, fmt.Errorf("vector search failed in hybrid: %w", err)
	}

	textResults, err := v.FindText(ctx, textQuery, candidateLimit, true)
	if err != nil {
		return nil, fmt.Errorf("text search failed in hybrid: %w", err)
	}

	return ReciprocalRankFusion(limit, 60.0, vectorResults, textResults), nil
}

func (v *Vessel) GetShardByID(ctx context.Context, id string) (Shard, error) {
	const sql = `SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, created_at, last_used FROM shards WHERE id = ?`
	stmt, _, err := v.conn.Prepare(sql)
	if err != nil {
		return Shard{}, err
	}
	defer stmt.Close()
	stmt.BindText(1, id)

	if stmt.Step() {
		return Shard{
			ID:         stmt.ColumnText(0),
			Category:   stmt.ColumnText(1),
			Content:    stmt.ColumnText(2),
			Vector:     stmt.ColumnBlob(3, nil),
			Metadata:   stmt.ColumnBlob(4, nil),
			SourceType: stmt.ColumnText(5),
			SourceRef:  stmt.ColumnText(6),
			Confidence: stmt.ColumnFloat(7),
			CreatedAt:  parseTime(stmt.ColumnText(8)),
			LastUsed:   parseTime(stmt.ColumnText(9)),
		}, nil
	}

	// Try archive fallback
	const arcSql = `SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, created_at, last_used FROM shards_archive WHERE id = ?`
	aStmt, _, err := v.conn.Prepare(arcSql)
	if err != nil {
		return Shard{}, err
	}
	defer aStmt.Close()
	aStmt.BindText(1, id)

	if aStmt.Step() {
		return Shard{
			ID:         aStmt.ColumnText(0),
			Category:   aStmt.ColumnText(1),
			Content:    aStmt.ColumnText(2),
			Vector:     aStmt.ColumnBlob(3, nil),
			Metadata:   aStmt.ColumnBlob(4, nil),
			SourceType: aStmt.ColumnText(5),
			SourceRef:  aStmt.ColumnText(6),
			Confidence: aStmt.ColumnFloat(7),
			CreatedAt:  parseTime(aStmt.ColumnText(8)),
			LastUsed:   parseTime(aStmt.ColumnText(9)),
		}, nil
	}

	return Shard{}, fmt.Errorf("shard %s not found in sqlite", id)
}

func (v *Vessel) GetArchivedShards(ctx context.Context) ([]Shard, error) {
	const query = `SELECT id, category, content, vector, metadata, source_type, source_ref, confidence, last_used, created_at FROM shards_archive`
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

// GetEvictionCandidates identifies low-survival shards using the Survival Formula v4.1.
// SQL handles hard exclusions (core protection, resonance filter).
// Go handles ranking: compute SurvivalScoreV4 for each candidate, sort ascending, return lowest N.
func (v *Vessel) GetEvictionCandidates(ctx context.Context, limit int) ([]string, error) {
	thresholdStr := os.Getenv("JANITOR_RESONANCE_THRESHOLD")
	if thresholdStr == "" {
		thresholdStr = "0.70"
	}
	threshold, err := strconv.ParseFloat(thresholdStr, 64)
	if err != nil {
		threshold = 0.70
	}

	// Fetch all non-core candidates with their bond counts and salience-relevant fields.
	// Resonance protection is applied in Go since SQLite's vec_distance_cosine is row-by-row anyway.
	const query = `
		SELECT s.id, s.category, s.content, s.last_used, s.confidence,
		       (SELECT COUNT(*) FROM shard_bonds WHERE from_id = s.id OR to_id = s.id) as bond_count,
		       s.vector
		FROM shards s
		WHERE s.category != 'core';
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	// Load core shard vectors for resonance check
	coreVectors, err := v.loadCoreVectors()
	if err != nil {
		return nil, fmt.Errorf("load core vectors: %w", err)
	}

	type scoredCandidate struct {
		id    string
		score float64
	}
	var candidates []scoredCandidate

	for stmt.Step() {
		id := stmt.ColumnText(0)
		lastUsed := parseTime(stmt.ColumnText(3))
		bondCount := stmt.ColumnInt(5)
		vec := stmt.ColumnBlob(6, nil)

		// Resonance-to-core protection: skip shards resonant with any core shard
		if isResonantToCore(vec, coreVectors, threshold) {
			continue
		}

		// SQLite doesn't store salience or retrieval_history — use defaults.
		// Salience defaults to 0.5 (neutral), retrieval_history is empty (ACT-R = epsilon).
		salience := 0.5
		var retrievalHistory []time.Time
		centrality := 0.0

		score := SurvivalScoreV4(bondCount, centrality, salience, retrievalHistory, lastUsed)
		candidates = append(candidates, scoredCandidate{id: id, score: score})

		slog.Debug("eviction candidate scored",
			"id", id, "survival", score, "bonds", bondCount, "salience", salience)
	}
	if err := stmt.Err(); err != nil {
		return nil, err
	}

	// Sort ascending — lowest survival scores get evicted first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}
	ids := make([]string, limit)
	for i := 0; i < limit; i++ {
		ids[i] = candidates[i].id
	}
	return ids, nil
}

// loadCoreVectors returns the raw vector blobs of all core shards for resonance checks.
func (v *Vessel) loadCoreVectors() ([][]byte, error) {
	const query = `SELECT vector FROM shards WHERE category = 'core' AND vector IS NOT NULL`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var vectors [][]byte
	for stmt.Step() {
		vectors = append(vectors, stmt.ColumnBlob(0, nil))
	}
	return vectors, stmt.Err()
}

// isResonantToCore checks if a shard's vector has cosine similarity above the threshold
// with any core shard vector.
func isResonantToCore(vec []byte, coreVectors [][]byte, threshold float64) bool {
	if len(vec) == 0 || len(coreVectors) == 0 {
		return false
	}
	v1 := DecodeVector(vec)
	if v1 == nil {
		return false
	}
	defer func() {
		if cap(v1) == vectorDimension {
			vectorPool.Put(v1[:vectorDimension])
		}
	}()

	for _, coreVec := range coreVectors {
		v2 := DecodeVector(coreVec)
		if v2 == nil || len(v2) != len(v1) {
			if v2 != nil && cap(v2) == vectorDimension {
				vectorPool.Put(v2[:vectorDimension])
			}
			continue
		}
		sim := cosineSimilarity(v1, v2)
		if cap(v2) == vectorDimension {
			vectorPool.Put(v2[:vectorDimension])
		}
		if sim >= threshold {
			return true
		}
	}
	return false
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

func (v *Vessel) SearchGraph(ctx context.Context, queryVector []byte, limit int, shouldTouch bool) ([]Shard, []ShardBond, error) {
	// SQLite fallback to vector search
	shards, err := v.FindResonant(ctx, queryVector, limit, shouldTouch)
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

func (v *Vessel) SaveActivity(ctx context.Context, entry ShardActivity) error {
	const query = `
		INSERT INTO activity_logs (type, message, shard_id)
		VALUES (?, ?, ?)
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	stmt.BindText(1, entry.Type)
	stmt.BindText(2, entry.Message)
	stmt.BindText(3, entry.ShardID)

	if err := stmt.Exec(); err != nil {
		return err
	}

	// TTL purge — keep last N days only
	retentionDays := os.Getenv("ACTIVITY_LOG_RETENTION_DAYS")
	if retentionDays == "" {
		retentionDays = "7"
	}

	purge, _, _ := v.conn.Prepare(
		`DELETE FROM activity_logs WHERE timestamp < datetime('now', '-' || ? || ' days')`,
	)
	if purge != nil {
		defer purge.Close()
		purge.BindText(1, retentionDays)
		_ = purge.Exec()
	}

	return nil
}

func (v *Vessel) GetRecentActivity(ctx context.Context, limit int) ([]ShardActivity, error) {
	const query = `
		SELECT timestamp, type, message, shard_id 
		FROM activity_logs 
		ORDER BY timestamp DESC 
		LIMIT ?
	`
	stmt, _, err := v.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	stmt.BindInt(1, limit)

	var logs []ShardActivity
	for stmt.Step() {
		logs = append(logs, ShardActivity{
			Timestamp: stmt.ColumnText(0),
			Type:      stmt.ColumnText(1),
			Message:   stmt.ColumnText(2),
			ShardID:   stmt.ColumnText(3),
		})
	}
	return logs, stmt.Err()
}

func (v *Vessel) CalculateCommunities(ctx context.Context) (int, []int64, error) {
	return 0, nil, nil
}

func (v *Vessel) PruneStaleSummaries(ctx context.Context) (int, error) {
	return 0, nil
}

func (v *Vessel) GetShardsByCommunity(ctx context.Context, communityID int64) ([]Shard, error) {
	return nil, nil
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

// DecodeVector converts a little-endian []byte blob into []float32.
// Used by SQLite cosine distance and by WorkingMemory centroid calculations.
func DecodeVector(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	limit := len(b) / 4
	var v []float32
	if limit <= vectorDimension {
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
