package storage

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"github.com/ncruces/go-sqlite3"

)

//go:embed schema.sql
var schema string

type Vessel struct {
	conn *sqlite3.Conn
}

// NewVessel opens the database at the given path and initializes the Shard-Link schema.
func NewVessel (path string) (*Vessel, error) {
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

		if v1 == nil || v2 == nil || len(v1) != len(v2) {
			ctx.ResultFloat(2.0) // Maximum distance for invalid comparison
			return
		}

		// Distance = 1.0 - Similarity
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
func (v *Vessel) SaveShard(s Shard) error {
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
func (v *Vessel) FindResonant(queryVector []byte, limit int) ([]Shard, error) {
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
	for stmt.Step () {
		shards = append(shards, Shard{
			ID: 		stmt.ColumnText(0),
			Category:	stmt.ColumnText(1),
			Content:	stmt.ColumnText(2),
			Vector:		stmt.ColumnBlob(3, nil),
			Metadata:	stmt.ColumnBlob(4, nil),
			// SQLite dates are strings by default in this driver,
			// for now we'll skip parsing them to keep it simple
		})
	}

	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("scan results: %w", err)
	}

	return shards, nil
}

// Close safely shuts down the database.
func (v *Vessel) Close() error {
	return v.conn.Close()
}

// --- Resonance Math Helpers ---

func decodeVector(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
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