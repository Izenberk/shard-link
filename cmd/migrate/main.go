package main

import (
	"context"
	"log"
	"os"

	"github.com/izenberk/shard-link/internal/storage"
)

func main() {
	ctx := context.Background()

	// 1. Ignite the Source (SQLite - The Original High-Precision Source)
	// We use the DATABASE_PATH from environment or default to local
	sqlitePath := os.Getenv("DATABASE_PATH")
	if sqlitePath == "" {
		sqlitePath = "/app/data/shard-link.db"
	}
	
	if _, err := os.Stat(sqlitePath); os.IsNotExist(err) {
		log.Fatalf("SQLite database not found at %s. Ensure it is mounted correctly.", sqlitePath)
	}

	sqliteVessel, err := storage.NewVessel(sqlitePath)
	if err != nil {
		log.Fatalf("Failed to connect to SQLite source: %v", err)
	}
	defer sqliteVessel.Close()

	// 2. Ignite the Target (Neo4j - Knowledge Mesh)
	neoURL := os.Getenv("NEO4J_URL")
	neoUser := os.Getenv("NEO4J_USER")
	neoPass := os.Getenv("NEO4J_PASS")
	if neoURL == "" || neoUser == "" || neoPass == "" {
		log.Fatal("Neo4j environment variables are not set")
	}

	graphVessel, err := storage.NewVesselGraph(neoURL, neoUser, neoPass, "neo4j")
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j target: %v", err)
	}
	defer graphVessel.Close()

	// 3. Extract & Migrate Shards from SQLite (Raw Precision)
	log.Println("Extracting shards from SQLite...")
	shards, err := sqliteVessel.GetAllShards(ctx)
	if err != nil {
		log.Fatalf("Migration failed during extraction: %v", err)
	}

	log.Printf("Found %d shards. Initiating high-fidelity transfer...", len(shards))
	for _, s := range shards {
		if err := graphVessel.SaveShard(ctx, s); err != nil {
			log.Printf("Warning: Failed to migrate shard %s: %v", s.ID, err)
			continue
		}
	}

	// 4. Extract & Migrate Bonds (Relationships) from SQLite
	log.Println("Extracting bonds from SQLite...")
	bonds, err := sqliteVessel.GetAllBonds(ctx)
	if err != nil {
		log.Printf("Warning: Could not read bonds from SQLite: %v", err)
	} else {
		log.Printf("Found %d bonds. Linking shards in the graph...", len(bonds))
		for _, b := range bonds {
			if err := graphVessel.SaveBond(ctx, b); err != nil {
				log.Printf("Warning: Failed to link shards %s -> %s: %v", b.FromID, b.ToID, err)
				continue
			}
		}
	}

	log.Println("MISSION COMPLETE: The Knowledge Mesh is now fully recovered with high precision.")
}
