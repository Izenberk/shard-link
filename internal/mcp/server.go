package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/time/rate"
)

type MCPServer struct {
	vessel      storage.Repository
	mcp         *server.MCPServer
	apiKey      string
	embedder    storage.Embedder
	summarizer  storage.Summarizer
	wm          *WorkingMemory
}

func NewMCPServer(v storage.Repository, apiKey string, e storage.Embedder, sum storage.Summarizer) *MCPServer {
	log.Println("[MCP] Initializing Shard-Link Hub MCP Server...")

	// 1. Create the base MCP server
	s := server.NewMCPServer(
	"Shard-Link Hub",
		"v0.1.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	wm := NewWorkingMemory(30 * time.Minute)
	wm.StartCleanup(context.Background())

	mcpSrv := &MCPServer{
		vessel:     v,
		mcp:        s,
		apiKey:     apiKey,
		embedder:   e,
		summarizer: sum,
		wm:         wm,
	}

	// 2. Register tools, resources, and prompts BEFORE return
	log.Println("[MCP] Registering Capabilities...")
	mcpSrv.RegisterTools()
	mcpSrv.RegisterResources()
	mcpSrv.RegisterPrompts()

	log.Println("[MCP] Server Initialization Complete.")
	return mcpSrv
}

// Rate limiting — per API key, 60 req/min with burst of 10
var (
	rateLimiters   = make(map[string]*rate.Limiter)
	rateLimitersMu sync.Mutex
)

func getLimiter(key string) *rate.Limiter {
	rateLimitersMu.Lock()
	defer rateLimitersMu.Unlock()
	if lim, ok := rateLimiters[key]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Every(time.Minute/60), 10)
	rateLimiters[key] = lim
	return lim
}

