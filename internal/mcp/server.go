package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
	vessel 	*storage.Vessel
	mcp 		*server.MCPServer
}

func NewMCPServer(v *storage.Vessel) *MCPServer {
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
	}

	// 2. Register the "Search" tool
	mcpSrv.registerTools()
	mcpSrv.registerResources()

	return mcpSrv
}

func (s *MCPServer) registerTools() {
	// Tool 1: search_memory
	searchTool := mcp.NewTool("search_memory",
		mcp.WithDescription("Search long-term memory using semantic resonance"),
		mcp.WithString("query_vector", mcp.Description("Base64 encoded float32 vector"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
	s.mcp.AddTool(searchTool, s.handleSearch)

	// Tool 2: save_memory
	saveTool := mcp.NewTool("save_memory",
		mcp.WithDescription("Save a new contextual fragment to long-term memory"),
		mcp.WithString("id", mcp.Description("Unique identifier"), mcp.Required()),
		mcp.WithString("content", mcp.Description("The text to remember"), mcp.Required()),
		mcp.WithString("category", mcp.Description("e.g. 'session', 'core', 'memory'"), mcp.Required()),
		mcp.WithString("vector", mcp.Description("Base64 encoded float32 vector"), mcp.Required()),
	)
	s.mcp.AddTool(saveTool, s.handleSave)
}

func (s *MCPServer) registerResources() {
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
	shards, err := s.vessel.GetCoreShards()
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
	results, err := s.vessel.FindResonant(queryVec, limit)
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

// StartSSE launches the production-grade HTTP bridge
func (s *MCPServer) StartSSE(port int, baseURL string) error {
	// 1. Warp the MCP server logic in an SSE transport
	sseServer := server.NewSSEServer(s.mcp, server.WithBaseURL(baseURL))

	// 2. Setup standard HTTP routing
	mux := http.NewServeMux()

	// 3. Heartbeat Middleware for Cloudflare/Proxy stability
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		// Hand off to the library's SSE handler
		f, ok := w.(http.Flusher)
		if !ok {
			sseServer.SSEHandler().ServeHTTP(w, r)
			return
		}

		// Use a context to stop the heartbeat when the request ends
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go func() {
			ticker := time.NewTicker(30 * time.Second)
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
	})

	// The library also provides a MessageHandler for the POST responses
	mux.Handle("/message", sseServer.MessageHandler())

	fmt.Printf("Shard-Link Bridge ignited on :%d/sse\n", port)
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

	if err := s.vessel.SaveShard(shard); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory saved: %s", id)), nil
}
