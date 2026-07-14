package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/izenberk/shard-link/internal/metrics"
	"github.com/izenberk/shard-link/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/time/rate"
)

// validRedirectHosts restricts OAuth redirect_uri to trusted origins only.
// Without this, an attacker could steal auth codes via redirect_uri=https://evil.com.
var validRedirectHosts = map[string]bool{
	"claude.ai": true,
}

type MCPServer struct {
	vessel     storage.Repository
	pgVessel   *storage.PostgresVessel // Archival engine — pinged independently for health checks
	mcp        *server.MCPServer
	apiKey     string
	embedder   storage.Embedder
	summarizer storage.Summarizer
	wm         *WorkingMemory
	ledger     *storage.Vessel // Activity Ledger (SQLite) for persistent logging
	httpServer *http.Server

	// OAuth confidential client credentials — validated at /authorize and /token
	oauthClientID     string
	oauthClientSecret string
}

func NewMCPServer(v storage.Repository, apiKey string, oauthClientID, oauthClientSecret string, e storage.Embedder, sum storage.Summarizer, ledger *storage.Vessel, pg *storage.PostgresVessel, ctx context.Context) *MCPServer {
	slog.Info("initializing MCP server")

	// 1. Create the base MCP server
	s := server.NewMCPServer(
		"Shard-Link Hub",
		"v0.1.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	wm := NewWorkingMemory(30 * time.Minute)
	wm.StartCleanup(ctx)

	mcpSrv := &MCPServer{
		vessel:            v,
		pgVessel:          pg,
		mcp:               s,
		apiKey:            apiKey,
		oauthClientID:     oauthClientID,
		oauthClientSecret: oauthClientSecret,
		embedder:          e,
		summarizer:        sum,
		wm:                wm,
		ledger:            ledger,
	}

	// 2. Register tools, resources, and prompts BEFORE return
	slog.Info("registering MCP capabilities")
	mcpSrv.RegisterTools()
	mcpSrv.RegisterResources()
	mcpSrv.RegisterPrompts()

	slog.Info("MCP server initialization complete")
	return mcpSrv
}

// limiterPool is a TTL-swept map of rate limiters. Without sweeping, one
// entry accumulates per bearer token / client IP forever (every OAuth grant
// issues a fresh JWT → a fresh map key) — a slow, unbounded memory leak.
// sweep() is called from the StartHub cleanup ticker.
type limiterPool struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	factory func() *rate.Limiter
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newLimiterPool(factory func() *rate.Limiter) *limiterPool {
	return &limiterPool{entries: make(map[string]*limiterEntry), factory: factory}
}

func (p *limiterPool) get(key string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[key]; ok {
		e.lastSeen = time.Now()
		return e.lim
	}
	e := &limiterEntry{lim: p.factory(), lastSeen: time.Now()}
	p.entries[key] = e
	return e.lim
}

func (p *limiterPool) sweep(olderThan time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	removed := 0
	for k, e := range p.entries {
		if now.Sub(e.lastSeen) > olderThan {
			delete(p.entries, k)
			removed++
		}
	}
	return removed
}

// Rate limiting — per API key / bearer token, 60 req/min with burst of 10
var rateLimiters = newLimiterPool(func() *rate.Limiter {
	return rate.NewLimiter(rate.Every(time.Minute/60), 10)
})

// OAuth-specific rate limiting — per client IP, 5 req/sec with burst of 3.
// Separate from the MCP data-path limiter because OAuth endpoints are
// unauthenticated (they establish auth), so they need tighter controls.
var oauthLimiters = newLimiterPool(func() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(5), 3)
})

// clientIP returns the caller's IP for rate-limiting purposes. Behind the
// Cloudflare Tunnel every request arrives from the tunnel container, so
// r.RemoteAddr would collapse all external traffic into a single bucket
// (one abuser exhausts the limit for every legitimate client). Cloudflare
// sets CF-Connecting-IP to the real client address. Trade-off: a direct
// LAN caller can spoof the header, but that only buys them a fresh rate
// bucket — authentication is unaffected.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// setSecurityHeaders adds standard security headers to all OAuth responses.
// - no-store: prevents proxies/browsers from caching tokens or codes
// - nosniff: prevents MIME type confusion attacks
// - DENY: prevents the OAuth pages from being framed (clickjacking)
// - HSTS: enforces HTTPS for all future requests (Cloudflare Tunnel is TLS-only)
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000")
}

// maxPendingCodes caps the number of outstanding authorization codes.
// Beyond this limit, /authorize returns 503 to prevent memory exhaustion DoS.
const maxPendingCodes = 100