func (s *MCPServer) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log all incoming requests to debug session/auth issues
		sessID := r.URL.Query().Get("sessionId")
		log.Printf("[MCP-HTTP] %s %s (SessID: %s) from %s", r.Method, r.URL.Path, sessID, r.RemoteAddr)

		// Skip auth if no key is configured (local dev only)
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("X-API-Key") != s.apiKey {
			log.Printf("[Auth] Denied %s %s (Key Missing/Invalid)", r.Method, r.URL.Path)
			http.Error(w, "Unauthorized: Invalid Shard Access Key", http.StatusUnauthorized)
			return
		}

		// Rate limiting — per API key
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.RemoteAddr
		}
		if !getLimiter(key).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *MCPServer) RegisterTools() {
	log.Println("[MCP] Registering Tool: search_memory")
	searchTool := mcp.NewTool("search_memory",
		mcp.WithDescription("Search long-term memory using semantic resonance (vector search)"),
		mcp.WithString("query_text", mcp.Description("Natural language query to embed")),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector (optional if query_text is provided)")),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
		mcp.WithNumber("lambda", mcp.Description("MMR diversity tuning (0.0=max diversity, 1.0=max relevance). Default: 0.7")),
		mcp.WithNumber("bias", mcp.Description("Cognitive bias strength (0.0=pure centroid, 1.0=pure query). Default from COGNITIVE_BIAS_LAMBDA env or 0.7")),
	)
	s.mcp.AddTool(searchTool, s.handleSearch)

	log.Println("[MCP] Registering Tool: search_text")
	searchTextTool := mcp.NewTool("search_text",
		mcp.WithDescription("Search long-term memory using keyword matching (SQL LIKE)"),
		mcp.WithString("query", mcp.Description("The keyword or phrase to search for"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
	s.mcp.AddTool(searchTextTool, s.handleSearchText)

	log.Println("[MCP] Registering Tool: save_memory")
	saveTool := mcp.NewTool("save_memory",
		mcp.WithDescription("Save a new contextual fragment to long-term memory"),
		mcp.WithString("id", mcp.Description("Unique identifier"), mcp.Required()),
		mcp.WithString("content", mcp.Description("The text to remember"), mcp.Required()),
		mcp.WithString("category", mcp.Description("e.g. 'session', 'core', 'memory'"), mcp.Required()),
		mcp.WithString("vector", mcp.Description("Base64 encoded float32 vector (optional)")),
	)
	s.mcp.AddTool(saveTool, s.handleSave)

	log.Println("[MCP] Registering Tool: search_graph")
	graphTool := mcp.NewTool("search_graph",
		mcp.WithDescription("Search the Knowledge Mesh by finding a central context and traversing its semantic neighbors (Multi-Hop)"),
		mcp.WithString("query_text", mcp.Description("Natural language query to embed")),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector (optional)")),
		mcp.WithNumber("limit", mcp.Description("Max neighbors to return")),
	)
	s.mcp.AddTool(graphTool, s.handleSearchGraph)

	log.Println("[MCP] Registering Tool: search_all")
	searchAllTool := mcp.NewTool("search_all",
		mcp.WithDescription("Comprehensive search across all memory engines (Vector, Text, and Graph Mesh). Deduplicates results and provides relational context."),
		mcp.WithString("query", mcp.Description("The keyword or natural language topic to search for"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results per engine"), mcp.DefaultNumber(5)),
		mcp.WithNumber("bias", mcp.Description("Cognitive bias strength (0.0=pure centroid, 1.0=pure query). Default from COGNITIVE_BIAS_LAMBDA env or 0.7")),
	)
	s.mcp.AddTool(searchAllTool, s.handleSearchAll)
}

func (s *MCPServer) handleSearchAll(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("[MCP] Calling Tool: search_all (%v)", request.Params.Arguments)
	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	if query == "" {
		return mcp.NewToolResultError("Query must be provided"), nil
	}

	// 1. Parallel Execution: Vector, Text, and Graph
	// Note: We use the embedder if available for vector-based searches
	var queryVec []byte
	if s.embedder != nil {
		floats, err := s.embedder.Embed(ctx, query)
		if err == nil {
			// Working Memory: bias toward session centroid
			floats = s.biasVector(ctx, floats, request.GetFloat("bias", -1))
			queryVec = storage.EncodeVector(floats)
		}
	}

	// Parallel fan-out across all engines
	var (
		mu         sync.Mutex
		seenShards = make(map[string]storage.Shard)
		allBonds   []storage.ShardBond
		wg         sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		results, _ := s.vessel.FindText(ctx, query, limit, false)
		mu.Lock()
		for _, sh := range results {
			seenShards[sh.ID] = sh
		}
		mu.Unlock()
	}()

	if queryVec != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, _ := s.vessel.FindResonant(ctx, queryVec, limit, false)
			mu.Lock()
			for _, sh := range results {
				seenShards[sh.ID] = sh
			}
			mu.Unlock()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			gShards, gBonds, _ := s.vessel.SearchGraph(ctx, queryVec, limit, false)
			mu.Lock()
			for _, sh := range gShards {
				seenShards[sh.ID] = sh
			}
			allBonds = append(allBonds, gBonds...)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(seenShards) == 0 {
		return mcp.NewToolResultText("No matching memory shards found across any engine."), nil
	}

	// AHA! MOMENT: Unified Reinforcement Step
	// Now that we've deduplicated, touch all identified shards EXACTLY ONCE.
	shardIDs := make([]string, 0, len(seenShards))
	for id := range seenShards {
		shardIDs = append(shardIDs, id)
	}
	_ = s.vessel.ReinforceShards(ctx, shardIDs)

	// Working Memory: update session centroid with retrieved shards
	s.updateCentroid(ctx, seenShards)

	// C. Format Response
	var response string
	response = fmt.Sprintf("Found %d unique shards and %d relational bonds:\n---\n", len(seenShards), len(allBonds))
	
	for _, shard := range seenShards {
		response += fmt.Sprintf("[%s] (Score: %.2f): %s\n---\n", shard.ID, shard.Confidence, shard.Content)
	}

	if len(allBonds) > 0 {
		response += "\nRelational Bonds (Mesh Geometry):\n"
		for _, bond := range allBonds {
			response += fmt.Sprintf("- %s <-> %s (Strength: %.2f)\n", bond.FromID, bond.ToID, bond.Weight)
		}
	}

	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleSearchGraph(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("[MCP] Calling Tool: search_graph (%v)", request.Params.Arguments)
	vecStr := request.GetString("query_vector", "")
	text := request.GetString("query_text", "")
	limit := int(request.GetFloat("limit", 10))

	var queryVec []byte
	var err error

	if vecStr != "" {
		queryVec, err = base64.StdEncoding.DecodeString(vecStr)
		if err != nil {
			return mcp.NewToolResultError("Invalid vector encoding"), nil
		}
	} else if text != "" && s.embedder != nil {
		floats, err := s.embedder.Embed(ctx, text)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Embedding failed: %v", err)), nil
		}
		// Working Memory: bias toward session centroid
		floats = s.biasVector(ctx, floats, -1)
		queryVec = storage.EncodeVector(floats)
	} else {
		return mcp.NewToolResultError("Either query_vector or query_text must be provided"), nil
	}

	shards, bonds, err := s.vessel.SearchGraph(ctx, queryVec, limit, true)
	if err != nil {
		log.Printf("[MCP ERROR] search_graph failed: %v", err)
		return nil, err
	}

	// Working Memory: update session centroid with retrieved shards
	s.updateCentroidSlice(ctx, shards)

	var response string
	if len(shards) == 0 {
		response = "No connected neighbors found for this context."
	} else {
		response = fmt.Sprintf("Found %d shards and %d bonds:\n---\n", len(shards), len(bonds))
		for _, shard := range shards {
			response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
		}
	}
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) RegisterResources() {
	log.Println("[MCP] Registering Resource: shard-link://core")
	res := mcp.NewResource("shard-link://core",
		"Core Identity",
		mcp.WithResourceDescription("Read-only access to user profile and system anchors"),
	)
	s.mcp.AddResource(res, s.handleReadCore)
}

func (s *MCPServer) handleReadCore(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	log.Printf("[MCP] Handling Resource Read: %s", request.Params.URI)
	shards, err := s.vessel.GetCoreShards(ctx)
	if err != nil {
		log.Printf("[MCP ERROR] GetCoreShards failed: %v", err)
		return nil, err
	}

	var body string
	for _, shard := range shards {
		body += fmt.Sprintf("[%s]\n%s\n---\n", shard.ID, shard.Content)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			Text:     body,
			MIMEType: "text/plain",
		},
	}, nil
}

func (s *MCPServer) handleSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("[MCP] Calling Tool: search_memory (%v)", request.Params.Arguments)
	vecStr := request.GetString("query_vector", "")
	text := request.GetString("query_text", "")
	limit := int(request.GetFloat("limit", 5))

	var queryVec []byte
	var err error

	if vecStr != "" {
		queryVec, err = base64.StdEncoding.DecodeString(vecStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid vector encoding: %v", err)), nil
		}
	} else if text != "" && s.embedder != nil {
		floats, err := s.embedder.Embed(ctx, text)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Embedding failed: %v", err)), nil
		}
		// Working Memory: bias toward session centroid
		floats = s.biasVector(ctx, floats, request.GetFloat("bias", -1))
		queryVec = storage.EncodeVector(floats)
	} else {
		return mcp.NewToolResultError("Either query_vector or query_text must be provided"), nil
	}

	results, err := s.vessel.FindResonant(ctx, queryVec, limit, true)
	if err != nil {
		log.Printf("[MCP ERROR] search_memory failed: %v", err)
		return nil, err
	}

	// MMR diversity re-ranking
	lambda := request.GetFloat("lambda", 0.7)
	if envL := os.Getenv("MMR_LAMBDA"); lambda == 0.7 && envL != "" {
		if parsed, err := strconv.ParseFloat(envL, 64); err == nil {
			lambda = parsed
		}
	}
	results = storage.MaximalMarginalRelevance(queryVec, results, limit, lambda)

	// Working Memory: update session centroid with retrieved shards
	s.updateCentroidSlice(ctx, results)

	var response string
	for _, shard := range results {
		response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
	}

	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleSearchText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("[MCP] Calling Tool: search_text (%v)", request.Params.Arguments)
	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	results, err := s.vessel.FindText(ctx, query, limit, true)
	if err != nil {
		log.Printf("[MCP ERROR] search_text failed: %v", err)
		return nil, err
	}

	// Working Memory: update session centroid with text results (if they carry vectors)
	s.updateCentroidSlice(ctx, results)

	var response string
	if len(results) == 0 {
		response = "No matching memory shards found."
	} else {
		for _, shard := range results {
			response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
		}
	}

	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) RegisterPrompts() {
	log.Println("[MCP] Registering Prompt: hub_search")
	shardPrompt := mcp.NewPrompt("hub_search",
		mcp.WithPromptDescription("Global search across Shard-Link memory (Neo4j Mesh). Uses the 'search_all' meta-tool for maximum context."),
		mcp.WithArgument("query", mcp.ArgumentDescription("Keyword or topic to search for"), mcp.RequiredArgument()),
	)
	s.mcp.AddPrompt(shardPrompt, s.handleShardPrompt)
}

func (s *MCPServer) handleShardPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	query := request.Params.Arguments["query"]
	log.Printf("[MCP] Handling Prompt Request: hub_search (query: %s)", query)
	
	message := mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
		fmt.Sprintf("Please use the 'search_all' tool to find all relevant shards and graph relationships for the topic: '%s'. Provide a high-signal summary of what you find.", query),
	))

	return mcp.NewGetPromptResult("Search Results", []mcp.PromptMessage{message}), nil
}

