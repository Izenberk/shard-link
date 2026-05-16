package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/joho/godotenv"
)

type VizNode struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Content   string  `json:"content"`
	Community int64   `json:"community"`
	PageRank  float64 `json:"pagerank"`
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
	http.HandleFunc("/api/graph", srv.handleGetGraph)
	http.HandleFunc("/api/search", srv.handleSearch)

	log.Printf("Visual Ego Live Dashboard ignited on :8081\n")
	log.Fatal(http.ListenAndServe(":8081", nil))
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
	for i, s := range shards {
		data.Nodes[i] = VizNode{ID: s.ID, Category: s.Category, Content: s.Content, Community: s.CommunityID, CreatedAt: s.CreatedAt.Format("2006-01-02 15:04:05")}
	}
	for i, b := range bonds {
		data.Links[i] = VizLink{Source: b.FromID, Target: b.ToID, Weight: b.Weight}
	}
	return data
}
