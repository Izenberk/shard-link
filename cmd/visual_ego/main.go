package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/joho/godotenv"
)

type ActivityEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"` // "info", "warn", "success", "evict", "bond"
	Message   string `json:"message"`
	ShardID   string `json:"shard_id,omitempty"`
}

var (
	activityFeed = make(chan ActivityEntry, 100)
	clients      = make(map[chan ActivityEntry]bool)
	clientMux    sync.Mutex
)

func broadcastActivity(e ActivityEntry) {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().Format("15:04:05")
	}
	clientMux.Lock()
	defer clientMux.Unlock()
	for c := range clients {
		select {
		case c <- e:
		default:
		}
	}
}

type VizNode struct {
	ID         string  `json:"id"`
	Category   string  `json:"category"`
	Content    string  `json:"content"`
	Community  int64   `json:"community"`
	PageRank   float64 `json:"pagerank"`
	Survival   float64 `json:"survival"`
	CreatedAt  string  `json:"created_at"`
	SourceType string  `json:"source_type"`
	SourceRef  string  `json:"source_ref"`
	Confidence float64 `json:"confidence"`
}

type VizLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

type VizData struct {
	Nodes     []VizNode `json:"nodes"`
	Links     []VizLink `json:"links"`
	Threshold float64   `json:"threshold"`
}

type Server struct {
	vessel         *storage.VesselGraph
	localVessel    *storage.Vessel
	archivalVessel *storage.PostgresVessel
	embedder       storage.Embedder
	bootTime       time.Time
}

func main() {
	_ = godotenv.Load()
	ctx := context.Background()
	bootTime := time.Now()

	v, err := storage.NewVesselGraph(os.Getenv("NEO4J_URL"), os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASS"), "neo4j")
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	// Connect to Postgres Archival Vessel
	pgURL := os.Getenv("POSTGRES_URL")
	if pgURL == "" {
		// Construct from components if available, with local-dev defaults
		user := os.Getenv("DB_USER")
		if user == "" { user = "sharduser" }
		pass := os.Getenv("DB_PASSWORD")
		if pass == "" { pass = "shardpass" }
		host := os.Getenv("DB_HOST")
		if host == "db" || host == "" { host = "localhost" } // Fallback to localhost if "db" is provided but we are local
		port := os.Getenv("DB_PORT")
		if port == "5432" || port == "" { port = "5434" } // Map to the docker-exposed port 5434
		dbName := os.Getenv("DB_NAME")
		if dbName == "" { dbName = "shardlink" }
		
		pgURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, dbName)
	}

	var av *storage.PostgresVessel
	if pgURL != "" {
		log.Printf("[Init] Connecting to Archival Vessel: %s", pgURL)
		av, err = storage.NewPostgresVessel(ctx, pgURL)
		if err != nil {
			log.Printf("Warning: Failed to connect to Postgres Archival Vessel: %v", err)
		} else {
			defer av.Close()
		}
	}

	// Connect to local SQLite for persistent logging
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./data/shard-link.db"
	}
	lv, err := storage.NewVessel(dbPath)
	if err != nil {
		log.Printf("Warning: Failed to init local vessel for logging: %v", err)
	}

	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = "gemini-embedding-001"
	}
	targetDim := 768
	if dimStr := os.Getenv("EMBEDDING_DIMENSION"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			targetDim = d
		}
	}
	emb, err := storage.NewGeminiEmbedder(ctx, os.Getenv("GEMINI_API_KEY"), model, targetDim)
	if err != nil {
		log.Printf("Warning: Failed to init embedder: %v. Search will be limited.", err)
	}

	srv := &Server{
		vessel:         v,
		localVessel:    lv,
		archivalVessel: av,
		embedder:       emb,
		bootTime:       bootTime,
	}

	storage.GlobalLogger = func(msg string, category string, shardID string) {
		entry := ActivityEntry{
			Message: msg,
			Type:    category,
			ShardID: shardID,
		}
		broadcastActivity(entry)

		// Persist to SQLite
		if srv.localVessel != nil {
			_ = srv.localVessel.SaveActivity(context.Background(), storage.ShardActivity{
				Type:    category,
				Message: msg,
				ShardID: shardID,
			})
		}
	}

	// 1. Background Analysis (Phase 10: Standalone Intelligence)
	go func() {
		log.Println("[Background] Starting initial mesh analysis...")
		_, _, _ = srv.vessel.CalculateCommunities(ctx)
		log.Println("[Background] Mesh analysis complete.")
	}()

	http.Handle("/", http.FileServer(http.Dir("web/static")))
	// 2. API Endpoints
	http.HandleFunc("/api/graph", srv.handleGetGraph)
	http.HandleFunc("/api/search", srv.handleSearch)
	http.HandleFunc("/api/bonds", srv.handleBonds)
	http.HandleFunc("/api/activity", srv.handleActivity)
	http.HandleFunc("/api/logs", srv.handleGetLogs)
	http.HandleFunc("/api/evict", srv.handleEvict)
	http.HandleFunc("/api/community", srv.handleGetCommunity)
	http.HandleFunc("/api/prune-summaries", srv.handlePruneSummaries)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK. Booted: %v", srv.bootTime.Format(time.RFC3339))
	})

	log.Printf("Visual Ego Live Dashboard ignited on 127.0.0.1:8081\n")
	log.Fatal(http.ListenAndServe("127.0.0.1:8081", nil))
}