func (s *MCPServer) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessID := r.URL.Query().Get("sessionId")
		slog.Debug("HTTP request", "method", r.Method, "path", r.URL.Path, "session_id", sessID, "remote", r.RemoteAddr)

		// Skip auth if no key is configured (local dev only)
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Accept X-API-Key (Claude Code CLI) or Authorization: Bearer (Claude.ai connectors)
		key := r.Header.Get("X-API-Key")
		if key == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				key = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		// Check direct API key match first (Claude Code CLI path).
		// Constant-time — string == short-circuits on the first differing
		// byte, leaking prefix-match length (same reasoning as /token).
		authorized := subtle.ConstantTimeCompare([]byte(key), []byte(s.apiKey)) == 1

		// If not a direct API key match, validate as JWT (OAuth path)
		if !authorized && key != "" {
			authorized = s.validateJWT(key)
		}

		if !authorized {
			slog.Warn("auth denied", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid Shard Access Key", http.StatusUnauthorized)
			return
		}

		// Rate limiting — per key identity
		limiterKey := key
		if limiterKey == "" {
			limiterKey = r.RemoteAddr
		}
		if !rateLimiters.get(limiterKey).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *MCPServer) RegisterTools() {
	slog.Debug("registering tool", "name", "search_memory")
	searchTool := mcp.NewTool("search_memory",
		mcp.WithDescription("Search long-term memory using semantic resonance (vector search)"),
		mcp.WithString("query_text", mcp.Description("Natural language query to embed")),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector (optional if query_text is provided)")),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
		mcp.WithNumber("lambda", mcp.Description("MMR diversity tuning (0.0=max diversity, 1.0=max relevance). Default: 0.7")),
		mcp.WithNumber("bias", mcp.Description("Cognitive bias strength (0.0=pure centroid, 1.0=pure query). Default from COGNITIVE_BIAS_LAMBDA env or 0.7")),
	)
	s.mcp.AddTool(searchTool, s.handleSearch)

	slog.Debug("registering tool", "name", "search_text")
	searchTextTool := mcp.NewTool("search_text",
		mcp.WithDescription("Search long-term memory using keyword matching (SQL LIKE)"),
		mcp.WithString("query", mcp.Description("The keyword or phrase to search for"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
	s.mcp.AddTool(searchTextTool, s.handleSearchText)

	slog.Debug("registering tool", "name", "save_memory")
	saveTool := mcp.NewTool("save_memory",
		mcp.WithDescription("Save a new contextual fragment to long-term memory"),
		mcp.WithString("id", mcp.Description("Unique identifier"), mcp.Required()),
		mcp.WithString("content", mcp.Description("The text to remember"), mcp.Required()),
		mcp.WithString("category", mcp.Description("e.g. 'session', 'memory'. Use 'core' only with allow_core=true"), mcp.Required()),
		mcp.WithBoolean("allow_core", mcp.Description("Must be true to save category='core' shards — permanently immune to eviction")),
		mcp.WithString("vector", mcp.Description("Base64 encoded float32 vector (optional)")),
	)
	s.mcp.AddTool(saveTool, s.handleSave)

	slog.Debug("registering tool", "name", "search_graph")
	graphTool := mcp.NewTool("search_graph",
		mcp.WithDescription("Search the Knowledge Mesh by finding a central context and traversing its semantic neighbors (Multi-Hop)"),
		mcp.WithString("query_text", mcp.Description("Natural language query to embed")),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector (optional)")),
		mcp.WithNumber("limit", mcp.Description("Max neighbors to return")),
	)
	s.mcp.AddTool(graphTool, s.handleSearchGraph)

	slog.Debug("registering tool", "name", "search_all")
	searchAllTool := mcp.NewTool("search_all",
		mcp.WithDescription("Comprehensive search across all memory engines (Vector, Text, and Graph Mesh). Deduplicates results and provides relational context."),
		mcp.WithString("query", mcp.Description("The keyword or natural language topic to search for"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results per engine"), mcp.DefaultNumber(5)),
		mcp.WithNumber("bias", mcp.Description("Cognitive bias strength (0.0=pure centroid, 1.0=pure query). Default from COGNITIVE_BIAS_LAMBDA env or 0.7")),
		mcp.WithString("category", mcp.Description("Optional category filter (e.g. 'contract', 'memory', 'core'). When set, only shards matching this category are returned.")),
	)
	s.mcp.AddTool(searchAllTool, s.handleSearchAll)

	slog.Debug("registering tool", "name", "get_status")
	statusTool := mcp.NewTool("get_status",
		mcp.WithDescription("Returns mesh statistics and service health for Shard-Link hub"),
	)
	s.mcp.AddTool(statusTool, s.handleGetStatus)

	slog.Debug("registering tool", "name", "get_shard")
	getShardTool := mcp.NewTool("get_shard",
		mcp.WithDescription("Fetch a single memory shard by its exact ID"),
		mcp.WithString("id", mcp.Description("The exact shard ID to retrieve"), mcp.Required()),
	)
	s.mcp.AddTool(getShardTool, s.handleGetShard)

	slog.Debug("registering tool", "name", "get_core_shards")
	getCoreShardsTool := mcp.NewTool("get_core_shards",
		mcp.WithDescription("Fetch all core identity shards"),
	)
	s.mcp.AddTool(getCoreShardsTool, s.handleGetCoreShards)

	// --- Observation tools (metadata-only, no touch) ---

	slog.Debug("registering tool", "name", "get_recent_shards")
	recentTool := mcp.NewTool("get_recent_shards",
		mcp.WithDescription("List shards by most recently updated. Returns metadata only (id, category, survival_score, timestamps) — use get_shard(id) for full content. Does NOT count as retrieval (no survival score impact)."),
		mcp.WithNumber("limit", mcp.Description("Max shards to return (default 10, max 100)")),
		mcp.WithString("category", mcp.Description("Optional category filter")),
	)
	s.mcp.AddTool(recentTool, s.handleGetRecentShards)

	slog.Debug("registering tool", "name", "get_shards_by_category")
	byCategoryTool := mcp.NewTool("get_shards_by_category",
		mcp.WithDescription("List all shards in a specific category. Returns metadata only (id, category, survival_score, timestamps) — use get_shard(id) for full content. Does NOT count as retrieval (no survival score impact)."),
		mcp.WithString("category", mcp.Description("Category to filter (e.g. core, memory, session, contract)"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max shards to return (default 50, max 100)")),
	)
	s.mcp.AddTool(byCategoryTool, s.handleGetShardsByCategory)

	slog.Debug("registering tool", "name", "get_at_risk_shards")
	atRiskTool := mcp.NewTool("get_at_risk_shards",
		mcp.WithDescription("Inspect shards with lowest survival scores (eviction candidates). Returns metadata only (id, category, survival_score, last_used) — use get_shard(id) for full content. Does NOT count as retrieval (no survival score impact). Core shards excluded."),
		mcp.WithNumber("limit", mcp.Description("Max shards to return (default 10, max 100)")),
		mcp.WithNumber("threshold", mcp.Description("Only shards with survival below this value (default 30, range 0-100)")),
	)
	s.mcp.AddTool(atRiskTool, s.handleGetAtRiskShards)

	// --- CRUD tools ---

	slog.Debug("registering tool", "name", "update_shard")
	updateTool := mcp.NewTool("update_shard",
		mcp.WithDescription("Update a shard's category and/or content. If content changes, the vector is re-embedded automatically. Blocked for comm-summary-* shards. Core shards require confirm_core=true."),
		mcp.WithString("id", mcp.Description("Exact shard ID to update"), mcp.Required()),
		mcp.WithString("content", mcp.Description("New content (triggers re-embedding if changed)")),
		mcp.WithString("category", mcp.Description("New category (must be in allowlist)")),
		mcp.WithBoolean("confirm_core", mcp.Description("Must be true to mutate a core-category shard")),
	)
	s.mcp.AddTool(updateTool, s.handleUpdateShard)

	slog.Debug("registering tool", "name", "delete_shard")
	deleteTool := mcp.NewTool("delete_shard",
		mcp.WithDescription("Permanently delete a shard and all its relationships. Blocked for comm-summary-* shards. Core shards require confirm_core=true."),
		mcp.WithString("id", mcp.Description("Exact shard ID to delete"), mcp.Required()),
		mcp.WithBoolean("confirm_core", mcp.Description("Must be true to delete a core-category shard")),
	)
	s.mcp.AddTool(deleteTool, s.handleDeleteShard)
}

func (s *MCPServer) handleSearchAll(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	metrics.SearchAllTotal.Add(1)

	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	slog.Info("search_all called", "query_len", len(query), "limit", limit)

	// 2.3 — Query length + limit validation
	if len(query) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Query exceeds max length of %d characters", maxQueryLen)), nil
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}

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

	// Parallel fan-out across all engines. Each goroutine writes only its own
	// slice — no shared map, no mutex needed.
	var (
		textResults, vecResults, graphShards []storage.Shard
		allBonds                             []storage.ShardBond
		wg                                   sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		textResults, _ = s.vessel.FindText(ctx, query, limit, false)
	}()

	if queryVec != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vecResults, _ = s.vessel.FindResonant(ctx, queryVec, limit, false)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			graphShards, allBonds, _ = s.vessel.SearchGraph(ctx, queryVec, limit, false)
		}()
	}

	wg.Wait()

	// Fuse the three ranked lists with Reciprocal Rank Fusion. This both
	// deduplicates by ID and produces a deterministic, relevance-ordered
	// result — iterating a map here would randomize the order on every call,
	// discarding the ranking each engine computed.
	fused := storage.ReciprocalRankFusion(3*limit, 60.0, vecResults, textResults, graphShards)

	// Post-filter by category when the caller specifies one.
	// Applied after fusion but before reinforcement — filtered shards don't get touched.
	if cat := request.GetString("category", ""); cat != "" {
		filtered := fused[:0]
		for _, sh := range fused {
			if sh.Category == cat {
				filtered = append(filtered, sh)
			}
		}
		fused = filtered
	}

	if len(fused) == 0 {
		return mcp.NewToolResultText("No matching memory shards found across any engine."), nil
	}

	// AHA! MOMENT: Unified Reinforcement Step
	// Now that we've deduplicated, touch all identified shards EXACTLY ONCE.
	shardIDs := make([]string, len(fused))
	for i, sh := range fused {
		shardIDs[i] = sh.ID
	}
	_ = s.vessel.ReinforceShards(ctx, shardIDs)

	// Working Memory: update session centroid with retrieved shards
	s.updateCentroidSlice(ctx, fused)

	// Only report bonds whose endpoints both survived fusion + filtering —
	// dangling bond references would point the LLM at shards it can't see.
	included := make(map[string]bool, len(fused))
	for _, sh := range fused {
		included[sh.ID] = true
	}
	bonds := allBonds[:0]
	for _, b := range allBonds {
		if included[b.FromID] && included[b.ToID] {
			bonds = append(bonds, b)
		}
	}

	// C. Format Response — ordered by fused relevance, most relevant first
	response := fmt.Sprintf("Found %d unique shards and %d relational bonds (ordered by relevance):\n---\n", len(fused), len(bonds))

	for _, shard := range fused {
		response += fmt.Sprintf("[%s] (%s): %s\n---\n", shard.ID, shard.Category, shard.Content)
	}

	if len(bonds) > 0 {
		response += "\nRelational Bonds (Mesh Geometry):\n"
		for _, bond := range bonds {
			response += fmt.Sprintf("- %s <-> %s (Strength: %.2f)\n", bond.FromID, bond.ToID, bond.Weight)
		}
	}

	metrics.SearchAllLatency.Observe(time.Since(start))
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleGetStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slog.Info("get_status called")

	// --- Mesh Stats ---
	shardCount, _ := s.vessel.GetCount(ctx)
	bondCount, _ := s.vessel.GetBondCount(ctx)
	communityCount, _ := s.vessel.GetCommunityCount(ctx)

	// --- Service Health ---
	// Hub: always online (if you're reading this response, the hub is alive)
	hubStatus := "online"

	// Neo4j: type-assert to VesselGraph and ping
	neo4jStatus := "offline"
	if vg, ok := s.vessel.(*storage.VesselGraph); ok {
		if err := vg.Ping(ctx); err == nil {
			neo4jStatus = "online"
		}
	}

	// Postgres: ping the archival vessel if wired
	pgStatus := "offline"
	if s.pgVessel != nil {
		if err := s.pgVessel.Ping(ctx); err == nil {
			pgStatus = "online"
		}
	}

	// --- Survival Distribution ---
	survival, survErr := s.vessel.GetSurvivalDistribution(ctx)
	if survErr != nil {
		slog.Warn("survival distribution failed", "error", survErr)
		survival = map[string]int{"24h": 0, "7d": 0, "30d": 0, "90d": 0, "older": 0}
	}

	status := map[string]any{
		"mesh": map[string]int{
			"shards":      shardCount,
			"bonds":       bondCount,
			"communities": communityCount,
		},
		"services": map[string]string{
			"hub":      hubStatus,
			"neo4j":    neo4jStatus,
			"postgres": pgStatus,
		},
		"survival": survival,
	}

	payload, err := json.Marshal(status)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal status: %v", err)), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
}

// shardResponse is the JSON shape the CLI expects from get_shard and get_core_shards.
type shardResponse struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toShardResponse(sh storage.Shard) shardResponse {
	return shardResponse{
		ID:        sh.ID,
		Content:   sh.Content,
		Category:  sh.Category,
		CreatedAt: sh.CreatedAt.Format(time.RFC3339),
		UpdatedAt: sh.LastUsed.Format(time.RFC3339),
	}
}

func (s *MCPServer) handleGetShard(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	slog.Info("get_shard called", "id", id)

	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	shard, err := s.vessel.GetShardByID(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("shard not found: %s", id)), nil
	}

	payload, err := json.Marshal(toShardResponse(shard))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal shard: %v", err)), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
}

func (s *MCPServer) handleGetCoreShards(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slog.Info("get_core_shards called")

	shards, err := s.vessel.GetCoreShards(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to fetch core shards: %v", err)), nil
	}

	responses := make([]shardResponse, len(shards))
	for i, sh := range shards {
		responses[i] = toShardResponse(sh)
	}

	payload, err := json.Marshal(responses)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal core shards: %v", err)), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
}

// --- Observation Handlers (metadata-only, no touch) ---

// metadataResponse is the JSON shape for observation tools.
// Deliberately excludes Content and Vector to prevent search bypass.
type metadataResponse struct {
	ID            string  `json:"id"`
	Category      string  `json:"category"`
	SurvivalScore float64 `json:"survival_score"`
	CreatedAt     string  `json:"created_at"`
	LastUsed      string  `json:"last_used"`
}

func toMetadataResponse(sm storage.ShardMetadata) metadataResponse {
	return metadataResponse{
		ID:            sm.ID,
		Category:      sm.Category,
		SurvivalScore: sm.SurvivalScore,
		CreatedAt:     sm.CreatedAt.Format(time.RFC3339),
		LastUsed:      sm.LastUsed.Format(time.RFC3339),
	}
}

func (s *MCPServer) handleGetRecentShards(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := int(request.GetFloat("limit", 10))
	category := request.GetString("category", "")

	slog.Info("get_recent_shards called", "limit", limit, "category", category)

	if limit > maxResultLimit {
		limit = maxResultLimit
	}

	shards, err := s.vessel.GetRecentShards(ctx, limit, category)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to fetch recent shards: %v", err)), nil
	}

	if len(shards) == 0 {
		return mcp.NewToolResultText("No shards found."), nil
	}

	responses := make([]metadataResponse, len(shards))
	for i, sm := range shards {
		responses[i] = toMetadataResponse(sm)
	}

	payload, err := json.Marshal(responses)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(payload)), nil
}

func (s *MCPServer) handleGetShardsByCategory(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	category := request.GetString("category", "")
	limit := int(request.GetFloat("limit", 50))

	slog.Info("get_shards_by_category called", "category", category, "limit", limit)

	if category == "" {
		return mcp.NewToolResultError("category is required"), nil
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}

	shards, err := s.vessel.GetShardsByCategory(ctx, category, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to fetch shards by category: %v", err)), nil
	}

	if len(shards) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No shards found in category %q.", category)), nil
	}

	responses := make([]metadataResponse, len(shards))
	for i, sm := range shards {
		responses[i] = toMetadataResponse(sm)
	}

	payload, err := json.Marshal(responses)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(payload)), nil
}

