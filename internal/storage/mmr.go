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
	qVec := decodeVector(queryVector)
	if qVec == nil {
		return candidates // Fallback if query vector is invalid
	}

	// 2. Pre-decode candidate vectors and calculate base similarities
	type candidateData struct {
		shard    Shard
		vector   []float32
		baseSim  float64
		selected bool
	}

	var data []candidateData
	for _, c := range candidates {
		v := decodeVector(c.Vector)
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

	var selected []Shard
	
	// 3. Iteratively select the best shard using the MMR formula
	for i := 0; i < limit && len(selected) < len(data); i++ {
		bestScore := math.Inf(-1)
		bestIdx := -1

		for j := range data {
			if data[j].selected {
				continue
			}

			// Calculate penalty (max similarity to already selected items)
			maxSimToSelected := 0.0
			for _, s := range selected {
				sVec := decodeVector(s.Vector)
				if sVec != nil {
					sim := cosineSimilarity(data[j].vector, sVec)
					if sim > maxSimToSelected {
						maxSimToSelected = sim
					}
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
		}
	}

	return selected
}
