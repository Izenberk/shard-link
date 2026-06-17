package synthesizer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/izenberk/shard-link/internal/storage"
)

// mockSynthRepo implements storage.Repository for Synthesizer gate tests.
// Only SyncBonds needs real tracking — all other methods are no-ops.
type mockSynthRepo struct {
	syncBondsCalled int
}

func (m *mockSynthRepo) SyncBonds(_ context.Context, _ float64) (int, error) {
	m.syncBondsCalled++
	return 0, nil
}
func (m *mockSynthRepo) SaveShard(_ context.Context, _ storage.Shard) error       { return nil }
func (m *mockSynthRepo) FindResonant(_ context.Context, _ []byte, _ int, _ bool) ([]storage.Shard, error) {
	return nil, nil
}
func (m *mockSynthRepo) FindText(_ context.Context, _ string, _ int, _ bool) ([]storage.Shard, error) {
	return nil, nil
}
func (m *mockSynthRepo) FindHybrid(_ context.Context, _ string, _ []byte, _ int) ([]storage.Shard, error) {
	return nil, nil
}
func (m *mockSynthRepo) GetAllShards(_ context.Context) ([]storage.Shard, error)    { return nil, nil }
func (m *mockSynthRepo) GetArchivedShards(_ context.Context) ([]storage.Shard, error) {
	return nil, nil
}
func (m *mockSynthRepo) GetShardByID(_ context.Context, _ string) (storage.Shard, error) {
	return storage.Shard{}, nil
}
func (m *mockSynthRepo) GetCoreShards(_ context.Context) ([]storage.Shard, error) { return nil, nil }
func (m *mockSynthRepo) ArchiveShard(_ context.Context, _ string) error           { return nil }
func (m *mockSynthRepo) GetCount(_ context.Context) (int, error)                  { return 0, nil }
func (m *mockSynthRepo) GetEvictionCandidates(_ context.Context, _ int) ([]string, error) {
	return nil, nil
}
func (m *mockSynthRepo) SaveBond(_ context.Context, _ storage.ShardBond) error { return nil }
func (m *mockSynthRepo) GetAllBonds(_ context.Context) ([]storage.ShardBond, error) {
	return nil, nil
}
func (m *mockSynthRepo) GetBondCount(_ context.Context) (int, error)      { return 0, nil }
func (m *mockSynthRepo) GetCommunityCount(_ context.Context) (int, error) { return 0, nil }
func (m *mockSynthRepo) SearchGraph(_ context.Context, _ []byte, _ int, _ bool) ([]storage.Shard, []storage.ShardBond, error) {
	return nil, nil, nil
}
func (m *mockSynthRepo) GetGraphData(_ context.Context) ([]storage.Shard, []storage.ShardBond, error) {
	return nil, nil, nil
}
func (m *mockSynthRepo) CalculateCommunities(_ context.Context) (int, []int64, error) {
	return 0, nil, nil
}
func (m *mockSynthRepo) GetShardsByCommunity(_ context.Context, _ int64) ([]storage.Shard, error) {
	return nil, nil
}
func (m *mockSynthRepo) PruneStaleSummaries(_ context.Context) (int, error) { return 0, nil }
func (m *mockSynthRepo) ReinforceShards(_ context.Context, _ []string) error { return nil }
func (m *mockSynthRepo) GetRecentShards(_ context.Context, _ int, _ string) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *mockSynthRepo) GetShardsByCategory(_ context.Context, _ string, _ int) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *mockSynthRepo) GetAtRiskShards(_ context.Context, _ int, _ float64) ([]storage.ShardMetadata, error) {
	return nil, nil
}
func (m *mockSynthRepo) UpdateShard(_ context.Context, _ string, _ storage.ShardUpdate) error {
	return nil
}
func (m *mockSynthRepo) DeleteShard(_ context.Context, _ string) error { return nil }
func (m *mockSynthRepo) GetSurvivalDistribution(_ context.Context) (map[string]int, error) {
	return nil, nil
}
func (m *mockSynthRepo) Optimize(_ context.Context) error { return nil }
func (m *mockSynthRepo) Close() error                     { return nil }

// --- filterSummarizable ---

func TestFilterSummarizable_RemovesContractShards(t *testing.T) {
	input := []storage.Shard{
		{ID: "mem-1", Category: "memory", Content: "Go concurrency patterns"},
		{ID: "con-1", Category: "contract", Content: "Hub: implement retry logic"},
		{ID: "core-1", Category: "core", Content: "User prefers Go"},
		{ID: "con-2", Category: "contract", Content: "Hub: add rate limiter"},
		{ID: "ses-1", Category: "session", Content: "Working on synthesizer"},
	}

	got := filterSummarizable(input)

	if len(got) != 3 {
		t.Fatalf("expected 3 shards after filter, got %d", len(got))
	}

	for _, s := range got {
		if s.Category == "contract" {
			t.Errorf("contract shard %q should have been filtered out", s.ID)
		}
	}

	// Verify the order is preserved.
	want := []string{"mem-1", "core-1", "ses-1"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("index %d: expected %s, got %s", i, id, got[i].ID)
		}
	}
}

func TestFilterSummarizable_AllContract(t *testing.T) {
	input := []storage.Shard{
		{ID: "con-1", Category: "contract", Content: "spec A"},
		{ID: "con-2", Category: "contract", Content: "spec B"},
	}

	got := filterSummarizable(input)

	if len(got) != 0 {
		t.Fatalf("expected 0 shards when all are contract, got %d", len(got))
	}
}

