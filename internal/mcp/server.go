package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

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
func (s *MCPServer) StartSSE(port int) error {
	// 1. Warp the MCP server logic in an SSE transport
	sseServer := server.NewSSEServer(s.mcp, server.WithBaseURL(fmt.Sprintf("http://localhost:%d", port)))

	// 2. Setup standard HTTP routing
	mux := http.NewServeMux()

	// The library provides an SSEHandler that handles the protocol handshake
	mux.Handle("/sse", sseServer.SSEHandler())

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
