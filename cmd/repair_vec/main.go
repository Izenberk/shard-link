package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"log"
	"math"
	"os"
	"strconv"

	"github.com/izenberk/shard-link/internal/storage"
)

var repairDim = 768

func main() {
	ctx := context.Background()

	// 0. Read target dimension
	if dimStr := os.Getenv("EMBEDDING_DIMENSION"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			repairDim = d
		}
	}

	// 1. Ignite the Vessel
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/shard-link.db"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("SQLite database not found at %s", dbPath)
	}

	v, err := storage.NewVessel(dbPath)
	if err != nil {
		log.Fatalf("Failed to open vessel: %v", err)
	}
	defer v.Close()

	// 2. Fetch all shards
	shards, err := v.GetAllShards(ctx)
	if err != nil {
		log.Fatalf("Failed to get shards: %v", err)
	}

	log.Printf("Repairing %d shards in %s...", len(shards), dbPath)

	for _, s := range shards {
		// 3. Generate Mock Embedding
		newVec := generateMockEmbedding(s.Content)
		
		// Convert []float32 to []byte
		s.Vector = encodeVector(newVec)

		// 4. Save back
		if err := v.SaveShard(ctx, s); err != nil {
			log.Printf("Failed to save shard %s: %v", s.ID, err)
		} else {
			log.Printf("Repaired shard: %s", s.ID)
		}
	}

	log.Println("REPAIR COMPLETE: All shards now have non-zero, deterministic vectors.")
}

func generateMockEmbedding(content string) []float32 {
	vec := make([]float32, repairDim)
	hash := sha256.Sum256([]byte(content))

	// Seed the vector with hash values
	for i := 0; i < repairDim; i++ {
		// Use different parts of the hash to create pseudo-random but deterministic floats
		start := (i * 7) % (32 - 4)
		chunk := hash[start : start+4]
		bits := binary.LittleEndian.Uint32(chunk)
		
		// Map uint32 to a float32 between -1.0 and 1.0
		f := float32(bits) / float32(math.MaxUint32)
		vec[i] = (f * 2.0) - 1.0
	}
	
	// Normalize the vector for cosine similarity
	var norm float64
	for _, f := range vec {
		norm += float64(f * f)
	}
	sqrtNorm := float32(math.Sqrt(norm))
	if sqrtNorm > 0 {
		for i := range vec {
			vec[i] /= sqrtNorm
		}
	}
	
	return vec
}

func encodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