func (s *MCPServer) handleGetAtRiskShards(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := int(request.GetFloat("limit", 10))
	threshold := request.GetFloat("threshold", 30)

	slog.Info("get_at_risk_shards called", "limit", limit, "threshold", threshold)

	if limit > maxResultLimit {
		limit = maxResultLimit
	}

	shards, err := s.vessel.GetAtRiskShards(ctx, limit, threshold)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to fetch at-risk shards: %v", err)), nil
	}

	if len(shards) == 0 {
		return mcp.NewToolResultText("No at-risk shards found below the threshold."), nil
	}

	responses := make([]metadataResponse, len(shards))
	for i, sm := range shards {
		responses[i] = toMetadataResponse(sm)
	}

	payload, err := json.Marshal(responses)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(payload)), nil
}

// --- CRUD Handlers ---

func (s *MCPServer) handleUpdateShard(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	content := request.GetString("content", "")
	category := request.GetString("category", "")
	confirmCore := request.GetBool("confirm_core", false)

	slog.Info("update_shard called", "id", id, "has_content", content != "", "category", category, "confirm_core", confirmCore)

	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	if content == "" && category == "" {
		return mcp.NewToolResultError("at least one of content or category must be provided"), nil
	}

	// Block comm-summary-* shards (system-managed by Synthesizer)
	if strings.HasPrefix(id, "comm-summary-") {
		return mcp.NewToolResultError("comm-summary-* shards are system-managed and cannot be updated via MCP"), nil
	}

	// Category allowlist check
	if category != "" && !allowedCategories[category] {
		return mcp.NewToolResultError(fmt.Sprintf("category %q is not allowed. Allowed: core, memory, session, tech, arch, contract", category)), nil
	}

	// Core-category guard: fetch current shard to check its category
	existing, err := s.vessel.GetShardByID(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("shard not found: %s", id)), nil
	}
	if existing.Category == "core" && !confirmCore {
		payload, _ := json.Marshal(map[string]string{
			"status":           "confirm_required",
			"id":               id,
			"current_category": "core",
			"message":          "This is a core identity shard. Re-issue with confirm_core=true to proceed.",
		})
		return mcp.NewToolResultText(string(payload)), nil
	}

	// Build update struct
	update := storage.ShardUpdate{
		Content:     content,
		Category:    category,
		ConfirmCore: confirmCore,
	}

	// Re-embed if content changed
	if content != "" && s.embedder != nil {
		floats, err := s.embedder.Embed(ctx, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("re-embedding failed: %v", err)), nil
		}
		update.Vector = storage.EncodeVector(floats)
	}

	if err := s.vessel.UpdateShard(ctx, id, update); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("update failed: %v", err)), nil
	}

	// Activity Ledger
	if s.ledger != nil {
		msg := fmt.Sprintf("Updated shard [%s]", id)
		if content != "" {
			msg += " (content re-embedded)"
		}
		if category != "" {
			msg += fmt.Sprintf(" (category: %s → %s)", existing.Category, category)
		}
		logType := "update"
		if existing.Category == "core" && confirmCore {
			logType = "update_core_override"
		}
		_ = s.ledger.SaveActivity(ctx, storage.ShardActivity{
			Type:    logType,
			Message: msg,
			ShardID: id,
		})
	}

	return mcp.NewToolResultText(fmt.Sprintf("Shard updated: %s", id)), nil
}

