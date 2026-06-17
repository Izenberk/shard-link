package mcp

import (
	"context"
	"strings"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/izenberk/shard-link/internal/storage"
)

// makeRequest builds a CallToolRequest with the given arguments map.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

// newTestServer creates a minimal MCPServer wired to a MockRepository and a
// MockEmbedder. nil summarizer and nil ledger are safe — handleSave guards both.
func newTestServer(mock *MockRepository) *MCPServer {
	return &MCPServer{
		vessel:   mock,
		embedder: &MockEmbedder{dim: 2},
		wm:       NewWorkingMemory(0), // zero TTL is fine for handler tests
	}
}

// TestSaveMemory_ReturnsOK verifies that a well-formed save_memory call returns
// a non-error result and reaches the repository (SaveShard called once).
func TestSaveMemory_ReturnsOK(t *testing.T) {
	mock := &MockRepository{}
	srv := newTestServer(mock)

	result, err := srv.handleSave(context.Background(), makeRequest(map[string]any{
		"id":       "integ-ok-1",
		"content":  "integration test shard content",
		"category": "memory",
	}))
	if err != nil {
		t.Fatalf("handleSave returned err: %v", err)
	}
	if result == nil {
		t.Fatal("handleSave returned nil result")
	}
	if result.IsError {
		t.Errorf("expected success result, got error: %+v", result)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.SaveShardCalls != 1 {
		t.Errorf("expected SaveShard called once, got %d", mock.SaveShardCalls)
	}
}

// TestSaveMemory_CoreBlocked verifies that save_memory with category=core and
// no allow_core=true returns a tool error BEFORE the repository is touched.
func TestSaveMemory_CoreBlocked(t *testing.T) {
	mock := &MockRepository{}
	srv := newTestServer(mock)

	result, err := srv.handleSave(context.Background(), makeRequest(map[string]any{
		"id":       "pref-golang",
		"content":  "user prefers Go over Python",
		"category": "core",
		// allow_core intentionally omitted
	}))
	if err != nil {
		t.Fatalf("handleSave returned err: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for core without allow_core=true, got success")
	}

	// Repository must not have been touched — gate fires before SaveShard
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.SaveShardCalls != 0 {
		t.Errorf("SaveShard must not be called when core gate blocks, got %d calls", mock.SaveShardCalls)
	}
}

// TestSaveMemory_CoreAllowedWithFlag verifies that save_memory with category=core
// and allow_core=true passes the gate and reaches the repository.
func TestSaveMemory_CoreAllowedWithFlag(t *testing.T) {
	mock := &MockRepository{}
	srv := newTestServer(mock)

	result, err := srv.handleSave(context.Background(), makeRequest(map[string]any{
		"id":         "pref-golang",
		"content":    "user prefers Go over Python",
		"category":   "core",
		"allow_core": true,
	}))
	if err != nil {
		t.Fatalf("handleSave returned err: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success with allow_core=true, got error: %+v", result)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.SaveShardCalls != 1 {
		t.Errorf("expected SaveShard called once with allow_core=true, got %d", mock.SaveShardCalls)
	}
}

// TestSearchAll_DeduplicatedTouch verifies the end-to-end search_all contract:
// ReinforceShards is called exactly once after fan-out, with all unique IDs.
func TestSearchAll_DeduplicatedTouch(t *testing.T) {
	shardA := storage.Shard{ID: "shard-a", Content: "hello world deduplicated"}
	shardB := storage.Shard{ID: "shard-b", Content: "goodbye world deduplicated"}

	mock := &MockRepository{
		TextResults:   []storage.Shard{shardA, shardB},
		VectorResults: []storage.Shard{shardA},         // A duplicated in vector results
		GraphShards:   []storage.Shard{shardA, shardB}, // both duplicated in graph
	}
	srv := newTestServer(mock)

	result, err := srv.handleSearchAll(context.Background(), makeRequest(map[string]any{
		"query": "hello world",
		"limit": float64(10),
	}))
	if err != nil {
		t.Fatalf("handleSearchAll returned err: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %+v", result)
	}

	// ReinforceShards must be called exactly once regardless of how many engines returned results
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.ReinforceCalls) != 1 {
		t.Fatalf("expected 1 ReinforceShards call, got %d", len(mock.ReinforceCalls))
	}

	// The single call must contain exactly 2 unique IDs (A and B)
	reinforced := mock.ReinforceCalls[0]
	if len(reinforced) != 2 {
		t.Errorf("expected 2 unique IDs in reinforce call, got %d: %v", len(reinforced), reinforced)
	}
}

// TestValidation_RejectedByHandler verifies that an oversized ID is rejected by
// handleSave before SaveShard is called — the repository must never be touched.
func TestValidation_RejectedByHandler(t *testing.T) {
	mock := &MockRepository{}
	srv := newTestServer(mock)

	oversizedID := strings.Repeat("x", maxIDLen+1)

	result, err := srv.handleSave(context.Background(), makeRequest(map[string]any{
		"id":       oversizedID,
		"content":  "valid content",
		"category": "memory",
	}))
	if err != nil {
		t.Fatalf("handleSave returned err: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for oversized ID, got success")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.SaveShardCalls != 0 {
		t.Errorf("SaveShard must not be called when validation rejects input, got %d calls", mock.SaveShardCalls)
	}
}
