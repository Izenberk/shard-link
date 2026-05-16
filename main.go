package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/izenberk/shard-link/internal/janitor"
	"github.com/izenberk/shard-link/internal/mcp"
	"github.com/izenberk/shard-link/internal/storage"
)

func main() {
	// 1. Load Credentials
	apiKey := os.Getenv("HUB_API_KEY")
	if apiKey == "" {
		log.Println("WARNING: HUB_API_KEY is not set. Server is running without authentication.")
	}

	// 2. Ignite the Vessel
	var v storage.Repository
	var err error

	neoURL := os.Getenv("NEO4J_URL")
	if neoURL != "" {
		// Knowledge Mesh Mode
		user := os.Getenv("NEO4J_USER")
		pass := os.Getenv("NEO4J_PASS")
		v, err = storage.NewVesselGraph(neoURL, user, pass, "neo4j")
		if err == nil {
			log.Println("SHARD-LINK: Knowledge Mesh Ignited (Neo4j Graph)")
		}
	} else if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
		// Legacy High Performance Mode
		v, err = storage.NewPostgresVessel(context.Background(), connStr)
		if err == nil {
			log.Println("SHARD-LINK: PostgreSQK Vessel Ignited (Lagacy storage)")
		}
	} else {
		// Local-First Mode
		dbPath := os.Getenv("DATABASE_PATH")
		if dbPath == "" { dbPath = "data/shard-liink.db" }
		v, err = storage.NewVessel(dbPath)
		if err == nil {
			log.Println("SHARD-LINK: SQLite Vessel Ignited")
		}
	}

	if err != nil {
		log.Fatalf("Vessel failed to ignit: %v", err)
	}
	defer v.Close()

	// 3. Summon the Janitor
	// Adjusted interval from 1m to 15m to reduce resource spikes
	jan := janitor.NewJanitor(v, 15*time.Minute, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go jan.Run(ctx)

	// 4. Launch the Authenticated Bridge
	srv := mcp.NewMCPServer(v, apiKey)

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8080"
	}

	go func() {
		// Port 8080 is always internal; Cloudflare provides the outer HTTPS layer
		if err := srv.StartSSE(8080, publicURL); err != nil {
			log.Fatalf("Bridge collapsed: %v", err)
		}
	}()

	log.Println("SHARD-LINK Hub is ONLINE (Authenticated).")

	// Graceful Shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan 
	log.Println("SHARD-LINK Hub shutting down...")
}