func (s *MCPServer) handleDeleteShard(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	confirmCore := request.GetBool("confirm_core", false)

	slog.Info("delete_shard called", "id", id, "confirm_core", confirmCore)

	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	// Block comm-summary-* shards (system-managed, no override)
	if strings.HasPrefix(id, "comm-summary-") {
		return mcp.NewToolResultError("comm-summary-* shards are system-managed and cannot be deleted via MCP"), nil
	}

	// Core-category guard
	existing, err := s.vessel.GetShardByID(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("shard not found: %s", id)), nil
	}
	if existing.Category == "core" && !confirmCore {
		payload, _ := json.Marshal(map[string]string{
			"status":           "confirm_required",
			"id":               id,
			"current_category": "core",
			"message":          "This is a core identity shard. Re-issue with confirm_core=true to proceed.",
		})
		return mcp.NewToolResultText(string(payload)), nil
	}

	if err := s.vessel.DeleteShard(ctx, id); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delete failed: %v", err)), nil
	}

	// Activity Ledger
	if s.ledger != nil {
		logType := "delete"
		if existing.Category == "core" && confirmCore {
			logType = "delete_core_override"
		}
		_ = s.ledger.SaveActivity(ctx, storage.ShardActivity{
			Type:    logType,
			Message: fmt.Sprintf("Deleted shard [%s] (category=%s)", id, existing.Category),
			ShardID: id,
		})
	}

	return mcp.NewToolResultText(fmt.Sprintf("Shard deleted: %s", id)), nil
}

