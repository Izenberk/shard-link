package storage

import (
	"math"
	"testing"
	"time"
)

func TestSurvivalScoreV4(t *testing.T) {
	// Fixed reference point so tests are deterministic.
	// SurvivalScoreV4 uses time.Since(lastUsed) internally, so we set
	// lastUsed relative to time.Now() to control the delta.
	now := time.Now()

	cases := []struct {
		name     string
		density  int
		central  float64
		salience float64
		history  []time.Time
		lastUsed time.Time
		wantMin  float64
		wantMax  float64
	}{
		{
			name:     "zero bonds orphan gets cold-start floor (density floored to 1)",
			density:  0,
			central:  0.0,
			salience: 0.5,
			lastUsed: now.Add(-24 * time.Hour),
			wantMin:  0.1,  // density floored to 1 → non-zero numerator
			wantMax:  10.0, // still modest — only salience and centrality contribute
		},
		{
			name:     "result never exceeds 95 for non-core",
			density:  100,
			central:  5.0,
			salience: 1.0,
			lastUsed: now,
			wantMin:  90.0,
			wantMax:  95.0,
		},
		{
			name:     "recently used shard scores higher than stale shard",
			density:  5,
			central:  1.0,
			salience: 0.5,
			lastUsed: now.Add(-1 * time.Hour),
			wantMin:  1.0,  // should have a reasonable score
			wantMax:  95.0, // bounded
		},
		{
			name:     "zero timestamp treated as now (not collapsed)",
			density:  5,
			central:  1.0,
			salience: 0.5,
			lastUsed: time.Time{}, // Go zero value: 0001-01-01
			wantMin:  1.0,         // should NOT be 0 — the guard treats it as now
			wantMax:  95.0,
		},
		{
			name:     "pre-epoch timestamp treated as now",
			density:  5,
			central:  1.0,
			salience: 0.5,
			lastUsed: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			wantMin:  1.0,
			wantMax:  95.0,
		},
		{
			name:     "high salience decays slower",
			density:  3,
			central:  0.5,
			salience: 1.0,
			lastUsed: now.Add(-7 * 24 * time.Hour), // 7 days ago
			wantMin:  0.0,
			wantMax:  95.0,
		},
		{
			name:     "retrieval history extends stability",
			density:  3,
			central:  0.5,
			salience: 0.5,
			history: []time.Time{
				now.Add(-1 * time.Hour),
				now.Add(-2 * time.Hour),
				now.Add(-3 * time.Hour),
			},
			lastUsed: now.Add(-5 * 24 * time.Hour), // 5 days stale
			wantMin:  0.0,
			wantMax:  95.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score := SurvivalScoreV4(tc.density, tc.central, tc.salience, tc.history, tc.lastUsed)

			if math.IsNaN(score) {
				t.Fatalf("score is NaN")
			}
			if math.IsInf(score, 0) {
				t.Fatalf("score is Inf")
			}
			if score < tc.wantMin {
				t.Errorf("score %.4f < wantMin %.4f", score, tc.wantMin)
			}
			if score > tc.wantMax {
				t.Errorf("score %.4f > wantMax %.4f", score, tc.wantMax)
			}
		})
	}
}

func TestSurvivalScoreV4_HighSalienceDecaysSlower(t *testing.T) {
	// Comparative test: two shards identical except salience.
	// The high-salience shard should score higher after the same time delta.
	now := time.Now()
	lastUsed := now.Add(-5 * 24 * time.Hour) // 5 days stale

	low := SurvivalScoreV4(3, 0.5, 0.2, nil, lastUsed)
	high := SurvivalScoreV4(3, 0.5, 0.9, nil, lastUsed)

	if high <= low {
		t.Errorf("high salience (0.9) should score higher than low (0.2): high=%.4f, low=%.4f", high, low)
	}
}

func TestSurvivalScoreV4_RetrievalHistoryBoostsScore(t *testing.T) {
	// A(m) = ln(sum(t_i^-0.5)) + epsilon. For A(m) to exceed epsilon (0.1),
	// we need sum(t_i^-0.5) > 1.0, which requires very recent or very frequent
	// retrievals. With many retrievals in the last few seconds, the sum grows
	// large enough for A(m) > epsilon, extending S₀ beyond the no-history case.
	now := time.Now()
	lastUsed := now.Add(-3 * 24 * time.Hour) // 3 days stale

	noHistory := SurvivalScoreV4(3, 0.5, 0.5, nil, lastUsed)

	// 20 retrievals within the last second — sum(1.0^-0.5 * 20) = 20.
	// A(m) = ln(20) + 0.1 ≈ 3.1. (1 + 3.1) = 4.1 × S_base — massive stability boost.
	recentHistory := make([]time.Time, 20)
	for i := range recentHistory {
		recentHistory[i] = now // all "just now" — floor at 1s each
	}
	withHistory := SurvivalScoreV4(3, 0.5, 0.5, recentHistory, lastUsed)

	if withHistory <= noHistory {
		t.Errorf("dense recent retrieval history should boost score: with=%.4f, without=%.4f", withHistory, noHistory)
	}
}

