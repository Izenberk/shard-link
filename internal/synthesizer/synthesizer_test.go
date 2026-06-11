package synthesizer

import (
	"testing"

	"github.com/izenberk/shard-link/internal/storage"
)

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
