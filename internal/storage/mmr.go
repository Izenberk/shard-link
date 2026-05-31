package storage

import (
	"math"
)

// MaximalMarginalRelevance re-ranks a set of candidate shards to ensure diversity.
// It balances relevance to the query with dissimilarity to already selected shards.
// lambda (0.0 to 1.0): 1.0 prioritizes pure relevance, 0.0 prioritizes pure diversity.
// Standard value is usually around 0.5 to 0.7.
func MaximalMarginalRelevance(queryVector []byte, candidates []Shard, limit int, lambda float64) []Shard {
	if len(candidates) == 0 {
		return nil
	}

	// 1. Decode the query vector once
	qVec := DecodeVector(queryVector)
	if qVec == nil {
		return candidates // Fallback if query vector is invalid
	}
	defer returnToPool(qVec)

	// 2. Pre-decode candidate vectors and calculate base similarities.
	// We decode each candidate vector ONCE here and cache it in the candidateData struct.
	// Before this fix, the inner loop at step 3 re-decoded selected shard vectors on every
	// iteration — O(selected × candidates) DecodeVector calls, none returned to pool.
	type candidateData struct {
		shard    Shard
		vector   []float32
		baseSim  float64
		selected bool
	}

	data := make([]candidateData, 0, len(candidates))
	for _, c := range candidates {
		v := DecodeVector(c.Vector)
		if v == nil {
			continue
		}
		data = append(data, candidateData{
			shard:    c,
			vector:   v,
			baseSim:  cosineSimilarity(qVec, v),
			selected: false,
		})
	}

	// Return all candidate vectors to the pool when we're done.
	// This is safe because we only use them inside this function scope.
	defer func() {
		for _, d := range data {
			returnToPool(d.vector)
		}
	}()

	var selected []Shard
	// Track selected indices so we can look up cached vectors in step 3.
	var selectedIdx []int

	// 3. Iteratively select the best shard using the MMR formula.
	// Key fix: use data[selectedIdx[k]].vector instead of re-decoding s.Vector.
	for i := 0; i < limit && len(selected) < len(data); i++ {
		bestScore := math.Inf(-1)
		bestIdx := -1

		for j := range data {
			if data[j].selected {
				continue
			}

			// Calculate penalty (max similarity to already selected items)
			maxSimToSelected := 0.0
			for _, sIdx := range selectedIdx {
				sim := cosineSimilarity(data[j].vector, data[sIdx].vector)
				if sim > maxSimToSelected {
					maxSimToSelected = sim
				}
			}

			// MMR Equation
			mmrScore := (lambda * data[j].baseSim) - ((1.0 - lambda) * maxSimToSelected)

			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = j
			}
		}

		if bestIdx != -1 {
			data[bestIdx].selected = true
			selected = append(selected, data[bestIdx].shard)
			selectedIdx = append(selectedIdx, bestIdx)
		}
	}

	return selected
}

// returnToPool returns a float32 slice to the vectorPool if it has the right capacity.
func returnToPool(v []float32) {
	if v != nil && cap(v) == vectorDimension {
		vectorPool.Put(v[:vectorDimension])
	}
}
