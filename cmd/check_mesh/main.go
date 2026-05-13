package main

import (
	"context"
	"log"
	"os"

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

	if neoURL == "" || neoUser == "" || neoPass == "" {
		log.Fatalf("Neo4j environment variables (NEO4J_URL, NEO4J_USER, NEO4J_PASS) are not set")
	}

	v, err := storage.NewVesselGraph(neoURL, neoUser, neoPass, "neo4j")
	if err != nil {
		log.Fatalf("Failed to connect to Knowledge Mesh: %v", err)
	}
	defer v.Close()

	log.Println(">>> Starting Knowledge Mesh Consistency Audit...")

	// 2. Vector Integrity Check
	log.Println("[1/2] Checking for invalid embeddings...")
	invalidIDs, err := v.FindInvalidShards(ctx)
	if err != nil {
		log.Printf("Error during vector audit: %v", err)
	} else if len(invalidIDs) > 0 {
		log.Printf("CRITICAL: Found %d shards with invalid/missing vectors:", len(invalidIDs))
		for _, id := range invalidIDs {
			log.Printf("  - %s", id)
		}
		log.Println("Recommendation: Run 'cmd/repair_vec' followed by 'cmd/migrate' or 'cmd/auto_link'.")
	} else {
		log.Println("OK: All shard vectors are present and valid.")
	}

	// 3. Connectivity Check
	log.Println("[2/2] Searching for orphaned shards (non-core)...")
	orphanIDs, err := v.FindOrphanShards(ctx)
	if err != nil {
		log.Printf("Error during connectivity audit: %v", err)
	} else if len(orphanIDs) > 0 {
		log.Printf("WARNING: Found %d orphaned shards (no semantic bonds):", len(orphanIDs))
		for _, id := range orphanIDs {
			log.Printf("  - %s", id)
		}
		log.Println("Recommendation: Run 'cmd/auto_link' to rebuild semantic relationships.")
	} else {
		log.Println("OK: All non-core shards are semantically bonded.")
	}

	log.Println(">>> Audit Complete.")
}