package storage

import (
	"testing"
)

// --- ReciprocalRankFusion ---

// TestRRF_Overlap verifies that a shard appearing in multiple lists accumulates
// score from each list, ranking it above shards that appear only once.
func TestRRF_Overlap(t *testing.T) {
	shardA := Shard{ID: "a", Content: "alpha"}
	shardB := Shard{ID: "b", Content: "beta"}
	shardC := Shard{ID: "c", Content: "gamma"}

	list1 := []Shard{shardA, shardB} // A rank 1, B rank 2
	list2 := []Shard{shardC, shardA} // C rank 1, A rank 2

	results := ReciprocalRankFusion(3, 60.0, list1, list2)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// A scores from both lists: 1/(1+60) + 1/(2+60) = 0.01639 + 0.01613 = 0.03252.
	// C scores from one list: 1/(1+60) = 0.01639.
	// B scores from one list: 1/(2+60) = 0.01613.
	// Order: A > C > B.
	if results[0].ID != "a" {
		t.Errorf("expected 'a' first (appears in both lists), got %q", results[0].ID)
	}
}

// TestRRF_NoOverlap verifies that disjoint lists are merged by rank score:
// all rank-1 items tie above all rank-2 items.
func TestRRF_NoOverlap(t *testing.T) {
	list1 := []Shard{{ID: "a"}, {ID: "b"}}
	list2 := []Shard{{ID: "c"}, {ID: "d"}}

	results := ReciprocalRankFusion(4, 60.0, list1, list2)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	// Rank-1 items (a, c) each score 1/(1+60). Rank-2 items (b, d) score 1/(2+60).
	// Top-2 must be a and c (in alphabetical tie-break order).
	topIDs := map[string]bool{results[0].ID: true, results[1].ID: true}
	if !topIDs["a"] || !topIDs["c"] {
		t.Errorf("expected rank-1 items {a, c} in top-2, got {%s, %s}", results[0].ID, results[1].ID)
	}
}

// TestRRF_LimitTruncates verifies the result list is capped at the given limit.
func TestRRF_LimitTruncates(t *testing.T) {
	list := []Shard{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}

	results := ReciprocalRankFusion(3, 60.0, list)

	if len(results) != 3 {
		t.Errorf("expected 3 results (limit=3), got %d", len(results))
	}
}

// TestRRF_K60Smoothing verifies the k=60 bias property: rank differences are compressed,
// making rank-1 and rank-2 scores close in value rather than order-of-magnitude apart.
func TestRRF_K60Smoothing(t *testing.T) {
	scoreRank1 := 1.0 / (1.0 + 60.0)
	scoreRank2 := 1.0 / (2.0 + 60.0)
	ratio := scoreRank1 / scoreRank2
	// With k=60, the ratio should be very close to 1.0 (high smoothing).
	if ratio > 1.1 {
		t.Errorf("k=60 not smoothing enough: rank1/rank2 ratio=%.4f, expected ≤ 1.1", ratio)
	}
}

// TestRRF_EmptyInput verifies nil is returned when no lists are provided.
func TestRRF_EmptyInput(t *testing.T) {
	results := ReciprocalRankFusion(10, 60.0)
	if results != nil {
		t.Errorf("expected nil for empty input, got %v", results)
	}
}

// --- MaximalMarginalRelevance ---

// TestMMR_PureRelevance verifies that lambda=1.0 degenerates to pure relevance ranking.
// The most query-relevant shards should appear first, regardless of redundancy.
func TestMMR_PureRelevance(t *testing.T) {
	query := EncodeVector([]float32{1.0, 0.0})

	// A and B are identically aligned with the query (sim=1.0). C is orthogonal (sim=0.0).
	shardA := Shard{ID: "a", Vector: EncodeVector([]float32{1.0, 0.0})}
	shardB := Shard{ID: "b", Vector: EncodeVector([]float32{1.0, 0.0})}
	shardC := Shard{ID: "c", Vector: EncodeVector([]float32{0.0, 1.0})}

	results := MaximalMarginalRelevance(query, []Shard{shardA, shardB, shardC}, 2, 1.0)

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// With lambda=1.0 (pure relevance): A and B both score 1.0, C scores 0.0.
	// C must not appear in the top-2 result.
	for _, s := range results {
		if s.ID == "c" {
			t.Error("lambda=1.0: irrelevant shard 'c' (sim=0) should not be in top-2")
		}
	}
}

// TestMMR_PureDiversity verifies that lambda=0.0 maximises diversity.
// After selecting the first shard, the next pick should be the one least similar
// to the already-selected set, not the most query-relevant.
func TestMMR_PureDiversity(t *testing.T) {
	query := EncodeVector([]float32{1.0, 0.0})

	// A and B are identical (both perfectly aligned with query).
	// C is orthogonal — zero similarity to A.
	shardA := Shard{ID: "a", Vector: EncodeVector([]float32{1.0, 0.0})}
	shardB := Shard{ID: "b", Vector: EncodeVector([]float32{1.0, 0.0})} // redundant with A
	shardC := Shard{ID: "c", Vector: EncodeVector([]float32{0.0, 1.0})} // orthogonal to A

	results := MaximalMarginalRelevance(query, []Shard{shardA, shardB, shardC}, 2, 0.0)

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// lambda=0.0: first pick = A (first in array, all scores tie at 0).
	// Second pick: B has penalty=sim(B,A)=1.0 → score=-1.0.
	//              C has penalty=sim(C,A)=0.0 → score= 0.0.
	// C wins the second slot.
	if results[1].ID != "c" {
		t.Errorf("lambda=0: second pick should be 'c' (orthogonal, zero penalty), got %q", results[1].ID)
	}
}

// TestMMR_EmptyInput verifies nil is returned when no candidates are provided.
func TestMMR_EmptyInput(t *testing.T) {
	query := EncodeVector([]float32{1.0, 0.0})
	results := MaximalMarginalRelevance(query, nil, 5, 0.7)
	if results != nil {
		t.Errorf("expected nil for empty candidates, got %v", results)
	}
}

// --- Vector Round-Trip ---

// TestVectorRoundTrip verifies that EncodeVector → DecodeVector preserves all
// float32 values exactly (bit-identical, no precision loss from the encoding).
func TestVectorRoundTrip(t *testing.T) {
	original := []float32{1.234, -0.567, 0.0, 3.14159}

	encoded := EncodeVector(original)
	decoded := DecodeVector(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: original %d, decoded %d", len(original), len(decoded))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("index %d: round-trip failed — original %.6f, decoded %.6f",
				i, original[i], decoded[i])
		}
	}
}
