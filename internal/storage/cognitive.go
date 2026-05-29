package storage

import (
	"math"
	"time"
)

// CalculateACTRActivation implements the ACT-R Base-Level Learning equation:
//
//	A(m) = ln( sum( t_i ^ -d ) ) + epsilon
//
// where t_i is the elapsed time (in seconds) since retrieval i, d is the decay
// parameter (typically 0.5), and epsilon is a noise floor ensuring zero-history
// shards still have minimal activation.
//
// This replaces raw use_count as a vitality signal — it weighs *when* retrievals
// happened, not just how many. A shard retrieved 3 times this week activates
// higher than one retrieved 10 times months ago.
func CalculateACTRActivation(history []time.Time, decayD float64) float64 {
	const epsilon = 0.1 // Minimum activation for zero-history shards

	if len(history) == 0 {
		return epsilon
	}

	now := time.Now()
	var sum float64
	for _, t := range history {
		elapsed := now.Sub(t).Seconds()
		if elapsed < 1.0 {
			elapsed = 1.0 // Floor at 1s to prevent Pow(0, -0.5) = Inf
		}
		sum += math.Pow(elapsed, -decayD)
	}

	return math.Log(sum) + epsilon
}

// SurvivalScoreV4 computes the cognitive-science-backed survival score:
//
//	S = min(95, (D * (C+1.0) * 10 * A(m) * Sal) / e^(dt / A(m)))
//
// Where:
//   - D = bond density (link count)
//   - C = centrality (PageRank)
//   - A(m) = ACT-R activation from retrieval history
//   - Sal = LLM-scored salience [0.1, 1.0]
//   - dt = hours since last use
//
// The exponential denominator models Ebbinghaus forgetting — shards with higher
// activation decay slower because A(m) appears in the exponent's denominator.
func SurvivalScoreV4(density int, centrality, salience float64, history []time.Time, lastUsed time.Time) float64 {
	activation := CalculateACTRActivation(history, 0.5)

	// Guard: floor activation to prevent division-by-near-zero in the exponent
	if activation < 0.01 {
		activation = 0.01
	}

	hoursSince := time.Since(lastUsed).Hours()
	if hoursSince < 1.0 {
		hoursSince = 1.0
	}

	numerator := float64(density) * (centrality + 1.0) * 10.0 * activation * salience
	denominator := math.Exp(hoursSince / activation)

	score := numerator / denominator

	// Clamp to [0, 95] — core shards get 100 externally
	if score > 95.0 {
		score = 95.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}

// SurvivalScoreV35 preserves the original v3.5 formula from packData() for
// benchmark comparison. This is the linear-decay, raw-use_count model.
//
//	Score = (links * (PageRank + 1.0) * 10 * vitality) / hoursSince
//	vitality = clamp(1.0 + useCount*0.1, 1.0, 5.0)
func SurvivalScoreV35(density int, centrality float64, useCount int, lastUsed time.Time) float64 {
	hoursSince := time.Since(lastUsed).Hours()
	if hoursSince < 1.0 {
		hoursSince = 1.0
	}

	vitality := 1.0
	if useCount > 1 {
		vitality = 1.0 + (float64(useCount) * 0.1)
		if vitality > 5.0 {
			vitality = 5.0
		}
	}

	score := (float64(density) * (centrality + 1.0) * 10.0 * vitality) / hoursSince

	if score > 95.0 {
		score = 95.0
	}
	if score < 0.0 {
		score = 0.0
	}
	return score
}