func (s *MCPServer) handleSearchGraph(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	metrics.SearchGraphTotal.Add(1)

	vecStr := request.GetString("query_vector", "")
	text := request.GetString("query_text", "")
	limit := int(request.GetFloat("limit", 10))

	slog.Info("search_graph called", "text_len", len(text), "limit", limit)

	// 2.3 — Limit validation
	if len(text) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Query exceeds max length of %d characters", maxQueryLen)), nil
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}

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
		slog.Error("search_graph failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("graph search failed: %v", err)), nil
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
	metrics.SearchGraphLatency.Observe(time.Since(start))
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) RegisterResources() {
	slog.Debug("registering resource", "uri", "shard-link://core")
	res := mcp.NewResource("shard-link://core",
		"Core Identity",
		mcp.WithResourceDescription("Read-only access to user profile and system anchors"),
	)
	s.mcp.AddResource(res, s.handleReadCore)
}

func (s *MCPServer) handleReadCore(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	slog.Debug("reading resource", "uri", request.Params.URI)
	shards, err := s.vessel.GetCoreShards(ctx)
	if err != nil {
		slog.Error("GetCoreShards failed", "error", err)
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
	start := time.Now()
	metrics.SearchMemoryTotal.Add(1)

	vecStr := request.GetString("query_vector", "")
	text := request.GetString("query_text", "")
	limit := int(request.GetFloat("limit", 5))

	slog.Info("search_memory called", "text_len", len(text), "limit", limit)

	// 2.3 — Limit validation
	if len(text) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Query exceeds max length of %d characters", maxQueryLen)), nil
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}

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
		slog.Error("search_memory failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("vector search failed: %v", err)), nil
	}

	// MMR diversity re-ranking. Sentinel default (-1) distinguishes "caller
	// did not pass lambda" from an explicit 0.7 — otherwise an explicit 0.7
	// would be silently overridden by the MMR_LAMBDA env var.
	lambda := request.GetFloat("lambda", -1)
	if lambda < 0 {
		lambda = 0.7
		if envL := os.Getenv("MMR_LAMBDA"); envL != "" {
			if parsed, err := strconv.ParseFloat(envL, 64); err == nil {
				lambda = parsed
			}
		}
	}
	results = storage.MaximalMarginalRelevance(queryVec, results, limit, lambda)

	// Working Memory: update session centroid with retrieved shards
	s.updateCentroidSlice(ctx, results)

	var response string
	for _, shard := range results {
		response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
	}

	metrics.SearchMemoryLatency.Observe(time.Since(start))
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleSearchText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	metrics.SearchTextTotal.Add(1)

	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	slog.Info("search_text called", "query_len", len(query), "limit", limit)

	// 2.3 — Query + limit validation
	if query == "" {
		return mcp.NewToolResultError("query must not be empty"), nil
	}
	if len(query) > maxQueryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Query exceeds max length of %d characters", maxQueryLen)), nil
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}

	results, err := s.vessel.FindText(ctx, query, limit, true)
	if err != nil {
		slog.Error("search_text failed", "error", err)
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

	metrics.SearchTextLatency.Observe(time.Since(start))
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) RegisterPrompts() {
	slog.Debug("registering prompt", "name", "hub_search")
	shardPrompt := mcp.NewPrompt("hub_search",
		mcp.WithPromptDescription("Global search across Shard-Link memory (Neo4j Mesh). Uses the 'search_all' meta-tool for maximum context."),
		mcp.WithArgument("query", mcp.ArgumentDescription("Keyword or topic to search for"), mcp.RequiredArgument()),
	)
	s.mcp.AddPrompt(shardPrompt, s.handleShardPrompt)
}

