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

// BenchmarkSearchPipeline_Sub5ms measures the end-to-end compute cost of a
// search_all request with pre-fetched results from all three engines.
// Pipeline: encode query → dedup 3×10 results → RRF merge → MMR re-rank (top 10).
//
// SLA: full search_all < 5ms (includes SQL round-trips). Pure compute must stay well
// under 1ms to leave headroom. Run benchstat against this baseline before any change
// to the search, RRF, or MMR code paths.
//
// NOTE: BenchmarkMMR currently measures ~550µs for 20 candidates at 768-D. The 50µs
// contract in TEST_STRATEGY.md is aspirational — MMR is O(k×n) decode + cosine calls.
// Track the delta, not the absolute value.
//
// Run: go test -bench=BenchmarkSearchPipeline -benchmem -count=5 ./internal/storage/
func BenchmarkSearchPipeline_Sub5ms(b *testing.B) {
	const dim = 768

	// Pre-encode query vector (simulates embedder output)
	queryVec := make([]float32, dim)
	queryVec[0] = 1.0
	queryBytes := EncodeVector(queryVec)

	// Build 3 engine result lists: 10 shards each, 5 IDs overlap between text and graph.
	// Total unique: 25. This matches a typical search_all with limit=10 per engine.
	textResults := make([]Shard, 10)
	vectorResults := make([]Shard, 10)
	graphResults := make([]Shard, 10)
	for i := range 10 {
		v := make([]float32, dim)
		v[i%dim] = 1.0
		enc := EncodeVector(v)
		textResults[i] = Shard{ID: "t" + string(rune('a'+i)), Vector: enc}
		vectorResults[i] = Shard{ID: "v" + string(rune('a'+i)), Vector: enc}
		if i < 5 {
			graphResults[i] = Shard{ID: "t" + string(rune('a'+i)), Vector: enc} // overlaps text
		} else {
			graphResults[i] = Shard{ID: "g" + string(rune('a'+i-5)), Vector: enc}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Stage 1: map dedup — models handleSearchAll fan-out merge
		seen := make(map[string]Shard, 25)
		for _, sh := range textResults {
			seen[sh.ID] = sh
		}
		for _, sh := range vectorResults {
			seen[sh.ID] = sh
		}
		for _, sh := range graphResults {
			seen[sh.ID] = sh
		}

		// Stage 2: RRF across the three engine lists (FindHybrid / future search_all path)
		merged := ReciprocalRankFusion(20, 60.0, textResults, vectorResults, graphResults)

		// Stage 3: MMR re-rank top 10 from merged results
		_ = MaximalMarginalRelevance(queryBytes, merged, 10, 0.7)

		_ = len(seen) // prevent dead-code elimination of the dedup stage
	}
}
