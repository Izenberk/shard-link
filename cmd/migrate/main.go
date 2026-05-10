package main

import (
	"context"
	"log"
	"os"

	"github.com/izenberk/shard-link/internal/storage"
)

func main() {
	ctx := context.Background()

	// 1. Ignite the Source (SQLite)
	sqliteVessel, err := storage.NewVessel("data/shard-link.db")
	if err != nil {
		log.Fatalf("Failed to connect to Postgres target: %v", err)
	}
	defer sqliteVessel.Close()

	// 2. Ignite the Target (PostgreSQL)
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	pgVessel, err := storage.NewPostgresVessel(ctx, connStr)
	if err != nil {
		log.Fatalf("Failed to connect to Postgres target: %v", err)
	}
	defer pgVessel.Close()

	// 3. Extract from SQLite
	log.Println("Extracting shards from SQLite...")
	shards, err := sqliteVessel.GetAllShards(ctx)
	if err != nil {
		log.Fatalf("Migration failed during extraction: %v", err)
	}

	// 4. Load into Postgres
	log.Printf("Found %d shards. Starting injection...", len(shards))
	for _, s := range shards {
		if err := pgVessel.SaveShard(ctx, s); err != nil {
			log.Printf("Warning: Failed to migrate shard %s: %v", s.ID, err)
			continue
		}
		log.Printf("✓ Migrated: %s", s.ID)
	}

	log.Println("MISSION COMPLETE: Data migration finished successfully.")
}