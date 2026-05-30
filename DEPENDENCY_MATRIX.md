# Shard-Link Technical Manual

> **Purpose:** Before modifying any component, look it up here. Find its upstream inputs
> and downstream outputs. Trace the blast radius before you touch anything.
>
> Think of this as a P&ID diagram — every component is a node, every data flow is a pipe.
>
> **Last Updated:** 2026-05-30

---

## Table of Contents

1. [System Topology](#1-system-topology)
2. [Component Reference Cards](#2-component-reference-cards)
3. [Field → Consumer Map](#3-field--consumer-map)
4. [Formula → Callsite Map](#4-formula--callsite-map)
5. [Touch Completeness Matrix](#5-touch-completeness-matrix)
6. [Shared State & Signaling](#6-shared-state--signaling)
7. [Environment Variable Reference](#7-environment-variable-reference)
8. [Data Flow Diagrams](#8-data-flow-diagrams)
9. [Upgrade Checklist Template](#9-upgrade-checklist-template)

---

## 1. System Topology

```
                         ┌───────────────────────────────────┐
                         │       Cloudflare Tunnel           │
                         │    hub.izenberk.com/mcp           │
                         └──────────┬────────────────────────┘
                                    │ HTTPS
                         ┌──────────▼────────────────────────┐
                         │       MCP Server (:8080)          │
                         │  OAuth 2.0 + PKCE / X-API-Key     │
                         │                                   │
                         │  ┌─────────────────────────────┐  │
                         │  │ Tool Handlers               │  │
                         │  │  search_memory  search_text │  │
                         │  │  search_graph   search_all  │  │
                         │  │  save_memory                │  │
                         │  └──────┬──────────────────────┘  │
                         │         │                         │
                         │  ┌──────▼──────┐  ┌────────────┐  │
                         │  │WorkingMemory│  │  Embedder  │  │
                         │  │(EMA Bias)   │  │Gemini/Olla.│  │
                         │  └─────────────┘  └────────────┘  │
                         │                    ┌───────────┐  │
                         │                    │Summarizer │  │
                         │                    │Gemini LLM │  │
                         │                    └───────────┘  │
                         └──────────┬────────────────────────┘
                                    │ Repository Interface
               ┌────────────────────┼────────────────────┐
               │                    │                    │
    ┌──────────▼─────┐   ┌──────────▼──────┐   ┌─────────▼───────┐
    │   Neo4j (GDS)  │   │    SQLite       │   │   PostgreSQL    │
    │  Living Mesh   │   │  Seed Memory    │   │  Archival Store │
    │                │   │  Activity Ledger│   │                 │
    │  Full Cognitive│   │  Minimal fields │   │  Minimal fields │
    └──────┬─────────┘   └────────┬────────┘   └─────────┬───────┘
           │                      │                      │
    ┌────────────────────────────────────────────────────┘
    │              Background Workers
    │  ┌────────────┐  ┌─────────────┐  ┌──────────────┐
    │  │  Janitor   │  │ Synthesizer │  │HygieneWorker │
    │  │ (15 min)   │  │ (30 min)    │  │ (24 hr)      │
    │  │ Eviction   │  │ Link+Cluster│  │ VACUUM+Index │
    │  └────────────┘  └─────────────┘  └──────────────┘
    │
    ▼──────────────┐
    │  Visual Ego  │
    │  (Dashboard) │
    │  Read-only   │
    └──────────────┘
```

---

## 2. Component Reference Cards

Each card lists: what the component does, what feeds into it (upstream), what it feeds (downstream), shared state it touches, and config that controls it.

---

### MCP Server

> `internal/mcp/server.go` — Streamable HTTP + SSE transport, OAuth 2.0 + PKCE

| | |
|---|---|
| **Upstream** | External clients via Cloudflare Tunnel (HTTPS) |
| **Downstream** | Repository (all 3 backends), Embedder, Summarizer, WorkingMemory, GlobalLogger |
| **Shared State** | `meshDirty` (write via SaveShard), `sessionTokens` + `pendingCodes` (OAuth), `GlobalLogger` |
| **Config** | `HUB_API_KEY`, `PUBLIC_URL` |

**Tool Handlers:**

| Handler | Calls | Touch? | Notes |
|---------|-------|:------:|-------|
| `search_memory` | Embedder.Embed → WorkingMemory.Bias → FindResonant → MMR re-rank → WorkingMemory.Update | Yes | Diversity via MMR (lambda from `MMR_LAMBDA`) |
| `search_text` | FindText → WorkingMemory.Update | Yes | No embedding needed |
| `search_graph` | Embedder.Embed → WorkingMemory.Bias → SearchGraph → WorkingMemory.Update | Yes | Multi-hop traversal, touches center + neighbors |
| `search_all` | FindText(false) + FindResonant(false) + SearchGraph(false) → deduplicate → ReinforceShards | Yes (unified) | Avoids double-touch via dedup + single ReinforceShards call |
| `save_memory` | Embedder.Embed → Summarizer.Summarize (salience) → SaveShard → Episode linking | N/A | Sets salience, triggers meshDirty, creates Episode node (Neo4j) |

**Resource/Prompt Handlers:**

| Handler | Purpose |
|---------|---------|
| `shard-link://core` | Read-only access to core identity shards via GetCoreShards() |
| `hub_search` prompt | Guided search meta-prompt — instructs LLM to call search_all |

---

### Embedder

> `internal/storage/embedder.go` — Converts text to 768-D vectors

| | |
|---|---|
| **Upstream** | MCP Server (query embedding, content embedding), Synthesizer (summary embedding) |
| **Downstream** | Shard.Vector field (via EncodeVector), WorkingMemory centroid |
| **Shared State** | None |
| **Config** | `EMBEDDING_MODE` (none/server/local), `GEMINI_API_KEY`, `EMBEDDING_MODEL`, `EMBEDDING_DIMENSION`, `OLLAMA_URL`, `OLLAMA_MODEL` |

**Implementations:**

| Mode | Implementation | Notes |
|------|---------------|-------|
| `none` | MockEmbedder | Pseudo-random vectors from text bytes, zero cost |
| `server` | GeminiEmbedder | Matryoshka truncation from 3072-D to `EMBEDDING_DIMENSION`, L2-normalized |
| `local` | OllamaEmbedder | Local Ollama HTTP API, privacy-first |

---

### Summarizer

> `internal/storage/summarizer.go` — LLM text generation for salience scoring and community narratives

| | |
|---|---|
| **Upstream** | MCP Server `save_memory` (salience scoring), Synthesizer (community summarization) |
| **Downstream** | Shard.Salience field, community summary content |
| **Shared State** | None |
| **Config** | `GEMINI_API_KEY`, `SUMMARIZER_MODEL` (default: gemini-2.5-flash) |

**Call Paths:**

| Caller | Purpose | Input | Output |
|--------|---------|-------|--------|
| `save_memory` handler | Salience scoring | "Rate importance 0.1-1.0..." + content | Float [0.1, 1.0] parsed from response |
| `Synthesizer.summarizeCommunities()` | Community narrative | Top 15 member contents by PageRank | Cohesive paragraph summary |

---

### WorkingMemory

> `internal/mcp/working_memory.go` — Per-session cognitive biasing via EMA centroid drift

| | |
|---|---|
| **Upstream** | MCP search handlers (query vectors, result vectors) |
| **Downstream** | Biased query vectors fed to FindResonant/SearchGraph |
| **Shared State** | In-memory session map (not persisted) |
| **Config** | `COGNITIVE_BIAS_LAMBDA` (default 0.7), TTL = 30 min, cleanup = 5 min ticker |

**Operations:**

| Method | What It Does |
|--------|-------------|
| `Bias(sessionID, queryVec, lambda)` | Returns `lambda * query + (1-lambda) * centroid`. First call returns query unchanged. |
| `Update(sessionID, shards)` | EMA blend: `new = 0.3 * mean(result_vecs) + 0.7 * old_centroid` |
| `StartCleanup(ctx)` | Background goroutine, purges sessions where `LastUsed > TTL` every 5 min |

---

### Janitor

> `internal/janitor/janitor.go` — Background size management via eviction

| | |
|---|---|
| **Upstream** | Timer (15 min), Repository.GetCount(), Repository.GetEvictionCandidates() |
| **Downstream** | Repository.ArchiveShard() (Neo4j: DETACH DELETE, SQLite: move to archive table), Repository.Optimize() |
| **Shared State** | `GlobalLogger` (write activity events) |
| **Config** | `JANITOR_RESONANCE_THRESHOLD` (default 0.70), Max shards = 1000, Interval = 15 min |

**Eviction Chain:**

```
Timer tick (15 min)
  → GetCount() — check if over limit
  → GetEvictionCandidates(overage)
      → Cypher: filter non-core + resonance < threshold
      → Go: SurvivalScoreV4(bondCount, pagerank, salience, retrievalHistory, lastUsed)
      → Sort ascending, return lowest N
  → ArchiveShard() × N — remove from living mesh
  → Optimize() — compact storage
```

**Reads from Shard:** `Category`, `BondCount`, `PageRank`, `Salience`, `RetrievalHistory`, `LastUsed`

---

### Synthesizer

> `internal/synthesizer/synthesizer.go` — Autonomous relational linking + community summarization

| | |
|---|---|
| **Upstream** | Timer (30 min) + `meshDirty` flag (set by SaveShard), Embedder, Summarizer |
| **Downstream** | Repository.SyncBonds(), Repository.CalculateCommunities(), Repository.SaveShard() (community summaries), Repository.PruneStaleSummaries() |
| **Shared State** | `meshDirty` (read + clear), `communityCache` (written by CalculateCommunities), `GlobalLogger` |
| **Config** | `MESH_LINK_THRESHOLD` (default 0.75), `SYNTHESIZER_INTERVAL_MINUTES` (default 30) |

**Synthesis Chain:**

```
Timer tick (30 min)
  → IsMeshDirty()? — skip if false
  → SyncBonds(threshold) — auto-link all pairs with cosine > threshold
  → ClearMeshDirty()
  → [async goroutine, 2 min timeout]
      → CalculateCommunities() — Louvain + PageRank (GDS)
          → Writes CommunityID + PageRank on all shards
          → Delta-write optimization via communityCache
      → PruneStaleSummaries() — delete orphaned comm-summary-* shards
  → [async goroutine, 5 min timeout]
      → For each changed community with >= 3 members:
          → GetShardsByCommunity(cid) — top 15 by PageRank DESC
          → Summarizer.Summarize(prompt) — generate narrative
          → Embedder.Embed(summary) — vectorize
          → SaveShard(comm-summary-{cid}) — upsert as core shard
          → 2 sec delay between API calls (rate limit)
```

**Reads from Shard:** `Vector` (pairwise similarity), `Content`, `ID`, `Category`, `PageRank` (ordering)
**Writes to Shard:** `CommunityID`, `PageRank` (all shards); new core shards (summaries)

---

### HygieneWorker

> `internal/hygiene/hygiene.go` — Storage maintenance

| | |
|---|---|
| **Upstream** | Timer (`HYGIENE_INTERVAL_HOURS`, default 24h) |
| **Downstream** | Repository.Optimize() on all 3 backends |
| **Shared State** | `GlobalLogger` |
| **Config** | `HYGIENE_INTERVAL_HOURS` (default 24) |

**Maintenance Cycle:**

| Backend | Operation |
|---------|-----------|
| PostgreSQL | `VACUUM ANALYZE shards` |
| Neo4j | `ensureIndexes()` — vector index + fulltext index integrity |
| SQLite | `PRAGMA optimize` + `VACUUM` |

---

### Visual Ego

> `cmd/visual_ego/main.go` — Real-time graph visualization dashboard

| | |
|---|---|
| **Upstream** | Repository.GetAllShards(), Repository.GetAllBonds(), Repository.GetRecentActivity() |
| **Downstream** | HTTP WebSocket to browser (read-only) |
| **Shared State** | None (read-only) |
| **Config** | None |

**Reads from Shard:** All fields. Computes both `SurvivalScoreV4` and `SurvivalScoreV35` for display/comparison. Core shards get hardcoded score=100.

---

### Activity Ledger

> SQLite `activity_logs` table, written via `Vessel.SaveActivity()`

| | |
|---|---|
| **Upstream** | `GlobalLogger` callback — fired by Janitor, Synthesizer, HygieneWorker, MCP Server |
| **Downstream** | `GetRecentActivity()` — consumed by Visual Ego dashboard |
| **Shared State** | `GlobalLogger` (set in main.go) |
| **Config** | `ACTIVITY_LOG_RETENTION_DAYS` (default 7) — auto-purges old entries on each write |

---

### Episode System

> Neo4j `Episode` nodes linked to shards via `EPISODE_OF`

| | |
|---|---|
| **Upstream** | MCP `save_memory` handler — creates/links Episode node per session |
| **Downstream** | Temporal narrative recall (session chaining) |
| **Shared State** | None |
| **Config** | Session ID from MCP transport |

---

### CLI Utilities (`cmd/`)

| Command | Purpose | Reads | Writes |
|---------|---------|-------|--------|
| `auto_link` | Manual bond trigger | Neo4j shards | Neo4j CONNECTED_TO bonds |
| `check_mesh` | Audit consistency | Neo4j | Logs only |
| `check_models` | API readiness | Embedder/Summarizer | Logs only |
| `check_vec` | Vector dimension check | Neo4j | Logs only |
| `check_sizes` | Usage stats | All backends | Logs only |
| `gen_vec` | Bulk embedding | CSV input | Neo4j (SaveShard) |
| `migrate` | Bootstrap archival | Neo4j | PostgreSQL schema |
| `repair_vec` | Fix broken embeddings | Database | Database (re-embed) |

---

## 3. Field → Consumer Map

Every field on the `Shard` struct (`internal/storage/shard.go`), who writes it, who reads it, and which backends apply.

### Core Identity Fields

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `ID` | `string` | All 3 | SaveShard (all); MCP save_memory; gen_vec, repair_vec | Search functions (all); GetEvictionCandidates (all); RRF ranking; Visual Ego; Janitor logging |
| `Category` | `string` | All 3 | SaveShard (all); MCP save_memory; Synthesizer (saves summaries as "core") | GetEvictionCandidates — filters != 'core' (all); GetCoreShards — filters = 'core' (all); GetShardsByCommunity — filters != 'archived' (Neo4j); Visual Ego node coloring |
| `Content` | `string` | All 3 | SaveShard (all); MCP save_memory; Synthesizer summary save | FindText — text search (all); MCP embedder — embeds if no vector; Salience scoring — LLM rates importance; Community summarization — builds prompt; Visual Ego |

### Vector & Embedding

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `Vector` | `[]byte` | All 3 | SaveShard (all); MCP embedder pipeline — base64 or Embed() → EncodeVector() | FindResonant — similarity search (all); isResonantToCore — eviction protection (SQLite); SyncBonds — auto-linking (Neo4j); SearchGraph — multi-hop (Neo4j); WorkingMemory — centroid EMA; MMR diversity |

### Provenance (Phase 9)

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `Metadata` | `[]byte` | All 3 | SaveShard (all); Synthesizer community metadata | Not actively queried (extensibility) |
| `SourceType` | `string` | All 3 | SaveShard (all) | Visual Ego display |
| `SourceRef` | `string` | All 3 | SaveShard (all) | Visual Ego display |
| `Confidence` | `float64` | All 3 | SaveShard (all); RRF overwrites with fusion score | Visual Ego display |

### Graph Intelligence

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `CommunityID` | `int64` | Neo4j only | CalculateCommunities — Louvain (vessel_graph.go); nodeToShardWithCache — from cache | GetShardsByCommunity; Synthesizer community iteration; PruneStaleSummaries; Visual Ego |
| `PageRank` | `float64` | Neo4j only | CalculateCommunities — GDS PageRank; nodeToShardWithCache — from cache | **SurvivalScoreV4** — centrality (cognitive.go:88); GetEvictionCandidates (Neo4j); SurvivalScoreV35 — legacy (Visual Ego only); GetShardsByCommunity — ORDER BY DESC; Visual Ego |
| `BondCount` | `int` | All 3 (computed) | **Not persisted** — recalculated from CONNECTED_TO degree (Neo4j) or shard_bonds count (SQLite/Postgres) | **SurvivalScoreV4** — density (cognitive.go:88); GetEvictionCandidates (all); Visual Ego |

### Temporal

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `LastUsed` | `time.Time` | All 3 | SaveShard — set on create/upsert (all); FindResonant if shouldTouch=true (all); FindText if shouldTouch=true (Neo4j direct, SQLite/Postgres via ReinforceShards); SearchGraph if shouldTouch=true — center + neighbors (Neo4j); ReinforceShards — bulk touch (all) | **SurvivalScoreV4** — delta-t decay (cognitive.go:83); SurvivalScoreV35 — hoursSince; GetEvictionCandidates (all); WorkingMemory TTL expiry; Visual Ego |
| `CreatedAt` | `time.Time` | All 3 | SaveShard — set on create, preserved on update | Visual Ego display; Fallback when LastUsed is zero |

### Cognitive Science (v4.0+)

| Field | Type | Backends | Writers | Readers |
|-------|------|----------|---------|---------|
| `UseCount` | `int` | Neo4j only | FindResonant if shouldTouch=true (Neo4j); SearchGraph if shouldTouch=true (Neo4j); ReinforceShards (Neo4j) | SurvivalScoreV35 — legacy vitality (Visual Ego only). **Not used in v4.1** — replaced by ACT-R |
| `Salience` | `float64` | Neo4j only | MCP save_memory — LLM-scored via Summarizer or default 0.5; SaveShard — persists to Neo4j | **SurvivalScoreV4** — numerator + SalToStability decay window (cognitive.go:80,88); GetEvictionCandidates — Neo4j reads from shard, SQLite/Postgres hardcode 0.5; Visual Ego |
| `RetrievalHistory` | `[]time.Time` | Neo4j only | FindResonant if shouldTouch=true — append + rolling 20 (Neo4j); FindText if shouldTouch=true (Neo4j); SearchGraph if shouldTouch=true — center + neighbors (Neo4j); ReinforceShards (Neo4j) | **CalculateACTRActivation** — iterates for A(m) (cognitive.go:19); **SurvivalScoreV4** — calls ACT-R (cognitive.go:73); GetEvictionCandidates — Neo4j reads from shard, SQLite/Postgres default to [] |

---

## 4. Formula → Callsite Map

When you change a formula, audit every callsite listed here.

### `SurvivalScoreV4` — Exponential Decay, FSRS-Calibrated

```
S = min(95, (D*(C+1.0)*10*Sal) / e^(delta_t_days / S0))
S0 = S_base(Sal) * (1 + A(m))
```

| Callsite | File | Inputs Source |
|----------|------|---------------|
| `GetEvictionCandidates()` | `vessel_graph.go` | Neo4j shard (full: PageRank, Salience, RetrievalHistory) |
| `GetEvictionCandidates()` | `vessel.go` | SQLite shard (defaults: centrality=0, salience=0.5, history=[]) |
| `GetEvictionCandidates()` | `vessel_postgres.go` | Postgres shard (defaults: centrality=0, salience=0.5, history=[]) |
| `packData()` | `cmd/visual_ego/main.go` | Neo4j shard via GetAllShards() |

**Depends on:** `CalculateACTRActivation()`, `SalToStability()`, `BondCount`, `PageRank`, `Salience`, `RetrievalHistory`, `LastUsed`

### `SurvivalScoreV35` — Legacy Linear Decay

```
Score = (links * (PageRank + 1.0) * 10 * vitality) / hoursSince
vitality = clamp(1.0 + useCount*0.1, 1.0, 5.0)
```

| Callsite | File | Notes |
|----------|------|-------|
| `packData()` | `cmd/visual_ego/main.go` | Legacy benchmark comparison only |

**Depends on:** `BondCount`, `PageRank`, `UseCount`, `LastUsed`

### `CalculateACTRActivation` — Base-Level Learning

```
A(m) = ln( sum(t_i^-d) ) + epsilon    (d=0.5, epsilon=0.1)
```

| Callsite | File | Notes |
|----------|------|-------|
| `SurvivalScoreV4()` | `cognitive.go:73` | Called with history and decay=0.5 |

**Depends on:** `RetrievalHistory`, `time.Now()`

### `SalToStability` — Salience to Time Window

```
S_base = 1.0 + (Sal - 0.1) * 14.44
Maps [0.1, 1.0] to [1 day, 14 days]
```

| Callsite | File | Notes |
|----------|------|-------|
| `SurvivalScoreV4()` | `cognitive.go:80` | Converts salience to baseline stability in days |

**Depends on:** `Salience`

### `cosineSimilarity` — Core Vector Math

```
sim = (a dot b) / (||a|| * ||b||)
```

| Callsite | File | Notes |
|----------|------|-------|
| SQLite `vec_distance_cosine` UDF | `vessel.go:62-80` | Registered on connection |
| `isResonantToCore()` | `vessel.go` | Eviction core-resonance protection |
| Neo4j `gds.similarity.cosine()` | `vessel_graph.go` | GDS built-in (SaveShard linking, SyncBonds, GetEvictionCandidates) |
| PostgreSQL `<=>` operator | `vessel_postgres.go` | pgvector distance |

### `ReciprocalRankFusion` — Multi-Source Ranking

```
Score_RRF = sum(1 / (rank + k)),  k=60.0
```

| Callsite | File | Notes |
|----------|------|-------|
| `FindHybrid()` | `vessel.go` | SQLite hybrid search |
| `FindHybrid()` | `vessel_graph.go` | Neo4j hybrid search |
| `FindHybrid()` | `vessel_postgres.go` | Postgres hybrid search |

**Depends on:** Vector search results, Text search results

### `MaximalMarginalRelevance` — Diversity Re-ranking

```
score = lambda * sim(query, candidate) - (1 - lambda) * max(sim(candidate, already_selected))
```

| Callsite | File | Notes |
|----------|------|-------|
| `handleSearch` (search_memory) | `server.go` | Applied after FindResonant, before returning results |

**Config:** `MMR_LAMBDA` (default 0.7)

### `CalculateCommunities` — Louvain + PageRank

| Callsite | File | Notes |
|----------|------|-------|
| `Synthesizer.performSynthesis()` | `synthesizer.go:90` | Triggered after SyncBonds if new bonds created |

**Side effects:** Updates `CommunityID` and `PageRank` on shards. Triggers community summarization and stale summary pruning.

### `SyncBonds` — Autonomous Linking

| Callsite | File | Notes |
|----------|------|-------|
| `Synthesizer.performSynthesis()` | `synthesizer.go:72` | Main async worker, guarded by `meshDirty` flag |

**Side effects:** Creates `CONNECTED_TO` relationships, affects `BondCount` on next load.

---

## 5. Touch Completeness Matrix

What gets updated when a shard is retrieved (touched). Check this when modifying retrieval paths.

| Operation | `LastUsed` | `UseCount` | `RetrievalHistory` | Neo4j | SQLite | Postgres |
|-----------|:----------:|:----------:|:------------------:|:-----:|:------:|:--------:|
| `FindResonant(shouldTouch=true)` | Y | Y | Y (rolling 20) | Full | LastUsed only | LastUsed only |
| `FindText(shouldTouch=true)` | Y | Y | Y (rolling 20) | Full | LastUsed only | LastUsed only |
| `SearchGraph(shouldTouch=true)` | Y | Y | Y (center + neighbors) | Full | N/A (fallback) | N/A (fallback) |
| `ReinforceShards()` | Y | Y | Y (rolling 20) | Full | LastUsed only | LastUsed only |
| `SaveShard()` (upsert) | Y | — | — | Y | Y | Y |

---

## 6. Shared State and Signaling

Global state that coordinates between components. When debugging race conditions or stale data, check here.

| State | Type | Location | Writers | Readers | Purpose |
|-------|------|----------|---------|---------|---------|
| `meshDirty` | `atomic.Bool` | `shard.go` | SaveShard (VesselGraph) via `MarkMeshDirty()` | Synthesizer via `IsMeshDirty()` / `ClearMeshDirty()` | Signals mesh changed, triggers synthesis cycle |
| `GlobalLogger` | `LogFunc` | `shard.go` | main.go (set once at startup) | Janitor, Synthesizer, HygieneWorker, VesselGraph (SaveShard, ArchiveShard) | Broadcasts system events to Activity Ledger |
| `vectorPool` | `sync.Pool` | `vessel.go` | `DecodeVector()` (returns buffers) | `vec_distance_cosine` UDF, MMR, `isResonantToCore()` | Reduces GC pressure for 768-D float buffers |
| `vectorDimension` | `int` | `vessel.go` | `SetVectorDimension()` in main.go (once) | Vessel pool allocation, Embedder | Controls pool buffer size |
| `communityCache` | `map[string]CommunityMetrics` | `vessel_graph.go` | `CalculateCommunities()` | `nodeToShardWithCache()` | Delta-write optimization — only writes changed PageRank/community values |
| `sessionTokens` | `map[string]time.Time` | `server.go` | `handleOAuthToken()` | `withAuth()` middleware | Ephemeral OAuth session tokens (24hr TTL) |
| `pendingCodes` | `map[string]authCode` | `server.go` | `handleOAuthAuthorize()` | `handleOAuthToken()` | One-time authorization codes (5 min TTL) |

---

## 7. Environment Variable Reference

| Variable | Component(s) | Type | Default | Purpose |
|----------|-------------|------|---------|---------|
| `HUB_API_KEY` | MCP Server | String | (optional) | Direct auth key; if empty, no auth enforced |
| `PUBLIC_URL` | MCP Server | URL | `http://localhost:8080` | OAuth redirect base URL |
| `EMBEDDING_MODE` | main.go | Enum | `none` | `none` / `server` / `local` — selects Embedder implementation |
| `EMBEDDING_MODEL` | GeminiEmbedder | String | `gemini-embedding-001` | Gemini embedding model name |
| `EMBEDDING_DIMENSION` | SetVectorDimension, Embedder, VesselGraph | Int | `768` | Vector dimensionality (Matryoshka truncation from 3072-D) |
| `GEMINI_API_KEY` | GeminiEmbedder, GeminiSummarizer | String | (required if mode=server) | Gemini API auth |
| `SUMMARIZER_MODEL` | GeminiSummarizer | String | `gemini-2.5-flash` | LLM model for summarization + salience scoring |
| `OLLAMA_URL` | OllamaEmbedder | URL | `http://localhost:11434` | Local Ollama embedding service |
| `OLLAMA_MODEL` | OllamaEmbedder | String | `nomic-embed-text` | Local embedding model name |
| `NEO4J_URL` | VesselGraph | URL | (optional) | Neo4j bolt connection URI |
| `NEO4J_USER` | VesselGraph | String | `neo4j` | Neo4j username |
| `NEO4J_PASS` | VesselGraph | String | (required if NEO4J_URL set) | Neo4j password |
| `DATABASE_URL` | PostgresVessel | URL | (optional) | PostgreSQL connection string |
| `DATABASE_PATH` | Vessel (SQLite) | Path | `./data/shard-link.db` | SQLite database file path |
| `MESH_LINK_THRESHOLD` | Synthesizer, VesselGraph (SaveShard linking) | Float | `0.75` | Min cosine similarity for auto-linking |
| `JANITOR_RESONANCE_THRESHOLD` | Janitor (GetEvictionCandidates) | Float | `0.70` | Min similarity to core for eviction protection |
| `SYNTHESIZER_INTERVAL_MINUTES` | Synthesizer | Int | `30` | Synthesis cycle frequency |
| `HYGIENE_INTERVAL_HOURS` | HygieneWorker | Int | `24` | Maintenance cycle frequency |
| `ACTIVITY_LOG_RETENTION_DAYS` | Vessel.SaveActivity | Int | `7` | Auto-purge activity logs older than N days |
| `MMR_LAMBDA` | search_memory handler | Float | `0.7` | Relevance vs. diversity balance (1.0 = pure relevance) |
| `COGNITIVE_BIAS_LAMBDA` | WorkingMemory.Bias | Float | `0.7` | Query vs. session centroid blend (1.0 = no bias) |

---

## 8. Data Flow Diagrams

### Save Path

```
Client → MCP save_memory
  ├── Validate category (reject "core" from external callers)
  ├── Embedder.Embed(content) ← if no vector provided
  ├── Summarizer.Summarize(salience_prompt) → [0.1, 1.0]
  ├── Repository.SaveShard(shard)
  │   ├── Upsert to storage
  │   ├── MarkMeshDirty() → signals Synthesizer
  │   └── [Neo4j] Immediate Associative Linking
  │       └── Auto-link to shards with cosine > MESH_LINK_THRESHOLD
  ├── [Neo4j] MERGE Episode node → link via EPISODE_OF
  └── GlobalLogger("Shard Saved") → Activity Ledger
```

### Search Path

```
Client → MCP search_[memory|text|graph|all]
  ├── [If vector needed] Embedder.Embed(query)
  ├── [If search_memory|graph] WorkingMemory.Bias(sessionID, vec, lambda)
  │   └── Blends query with session centroid (EMA)
  ├── Repository.Find[Resonant|Text|Hybrid] / SearchGraph
  │   └── Touch: update LastUsed [+ UseCount + RetrievalHistory on Neo4j]
  ├── [search_all only] Deduplicate → ReinforceShards(unique_ids)
  ├── [search_memory only] MMR re-ranking for diversity
  ├── WorkingMemory.Update(sessionID, results)
  │   └── EMA: new_centroid = 0.3 * mean(result_vecs) + 0.7 * old
  └── Return formatted results to client
```

### Eviction Path

```
Janitor timer (15 min)
  ├── GetCount() → check if over max (1000)
  ├── GetEvictionCandidates(overage)
  │   ├── [Cypher] Filter: non-core + resonance < threshold
  │   ├── [Go] For each candidate:
  │   │   └── SurvivalScoreV4(bondCount, pagerank, salience, retrievalHistory, lastUsed)
  │   │       ├── CalculateACTRActivation(history, 0.5)
  │   │       ├── SalToStability(salience) → base stability in days
  │   │       └── Exponential decay: numerator / e^(days / stability)
  │   └── Sort ascending → return lowest N
  ├── ArchiveShard(id) × N
  │   └── [Neo4j] DETACH DELETE / [SQLite] move to shards_archive
  ├── Optimize() → VACUUM / index rebuild
  └── GlobalLogger("Evicted N shards") → Activity Ledger
```

### Synthesis Path

```
Synthesizer timer (30 min)
  ├── IsMeshDirty()? → skip if false
  ├── SyncBonds(threshold)
  │   └── [Neo4j] Match all pairs, cosine > threshold → MERGE CONNECTED_TO
  ├── ClearMeshDirty()
  ├── [async, 2 min timeout]
  │   ├── CalculateCommunities()
  │   │   ├── GDS Louvain → assign CommunityID
  │   │   ├── GDS PageRank → assign PageRank
  │   │   └── Delta-write only changed nodes (via communityCache)
  │   └── PruneStaleSummaries() → delete orphaned comm-summary-* shards
  └── [async, 5 min timeout]
      └── For each changed community (>= 3 members):
          ├── GetShardsByCommunity(cid) → top 15 by PageRank
          ├── Summarizer.Summarize(prompt) → narrative
          ├── Embedder.Embed(summary) → vector
          ├── SaveShard(comm-summary-{cid}) → core shard (never evicted)
          └── Sleep 2 sec (Gemini rate limit)
```

---

## 9. Upgrade Checklist Template

Copy this when planning any partial upgrade:

```
## Upgrade: [Component / Field / Formula Name]

### What changed:
- [ ] File: _____ Function: _____
- [ ] Description: _____

### Upstream audit (what feeds INTO this component):
- [ ] _____ — still provides correct input format?
- [ ] _____ — still triggers at the right time?

### Downstream audit (what this component FEEDS):
- [ ] _____ — still receives expected output?
- [ ] _____ — still handles edge cases (nil, zero, empty)?

### Backend parity check:
- [ ] Neo4j — uses new behavior?
- [ ] SQLite — uses new behavior or correct default?
- [ ] Postgres — uses new behavior or correct default?

### Shared state check:
- [ ] Any globals read/written? (meshDirty, communityCache, GlobalLogger, vectorPool)
- [ ] Race condition risk? (concurrent goroutines?)

### Tests:
- [ ] go build ./...
- [ ] go test ./internal/janitor/...
- [ ] go test ./internal/storage/...
- [ ] Manual: Visual Ego scores match Janitor ranking?
- [ ] Manual: save shard → verify touch → verify survival score change
```