func (s *MCPServer) handleShardPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	query := request.Params.Arguments["query"]
	slog.Debug("prompt request", "name", "hub_search", "query_len", len(query))

	message := mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(
		fmt.Sprintf("Please use the 'search_all' tool to find all relevant shards and graph relationships for the topic: '%s'. Provide a high-signal summary of what you find.", query),
	))

	return mcp.NewGetPromptResult("Search Results", []mcp.PromptMessage{message}), nil
}

// --- OAuth 2.0 Authorization Code + PKCE (for Claude.ai connectors) ---
//
// Claude.ai uses the full Authorization Code flow with PKCE (RFC 7636):
//   1. Browser redirects to /authorize with code_challenge (S256)
//   2. /authorize auto-approves (single-user) and redirects back with a code
//   3. Claude.ai POSTs to /token with code + code_verifier
//   4. /token validates PKCE and returns a Bearer token
//
// The Bearer token is a stateless JWT signed with HUB_API_KEY (HS256).

// issueJWT creates an HMAC-SHA256 signed JWT with the given TTL.
// Claims: iss=shard-link, sub=oauth-session, exp, iat.
func (s *MCPServer) issueJWT(ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    "shard-link",
		Subject:   "oauth-session",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.apiKey))
}

// validateJWT parses and validates an HS256-signed JWT.
// Guards against alg:none by enforcing HMAC method check before signature verification.
func (s *MCPServer) validateJWT(tokenString string) bool {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		// Prevent alg substitution attacks (e.g. alg:none)
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.apiKey), nil
	}, jwt.WithIssuer("shard-link"), jwt.WithExpirationRequired())
	if err != nil {
		slog.Warn("JWT validation failed", "error", err)
		return false
	}
	return token.Valid
}

// authCode holds a pending authorization code with its PKCE challenge.
type authCode struct {
	challenge string
	method    string
	expiresAt time.Time
}

// pendingCodes stores authorization codes awaiting token exchange.
// Map key is the code string. Cleaned up on use or expiry.
var pendingCodes = struct {
	sync.Mutex
	codes map[string]authCode
}{codes: make(map[string]authCode)}

// handleOAuthMetadata serves OAuth 2.0 Authorization Server Metadata (RFC 8414).
func (s *MCPServer) handleOAuthMetadata(baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		slog.Debug("OAuth metadata discovery", "remote", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                baseURL,
			"authorization_endpoint":                baseURL + "/authorize",
			"token_endpoint":                        baseURL + "/token",
			"grant_types_supported":                 []string{"authorization_code"},
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
		})
	}
}

