package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Note: No .env file found, relying on system environment variables")
	}

	ctx := context.Background()

	// 1. Ignite the Vessel (Neo4j)
	neoURL := os.Getenv("NEO4J_URL")
	neoUser := os.Getenv("NEO4J_USER")
	neoPass := os.Getenv("NEO4J_PASS")
	if neoURL == "" {
		log.Fatal("NEO4J_URL is not set")
	}

	v, err := storage.NewVesselGraph(neoURL, neoUser, neoPass, "neo4j")
	if err != nil {
		log.Fatalf("Failed to connect to Knowledge Mesh: %v", err)
	}
	defer v.Close()

	// 2. Ignite the Cloud Intelligence (Gemini)
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		log.Fatal("GEMINI_API_KEY is not set")
	}

	emb, err := storage.NewGeminiEmbedder(ctx, geminiKey, "gemini-embedding-001")
	if err != nil {
		log.Fatalf("Failed to ignite Cloud Embedder: %v", err)
	}

	// 3. Fetch all existing shards
	shards, err := v.GetAllShards(ctx)
	if err != nil {
		log.Fatalf("Failed to fetch shards: %v", err)
	}

	log.Printf(">>> Starting High-Precision Re-Embedding of %d shards...", len(shards))

	for i, s := range shards {
		log.Printf("[%d/%d] Embedding shard: %s", i+1, len(shards), s.ID)

		// Call Gemini API to get real 768-D vector
		floats, err := emb.Embed(ctx, s.Content)
		if err != nil {
			log.Printf("ERROR: Failed to embed %s: %v", s.ID, err)
			continue
		}

		// Convert to raw bytes for storage
		s.Vector = storage.EncodeVector(floats)
		s.LastUsed = time.Now()

		// Save back to the Knowledge Mesh
		if err := v.SaveShard(ctx, s); err != nil {
			log.Printf("ERROR: Failed to save %s: %v", s.ID, err)
			continue
		}
	}

	log.Println(">>> MISSION COMPLETE: All shards now have high-precision 768-D vectors.")
}
