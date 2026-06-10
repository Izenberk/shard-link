package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/izenberk/shard-link/internal/hygiene"
	"github.com/izenberk/shard-link/internal/janitor"
	"github.com/izenberk/shard-link/internal/mcp"
	"github.com/izenberk/shard-link/internal/storage"
	"github.com/izenberk/shard-link/internal/synthesizer"
)

func main() {
	// 0. Configure structured logging.
	// JSON in production (Docker log driver), text for local development.
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		handler = slog.NewTextHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(handler))

	// 1. Load Credentials
	apiKey := os.Getenv("HUB_API_KEY")
	if apiKey == "" {
		slog.Warn("HUB_API_KEY is not set — server running without authentication")
	}

	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	oauthClientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	if oauthClientID == "" || oauthClientSecret == "" {
		slog.Error("OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET are required")
		os.Exit(1)
	}

	// 1.5 Configure Embedding Dimension (before any vessel or pool usage)
	targetDim := 768
	if dimStr := os.Getenv("EMBEDDING_DIMENSION"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			targetDim = d
		}
	}
	storage.SetVectorDimension(targetDim)

	// 2. Ignite the Vessel with Retry Logic
	var v storage.Repository
	var err error

	neoURL := os.Getenv("NEO4J_URL")
	maxRetries := 15
	retryDelay := 10 * time.Second

	for i := 0; i < maxRetries; i++ {
		if neoURL != "" {
			user := os.Getenv("NEO4J_USER")
			pass := os.Getenv("NEO4J_PASS")
			v, err = storage.NewVesselGraph(neoURL, user, pass, "neo4j")
			if err == nil {
				slog.Info("vessel ignited", "engine", "neo4j")
				break
			}
		} else if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
			v, err = storage.NewPostgresVessel(context.Background(), connStr)
			if err == nil {
				slog.Info("vessel ignited", "engine", "postgres")
				break
			}
		} else {
			dbPath := os.Getenv("DATABASE_PATH")
			if dbPath == "" {
				dbPath = "data/shard-link.db"
			}
			v, err = storage.NewVessel(dbPath)
			if err == nil {
				slog.Info("vessel ignited", "engine", "sqlite")
				break
			}
		}

		slog.Warn("vessel failed to ignite — retrying",
			"attempt", i+1,
			"max_retries", maxRetries,
			"error", err,
			"retry_delay", retryDelay,
		)
		time.Sleep(retryDelay)
	}

	if err != nil {
		slog.Error("vessel failed to ignite — giving up",
			"attempts", maxRetries,
			"error", err,
		)
		os.Exit(1)
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
		slog.Info("activity ledger connected", "engine", "sqlite")
	}

	// 3. Cancellation context + WaitGroup for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 3.1 Summon the Janitor
	jan := janitor.NewJanitor(v, 15*time.Minute, 1000, storage.GlobalLogger)

	wg.Add(1)
	go func() {
		defer wg.Done()
		jan.Run(ctx)
	}()

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
	hygieneWorker := hygiene.NewHygieneWorker(graphV, av, lv, hygieneInterval, storage.GlobalLogger)

	wg.Add(1)
	go func() {
		defer wg.Done()
		hygieneWorker.Run(ctx)
	}()

	// 5. Configure Intelligence
	var emb storage.Embedder
	mode := os.Getenv("EMBEDDING_MODE")

	switch mode {
	case "server":
		geminiKey := os.Getenv("GEMINI_API_KEY")
		if geminiKey == "" {
			slog.Error("EMBEDDING_MODE=server but GEMINI_API_KEY is not set")
			os.Exit(1)
		}

		model := os.Getenv("EMBEDDING_MODEL")
		if model == "" {
			model = "gemini-embedding-001"
		}

		geminiEmb, err := storage.NewGeminiEmbedder(ctx, geminiKey, model, targetDim)
		if err != nil {
			slog.Error("cloud embedder failed to ignite", "error", err)
			os.Exit(1)
		}
		emb = geminiEmb
		slog.Info("intelligence active", "mode", "cloud", "model", model, "dim", targetDim)

	case "local":
		ollamaURL := os.Getenv("OLLAMA_URL")
		ollamaModel := os.Getenv("OLLAMA_MODEL")
		emb = storage.NewOllamaEmbedder(ollamaURL, ollamaModel)
		slog.Info("intelligence active", "mode", "local", "model", ollamaModel)

	default:
		emb = storage.NewMockEmbedder(targetDim)
		slog.Info("intelligence active", "mode", "mock")
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
			slog.Warn("summarizer failed to ignite — community summaries disabled", "error", err)
			sum = storage.NewMockSummarizer()
		} else {
			sum = geminiSum
			slog.Info("summarizer active", "model", sumModel)
		}
	default:
		sum = storage.NewMockSummarizer()
	}

	synthInterval := 30 * time.Minute
	if si := os.Getenv("SYNTHESIZER_INTERVAL_MINUTES"); si != "" {
		if mins, err := strconv.Atoi(si); err == nil && mins > 0 {
			synthInterval = time.Duration(mins) * time.Minute
		}
	}
	syn := synthesizer.NewSynthesizer(v, synthInterval, emb, sum, storage.GlobalLogger)

	wg.Add(1)
	go func() {
		defer wg.Done()
		syn.Run(ctx)
	}()

	// 6. Launch the Authenticated Bridge
	srv := mcp.NewMCPServer(v, apiKey, oauthClientID, oauthClientSecret, emb, sum, lv, av, ctx)

	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8080"
	}

	go func() {
		if err := srv.StartHub(ctx, 8080, publicURL); err != nil && err != http.ErrServerClosed {
			slog.Error("bridge collapsed", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("hub online", "port", 8080, "public_url", publicURL)

	// 7. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	sig := <-sigChan

	slog.Info("shutdown signal received", "signal", sig.String())

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP server drain error", "error", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("shutdown complete — all goroutines drained")
	case <-time.After(10 * time.Second):
		slog.Warn("shutdown timed out — forcing exit")
	}
}
