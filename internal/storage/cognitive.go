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

// SalToStability maps salience [0.1, 1.0] to a baseline stability in days,
// using FSRS-calibrated anchors:
//
//	Sal 0.1 → 1 day   (ephemera — forget quickly)
//	Sal 0.5 → ~6.8 days (project context — survive ~1 week)
//	Sal 1.0 → 14 days  (critical knowledge — survive ~2 weeks)
//
// Linear interpolation: S_base = 1.0 + (Sal - 0.1) * 14.44
func SalToStability(salience float64) float64 {
	return 1.0 + (salience-0.1)*14.44
}

// SurvivalScoreV4 computes the v4.1 cognitive-science-backed survival score:
//
//	S = min(95, (D * (C+1.0) * 10 * Sal) / e^(Δt_days / S₀))
//	S₀ = S_base(Sal) * (1 + A(m))
//
// Where:
//   - D = bond density (link count)
//   - C = centrality (PageRank)
//   - Sal = LLM-scored salience [0.1, 1.0]
//   - S_base = salience-mapped initial stability in days (FSRS-calibrated)
//   - A(m) = ACT-R activation — multiplies stability so retrieved shards decay slower
//   - Δt_days = time since last use in days
//
// v4.0 → v4.1 changes:
//  1. Replaced fixed τ=168h with S_base(Sal) — salience now directly controls
//     the decay window instead of only scaling the numerator.
//  2. Time unit switched from hours to days — aligns with FSRS stability semantics.
//  3. A(m) moved from raw multiplier (A(m) * τ) to stability extension (1 + A(m)).
//     At A(m)=0 the shard still gets its full S_base window. Retrieval history
//     extends it (A(m)=1.5 → 2.5× base), but can never collapse it to near-zero
//     the way v4.0 did when clustered timestamps killed A(m).
func SurvivalScoreV4(density int, centrality, salience float64, history []time.Time, lastUsed time.Time) float64 {
	activation := CalculateACTRActivation(history, 0.5)

	// Guard: floor activation so (1 + A(m)) >= 1.0
	if activation < 0.0 {
		activation = 0.0
	}

	// Guard: zero-timestamp defense. If lastUsed is zero (0001-01-01) or pre-epoch,
	// treat it as "just now" rather than letting time.Since() return ~740,000 days
	// which collapses the Ebbinghaus denominator to infinity.
	if lastUsed.IsZero() || lastUsed.Year() < 2000 {
		lastUsed = time.Now()
	}

	sBase := SalToStability(salience)
	s0 := sBase * (1.0 + activation)

	daysSince := time.Since(lastUsed).Hours() / 24.0
	if daysSince < 1.0/24.0 {
		daysSince = 1.0 / 24.0 // Floor at 1 hour
	}

	numerator := float64(density) * (centrality + 1.0) * 10.0 * salience
	denominator := math.Exp(daysSince / s0)

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
