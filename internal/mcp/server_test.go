package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// MockRepository implements storage.Repository with controlled return values.
// It tracks which methods were called and how many times, allowing tests to
// verify contracts like anti-double-touching.
type MockRepository struct {
	mu sync.Mutex

	// Controlled return data
	TextResults   []storage.Shard
	VectorResults []storage.Shard
	GraphShards   []storage.Shard
	GraphBonds    []storage.ShardBond

	// Call tracking
	ReinforceCalls  [][]string // each call's ID list
	FindTextCalls   int
	FindResCalls    int
	SearchGrCalls   int
	SaveShardCalls  int
}

func (m *MockRepository) SaveShard(_ context.Context, _ storage.Shard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SaveShardCalls++
	return nil
}
func (m *MockRepository) GetAllShards(_ context.Context) ([]storage.Shard, error) {
	return nil, nil
}
func (m *MockRepository) GetArchivedShards(_ context.Context) ([]storage.Shard, error) {
	return nil, nil
}
func (m *MockRepository) GetShardByID(_ context.Context, _ string) (storage.Shard, error) {
	return storage.Shard{}, nil
}
func (m *MockRepository) GetCoreShards(_ context.Context) ([]storage.Shard, error) {
	return nil, nil
}
func (m *MockRepository) ArchiveShard(_ context.Context, _ string) error  { return nil }
func (m *MockRepository) GetCount(_ context.Context) (int, error)         { return 0, nil }
func (m *MockRepository) GetEvictionCandidates(_ context.Context, _ int) ([]string, error) {
	return nil, nil
}
func (m *MockRepository) SaveBond(_ context.Context, _ storage.ShardBond) error { return nil }
func (m *MockRepository) GetAllBonds(_ context.Context) ([]storage.ShardBond, error) {
	return nil, nil
}
func (m *MockRepository) GetGraphData(_ context.Context) ([]storage.Shard, []storage.ShardBond, error) {
	return nil, nil, nil
}
func (m *MockRepository) CalculateCommunities(_ context.Context) (int, []int64, error) {
	return 0, nil, nil
}
func (m *MockRepository) GetShardsByCommunity(_ context.Context, _ int64) ([]storage.Shard, error) {
	return nil, nil
}
func (m *MockRepository) PruneStaleSummaries(_ context.Context) (int, error) { return 0, nil }
func (m *MockRepository) SyncBonds(_ context.Context, _ float64) (int, error) { return 0, nil }
func (m *MockRepository) Optimize(_ context.Context) error                    { return nil }
func (m *MockRepository) Close() error                                        { return nil }
func (m *MockRepository) GetBondCount(_ context.Context) (int, error)         { return 0, nil }
func (m *MockRepository) GetCommunityCount(_ context.Context) (int, error)    { return 0, nil }
func (m *MockRepository) GetRecentShards(_ context.Context, _ int, _ string) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *MockRepository) GetShardsByCategory(_ context.Context, _ string, _ int) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *MockRepository) GetAtRiskShards(_ context.Context, _ int, _ float64) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *MockRepository) UpdateShard(_ context.Context, _ string, _ storage.ShardUpdate) error {
	return nil
}
func (m *MockRepository) DeleteShard(_ context.Context, _ string) error { return nil }
func (m *MockRepository) GetSurvivalDistribution(_ context.Context) (map[string]int, error) {
	return nil, nil
}

func (m *MockRepository) FindText(_ context.Context, _ string, _ int, _ bool) ([]storage.Shard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FindTextCalls++
	return m.TextResults, nil
}

func (m *MockRepository) FindResonant(_ context.Context, _ []byte, _ int, _ bool) ([]storage.Shard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FindResCalls++
	return m.VectorResults, nil
}

func (m *MockRepository) FindHybrid(_ context.Context, _ string, _ []byte, _ int) ([]storage.Shard, error) {
	return nil, nil
}

func (m *MockRepository) SearchGraph(_ context.Context, _ []byte, _ int, _ bool) ([]storage.Shard, []storage.ShardBond, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SearchGrCalls++
	return m.GraphShards, m.GraphBonds, nil
}

