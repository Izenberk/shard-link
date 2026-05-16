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
			log.Println("SHARD-LINK: PostgreSQL Vessel Ignited (Legacy storage)")
		}
	} else {
		// Local-First Mode
		dbPath := os.Getenv("DATABASE_PATH")
		if dbPath == "" { dbPath = "data/shard-link.db" }
		v, err = storage.NewVessel(dbPath)
		if err == nil {
			log.Println("SHARD-LINK: SQLite Vessel Ignited")
		}
	}

	if err != nil {
		log.Fatalf("Vessel failed to ignite: %v", err)
	}
	defer v.Close()

	// 3. Summon the Janitor
	jan := janitor.NewJanitor(v, 15*time.Minute, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go jan.Run(ctx)

	// 4. Configure Intelligence (Phase 10: Standalone Intelligence)
	var emb storage.Embedder
	mode := os.Getenv("EMBEDDING_MODE") // options: "none", "server", "local"
	
	switch mode {
	case "server":
		// Cloud Mode: Zero local hardware load, utilizes Gemini API
		geminiKey := os.Getenv("GEMINI_API_KEY")
		if geminiKey == "" {
			log.Fatal("EMBEDDING_MODE='server' but GEMINI_API_KEY is not set.")
		}

		model := os.Getenv("EMBEDDING_MODEL")
		if model == "" {
			model = "embedding-001"
		}
		
		geminiEmb, err := storage.NewGeminiEmbedder(ctx, geminiKey, model)
		if err != nil {
			log.Fatalf("Failed to ignite Cloud Embedder: %v", err)
		}
		emb = geminiEmb
		log.Printf("SHARD-LINK: Cloud Intelligence Active (%s)", model)
		
	case "local":
		// Local Mode: Privacy-first, runs a tiny model on local CPU
		emb = storage.NewTinyLocalEmbedder()
		log.Println("SHARD-LINK: Tiny Local Intelligence Active (Placeholder)")

	default:
		// None Mode: Hardware/Budget constraint fallback
		emb = storage.NewMockEmbedder(768)
		log.Println("SHARD-LINK: Intelligence is in 'Manual/Mock' mode (No embedding)")
	}

	// 5. Launch the Authenticated Bridge
	srv := mcp.NewMCPServer(v, apiKey, emb)

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8080"
	}

	go func() {
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