func (s *Server) handleEvict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Shard ID required", 400)
		return
	}

	log.Printf("[API] Manual Eviction Request: %s", id)

	// 1. Get full shard data from Mesh
	shard, err := s.vessel.GetShardByID(r.Context(), id)
	if err != nil {
		log.Printf("[API ERROR] Shard %s not found in Mesh: %v", id, err)
		http.Error(w, "Shard not found in Living Memory", 404)
		return
	}

	// 1b. Immutable Protection: Core shards cannot be evicted
	if shard.Category == "core" {
		log.Printf("[API WARN] Blocked eviction attempt of CORE shard: %s", id)
		http.Error(w, "Immutable Protection: CORE shards cannot be evicted.", http.StatusForbidden)
		return
	}

	// 2. Log manual eviction intent
	if storage.GlobalLogger != nil {
		storage.GlobalLogger(fmt.Sprintf("Manual eviction initiated: %s", id), "warn", id)
	}

	// 3. Move to Archival Vessel (Postgres)
	if s.archivalVessel != nil {
		shard.Category = "archived" // Mark as White Dwarf
		// Use the dedicated ArchiveShard method which handles the move to shards_archive
		if err := s.archivalVessel.SaveArchivedShard(r.Context(), shard); err != nil {
			log.Printf("[API ERROR] Failed to move shard %s to Archival Vessel: %v", id, err)
			http.Error(w, "Archival transition failed", 500)
			return
		}
	}

	// 3. Remove from Mesh (Neo4j)
	if err := s.vessel.ArchiveShard(r.Context(), id); err != nil {
		log.Printf("[API ERROR] Failed to evict shard %s: %v", id, err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if s.localVessel == nil {
		http.Error(w, "Persistent logging not available", 500)
		return
	}
	logs, err := s.localVessel.GetRecentActivity(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan ActivityEntry, 10)
	clientMux.Lock()
	clients[ch] = true
	clientMux.Unlock()

	defer func() {
		clientMux.Lock()
		delete(clients, ch)
		clientMux.Unlock()
		close(ch)
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case e := <-ch:
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
	}
}

func (s *Server) handleBonds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodPost:
		var b storage.ShardBond
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			log.Printf("[API ERROR] Failed to decode bond JSON: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		log.Printf("[API] Manual Bond Request: %s <-> %s (w: %.2f)", b.FromID, b.ToID, b.Weight)
		if err := s.vessel.SaveBond(ctx, b); err != nil {
			log.Printf("[API ERROR] Failed to save bond: %v", err)
			http.Error(w, err.Error(), 500)
			return
		}
		if storage.GlobalLogger != nil {
			storage.GlobalLogger(fmt.Sprintf("Manual bond created: %s <-> %s", b.FromID, b.ToID), "bond", b.FromID)
		}
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if from == "" || to == "" {
			http.Error(w, "from and to IDs required", 400)
			return
		}
		log.Printf("[API] Manual Unlink Request: %s <-/-> %s", from, to)
		
		// Use bidirectional match to ensure we catch the bond regardless of direction
		query := `
		MATCH (f:Shard {id: $from})-[r:CONNECTED_TO]-(t:Shard {id: $to}) 
		DELETE r
		RETURN count(r) as deleted
		`
		result, err := s.vessel.ExecuteQuery(ctx, query, map[string]any{"from": from, "to": to})
		if err != nil {
			log.Printf("[API ERROR] Failed to delete bond: %v", err)
			http.Error(w, err.Error(), 500)
			return
		}
		
		deleted, _ := result.Records[0].Get("deleted")
		if deleted.(int64) > 0 {
			if storage.GlobalLogger != nil {
				storage.GlobalLogger(fmt.Sprintf("Bond Severed: %s <-/-> %s", from, to), "warn", from)
			}
		}
		log.Printf("[API] Successfully removed %v semantic bonds.", deleted)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	shards, bonds, err := s.vessel.GetGraphData(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Fetch Archived Shards from Postgres
	if s.archivalVessel != nil {
		archived, err := s.archivalVessel.GetArchivedShards(ctx)
		if err != nil {
			log.Printf("[API WARN] Failed to fetch archived shards: %v", err)
		} else {
			shards = append(shards, archived...)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.packData(shards, bonds))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query required", 400)
		return
	}

	log.Printf("[API] Searching Mesh: %s", query)
	if s.embedder == nil {
		log.Printf("[API ERROR] Embedder not available for search")
		http.Error(w, `{"error":"Embedder not initialized. Check GEMINI_API_KEY."}`, 503)
		return
	}
	floats, err := s.embedder.Embed(r.Context(), query)
	if err != nil {
		log.Printf("[API ERROR] Embedding failed for query '%s': %v", query, err)
		http.Error(w, fmt.Sprintf("Embedding failed: %v", err), 500)
		return
	}

	shards, bonds, err := s.vessel.SearchGraph(r.Context(), storage.EncodeVector(floats), 20, true)
	if err != nil {
		log.Printf("[API ERROR] SearchGraph failed: %v", err)
		http.Error(w, "Search failed", 500)
		return
	}

	log.Printf("[API] Found %d shards and %d bonds for query: %s", len(shards), len(bonds), query)
	if storage.GlobalLogger != nil {
		storage.GlobalLogger(fmt.Sprintf("Dashboard search: '%s' -> %d shards found", query, len(shards)), "search", "")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.packData(shards, bonds))
}

func (s *Server) handleGetCommunity(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "community id required", 400)
		return
	}

	shardID := fmt.Sprintf("comm-summary-%s", idStr)
	shard, err := s.vessel.GetShardByID(r.Context(), shardID)

	type CommunityResponse struct {
		Summary     string `json:"summary"`
		CommunityID string `json:"community_id"`
	}

	resp := CommunityResponse{CommunityID: idStr}
	if err == nil {
		resp.Summary = shard.Content
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePruneSummaries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("[API] Manual prune-summaries triggered")
	pruned, err := s.vessel.PruneStaleSummaries(r.Context())
	if err != nil {
		log.Printf("[API ERROR] Prune failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}

	msg := fmt.Sprintf("Pruned %d stale community summaries", pruned)
	log.Printf("[API] %s", msg)
	if storage.GlobalLogger != nil {
		storage.GlobalLogger(msg, "system", "")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"pruned": pruned})
}

func (s *Server) packData(shards []storage.Shard, bonds []storage.ShardBond) VizData {
	tStr := os.Getenv("MESH_LINK_THRESHOLD")
	threshold, _ := strconv.ParseFloat(tStr, 64)
	if threshold == 0 {
		threshold = 0.75
	}

	data := VizData{
		Nodes:     make([]VizNode, len(shards)),
		Links:     make([]VizLink, len(bonds)),
		Threshold: threshold,
	}

	// Calculate bond counts for survival estimation
	bondCounts := make(map[string]int)
	for _, b := range bonds {
		bondCounts[b.FromID]++
		bondCounts[b.ToID]++
	}

	for i, s := range shards {
		// Usage Reinforcement: Survival is based on LastUsed (Recency) rather than Creation
		lastUsed := s.LastUsed
		if lastUsed.IsZero() || lastUsed.Year() < 2000 {
			lastUsed = s.CreatedAt
			if lastUsed.IsZero() || lastUsed.Year() < 2000 {
				lastUsed = time.Now().Add(-1 * time.Hour)
			}
		}

		links := bondCounts[s.ID]

		// Backward compat: unscored shards default to 0.5
		salience := s.Salience
		if salience < 0.1 {
			salience = 0.5
		}

		// Dual-score benchmark: v3.5 (legacy) vs v4.1 (FSRS-calibrated decay)
		v35 := storage.SurvivalScoreV35(links, s.PageRank, s.UseCount, lastUsed)
		v41 := storage.SurvivalScoreV4(links, s.PageRank, salience, s.RetrievalHistory, lastUsed)

		survival := v41
		if s.Category == "core" {
			survival = 100 // Core shards are the "Gold Standard" (100)
		}

		log.Printf("[BENCHMARK] shard=%s v35=%.2f v41=%.2f delta=%.2f", s.ID, v35, v41, v41-v35)

		data.Nodes[i] = VizNode{
			ID:         s.ID,
			Category:   s.Category,
			Content:    s.Content,
			Community:  s.CommunityID,
			PageRank:   s.PageRank,
			Survival:   survival,
			CreatedAt:  s.CreatedAt.Format("2006-01-02 15:04:05"),
			SourceType: s.SourceType,
			SourceRef:  s.SourceRef,
			Confidence: s.Confidence,
		}
	}
	for i, b := range bonds {
		data.Links[i] = VizLink{Source: b.FromID, Target: b.ToID, Weight: b.Weight}
	}
	return data
}
