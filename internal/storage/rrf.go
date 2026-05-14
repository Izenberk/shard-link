package storage

import (
	"sort"
)

// ReciprocalRankFusion combines multiple ranked lists of Shards into one.
// Scores are calculated as: Σ (1 / (rank + k))
// k is a smoothing constant (standard is 60.0).
func ReciprocalRankFusion(limit int, k float64, lists ...[]Shard) []Shard {
	if len(lists) == 0 {
		return nil
	}

	scores := make(map[string]float64)
	shardMap := make(map[string]Shard)

	// 1. Accumulate scores across all lists
	for _, list := range lists {
		for rank, shard := range list {
			// We use 1-based ranking for the math
			scores[shard.ID] += 1.0 / (float64(rank+1) + k)
			shardMap[shard.ID] = shard
		}
	}

	// 2. Prepare for sorting
	type scoredShard struct {
		id    string
		score float64
	}
	var sorted []scoredShard
	for id, score := range scores {
		sorted = append(sorted, scoredShard{id, score})
	}

	// 3. Sort by fusion score descending
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].score == sorted[j].score {
			return sorted[i].id < sorted[j].id // Consistent tie-break
		}
		return sorted[i].score > sorted[j].score
	})

	// 4. Collect the top results up to the limit
	var result []Shard
	for i := 0; i < len(sorted) && i < limit; i++ {
		result = append(result, shardMap[sorted[i].id])
	}

	return result
}