// handleOAuthAuthorize auto-approves the authorization request (single-user system)
// and redirects back to Claude.ai with a one-time authorization code.
func (s *MCPServer) handleOAuthAuthorize() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		// Rate limit: per client IP (CF-Connecting-IP behind the tunnel), 5 req/sec burst 3
		if !oauthLimiters.get(clientIP(r)).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Validate client_id early — no point generating an auth code for an unknown client.
		clientID := r.URL.Query().Get("client_id")
		if clientID != s.oauthClientID {
			slog.Warn("OAuth rejected unknown client_id", "client_id", clientID, "remote", r.RemoteAddr)
			http.Error(w, "Invalid client_id", http.StatusUnauthorized)
			return
		}

		challenge := r.URL.Query().Get("code_challenge")
		method := r.URL.Query().Get("code_challenge_method")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		slog.Info("OAuth authorize request", "client_id", clientID, "method", method, "remote", r.RemoteAddr)

		if redirectURI == "" || challenge == "" {
			http.Error(w, "Missing redirect_uri or code_challenge", http.StatusBadRequest)
			return
		}

		// 1.1 — Whitelist redirect_uri to prevent open redirect → token theft.
		// Only HTTPS to trusted hosts is allowed.
		parsed, err := url.Parse(redirectURI)
		if err != nil || parsed.Scheme != "https" || !validRedirectHosts[parsed.Hostname()] {
			slog.Warn("OAuth rejected redirect_uri", "uri", redirectURI)
			http.Error(w, "Invalid redirect_uri: host not in allowlist", http.StatusBadRequest)
			return
		}

		// 1.4 — Require S256 PKCE method. Reject plain or missing method.
		if method != "S256" {
			http.Error(w, "code_challenge_method must be S256", http.StatusBadRequest)
			return
		}

		// Cap pending codes to prevent memory exhaustion DoS
		pendingCodes.Lock()
		if len(pendingCodes.codes) >= maxPendingCodes {
			pendingCodes.Unlock()
			http.Error(w, "Too many pending authorizations, try again later", http.StatusServiceUnavailable)
			return
		}

		// Generate a random one-time code
		codeBytes := make([]byte, 32)
		if _, err := rand.Read(codeBytes); err != nil {
			pendingCodes.Unlock()
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		code := base64.RawURLEncoding.EncodeToString(codeBytes)

		pendingCodes.codes[code] = authCode{
			challenge: challenge,
			method:    method,
			expiresAt: time.Now().Add(5 * time.Minute),
		}
		pendingCodes.Unlock()

		// 1.5 — URL-encode code and state to prevent parameter injection
		sep := "?"
		if strings.Contains(redirectURI, "?") {
			sep = "&"
		}
		location := fmt.Sprintf("%s%scode=%s&state=%s", redirectURI, sep,
			url.QueryEscape(code), url.QueryEscape(state))
		http.Redirect(w, r, location, http.StatusFound)
	}
}

// handleOAuthToken exchanges an authorization code + PKCE verifier for an ephemeral Bearer token.
func (s *MCPServer) handleOAuthToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		// Rate limit: per client IP (CF-Connecting-IP behind the tunnel), 5 req/sec burst 3
		if !oauthLimiters.get(clientIP(r)).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()

		// Validate client credentials before anything else.
		// Uses constant-time comparison to prevent timing side-channel attacks
		// on the secret — string == short-circuits on the first differing byte.
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")
		idMatch := subtle.ConstantTimeCompare([]byte(clientID), []byte(s.oauthClientID)) == 1
		secretMatch := subtle.ConstantTimeCompare([]byte(clientSecret), []byte(s.oauthClientSecret)) == 1
		if !idMatch || !secretMatch {
			slog.Warn("OAuth invalid client credentials", "client_id", clientID, "remote", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}

		grantType := r.FormValue("grant_type")
		code := r.FormValue("code")
		verifier := r.FormValue("code_verifier")

		slog.Info("OAuth token request", "grant_type", grantType, "remote", r.RemoteAddr)

		if grantType != "authorization_code" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "unsupported_grant_type",
				"error_description": "Only authorization_code is supported",
			})
			return
		}

		// 1.4 — PKCE is mandatory. Reject if code_verifier is missing.
		if verifier == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_request",
				"error_description": "code_verifier is required",
			})
			return
		}

		// Look up and consume the pending code (one-time use)
		pendingCodes.Lock()
		pending, exists := pendingCodes.codes[code]
		if exists {
			delete(pendingCodes.codes, code)
		}
		pendingCodes.Unlock()

		if !exists || time.Now().After(pending.expiresAt) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_grant",
				"error_description": "Authorization code is invalid or expired",
			})
			return
		}

		// 1.3 — Validate PKCE with constant-time comparison to prevent timing attacks.
		// String == short-circuits on the first differing byte, leaking how many
		// leading bytes matched. subtle.ConstantTimeCompare always takes the same time.
		h := sha256.Sum256([]byte(verifier))
		computed := base64.RawURLEncoding.EncodeToString(h[:])
		if subtle.ConstantTimeCompare([]byte(computed), []byte(pending.challenge)) != 1 {
			slog.Warn("OAuth PKCE verification failed", "remote", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_grant",
				"error_description": "PKCE verification failed",
			})
			return
		}

		// 1.2 — Issue a stateless JWT instead of storing an ephemeral token.
		// Survives container restarts — no server-side state required.
		sessionToken, err := s.issueJWT(30 * 24 * time.Hour)
		if err != nil {
			slog.Error("failed to issue JWT", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		slog.Info("OAuth JWT issued", "remote", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": sessionToken,
			"token_type":   "Bearer",
			"expires_in":   2592000,
		})
	}
}

// StartHub ignites the multi-protocol MCP bridge.
// Primary transport: Streamable HTTP on /mcp (MCP spec 2024-11-05).
// Legacy transport:  Standard SSE on /sse + /message (kept for backward compat).
func (s *MCPServer) StartHub(ctx context.Context, port int, baseURL string) error {
	slog.Info("starting MCP hub", "port", port, "base_url", baseURL)

	// 1.6 — Background cleanup: purge expired auth codes and session tokens every minute.
	// Without this, expired entries accumulate forever (memory leak under sustained use).
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()

				// Clean expired pending authorization codes
				pendingCodes.Lock()
				for code, ac := range pendingCodes.codes {
					if now.After(ac.expiresAt) {
						delete(pendingCodes.codes, code)
					}
				}
				pendingCodes.Unlock()

				// Sweep idle rate-limiter entries (per-token and per-IP maps
				// otherwise grow unbounded — see limiterPool).
				rateLimiters.sweep(1 * time.Hour)
				oauthLimiters.sweep(1 * time.Hour)
			}
		}
	}()

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

	// OAuth 2.0 Authorization Code + PKCE — enables Claude.ai connector auth
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleOAuthMetadata(baseURL))
	mux.HandleFunc("/authorize", s.handleOAuthAuthorize())
	mux.HandleFunc("/token", s.handleOAuthToken())

	// Primary route — Streamable HTTP
	mux.Handle("/mcp", s.withAuth(streamableSrv))

	// Legacy routes — Standard SSE
	mux.Handle("/sse", s.withAuth(sseSrv.SSEHandler()))
	mux.Handle("/message", s.withAuth(sseSrv.MessageHandler()))

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	slog.Info("hub ignited", "port", port, "primary", "/mcp", "legacy", "/sse")
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully drains in-flight HTTP requests within the given context deadline.
func (s *MCPServer) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
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

