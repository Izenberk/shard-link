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

*Research Track v1.1 | 2026-05-29* *Authors: BB & Brainy Bestie* *"The math should be as alive as the mesh."*
