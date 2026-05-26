package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/izenberk/shard-link/internal/hygiene"
	"github.com/izenberk/shard-link/internal/janitor"
	"github.com/izenberk/shard-link/internal/mcp"
	"github.com/izenberk/shard-link/internal/storage"
	"github.com/izenberk/shard-link/internal/synthesizer"
)

func main() {
	// 1. Load Credentials
	apiKey := os.Getenv("HUB_API_KEY")
	if apiKey == "" {
		log.Println("WARNING: HUB_API_KEY is not set. Server is running without authentication.")
	}

	// 2. Ignite the Vessel with Retry Logic
	var v storage.Repository
	var err error

	neoURL := os.Getenv("NEO4J_URL")
	maxRetries := 15
	retryDelay := 10 * time.Second

	for i := 0; i < maxRetries; i++ {
		if neoURL != "" {
			// Knowledge Mesh Mode
			user := os.Getenv("NEO4J_USER")
			pass := os.Getenv("NEO4J_PASS")
			v, err = storage.NewVesselGraph(neoURL, user, pass, "neo4j")
			if err == nil {
				log.Println("SHARD-LINK: Knowledge Mesh Ignited (Neo4j Graph)")
				break
			}
		} else if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
			// Legacy High Performance Mode
			v, err = storage.NewPostgresVessel(context.Background(), connStr)
			if err == nil {
				log.Println("SHARD-LINK: PostgreSQL Vessel Ignited (Legacy storage)")
				break
			}
		} else {
			// Local-First Mode
			dbPath := os.Getenv("DATABASE_PATH")
			if dbPath == "" { dbPath = "data/shard-link.db" }
			v, err = storage.NewVessel(dbPath)
			if err == nil {
				log.Println("SHARD-LINK: SQLite Vessel Ignited")
				break
			}
		}

		log.Printf("Vessel failed to ignite (attempt %d/%d): %v. Retrying in %v...", i+1, maxRetries, err, retryDelay)
		time.Sleep(retryDelay)
	}

	if err != nil {
		log.Fatalf("Vessel failed to ignite after %d attempts: %v", maxRetries, err)
	}
	defer v.Close()

	// 2.5 Initialize Activity Ledger (SQLite) for cross-process logging
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/shard-link.db"
	}
	lv, err := storage.NewVessel(dbPath)
	if err == nil {
		storage.GlobalLogger = func(msg string, category string, shardID string) {
			_ = lv.SaveActivity(context.Background(), storage.ShardActivity{
				Type:    category,
				Message: msg,
				ShardID: shardID,
			})
		}
		log.Println("SHARD-LINK: Activity Ledger Connected (SQLite)")
	}

	// 3. Summon the Servants (Janitor & Synthesizer)
	jan := janitor.NewJanitor(v, 15*time.Minute, 1000)

	// Synthesizer wiring is deferred until after embedder init (step 5)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go jan.Run(ctx)

	// 4. Summon the HygieneWorker (Storage Maintenance)
	graphV, _ := v.(*storage.VesselGraph)
	var av *storage.PostgresVessel
	if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
		av, _ = storage.NewPostgresVessel(ctx, connStr)
	}

	hygieneInterval := 24 * time.Hour
	if h := os.Getenv("HYGIENE_INTERVAL_HOURS"); h != "" {
		if hours, err := strconv.Atoi(h); err == nil {
			hygieneInterval = time.Duration(hours) * time.Hour
		}
	}
	hygieneWorker := hygiene.NewHygieneWorker(graphV, av, lv, hygieneInterval)
	go hygieneWorker.Run(ctx)

	// 5. Configure Intelligence
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
			model = "gemini-embedding-001"
		}
		
		geminiEmb, err := storage.NewGeminiEmbedder(ctx, geminiKey, model)
		if err != nil {
			log.Fatalf("Failed to ignite Cloud Embedder: %v", err)
		}
		emb = geminiEmb
		log.Printf("SHARD-LINK: Cloud Intelligence Active (%s)", model)
		
	case "local":
		// Local Mode: Privacy-first, runs a model via Ollama
		ollamaURL := os.Getenv("OLLAMA_URL")
		ollamaModel := os.Getenv("OLLAMA_MODEL")
		emb = storage.NewOllamaEmbedder(ollamaURL, ollamaModel)
		log.Printf("SHARD-LINK: Ollama Intelligence Active (%s)", ollamaModel)

	default:
		// None Mode: Hardware/Budget constraint fallback
		emb = storage.NewMockEmbedder(3072)
		log.Println("SHARD-LINK: Intelligence is in 'Manual/Mock' mode (No embedding)")
	}

	// 5.5 Initialize Summarizer and launch Synthesizer
	var sum storage.Summarizer
	switch mode {
	case "server":
		sumModel := os.Getenv("SUMMARIZER_MODEL")
		if sumModel == "" {
			sumModel = "gemini-2.5-flash"
		}
		geminiKey := os.Getenv("GEMINI_API_KEY")
		geminiSum, err := storage.NewGeminiSummarizer(ctx, geminiKey, sumModel)
		if err != nil {
			log.Printf("WARNING: Failed to ignite Summarizer: %v. Community summaries disabled.", err)
			sum = storage.NewMockSummarizer()
		} else {
			sum = geminiSum
			log.Printf("SHARD-LINK: Community Summarizer Active (%s)", sumModel)
		}
	default:
		sum = storage.NewMockSummarizer()
	}

	syn := synthesizer.NewSynthesizer(v, 10*time.Minute, emb, sum)
	go syn.Run(ctx)

	// 6. Launch the Authenticated Bridge
	srv := mcp.NewMCPServer(v, apiKey, emb)

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8080"
	}

	go func() {
		if err := srv.StartHub(8080, publicURL); err != nil {
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