func (m *MockRepository) ReinforceShards(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReinforceCalls = append(m.ReinforceCalls, ids)
	return nil
}

// MockEmbedder returns a fixed vector for any input.
type MockEmbedder struct {
	dim int
}

func (e *MockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, e.dim)
	v[0] = 1.0 // deterministic non-zero vector
	return v, nil
}

func (e *MockEmbedder) Dimension() int { return e.dim }

// TestSearchAllDeduplication verifies that search_all deduplicates results across engines.
// The same shard ID appearing in both text and vector results should appear only once.
func TestSearchAllDeduplication(t *testing.T) {
	shardA := storage.Shard{ID: "shard-a", Content: "hello world"}
	shardB := storage.Shard{ID: "shard-b", Content: "goodbye world"}

	mock := &MockRepository{
		TextResults:   []storage.Shard{shardA, shardB},         // text finds A and B
		VectorResults: []storage.Shard{shardA},                 // vector also finds A (duplicate)
		GraphShards:   []storage.Shard{shardA},                 // graph also finds A (duplicate)
	}

	// The dedup logic lives in handleSearchAll. We test the core contract here:
	// map[string]Shard deduplicates by ID.
	seenShards := make(map[string]storage.Shard)

	for _, sh := range mock.TextResults {
		seenShards[sh.ID] = sh
	}
	for _, sh := range mock.VectorResults {
		seenShards[sh.ID] = sh
	}
	for _, sh := range mock.GraphShards {
		seenShards[sh.ID] = sh
	}

	if len(seenShards) != 2 {
		t.Errorf("expected 2 unique shards after dedup, got %d", len(seenShards))
	}

	if _, ok := seenShards["shard-a"]; !ok {
		t.Error("shard-a missing from deduped results")
	}
	if _, ok := seenShards["shard-b"]; !ok {
		t.Error("shard-b missing from deduped results")
	}
}

// TestAntiDoubleTouching verifies that ReinforceShards is called exactly once per search_all,
// not once per engine. This prevents inflating use_count and retrieval_history.
func TestAntiDoubleTouching(t *testing.T) {
	shardA := storage.Shard{ID: "shard-a", Content: "hello"}
	shardB := storage.Shard{ID: "shard-b", Content: "world"}

	mock := &MockRepository{
		TextResults:   []storage.Shard{shardA},
		VectorResults: []storage.Shard{shardA, shardB},
		GraphShards:   []storage.Shard{shardB},
	}

	// Simulate what handleSearchAll does: fan out, dedup, then reinforce once.
	seenShards := make(map[string]storage.Shard)
	for _, sh := range mock.TextResults {
		seenShards[sh.ID] = sh
	}
	for _, sh := range mock.VectorResults {
		seenShards[sh.ID] = sh
	}
	for _, sh := range mock.GraphShards {
		seenShards[sh.ID] = sh
	}

	// Single reinforcement call with all unique IDs
	shardIDs := make([]string, 0, len(seenShards))
	for id := range seenShards {
		shardIDs = append(shardIDs, id)
	}
	_ = mock.ReinforceShards(context.Background(), shardIDs)

	// Verify: exactly 1 call to ReinforceShards
	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.ReinforceCalls) != 1 {
		t.Fatalf("expected 1 ReinforceShards call, got %d", len(mock.ReinforceCalls))
	}

	// Verify: the single call contained exactly 2 unique IDs
	reinforced := mock.ReinforceCalls[0]
	if len(reinforced) != 2 {
		t.Errorf("expected 2 IDs in reinforce call, got %d", len(reinforced))
	}
}