func TestFilterSummarizable_NoContract(t *testing.T) {
	input := []storage.Shard{
		{ID: "mem-1", Category: "memory", Content: "fact A"},
		{ID: "mem-2", Category: "memory", Content: "fact B"},
		{ID: "core-1", Category: "core", Content: "identity"},
	}

	got := filterSummarizable(input)

	if len(got) != 3 {
		t.Fatalf("expected all 3 shards preserved, got %d", len(got))
	}
}

func TestFilterSummarizable_Empty(t *testing.T) {
	got := filterSummarizable([]storage.Shard{})

	if len(got) != 0 {
		t.Fatalf("expected 0 shards for empty input, got %d", len(got))
	}
}

// --- buildSummaryPrompt ---

// TestBuildSummaryPrompt_Header verifies the prompt contains the required structural
// fields: community ID, member count, and the fragments section header.
func TestBuildSummaryPrompt_Header(t *testing.T) {
	members := []storage.Shard{
		{ID: "mem-1", Category: "memory", Content: "fact one"},
		{ID: "mem-2", Category: "memory", Content: "fact two"},
		{ID: "mem-3", Category: "memory", Content: "fact three"},
	}

	prompt := buildSummaryPrompt(42, members)

	checks := []string{
		"Community ID: 42",
		"Member count: 3",
		"Fragments (ordered by centrality)",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

// TestBuildSummaryPrompt_CharBudget verifies the 12 000-char budget is respected
// even when member content is deliberately large.
func TestBuildSummaryPrompt_CharBudget(t *testing.T) {
	var members []storage.Shard
	for i := 0; i < 5; i++ {
		members = append(members, storage.Shard{
			ID:       fmt.Sprintf("shard-%d", i),
			Category: "memory",
			Content:  strings.Repeat("x", 4000), // 5 × 4 000 = 20 000 chars total
		})
	}

	prompt := buildSummaryPrompt(1, members)

	// Allow modest header overhead above the 12 000-char content budget.
	if len(prompt) > 12500 {
		t.Errorf("prompt exceeds char budget: len=%d", len(prompt))
	}
}

// TestBuildSummaryPrompt_MemberCap verifies at most 15 members are included
// even when more are passed in.
func TestBuildSummaryPrompt_MemberCap(t *testing.T) {
	var members []storage.Shard
	for i := 0; i < 20; i++ {
		members = append(members, storage.Shard{
			ID:       fmt.Sprintf("shard-%d", i),
			Category: "memory",
			Content:  "short content",
		})
	}

	prompt := buildSummaryPrompt(99, members)

	// Each member entry starts with "--- [shard-N]"
	count := strings.Count(prompt, "--- [shard-")
	if count != 15 {
		t.Errorf("expected 15 members in prompt (cap), found %d", count)
	}
}

// --- performSynthesis dynamic gate ---

// TestDynamicGate_BelowThreshold verifies that synthesis is deferred when dirty < min
// and not enough time has passed since last synthesis.
func TestDynamicGate_BelowThreshold(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount()) // reset + stamp now

	for i := 0; i < 3; i++ { // 3 dirty < threshold of 5
		storage.MarkShardDirty()
	}
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	mock := &mockSynthRepo{}
	syn := &Synthesizer{
		vessel:         mock,
		minDirtyShards: 5,
		maxDeferral:    24 * time.Hour, // just reset above, so deferred ≈ 0 << 24h
	}

	syn.performSynthesis(context.Background())

	if mock.syncBondsCalled != 0 {
		t.Errorf("expected SyncBonds not called when dirty=3 < min=5 and not deferred, got %d calls", mock.syncBondsCalled)
	}
}

// TestDynamicGate_ThresholdReached verifies that synthesis fires when dirty >= minDirtyShards.
func TestDynamicGate_ThresholdReached(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount())

	for i := 0; i < 5; i++ { // exactly at threshold
		storage.MarkShardDirty()
	}
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	mock := &mockSynthRepo{}
	syn := &Synthesizer{
		vessel:         mock,
		minDirtyShards: 5,
		maxDeferral:    24 * time.Hour,
	}

	syn.performSynthesis(context.Background())

	if mock.syncBondsCalled != 1 {
		t.Errorf("expected SyncBonds called once when dirty=5 >= min=5, got %d", mock.syncBondsCalled)
	}
}

// TestDynamicGate_MaxDeferralExceeded verifies that synthesis fires when the max
// deferral ceiling is exceeded, even if dirty < minDirtyShards.
func TestDynamicGate_MaxDeferralExceeded(t *testing.T) {
	storage.ConsumeDirtyShards(storage.DirtyShardCount())

	storage.MarkShardDirty() // 1 dirty < threshold of 5
	defer storage.ConsumeDirtyShards(storage.DirtyShardCount())

	mock := &mockSynthRepo{}
	syn := &Synthesizer{
		vessel:         mock,
		minDirtyShards: 5,
		maxDeferral:    0, // zero duration: time.Since(anything) >= 0 always true
	}

	syn.performSynthesis(context.Background())

	if mock.syncBondsCalled != 1 {
		t.Errorf("expected SyncBonds called once when max deferral exceeded, got %d", mock.syncBondsCalled)
	}
}
