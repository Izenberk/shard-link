package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	vessel 	storage.Repository
	mcp 		*server.MCPServer
	apiKey	string
}

func NewMCPServer(v storage.Repository, apiKey string) *MCPServer {
	// 1. Create the base MCP server
	s := server.NewMCPServer(
		"Shard-Link Hub",
		"v0.1.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
	)

	mcpSrv := &MCPServer{
		vessel:	v,
		mcp:		s,
		apiKey: apiKey,
	}

	// 2. Register tools and resources BEFORE return
	mcpSrv.RegisterTools()
	mcpSrv.RegisterResources()

	return mcpSrv
}

func (s *MCPServer) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no key is configured (local dev only)
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("X-API-Key") != s.apiKey {
			log.Printf("[Auth] Denied %s %s from %s (invalid key)", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid Shard Access Key", http.StatusUnauthorized)
			return
		}

		log.Printf("[Auth] Granted %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *MCPServer) RegisterTools() {
	// Tool 1: search_memory
	searchTool := mcp.NewTool("search_memory",
		mcp.WithDescription("Search long-term memory using semantic resonance (vector search)"),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
	s.mcp.AddTool(searchTool, s.handleSearch)

	// Tool 2: search_text
	searchTextTool := mcp.NewTool("search_text",
		mcp.WithDescription("Search long-term memory using keyword matching (SQL LIKE)"),
		mcp.WithString("query", mcp.Description("The keyword or phrase to search for"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
	s.mcp.AddTool(searchTextTool, s.handleSearchText)

	// Tool 3: save_memory
	saveTool := mcp.NewTool("save_memory",
		mcp.WithDescription("Save a new contextual fragment to long-term memory"),
		mcp.WithString("id", mcp.Description("Unique identifier"), mcp.Required()),
		mcp.WithString("content", mcp.Description("The text to remember"), mcp.Required()),
		mcp.WithString("category", mcp.Description("e.g. 'session', 'core', 'memory'"), mcp.Required()),
		mcp.WithString("vector", mcp.Description("Base64 encoded float32 vector"), mcp.Required()),
	)
	s.mcp.AddTool(saveTool, s.handleSave)

	// Tool 4: search_graph
	graphTool := mcp.NewTool("search_graph",
		mcp.WithDescription("Search the Knowledge Mesh by finding a central context and traversing its semantic neighbors (Multi-Hop)"),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max neighbors to return")),
	)
	s.mcp.AddTool(graphTool, s.handleSearchGraph)
}

func (s *MCPServer) handleSearchGraph(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vecStr := request.GetString("query_vector", "")
	limit := int(request.GetFloat("limit", 10))

	queryVec, err := base64.StdEncoding.DecodeString(vecStr)
	if err != nil {
		return mcp.NewToolResultError("Invalid vector encoding"), nil
	}

	results, err := s.vessel.SearchGraph(ctx, queryVec, limit)
	if err != nil {
		return nil, err
	}

	var response string
	if len(results) == 0 {
		response = "No connected neighbors found for this context."
	} else {
		for _, shard := range results {
			response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
		}
	}
	return mcp.NewToolResultText(response), nil
}


func (s *MCPServer) RegisterResources() {
	// 1. Define the resource
	res := mcp.NewResource("shard-link://core",
		"Core Identity",
		mcp.WithResourceDescription("Read-only access to user profile and system anchors"),
	)
	// 2. Register it with the library
	s.mcp.AddResource(res, s.handleReadCore)
}

func (s *MCPServer) handleReadCore(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// 1. Get the data from the Vessel
	shards, err := s.vessel.GetCoreShards(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Format it as plain text
	var body string
	for _, shard := range shards {
		body += fmt.Sprintf("[%s]\n%s\n---\n", shard.ID, shard.Content)
	}

	// 3. Return it using the library's helper
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:			request.Params.URI,
			Text:			body,
			MIMEType: "text/plain",
		},
	}, nil
}

func (s *MCPServer) handleSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// 1. Parse Arguments
	vecStr 	:= request.GetString("query_vector", "")
	limit 	:= int(request.GetFloat("limit", 5))

	// 2. Decode Vector from Base64
	queryVec, err := base64.StdEncoding.DecodeString(vecStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid vector encoding: %v", err)), nil
	}

	// 3. Query the Vessel
	results, err := s.vessel.FindResonant(ctx, queryVec, limit)
	if err != nil {
		return nil, err
	}

	// 4. Format for AI
	var response string
	for _, shard := range results {
		response += fmt.Sprintf("[%s]: %s\n---\n", shard.ID, shard.Content)
	}

	return mcp.NewToolResultText(response), nil
}

func (s *MCPServer) handleSearchText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// 1. Parse Arguments
	query := request.GetString("query", "")
	limit := int(request.GetFloat("limit", 5))

	// 2. Query the Vessel
	results, err := s.vessel.FindText(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	// 3. Format for AI
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

// StartSSE launches the production-grade HTTP bridge with API Key protection.
func (s *MCPServer) StartSSE(port int, baseURL string) error {
	// 1. Wrap the MCP server logic in an SSE transport
	sseServer := server.NewSSEServer(s.mcp, server.WithBaseURL(baseURL))

	// 2. Setup standard HTTP routing
	mux := http.NewServeMux()

	// 3. SSE Handler with Auth & Heartbeat (Cloudflare stable)
	mux.Handle("/sse", s.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			sseServer.SSEHandler().ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go func() {
			ticker := time.NewTicker(15 * time.Second) // Reduced for Cloudflare stability
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Send an SSE comment as a heartbeat (ignored by client but keeps connection alive)
					fmt.Fprintf(w, ": heartbeat\n\n")
					f.Flush()
				}
			}
		}()

		sseServer.SSEHandler().ServeHTTP(w, r)
	})))

	// 4. Message Handler with Auth
	mux.Handle("/message", s.withAuth(sseServer.MessageHandler()))

	fmt.Printf("Shard-Link Authenticated Bridge ignited on :%d/sse\n", port)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

func (s *MCPServer) handleSave(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	content := request.GetString("content", "")
	category := request.GetString("category", "memory")
	vecStr := request.GetString("vector", "")

	vec, err := base64.StdEncoding.DecodeString(vecStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid vector encoding: %v", err)), nil
	}

	shard := storage.Shard{
		ID:					id,
		Content:		content,
		Category: 	category,
		Vector: 		vec,
	}

	if err := s.vessel.SaveShard(ctx, shard); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory saved: %s", id)), nil
}
