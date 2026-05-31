package storage

import (
	"testing"
	"time"
)

// BenchmarkSurvivalScoreV4 measures the per-shard cost of the Survival Formula.
// The Janitor calls this once per non-core shard every eviction cycle.
// At 1000 shards and 15-minute intervals, this runs ~67 times/second sustained.
//
// Run: go test -bench=BenchmarkSurvivalScoreV4 -benchmem -count=5 ./internal/storage/
func BenchmarkSurvivalScoreV4(b *testing.B) {
	now := time.Now()
	history := []time.Time{
		now.Add(-1 * time.Hour),
		now.Add(-3 * time.Hour),
		now.Add(-6 * time.Hour),
		now.Add(-12 * time.Hour),
		now.Add(-24 * time.Hour),
	}
	lastUsed := now.Add(-2 * time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SurvivalScoreV4(5, 0.8, 0.7, history, lastUsed)
	}
}

// BenchmarkSurvivalScoreV4_EmptyHistory measures the fast path (no retrieval history).
// Most shards in a fresh mesh hit this path.
func BenchmarkSurvivalScoreV4_EmptyHistory(b *testing.B) {
	lastUsed := time.Now().Add(-6 * time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SurvivalScoreV4(3, 0.5, 0.5, nil, lastUsed)
	}
}

// BenchmarkSurvivalScoreV4_FullHistory measures worst case — 20 retrieval timestamps.
func BenchmarkSurvivalScoreV4_FullHistory(b *testing.B) {
	now := time.Now()
	history := make([]time.Time, 20)
	for i := range history {
		history[i] = now.Add(-time.Duration(i+1) * time.Hour)
	}
	lastUsed := now.Add(-30 * time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SurvivalScoreV4(10, 2.0, 0.9, history, lastUsed)
	}
}

// BenchmarkACTRActivation measures the ACT-R base-level learning equation.
// Called once per shard inside SurvivalScoreV4.
func BenchmarkACTRActivation(b *testing.B) {
	now := time.Now()
	history := make([]time.Time, 20)
	for i := range history {
		history[i] = now.Add(-time.Duration(i+1) * time.Hour)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateACTRActivation(history, 0.5)
	}
}

// BenchmarkCosineSimilarity measures the hot inner loop of MMR and resonance checks.
// This is the single most-called function during search — O(candidates²) in MMR.
func BenchmarkCosineSimilarity(b *testing.B) {
	dim := 768
	a := make([]float32, dim)
	c := make([]float32, dim)
	for i := range a {
		a[i] = float32(i) / float32(dim)
		c[i] = float32(dim-i) / float32(dim)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cosineSimilarity(a, c)
	}
}

// BenchmarkDecodeVector measures the pool-backed vector decoding.
// Called per shard on every search path.
func BenchmarkDecodeVector(b *testing.B) {
	dim := 768
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}
	encoded := EncodeVector(vec)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := DecodeVector(encoded)
		if v != nil && cap(v) == vectorDimension {
			vectorPool.Put(v[:vectorDimension])
		}
	}
}

// BenchmarkMMR measures the full Maximal Marginal Relevance re-ranking pipeline.
// This is the most allocation-heavy search path.
func BenchmarkMMR(b *testing.B) {
	dim := 768
	queryVec := make([]float32, dim)
	queryVec[0] = 1.0
	queryBytes := EncodeVector(queryVec)

	// Build 20 candidate shards with varied vectors
	candidates := make([]Shard, 20)
	for i := range candidates {
		v := make([]float32, dim)
		v[i%dim] = 1.0
		candidates[i] = Shard{
			ID:      "shard-" + string(rune('a'+i)),
			Content: "test content",
			Vector:  EncodeVector(v),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MaximalMarginalRelevance(queryBytes, candidates, 10, 0.7)
	}
}

// BenchmarkRRF measures Reciprocal Rank Fusion across three result lists.
func BenchmarkRRF(b *testing.B) {
	list1 := make([]Shard, 10)
	list2 := make([]Shard, 10)
	list3 := make([]Shard, 10)
	for i := 0; i < 10; i++ {
		list1[i] = Shard{ID: "a-" + string(rune('0'+i))}
		list2[i] = Shard{ID: "b-" + string(rune('0'+i))}
		list3[i] = Shard{ID: "a-" + string(rune('0'+i))} // overlap with list1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ReciprocalRankFusion(10, 60.0, list1, list2, list3)
	}
}