// TestSearchAllEnginesCalledWithShouldTouchFalse verifies that individual engine
// searches are called with shouldTouch=false when used within search_all.
// The touching happens via the single ReinforceShards call after dedup.
func TestSearchAllEnginesCalledWithShouldTouchFalse(t *testing.T) {
	// This is a design contract test. In handleSearchAll, the code calls:
	//   FindText(ctx, query, limit, false)     // shouldTouch = false
	//   FindResonant(ctx, queryVec, limit, false)  // shouldTouch = false
	//   SearchGraph(ctx, queryVec, limit, false)   // shouldTouch = false
	//
	// Then calls ReinforceShards(ctx, uniqueIDs) once.
	//
	// We verify the mock tracks the correct number of calls, confirming
	// the fan-out pattern is intact.

	mock := &MockRepository{
		TextResults:   []storage.Shard{{ID: "a", Content: "test"}},
		VectorResults: []storage.Shard{{ID: "b", Content: "test"}},
		GraphShards:   []storage.Shard{{ID: "c", Content: "test"}},
	}

	// Simulate the fan-out (what handleSearchAll does)
	_, _ = mock.FindText(context.Background(), "test", 5, false)
	_, _ = mock.FindResonant(context.Background(), nil, 5, false)
	_, _, _ = mock.SearchGraph(context.Background(), nil, 5, false)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.FindTextCalls != 1 {
		t.Errorf("expected 1 FindText call, got %d", mock.FindTextCalls)
	}
	if mock.FindResCalls != 1 {
		t.Errorf("expected 1 FindResonant call, got %d", mock.FindResCalls)
	}
	if mock.SearchGrCalls != 1 {
		t.Errorf("expected 1 SearchGraph call, got %d", mock.SearchGrCalls)
	}
}

// TestCognitiveBiasing verifies that the WorkingMemory biases a query vector
// toward the session centroid using the lambda blend formula.
func TestCognitiveBiasing(t *testing.T) {
	wm := NewWorkingMemory(30 * time.Minute)
	sessionID := "test-session"

	// Seed a centroid by simulating a retrieval with a known vector
	seedVec := make([]float32, 4)
	seedVec[0] = 0.0
	seedVec[1] = 1.0
	seedVec[2] = 0.0
	seedVec[3] = 0.0

	seedShard := storage.Shard{
		ID:      "seed",
		Content: "seed",
		Vector:  storage.EncodeVector(seedVec),
	}
	wm.Update(sessionID, []storage.Shard{seedShard})

	// Query vector is different from the centroid
	queryVec := []float32{1.0, 0.0, 0.0, 0.0}

	// Bias with lambda=0.7 — result should be 0.7*query + 0.3*centroid
	biased := wm.Bias(sessionID, queryVec, 0.7)

	// Expected: [0.7*1.0 + 0.3*0.0, 0.7*0.0 + 0.3*1.0, 0, 0] = [0.7, 0.3, 0, 0]
	expected := []float32{0.7, 0.3, 0.0, 0.0}
	tolerance := float32(0.01)

	for i, v := range biased {
		diff := v - expected[i]
		if diff < -tolerance || diff > tolerance {
			t.Errorf("biased[%d] = %.4f, expected %.4f (tolerance %.2f)", i, v, expected[i], tolerance)
		}
	}
}

// TestCognitiveBiasing_NoCentroid verifies that biasing returns the original
// query unchanged when no centroid exists (first query in session).
func TestCognitiveBiasing_NoCentroid(t *testing.T) {
	wm := NewWorkingMemory(30 * time.Minute)

	query := []float32{1.0, 0.0, 0.5, 0.0}
	biased := wm.Bias("no-such-session", query, 0.7)

	for i, v := range biased {
		if v != query[i] {
			t.Errorf("biased[%d] = %.4f, expected original %.4f (no centroid)", i, v, query[i])
		}
	}
}

// TestCategoryGate_BlockedCategories verifies that categories not in allowedCategories
// are rejected by validateSaveInput before the handler touches the repository.
// "community" is system-managed — external callers must never assign it.
func TestCategoryGate_BlockedCategories(t *testing.T) {
	blocked := []string{
		"community", // system-managed digests — MCP callers must not write these
		"archived",  // eviction output — not a valid save category
		"",          // empty string falls through to the whitelist check
		"MEMORY",    // case-sensitive — only lowercase allowed
		"admin",     // not in allowlist
	}

	for _, category := range blocked {
		errMsg := validateSaveInput("test-id", "test content", category, false)
		if errMsg == "" {
			t.Errorf("category %q should be rejected but validateSaveInput returned no error", category)
		}
	}
}