// StartHub ignites the multi-protocol MCP bridge.
// Primary transport: Streamable HTTP on /mcp (MCP spec 2024-11-05).
// Legacy transport:  Standard SSE on /sse + /message (kept for backward compat).
func (s *MCPServer) StartHub(port int, baseURL string) error {
	log.Printf("[MCP] Starting Hub on :%d | Streamable-HTTP → /mcp | SSE → /sse (baseURL: %s)", port, baseURL)

	// 1. Primary: Streamable HTTP transport (MCP spec 2024-11-05)
	streamableSrv := server.NewStreamableHTTPServer(s.mcp,
		server.WithHeartbeatInterval(10*time.Second),
	)

	// 2. Legacy: Standard SSE transport (backward compat)
	sseSrv := server.NewSSEServer(s.mcp,
		server.WithBaseURL(baseURL),
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)

	mux := http.NewServeMux()

	// Primary route — Streamable HTTP
	mux.Handle("/mcp", s.withAuth(streamableSrv))

	// Legacy routes — Standard SSE
	mux.Handle("/sse", s.withAuth(sseSrv.SSEHandler()))
	mux.Handle("/message", s.withAuth(sseSrv.MessageHandler()))

	log.Printf("[MCP] Hub ignited on :%d — Primary: Streamable HTTP (/mcp) | Legacy: SSE (/sse)\n", port)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

// --- Working Memory Helpers ---

// sessionID extracts the MCP session ID from context, falling back to "unknown".
func sessionID(ctx context.Context) string {
	if sess := server.ClientSessionFromContext(ctx); sess != nil {
		return sess.SessionID()
	}
	return "unknown"
}

// biasVector applies cognitive biasing to a query vector using the session centroid.
// biasParam < 0 means "use default from env or 0.7".
func (s *MCPServer) biasVector(ctx context.Context, floats []float32, biasParam float64) []float32 {
	lambda := biasParam
	if lambda < 0 {
		lambda = 0.7
		if envB := os.Getenv("COGNITIVE_BIAS_LAMBDA"); envB != "" {
			if parsed, err := strconv.ParseFloat(envB, 64); err == nil {
				lambda = parsed
			}
		}
	}
	return s.wm.Bias(sessionID(ctx), floats, lambda)
}

// updateCentroid updates the working memory centroid from a deduplicated shard map.
func (s *MCPServer) updateCentroid(ctx context.Context, shardMap map[string]storage.Shard) {
	shards := make([]storage.Shard, 0, len(shardMap))
	for _, sh := range shardMap {
		shards = append(shards, sh)
	}
	s.wm.Update(sessionID(ctx), shards)
}

// updateCentroidSlice updates the working memory centroid from a shard slice.
func (s *MCPServer) updateCentroidSlice(ctx context.Context, shards []storage.Shard) {
	s.wm.Update(sessionID(ctx), shards)
}

func (s *MCPServer) handleSave(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	log.Printf("[MCP] Calling Tool: save_memory (%v)", request.Params.Arguments)
	id := request.GetString("id", "")
	content := request.GetString("content", "")
	category := request.GetString("category", "memory")
	vecStr := request.GetString("vector", "")

	var vec []byte
	var err error

	if vecStr != "" {
		vec, err = base64.StdEncoding.DecodeString(vecStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid vector encoding: %v", err)), nil
		}
	} else if content != "" && s.embedder != nil {
		floats, err := s.embedder.Embed(ctx, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Embedding failed: %v", err)), nil
		}
		vec = storage.EncodeVector(floats)
	} else {
		return mcp.NewToolResultError("Vector must be provided if the server cannot generate it"), nil
	}

	// Salience Scoring: Ask the LLM to rate importance [0.1, 1.0]
	salience := 0.5 // Safe default for mock mode or parse failures
	if s.summarizer != nil && content != "" {
		saliencePrompt := fmt.Sprintf(
			"Rate the long-term importance of this memory fragment on a scale from 0.1 (trivial/ephemeral) to 1.0 (critical/identity-defining). "+
				"Respond with ONLY a decimal number, nothing else.\n\nFragment: %s", content)
		if resp, err := s.summarizer.Summarize(ctx, saliencePrompt); err == nil {
			resp = strings.TrimSpace(resp)
			if parsed, err := strconv.ParseFloat(resp, 64); err == nil && parsed >= 0.1 && parsed <= 1.0 {
				salience = parsed
			}
		}
	}
	log.Printf("[MCP] Salience scored: %.2f for shard %s", salience, id)

	shard := storage.Shard{
		ID:        id,
		Content:   content,
		Category:  category,
		Vector:    vec,
		Salience:  salience,
		CreatedAt: time.Now(),
	}

	if err := s.vessel.SaveShard(ctx, shard); err != nil {
		log.Printf("[MCP ERROR] save_memory failed: %v", err)
		return nil, err
	}

	// Episodic Session Chain: Link shard to its MCP session Episode node
	sessID := sessionID(ctx)
	if sessID != "unknown" {
		if vg, ok := s.vessel.(*storage.VesselGraph); ok {
			episodeQuery := `
			MERGE (e:Episode {session_id: $sessionID})
			ON CREATE SET e.started_at = datetime(), e.shard_count = 1
			ON MATCH SET e.shard_count = e.shard_count + 1, e.last_active = datetime()
			WITH e
			MATCH (sh:Shard {id: $shardID})
			MERGE (sh)-[:EPISODE_OF]->(e)
			`
			_, _ = vg.ExecuteQuery(ctx, episodeQuery, map[string]any{
				"sessionID": sessID, "shardID": id,
			})
			log.Printf("[MCP] Shard %s linked to episode %s", id, sessID)
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory saved: %s", id)), nil
}