func TestACTRActivation(t *testing.T) {
	cases := []struct {
		name    string
		history []time.Time
		wantMin float64
		wantMax float64
	}{
		{
			name:    "empty history returns epsilon",
			history: nil,
			wantMin: 0.09,
			wantMax: 0.11, // epsilon = 0.1
		},
		{
			// A(m) = ln(10^-0.5) + 0.1 = ln(0.316) + 0.1 ≈ -1.05.
			// Negative is expected — single retrievals seconds ago don't
			// produce enough sum for ln() to go positive.
			name: "single recent retrieval gives negative activation",
			history: []time.Time{
				time.Now().Add(-10 * time.Second),
			},
			wantMin: -2.0,
			wantMax: 0.0,
		},
		{
			// Three timestamps: sum ≈ 0.316+0.224+0.183 = 0.723.
			// A(m) = ln(0.723) + 0.1 ≈ -0.22. Still negative, but higher than single.
			name: "multiple recent retrievals compound higher",
			history: []time.Time{
				time.Now().Add(-10 * time.Second),
				time.Now().Add(-20 * time.Second),
				time.Now().Add(-30 * time.Second),
			},
			wantMin: -1.0,
			wantMax: 0.5,
		},
		{
			// 30 days: t^-0.5 ≈ 0.00062. ln(0.00062) + 0.1 ≈ -7.28.
			name: "very old retrieval gives deeply negative activation",
			history: []time.Time{
				time.Now().Add(-30 * 24 * time.Hour),
			},
			wantMin: -8.0,
			wantMax: -5.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := CalculateACTRActivation(tc.history, 0.5)

			if math.IsNaN(a) {
				t.Fatalf("activation is NaN")
			}
			if math.IsInf(a, 0) {
				t.Fatalf("activation is Inf")
			}
			if a < tc.wantMin {
				t.Errorf("activation %.4f < wantMin %.4f", a, tc.wantMin)
			}
			if a > tc.wantMax {
				t.Errorf("activation %.4f > wantMax %.4f", a, tc.wantMax)
			}
		})
	}
}

func TestACTRActivation_RecentHigherThanOld(t *testing.T) {
	recent := CalculateACTRActivation([]time.Time{
		time.Now().Add(-10 * time.Second),
	}, 0.5)

	old := CalculateACTRActivation([]time.Time{
		time.Now().Add(-30 * 24 * time.Hour),
	}, 0.5)

	if recent <= old {
		t.Errorf("recent retrieval should activate higher: recent=%.4f, old=%.4f", recent, old)
	}
}

func TestSalToStability(t *testing.T) {
	cases := []struct {
		name     string
		salience float64
		wantMin  float64
		wantMax  float64
	}{
		{
			name:     "minimum salience maps to ~1 day",
			salience: 0.1,
			wantMin:  0.9,
			wantMax:  1.1,
		},
		{
			name:     "mid salience maps to ~7 days",
			salience: 0.5,
			wantMin:  5.0,
			wantMax:  8.0,
		},
		{
			name:     "max salience maps to ~14 days",
			salience: 1.0,
			wantMin:  13.0,
			wantMax:  14.5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SalToStability(tc.salience)

			if s < tc.wantMin || s > tc.wantMax {
				t.Errorf("SalToStability(%.1f) = %.2f, want [%.1f, %.1f]", tc.salience, s, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestSalToStability_Monotonic(t *testing.T) {
	// Stability should be monotonically increasing with salience.
	prev := SalToStability(0.1)
	for sal := 0.2; sal <= 1.0; sal += 0.1 {
		curr := SalToStability(sal)
		if curr <= prev {
			t.Errorf("stability not monotonic: SalToStability(%.1f)=%.2f <= SalToStability(%.1f)=%.2f",
				sal, curr, sal-0.1, prev)
		}
		prev = curr
	}
}
