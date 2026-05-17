package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/joho/godotenv"
)

type VizNode struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Content   string  `json:"content"`
	Community int64   `json:"community"`
	PageRank  float64 `json:"pagerank"`
	Survival  float64 `json:"survival"`
	CreatedAt string  `json:"created_at"`
}

type VizLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

type VizData struct {
	Nodes []VizNode `json:"nodes"`
	Links []VizLink `json:"links"`
}

type Server struct {
	vessel   *storage.VesselGraph
	embedder storage.Embedder
}

func main() {
	_ = godotenv.Load()
	ctx := context.Background()

	v, err := storage.NewVesselGraph(os.Getenv("NEO4J_URL"), os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASS"), "neo4j")
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = "gemini-embedding-001"
	}
	emb, err := storage.NewGeminiEmbedder(ctx, os.Getenv("GEMINI_API_KEY"), model)
	if err != nil {
		log.Printf("Warning: Failed to init embedder: %v. Search will be limited.", err)
	}

	srv := &Server{
		vessel:   v,
		embedder: emb,
	}

	// 1. Background Analysis (Phase 10: Standalone Intelligence)
	go func() {
		log.Println("[Background] Starting initial mesh analysis...")
		srv.vessel.CalculateCommunities(ctx)
		log.Println("[Background] Mesh analysis complete.")
	}()

	http.Handle("/", http.FileServer(http.Dir("web/static")))
	// 2. API Endpoints
	http.HandleFunc("/api/graph", srv.handleGetGraph)
	http.HandleFunc("/api/search", srv.handleSearch)
	http.HandleFunc("/api/bonds", srv.handleBonds)

	log.Printf("Visual Ego Live Dashboard ignited on :8081\n")
	log.Fatal(http.ListenAndServe(":8081", nil))
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
	floats, err := s.embedder.Embed(r.Context(), query)
	if err != nil {
		log.Printf("[API ERROR] Embedding failed for query '%s': %v", query, err)
		http.Error(w, fmt.Sprintf("Embedding failed: %v", err), 500)
		return
	}

	shards, bonds, err := s.vessel.SearchGraph(r.Context(), storage.EncodeVector(floats), 20)
	if err != nil {
		log.Printf("[API ERROR] SearchGraph failed: %v", err)
		http.Error(w, "Search failed", 500)
		return
	}

	log.Printf("[API] Found %d shards and %d bonds for query: %s", len(shards), len(bonds), query)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.packData(shards, bonds))
}

func (s *Server) packData(shards []storage.Shard, bonds []storage.ShardBond) VizData {
	data := VizData{
		Nodes: make([]VizNode, len(shards)),
		Links: make([]VizLink, len(bonds)),
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

		hoursSince := time.Since(lastUsed).Hours()
		if hoursSince < 1 {
			hoursSince = 1
		}

		// Frequency-Weighted Retention (Long-Term Potentiation)
		// More hits = slower decay. Each hit adds "Decay Resistance".
		// We use log2(hits + 1) to provide diminishing returns for extreme popularity.
		vitality := 1.0
		if s.UseCount > 1 {
			// Using 1.0 + math.Log2(float64(s.UseCount)) as a multiplier
			vitality = 1.0 + (float64(s.UseCount) * 0.1) // Simpler linear boost for now: +10% per hit
			if vitality > 5.0 {
				vitality = 5.0 // Cap vitality boost at 5x
			}
		}

		links := float64(bondCounts[s.ID])
		// Score = (Density * Centrality * Vitality * 10) / TimeFactor
		rawScore := (links * (s.PageRank + 1.0) * 10 * vitality) / hoursSince
		
		// Normalize to 0-100 scale for UI consistency
		survival := rawScore
		if survival > 95 {
			survival = 95 // Cap non-core at 95 to keep Core as the absolute ceiling
		}

		if s.Category == "core" {
			survival = 100 // Core shards are the "Gold Standard" (100)
		}

		data.Nodes[i] = VizNode{
			ID:        s.ID,
			Category:  s.Category,
			Content:   s.Content,
			Community: s.CommunityID,
			PageRank:  s.PageRank,
			Survival:  survival,
			CreatedAt: s.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	for i, b := range bonds {
		data.Links[i] = VizLink{Source: b.FromID, Target: b.ToID, Weight: b.Weight}
	}
	return data
}
