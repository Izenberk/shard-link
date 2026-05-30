# Shard-Link: Cognitive Science Research & Formula Upgrade Track

**Status:** PARTIALLY IMPLEMENTED — Priority 1 + Idea C shipped (HARDENING.md Phase 6.6). Priority 2 items remain as research candidates. **Authors:** BB & Brainy Bestie **Last Updated:** 2026-05-29 **Classification:** Nerd among nerds 🧠

This document captures research-grade ideas borrowed from cognitive science and academic memory systems literature. Priority 1 (Formula Core) and Idea C (Episodic Session Chains) have been **implemented** — see HARDENING.md Phase 6.6 and `internal/storage/cognitive.go`. Remaining items are Phase 2+ candidates requiring further benchmarking.

---

## Why This Doc Exists

Shard-Link's memory model is already grounded in neuroscience analogies — Janitor (forgetting), Synthesizer (consolidation), LTP (reinforcement). But the **math underneath** was engineered intuitively. This document brings the formulas into alignment with published cognitive science research to make the system provably more human-like.

---

## Current Baseline: Survival Formula v3.5

S \= min(95, (D · (C \+ 1.0) · 10 · V) / T)

Where:

  D \= Neural Density    — number of active semantic bonds

  C \= Relational Centrality — PageRank score

  V \= Vitality/Potentiation — frequency-weighted boost (+10% per hit, capped 5x)

  T \= Time Decay        — hours since last use (linear)

**Known weaknesses:**

- `T` is linear — real forgetting is exponential early, then flattens  
- `V` is a simple count multiplier — ignores *when* retrievals happened  
- No salience/importance weighting — a trivial shard and a critical one decay identically  
- No per-shard retrieval history — ACT-R shows history matters more than count

---

## Research Sources

| Paper | Key Contribution |
| :---- | :---- |
| Ebbinghaus (1885) / MemoryBank (Zhong et al. 2024\) | Exponential decay curve — steep drop early, flattens over time |
| ACT-R Architecture (Anderson et al.) / HAI Paper 2026 | Activation \= sum of power-law decayed past retrievals |
| SuperLocalMemory V3.3 (arXiv 2604.04514) | Salience \+ Bayesian trust modulate decay rate |
| Mathematical Modeling of Human Memory (ResearchGate 2025\) | Power Law of Forgetting, contextual drift via cosine similarity |
| SuperMemo SM2 / Spaced Repetition (PNAS 2019\) | Optimal review scheduling from retrieval history |

---

## Proposed Upgrade: Survival Formula v4.0

### The Formula

S \= min(95, (D · (C \+ 1.0) · 10 · A(m) · Sal) / e^(Δt / A(m)))

New terms replacing V and T:

  A(m)  \= ACT-R Activation Score  (replaces raw V)

  Sal   \= Salience Score           (new — LLM-assigned at save time)

  e^()  \= Exponential decay        (replaces linear T)

  Δt    \= Hours since last use     (same as before, new role)

### Term 1 — ACT-R Activation: `A(m)`

**Replaces:** raw `use_count` Vitality multiplier

**Formula:**

A(m) \= ln( Σ tᵢ⁻ᵈ ) \+ ε

Where:

  tᵢ  \= time (in hours) since each past retrieval event

  d   \= decay constant \= 0.5  (human-calibrated, per ACT-R)

  ε   \= small noise floor \= 0.1  (prevents ln(0))

  Σ   \= sum over last N retrieval timestamps (cap at 20 events)

**Why it's better than `use_count`:** A shard retrieved 10 times six months ago scores lower than one retrieved 3 times this week. Raw count cannot distinguish these cases. ACT-R activation can.

**Schema change required:**

// Add to Shard struct