// updateCentroidSlice updates the working memory centroid from a shard slice.
func (s *MCPServer) updateCentroidSlice(ctx context.Context, shards []storage.Shard) {
	s.wm.Update(sessionID(ctx), shards)
}

// allowedCategories restricts which categories MCP callers can assign.
// "core" is in the map but save_memory also requires allow_core=true (see handleSave 2.1a).
var allowedCategories = map[string]bool{
	"core":     true,
	"memory":   true,
	"session":  true,
	"tech":     true,
	"arch":     true,
	"contract": true,
}

// Input size limits — prevents abuse via oversized payloads that would
// trigger expensive embedding API calls and bloat storage.
const (
	maxIDLen       = 256
	maxContentLen  = 100 * 1024 // 100KB
	maxCategoryLen = 50
	maxQueryLen    = 10_000
	maxResultLimit = 100
)

func (s *MCPServer) handleSave(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	metrics.SaveMemoryTotal.Add(1)

	id := request.GetString("id", "")
	content := request.GetString("content", "")
	category := request.GetString("category", "memory")
	allowCore := request.GetBool("allow_core", false)
	vecStr := request.GetString("vector", "")

	slog.Info("save_memory called", "id", id, "content_len", len(content), "category", category, "allow_core", allowCore)

	// 2.1 — Category whitelist
	if !allowedCategories[category] {
		return mcp.NewToolResultError(fmt.Sprintf("Category %q is not allowed via MCP. Allowed: core (requires allow_core=true), memory, session, tech, arch, contract", category)), nil
	}
	// 2.1a — Core intent gate: core shards are permanently eviction-immune — require explicit opt-in
	if category == "core" && !allowCore {
		return mcp.NewToolResultError("category 'core' requires allow_core=true — core shards are permanently immune to eviction and cannot be bulk-evicted"), nil
	}

	// 2.2 — Content size limits
	if id == "" {
		return mcp.NewToolResultError("ID is required — provide a descriptive kebab-case identifier (e.g. 'contract-cli-hub-feature-name')"), nil
	}
	if len(id) > maxIDLen {
		return mcp.NewToolResultError(fmt.Sprintf("ID exceeds max length of %d characters", maxIDLen)), nil
	}
	if content == "" {
		return mcp.NewToolResultError("content is required"), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("Content exceeds max size of %dKB", maxContentLen/1024)), nil
	}
	if len(category) > maxCategoryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Category exceeds max length of %d characters", maxCategoryLen)), nil
	}

	// 2.4 — Upsert guard. SaveShard uses MERGE, so saving an existing ID
	// silently overwrites it. Without these checks, save_memory would be a
	// backdoor around the protections update_shard/delete_shard enforce:
	// re-saving a core shard with category='memory' would demote it (making
	// it evictable) with no confirm_core, and system-managed community
	// summaries could be clobbered.
	if strings.HasPrefix(id, "comm-summary-") {
		return mcp.NewToolResultError("comm-summary-* shards are system-managed and cannot be written via MCP"), nil
	}
	if existing, err := s.vessel.GetShardByID(ctx, id); err == nil {
		if existing.Category == "core" && !allowCore {
			return mcp.NewToolResultError(fmt.Sprintf("shard %q already exists as a core identity shard — overwriting requires allow_core=true (or use update_shard with confirm_core=true)", id)), nil
		}
		slog.Info("save_memory overwriting existing shard", "id", id, "old_category", existing.Category, "new_category", category)
	}

	var vec []byte
	var err error

	if vecStr != "" {
		vec, err = base64.StdEncoding.DecodeString(vecStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid vector encoding: %v", err)), nil
		}
		// Dimension guard — a wrong-length vector would be persisted and
		// silently excluded from linking/search by size() checks downstream.
		if len(vec)%4 != 0 {
			return mcp.NewToolResultError("Invalid vector: byte length must be a multiple of 4 (float32)"), nil
		}
		if s.embedder != nil {
			if got, want := len(vec)/4, s.embedder.Dimension(); got != want {
				return mcp.NewToolResultError(fmt.Sprintf("Vector has %d dimensions, expected %d", got, want)), nil
			}
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
	slog.Info("salience scored", "salience", salience, "shard_id", id)

	now := time.Now()
	shard := storage.Shard{
		ID:        id,
		Content:   content,
		Category:  category,
		Vector:    vec,
		Salience:  salience,
		CreatedAt: now,
		LastUsed:  now,
	}

	if err := s.vessel.SaveShard(ctx, shard); err != nil {
		slog.Error("save_memory failed", "error", err)
		return nil, err
	}

	// Log to Activity Ledger
	if s.ledger != nil {
		_ = s.ledger.SaveActivity(ctx, storage.ShardActivity{
			Type:    "save",
			Message: fmt.Sprintf("Saved shard [%s] (category=%s, salience=%.2f)", id, category, salience),
			ShardID: id,
		})
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
			slog.Debug("shard linked to episode", "shard_id", id, "session_id", sessID)
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory saved: %s", id)), nil
}