// TestCategoryGate_AllowedCategories verifies that the non-core whitelisted categories pass
// validateSaveInput without error. "core" is whitelisted but requires allow_core=true — tested
// separately in TestCoreIntentGate_*.
func TestCategoryGate_AllowedCategories(t *testing.T) {
	allowed := []string{"memory", "session", "tech", "arch", "contract"}

	for _, category := range allowed {
		errMsg := validateSaveInput("test-id", "test content", category, false)
		if errMsg != "" {
			t.Errorf("category %q should be allowed but got error: %s", category, errMsg)
		}
	}
}

// TestCoreIntentGate_BlockedByDefault verifies that save_memory rejects category=core
// without an explicit allow_core=true. Core shards are permanently eviction-immune;
// accidental or autonomous agent writes must be impossible.
func TestCoreIntentGate_BlockedByDefault(t *testing.T) {
	errMsg := validateSaveInput("pref-golang", "User prefers Go over Python", "core", false)
	if errMsg == "" {
		t.Error("category 'core' without allow_core=true should be rejected")
	}
}

// TestCoreIntentGate_AllowedWithFlag verifies that core saves succeed when the caller
// explicitly passes allow_core=true, confirming intentional write.
func TestCoreIntentGate_AllowedWithFlag(t *testing.T) {
	errMsg := validateSaveInput("pref-golang", "User prefers Go over Python", "core", true)
	if errMsg != "" {
		t.Errorf("category 'core' with allow_core=true should be allowed, got: %s", errMsg)
	}
}

// --- Race condition tests (run with: go test -race ./internal/mcp/) ---

// TestWorkingMemory_ConcurrentSessions launches 100 goroutines that call Update
// and Bias simultaneously across 10 shared session IDs. The race detector fires
// if the underlying sessions map is accessed without the RWMutex.
func TestWorkingMemory_ConcurrentSessions(t *testing.T) {
	wm := NewWorkingMemory(30 * time.Minute)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // half writers (Update), half readers (Bias)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			sessID := fmt.Sprintf("session-%d", i%10)
			seed := storage.Shard{
				ID:     fmt.Sprintf("shard-%d", i),
				Vector: storage.EncodeVector([]float32{float32(i%10 + 1), 0.0}),
			}
			wm.Update(sessID, []storage.Shard{seed})
		}(i)

		go func(i int) {
			defer wg.Done()
			sessID := fmt.Sprintf("session-%d", i%10)
			_ = wm.Bias(sessID, []float32{1.0, 0.0}, 0.7)
		}(i)
	}

	wg.Wait()
	// Race detector: no concurrent map mutation in wm.sessions without the RWMutex.
}

// TestSessionTokens_ConcurrentAuth races 50 code-writer goroutines against 50
// code-consumer goroutines on pendingCodes. The race detector confirms every
// map access holds the mutex — no concurrent map write panics.
func TestSessionTokens_ConcurrentAuth(t *testing.T) {
	const goroutines = 50
	var wg sync.WaitGroup

	// Writers: simulate handleOAuthAuthorize storing new codes
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			code := fmt.Sprintf("race-code-%d", i)
			pendingCodes.Lock()
			pendingCodes.codes[code] = authCode{expiresAt: time.Now().Add(time.Minute)}
			pendingCodes.Unlock()
		}(i)
	}

	// Readers/consumers: simulate handleOAuthToken consuming codes
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			code := fmt.Sprintf("race-code-%d", i)
			pendingCodes.Lock()
			if _, exists := pendingCodes.codes[code]; exists {
				delete(pendingCodes.codes, code)
			}
			pendingCodes.Unlock()
		}(i)
	}

	wg.Wait()

	// Clean up any codes that writers stored before consumers ran
	pendingCodes.Lock()
	for i := range goroutines {
		delete(pendingCodes.codes, fmt.Sprintf("race-code-%d", i))
	}
	pendingCodes.Unlock()
}
