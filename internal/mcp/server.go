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
	vessel      storage.Repository
	pgVessel    *storage.PostgresVessel // Archival engine — pinged independently for health checks
	mcp         *server.MCPServer
	apiKey      string
	embedder    storage.Embedder
	summarizer  storage.Summarizer
	wm          *WorkingMemory
	ledger      *storage.Vessel // Activity Ledger (SQLite) for persistent logging
	httpServer  *http.Server

	// Ephemeral session tokens — OAuth clients get these instead of the raw API key.
	// Key: token string, Value: expiry time.
	sessionTokens   map[string]time.Time
	sessionTokensMu sync.RWMutex
}

func NewMCPServer(v storage.Repository, apiKey string, e storage.Embedder, sum storage.Summarizer, ledger *storage.Vessel, pg *storage.PostgresVessel, ctx context.Context) *MCPServer {
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
		vessel:        v,
		pgVessel:      pg,
		mcp:           s,
		apiKey:        apiKey,
		embedder:      e,
		summarizer:    sum,
		wm:            wm,
		ledger:        ledger,
		sessionTokens: make(map[string]time.Time),
	}

	// 2. Register tools, resources, and prompts BEFORE return
	slog.Info("registering MCP capabilities")
	mcpSrv.RegisterTools()
	mcpSrv.RegisterResources()
	mcpSrv.RegisterPrompts()

	slog.Info("MCP server initialization complete")
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

// OAuth-specific rate limiting — per IP, 5 req/sec with burst of 3.
// Separate from the MCP data-path limiter because OAuth endpoints are
// unauthenticated (they establish auth), so they need tighter controls.
var (
	oauthLimiters   = make(map[string]*rate.Limiter)
	oauthLimitersMu sync.Mutex
)

func getOAuthLimiter(ip string) *rate.Limiter {
	oauthLimitersMu.Lock()
	defer oauthLimitersMu.Unlock()
	if lim, ok := oauthLimiters[ip]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(5), 3)
	oauthLimiters[ip] = lim
	return lim
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

		// Check direct API key match first (Claude Code CLI path)
		authorized := key == s.apiKey

		// If not a direct API key match, check ephemeral session tokens (OAuth path)
		if !authorized && key != "" {
			s.sessionTokensMu.RLock()
			if expiry, ok := s.sessionTokens[key]; ok && time.Now().Before(expiry) {
				authorized = true
			}
			s.sessionTokensMu.RUnlock()
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
		if !getLimiter(limiterKey).Allow() {
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
		mcp.WithString("category", mcp.Description("e.g. 'session', 'core', 'memory'"), mcp.Required()),
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
	)
	s.mcp.AddTool(searchAllTool, s.handleSearchAll)

	slog.Debug("registering tool", "name", "get_status")
	statusTool := mcp.NewTool("get_status",
		mcp.WithDescription("Returns mesh statistics and service health for Shard-Link hub"),
	)
	s.mcp.AddTool(statusTool, s.handleGetStatus)
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
	}

	payload, err := json.Marshal(status)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal status: %v", err)), nil
	}

	return mcp.NewToolResultText(string(payload)), nil
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

	metrics.SearchMemoryLatency.Observe(time.Since(start))
	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleSearchText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	metrics.SearchTextTotal.Add(1)

	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	slog.Info("search_text called", "query_len", len(query), "limit", limit)

	// 2.3 — Limit validation
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
// The Bearer token IS the HUB_API_KEY — no separate token store needed.

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
			"code_challenge_methods_supported":       []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		})
	}
}

// handleOAuthAuthorize auto-approves the authorization request (single-user system)
// and redirects back to Claude.ai with a one-time authorization code.
func (s *MCPServer) handleOAuthAuthorize() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		// Rate limit: per-IP, 5 req/sec burst 3
		if !getOAuthLimiter(r.RemoteAddr).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		challenge := r.URL.Query().Get("code_challenge")
		method := r.URL.Query().Get("code_challenge_method")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		slog.Info("OAuth authorize request", "method", method, "remote", r.RemoteAddr)

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

		// Rate limit: per-IP, 5 req/sec burst 3
		if !getOAuthLimiter(r.RemoteAddr).Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()

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

		// 1.2 — Generate an ephemeral session token instead of returning the raw API key.
		// If this token leaks, only this 24hr session is compromised — not the master key.
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		sessionToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
		expiry := time.Now().Add(30 * 24 * time.Hour)

		s.sessionTokensMu.Lock()
		s.sessionTokens[sessionToken] = expiry
		s.sessionTokensMu.Unlock()

		slog.Info("OAuth ephemeral token issued", "remote", r.RemoteAddr, "expires", expiry.Format(time.RFC3339))
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

				// Clean expired session tokens
				s.sessionTokensMu.Lock()
				for token, expiry := range s.sessionTokens {
					if now.After(expiry) {
						delete(s.sessionTokens, token)
					}
				}
				s.sessionTokensMu.Unlock()
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

// allowedCategories restricts which categories MCP callers can assign.
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
	vecStr := request.GetString("vector", "")

	slog.Info("save_memory called", "id", id, "content_len", len(content), "category", category)

	// 2.1 — Category whitelist: reject "core" from MCP callers
	if !allowedCategories[category] {
		return mcp.NewToolResultError(fmt.Sprintf("Category %q is not allowed via MCP. Allowed: core, memory, session, tech, arch, contract", category)), nil
	}

	// 2.2 — Content size limits
	if len(id) > maxIDLen {
		return mcp.NewToolResultError(fmt.Sprintf("ID exceeds max length of %d characters", maxIDLen)), nil
	}
	if len(content) > maxContentLen {
		return mcp.NewToolResultError(fmt.Sprintf("Content exceeds max size of %dKB", maxContentLen/1024)), nil
	}
	if len(category) > maxCategoryLen {
		return mcp.NewToolResultError(fmt.Sprintf("Category exceeds max length of %d characters", maxCategoryLen)), nil
	}

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