RetrievalHistory \[\]time.Time \`json:"retrieval\_history"\` // last 20 timestamps

**Storage:** Store as JSON array in Neo4j shard node property. Cap at 20 entries (rolling window).

---

### Term 2 — Salience Score: `Sal`

**New field — no current equivalent**

**Formula:** `Sal ∈ [0.1, 1.0]`  (float32, never zero — prevents formula collapse)

**Assignment:** LLM scores salience at `save_memory` time via a lightweight prompt:

System: You are a memory importance classifier.

Score this memory's long-term importance from 0.1 to 1.0:

  1.0 \= core identity, career goals, critical decisions

  0.5 \= useful project context, preferences, patterns

  0.1 \= transient notes, casual observations, ephemera

Memory: "{content}"

Return ONLY a float. Example: 0.7

**Effect on survival:**

- `Sal = 1.0` → full survival weight (identity-level memory)  
- `Sal = 0.5` → half weight (normal context)  
- `Sal = 0.1` → fast eviction candidate (10x faster decay than core)

**Bayesian Trust Modifier (optional Phase 2):** Memories that are later contradicted by new shards receive a trust penalty:

effective\_Sal \= Sal × (1 \- contradiction\_weight)

Low-trust memories decay 3× faster (per SuperLocalMemory V3.3 research).

---

### Term 3 — Exponential Time Decay

**Replaces:** linear `T` division

**Formula:**

decay\_factor \= e^(Δt / A(m))

Where:

  Δt    \= hours since last retrieval

  A(m)  \= ACT-R activation (high activation \= slower decay)

**Why it's better:** Linear decay (`T = hours`) means a 100-hour-old shard and a 200-hour-old shard differ by exactly 2×. Exponential decay matches the Ebbinghaus curve — steep drop in the first hours, then flattening for long-term survivors.

A shard with high activation (`A(m) = 3.0`) decays far slower than one with low activation (`A(m) = 0.5`) — the math automatically protects frequently-used memories.

---

## Full Comparison: v3.5 vs v4.0

Scenario: Shard retrieved 3× recently vs. 10× long ago

v3.5:

  Recent (3×):  V \= 1.3,  T \= 48h   → S \= (D·C·10·1.3) / 48   \= moderate

  Old    (10×): V \= 2.0,  T \= 720h  → S \= (D·C·10·2.0) / 720  \= low (penalized by age)

  Result: OLD shard incorrectly scores lower despite more total usage

v4.0:

  Recent (3×):  A(m) ≈ 2.1, Sal=0.6  → e^(48/2.1)  ≈ e^22.8 → survival protected

  Old    (10×): A(m) ≈ 0.8, Sal=0.6  → e^(720/0.8) ≈ massive decay → correctly evicted

  Result: RECENT shard correctly protected, old shard correctly fades

---

## Additional Research Ideas (Phase 3 Candidates)

### Idea A — Spaced Repetition Scheduling (SuperMemo SM2)

Instead of reactive eviction, proactively schedule "re-consolidation" events. The Synthesizer could flag shards due for reinforcement before they decay below threshold.

next\_review \= last\_used \+ interval × ease\_factor

ease\_factor updated per retrieval quality (did the LLM use the shard effectively?)

**Effort:** Medium — requires new Synthesizer job \+ ease\_factor field **Impact:** Proactive memory health vs. reactive eviction

---

### Idea B — Progressive Embedding Compression

As shards decay, compress their vector precision:

Active  (S \> 70): 32-bit float  → full precision

Warm    (S 40-70): 16-bit float → half precision

Cold    (S 20-40): 8-bit int    → quantized

Archive (S \< 20): 4-bit int     → pre-eviction

**Why:** Saves storage, mirrors biological memory (vivid recent vs. hazy old). **Effort:** High — requires pgvector precision management \+ Go quantization **Impact:** Medium — storage optimization \+ semantically meaningful precision loss

---

### Idea C — Episodic Session Chains ✅ SHIPPED

~~Tag shards saved within the same MCP session as `EPISODE_OF` relationships in Neo4j. The Synthesizer builds narrative arcs — not just topic clusters, but story threads.~~

**Implemented in `internal/mcp/server.go` → `handleSave()`.** Episode nodes are created via `MERGE (e:Episode {session_id: $sessionID})` and shards are linked via `MERGE (sh)-[:EPISODE_OF]->(e)`. Neo4j-only feature — SQLite/Postgres modes silently skip.

MATCH (a:Shard)-\[:EPISODE\_OF\]-\>(e:Episode)\<-\[:EPISODE\_OF\]-(b:Shard)

RETURN a, b, e

---

### Idea D — Contradiction Detection

On `save_memory`, run semantic search for high-similarity shards with opposing polarity. Flag as `CONTRADICTS` edges. Surface for explicit resolution.

if cosine\_similarity(new\_shard, existing) \> 0.85:

    run LLM classification: "do these contradict?"

    if yes: create CONTRADICTS edge, lower Sal of older shard

**Effort:** Medium — LLM call per save \+ new edge type **Impact:** High — prevents mesh from holding conflicting beliefs simultaneously

---

## Implementation Order

Priority 1 (Formula Core) — **SHIPPED (2026-05-29, HARDENING.md Phase 6.6):**

  1\. ~~Add RetrievalHistory \[\]time.Time to Shard struct~~ ✅

  2\. ~~Implement ACT-R A(m) calculation in Go~~ ✅ (`internal/storage/cognitive.go`)

  3\. ~~Add Salience field \+ LLM scoring at save\_memory time~~ ✅

  4\. ~~Swap Survival Formula v3.5 → v4.0 in Visual Ego~~ ✅ (dual-score benchmark active)

  5\. Benchmark: run both formulas in parallel, compare eviction sets — **IN PROGRESS** (benchmark logging live)

  6\. ~~Episodic session chains (Idea C)~~ ✅ (shipped with Priority 1)

Priority 2 (Extensions) — **NOT STARTED:**

  7\. Contradiction detection (Idea D)

  8\. Spaced repetition scheduling (Idea A)

  9\. Progressive embedding compression (Idea B) — last, highest effort

---

## Go Implementation Sketch — ACT-R Activation

// CalculateACTRActivation computes memory activation from retrieval history.

// Based on ACT-R base-level learning equation: A(m) \= ln(Σ tᵢ⁻ᵈ) \+ ε

func CalculateACTRActivation(history \[\]time.Time, decayD float64) float64 {

    const epsilon \= 0.1

    if len(history) \== 0 {

        return epsilon

    }

    now := time.Now()

    var sum float64

    for \_, t := range history {

        hoursAgo := now.Sub(t).Hours()

        if hoursAgo \< 0.001 {

            hoursAgo \= 0.001 // prevent division by zero

        }

        sum \+= math.Pow(hoursAgo, \-decayD)

    }

    if sum \<= 0 {

        return epsilon

    }

    return math.Log(sum) \+ epsilon

}

// SurvivalScoreV4 computes the v4.0 survival score for a shard.

func SurvivalScoreV4(density int, centrality float64, salience float64,

    history \[\]time.Time, lastUsed time.Time) float64 {

    activation := CalculateACTRActivation(history, 0.5)

    deltaT := time.Since(lastUsed).Hours()

    decayFactor := math.Exp(deltaT / activation)

    score := (float64(density) \* (centrality \+ 1.0) \* 10.0 \* activation \* salience) / decayFactor

    return math.Min(95.0, score)

}

---

## Benchmark Plan (Before Shipping)

Before replacing v3.5, run both in parallel for 7 days:

scoreV35 := SurvivalScoreV35(density, centrality, vitality, hoursOld)

scoreV40 := SurvivalScoreV4(density, centrality, salience, history, lastUsed)

log.Printf("\[BENCHMARK\] shard=%s v35=%.2f v40=%.2f delta=%.2f",

    shardID, scoreV35, scoreV40, scoreV40-scoreV35)

**Pass criteria:**

- Core shards: both formulas score \> 90 ✓  
- High-frequency recent shards: v4.0 scores higher ✓  
- Low-frequency old shards: v4.0 scores lower ✓  
- No valid shard evicted that v3.5 would protect ✓

---

---

## References

| \# | Paper | Authors | Year | Field | URL |
| :---- | :---- | :---- | :---- | :---- | :---- |
| 1 | Cognitive Memory in Large Language Models | Zhong et al. | 2025 | Computer Science / AI | [https://arxiv.org/abs/2504.02441](https://arxiv.org/abs/2504.02441) |
| 2 | Mathematical Modeling of Human Memory | ResearchGate | 2025 | Cognitive Psychology / Mathematics | [https://www.researchgate.net/publication/391440097\_Mathematical\_Modeling\_of\_Human\_Memory](https://www.researchgate.net/publication/391440097_Mathematical_Modeling_of_Human_Memory) |
| 3 | Human-Like Remembering and Forgetting in LLM Agents: An ACT-R-Inspired Memory Architecture | ACM HAI | 2026 | Computer Science / Cognitive Science | [https://dl.acm.org/doi/10.1145/3765766.3765803](https://dl.acm.org/doi/10.1145/3765766.3765803) |
| 4 | Memory Models for Spaced Repetition Systems | Randazzo, Politecnico di Milano | 2022 | Educational Technology / Mathematics | [https://www.politesi.polimi.it/retrieve/b39227dd-0963-40f2-a44b-624f205cb224/2022\_4\_Randazzo\_01.pdf](https://www.politesi.polimi.it/retrieve/b39227dd-0963-40f2-a44b-624f205cb224/2022_4_Randazzo_01.pdf) |
| 5 | Human-like Forgetting Curves in Deep Neural Networks | arXiv | 2025 | Computer Science / Neuroscience | [https://arxiv.org/abs/2506.12034](https://arxiv.org/abs/2506.12034) |
| 6 | Adaptive Forgetting Curves for Spaced Repetition Language Learning | Zaidi et al., University of Cambridge | 2020 | Computational Linguistics / Educational Technology | [https://arxiv.org/abs/2004.11327](https://arxiv.org/abs/2004.11327) |
| 7 | Enhancing Human Learning via Spaced Repetition Optimization | PNAS | 2019 | Cognitive Psychology / Applied Mathematics | [https://www.pnas.org/doi/10.1073/pnas.1815156116](https://www.pnas.org/doi/10.1073/pnas.1815156116) |
| 8 | SuperLocalMemory V3.3: The Living Brain — Biologically-Inspired Forgetting, Cognitive Quantization, and Multi-Channel Retrieval | arXiv | 2026 | Computer Science / AI | [https://arxiv.org/abs/2604.04514](https://arxiv.org/abs/2604.04514) |

---

*Research Track v1.2 | 2026-05-29* *Authors: BB & Brainy Bestie* *"The math should be as alive as the mesh."*

---
---

## Survival Formula v4.1 — Decay Scaling Research (2026-05-29)

**Status:** IMPLEMENTED — shipped 2026-05-30
**Trigger:** v4.0 shipped but shards decay too aggressively. A well-connected shard (15 bonds, 0.95 salience) drops below the Janitor eviction threshold of 20 in ~40 hours without retrieval. Cross-referencing cognitive science, neuroscience, and state-of-the-art AI memory systems reveals Shard-Link forgets **2-10x faster** than any published model.

---

### The Problem: Dimensional Mismatch in v4.0

The v4.0 formula uses ACT-R activation `A(m)` in two roles:

1. **Numerator multiplier** — directly scales the score (removed in hotfix, but the decay issue persists)
2. **Denominator decay rate** — controls how fast the exponential kills the score

For an untouched shard, `A(m) = epsilon = 0.1`. Even with the `tau = 168h` bridge constant, the effective decay half-life is:

```
effective_half_life = A(m) * tau = 0.1 * 168 = 16.8 hours
```

This means an unretrieved shard loses half its score every ~17 hours. No model in the literature supports forgetting this fast for meaningful content.

---

### Cross-Model Comparison: How Long Do Untouched Memories Survive?

| Model | Type | Unretrieved Half-Life | Source |
| :---- | :---- | :---- | :---- |
| Ebbinghaus (1885, meaningless syllables) | Psychology | ~20 hours (33% at 1 day) | Original experiment, replicated Murre & Dros 2015 (PLOS ONE) |
| Ebbinghaus (meaningful content) | Psychology | ~3-5 days (estimated) | 3-5x multiplier for meaningful vs. meaningless material |
| MemoryBank (Zhong et al. 2024) | AI / LLM Memory | S=1 day initial → ~1 day | AAAI 2024, `R = e^(-t/S)`, S += 1 per retrieval |
| FSRS v7 ("Good" rating) | Spaced Repetition | **S₀ = 3.13 days** | State-of-the-art, powers Anki, 19 trainable params |
| FSRS v7 ("Easy" rating) | Spaced Repetition | **S₀ = 15.47 days** | Same — high-confidence items survive 2+ weeks |
| SuperMemo SM-2 | Spaced Repetition | R = 0.9^(t/S), first interval = 1 day | Wozniak 1987, foundational SRS algorithm |
| Biological (synaptic consolidation) | Neuroscience | **~2 days** for memory trace stability | Stochastic Consolidation (Roxin & Fusi 2022, PMC9339009) |
| Biological (active forgetting) | Neuroscience | Rac1 peak activation at 1 hour, but traces persist beyond behavioral recall | Davis & Zhong 2017 (PMC5657245) |
| **Shard-Link v4.0 (current)** | **AI Memory** | **~16.8 hours** | **2-10x too aggressive** |

---

### Key Research Findings

#### 1. Ebbinghaus Forgetting Curve — The Baseline

Formula: `R = e^(-t/S)` where S = memory strength (stability)

Original data for **meaningless syllables** (worst case):

| Time | Retained |
| :---- | :---- |
| 20 minutes | 58% |
| 1 hour | 44% |
| 9 hours | 36% |
| 1 day | 33% |
| 6 days | 25% |
| 31 days | 21% |

Critical insight: even meaningless content retains **21% after a full month**. Our shards are structured, meaningful, LLM-summarized content — they should survive far longer than syllables.

Each review increases S, making the curve flatter. This maps directly to our ACT-R retrieval history model.

#### 2. MemoryBank (AAAI 2024) — Closest AI Analog

- Formula: `R = e^(-t/S)`, initial **S = 1 day**
- Each retrieval: `S += 1`, reset `t = 0`
- After 3 retrievals: S = 4 days → memory survives nearly a week at 37% retention
- Authors explicitly note: "this is an exploratory and highly simplified memory updating model"
- **Takeaway for Shard-Link:** Even the simplest AI memory model starts at S = 1 day, not 16.8 hours

#### 3. FSRS v7 — State of the Art (Powers Anki)

- Formula: `R = (1 + FACTOR * t/S)^DECAY` where DECAY = -0.5, FACTOR = 19/81
- Uses **power-law decay**, not exponential — gentler long-term tail
- Initial stability is mapped from **content difficulty ratings**:

| Rating | Initial S₀ (days) | Shard-Link Equivalent |
| :---- | :---- | :---- |
| Again (0.41 days) | ~10 hours | Sal < 0.2 (trivial ephemera) |
| Hard (1.18 days) | ~28 hours | Sal 0.2-0.4 (low-importance context) |
| Good (3.13 days) | ~75 hours | Sal 0.4-0.7 (useful project context) |
| Easy (15.47 days) | ~371 hours | Sal 0.7-1.0 (identity/critical knowledge) |

- Each successful retrieval multiplies S by a growth factor (typically 1.5-3x)
- **Takeaway for Shard-Link:** Map salience → initial stability directly, FSRS-style. This is the most principled approach — it's been validated on millions of Anki users.

#### 4. Biological Neuroscience

**Memory Phases:**
- Short-term memory: seconds to minutes (synaptic activity only)
- Intermediate-term: hours (requires protein synthesis to begin consolidation)
- **Consolidation threshold: ~2 days** for a memory trace to achieve relative stability
- Long-term potentiation (LTP): once consolidated, traces can persist years

**Active Forgetting (not just passive decay):**
- Rac1-dependent pathway: dopamine → Rac1 → cofilin cascade remodels actin cytoskeleton
- Cdc42-dependent pathway: distinct mechanism for anesthesia-resistant memories
- Neurogenesis: new hippocampal neurons physically displace existing traces

**Key biological insight:** "Biochemical memory traces persist beyond the time at which memory can be successfully retrieved." Forgotten memories leave traces that can be reactivated with the right cues — analogous to archived shards in Shard-Link's Postgres Archival Vessel.

**Takeaway for Shard-Link:** Biology gives new memories a ~2 day grace period for consolidation. The Janitor should respect a similar window.

---

### Proposed Fix: Salience-Mapped Initial Stability

**Core Idea:** Replace the fixed `tau` constant with a salience-mapped stability value `S₀` measured in **days**, following the FSRS/Ebbinghaus `R = e^(-t/S)` pattern.

#### New Formula: v4.1

```
S = min(95, (D * (C + 1.0) * 10 * Sal) / e^(Δt_days / S₀(Sal, A(m))))
```

Where:

```
S₀(Sal, A(m)) = S_base(Sal) * (1 + A(m))
```

- `Δt_days` = time since last use, in **days** (not hours)
- `S_base(Sal)` = salience-mapped initial stability (days), interpolated from FSRS-calibrated anchors
- `A(m)` = ACT-R activation — multiplies stability so retrieved shards decay slower
- `(1 + A(m))` ensures: at A(m)=0 (no history), S₀ = S_base; at A(m)=2.0, S₀ = 3x base

#### Salience → Stability Mapping (FSRS-Calibrated)

| Salience Range | S_base (days) | FSRS Equivalent | Rationale |
| :---- | :---- | :---- | :---- |
| 0.1 - 0.2 | 1.0 | Again/Hard | Trivial ephemera — forget in ~1 day |
| 0.2 - 0.4 | 3.0 | Good | Low-importance context — survive ~3 days |
| 0.4 - 0.7 | 7.0 | Good/Easy | Useful project context — survive ~1 week |
| 0.7 - 1.0 | 14.0 | Easy | Critical knowledge — survive ~2 weeks |

Interpolation: `S_base = lerp(1.0, 14.0, (Sal - 0.1) / 0.9)`

This gives a continuous mapping: `S_base = 1.0 + (Sal - 0.1) * 14.44`

#### Worked Examples: v4.1 vs v4.0

**Scenario A: Hub shard (15 bonds, centrality=0.48, salience=0.95, no retrievals)**

v4.0 (current): drops below 20 at ~40 hours

v4.1:
- S_base = 1.0 + (0.95 - 0.1) * 14.44 = **13.27 days**
- S₀ = 13.27 * (1 + 0.1) = **14.6 days** (A(m)=0.1 for zero history)
- numerator = 15 * 1.48 * 10 * 0.95 = 210.9
- At day 1: e^(1/14.6) = 1.07 → score = 197 → **95 (capped)**
- At day 7: e^(7/14.6) = 1.62 → score = 130 → **95 (capped)**
- At day 14: e^(14/14.6) = 2.61 → score = **80.8**
- At day 21: e^(21/14.6) = 4.22 → score = **50.0**
- At day 30: e^(30/14.6) = 7.86 → score = **26.8** (still above eviction)
- At day 40: e^(40/14.6) = 15.5 → score = **13.6** (Janitor candidate)

**40 days** before eviction risk vs. **40 hours** in v4.0.

**Scenario B: Weak shard (2 bonds, centrality=0, salience=0.3, no retrievals)**

v4.1:
- S_base = 1.0 + (0.3 - 0.1) * 14.44 = **3.89 days**
- S₀ = 3.89 * 1.1 = **4.28 days**
- numerator = 2 * 1.0 * 10 * 0.3 = 6.0
- At day 1: e^(1/4.28) = 1.26 → score = **4.8**
- At day 3: e^(3/4.28) = 2.02 → score = **3.0**

Low-importance shard starts low (4.8) and fades within days — correctly targeted for eviction.

**Scenario C: Active shard (10 bonds, centrality=0.3, salience=0.6, retrieved 5x this week)**

v4.1:
- A(m) ≈ 1.5 (strong recent retrieval history)
- S_base = 1.0 + (0.6 - 0.1) * 14.44 = **8.22 days**
- S₀ = 8.22 * (1 + 1.5) = **20.55 days**
- numerator = 10 * 1.3 * 10 * 0.6 = 78.0
- At day 14: e^(14/20.55) = 1.98 → score = **39.4** (healthy)
- At day 30: e^(30/20.55) = 4.31 → score = **18.1** (approaching eviction)

Active retrieval extends survival from 2 weeks (base) to 4+ weeks. ACT-R activation is doing its job — in the denominator, not the numerator.

---

### Why v4.1 Is More Principled Than v4.0

| Aspect | v4.0 | v4.1 |
| :---- | :---- | :---- |
| Decay time unit | Hours (with tau hack) | Days (natural unit for memory) |
| Initial stability | Fixed: A(m) * tau = 16.8h | Salience-mapped: 1-14 days (FSRS-calibrated) |
| A(m) role | Numerator multiplier + decay rate (conflated) | Decay rate modulator only (proper Ebbinghaus role) |
| Salience role | Numerator weight only | Determines initial stability + numerator weight |
| Grounding | Intuitive tau constant | FSRS v7 defaults validated on millions of users |
| Biological alignment | ~17h half-life (too fast) | 1-14 day range matches consolidation window |

---

### Implementation Checklist

- [x] Replace `tau` constant with `S_base(Sal)` interpolation in `cognitive.go`
- [x] Change `Δt` from hours to days in `SurvivalScoreV4`
- [x] Add `(1 + A(m))` stability multiplier in denominator
- [x] Update Visual Ego `packData()` benchmark logging
- [x] Update CLAUDE.md formula reference
- [x] Patch zero-value `created_at` timestamps (54 shards migrated)
- [ ] Run dual-score benchmark: v3.5 vs v4.1 for 7 days
- [ ] Validate: no high-salience shard evicted before 7 days without retrieval
- [ ] Validate: low-salience orphan shards still evicted within 1-3 days

---

### New References (v4.1 Research)

| \# | Paper | Authors | Year | Field | URL |
| :---- | :---- | :---- | :---- | :---- | :---- |
| 9 | MemoryBank: Enhancing Large Language Models with Long-Term Memory | Zhong et al. | 2024 | Computer Science / AI (AAAI) | [https://ar5iv.labs.arxiv.org/html/2305.10250](https://ar5iv.labs.arxiv.org/html/2305.10250) |
| 10 | FSRS v7: Free Spaced Repetition Scheduler Algorithm | open-spaced-repetition | 2024 | Spaced Repetition / ML | [https://github.com/open-spaced-repetition/free-spaced-repetition-scheduler](https://github.com/open-spaced-repetition/free-spaced-repetition-scheduler) |
| 11 | The Biology of Forgetting — A Perspective | Davis & Zhong | 2017 | Neuroscience (NIH/PMC) | [https://pmc.ncbi.nlm.nih.gov/articles/PMC5657245/](https://pmc.ncbi.nlm.nih.gov/articles/PMC5657245/) |
| 12 | Stochastic Consolidation of Lifelong Memory | Roxin & Fusi | 2022 | Computational Neuroscience (NIH/PMC) | [https://www.ncbi.nlm.nih.gov/pmc/articles/PMC9339009/](https://www.ncbi.nlm.nih.gov/pmc/articles/PMC9339009/) |
| 13 | Memory Engram Stability and Flexibility | Bhatt et al. | 2024 | Neuroscience (NIH/PMC) | [https://pmc.ncbi.nlm.nih.gov/articles/PMC11525749/](https://pmc.ncbi.nlm.nih.gov/articles/PMC11525749/) |
| 14 | Ebbinghaus Forgetting Curve (1885), replicated | Murre & Dros | 2015 | Cognitive Psychology (PLOS ONE) | [https://journals.plos.org/plosone/article?id=10.1371/journal.pone.0120644](https://journals.plos.org/plosone/article?id=10.1371/journal.pone.0120644) |
