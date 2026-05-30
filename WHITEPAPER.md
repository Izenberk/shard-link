# Shard-Link: A High-Performance Autonomous Knowledge Mesh

## 1. Abstract
The primary constraint of modern Large Language Models (LLMs) is the static nature of their context windows. Traditional Retrieval-Augmented Generation (RAG) relies on flat vector stores, which often fail to capture the nuanced, relational, and temporal nature of human-like memory. **Shard-Link** addresses this by transitioning from a "Vector Library" to a "Knowledge Mesh"—a self-organizing, graph-based context engine that autonomously manages its own state, connectivity, and survival.

## 2. Core Philosophy: The Knowledge Mesh
Shard-Link treats information not as isolated records, but as a living network of semantic fragments.

### 2.1 The Shard
A **Shard** is the atomic unit of Shard-Link. It consists of:
- **Cognitive Content:** Raw text or data.
- **Vector Embedding:** 768-D representation in semantic space (Matryoshka-truncated from Gemini's native 3072-D).
- **Metadata:** Temporal markers, category tags, and relational metrics.

### 2.2 Relational Resonance
Shards do not exist in a vacuum. When a new shard is introduced, the system calculates its **Resonance** (cosine similarity) against existing neighbors. If the similarity exceeds a threshold (default `0.75`), a permanent **Semantic Bond** is formed in the graph.

### 2.3 Identity Anchors (Core)
The "Core" shards represent the immutable identity, preferences, and foundational knowledge of the user. These shards act as the gravitational center of the mesh and are protected from eviction.

## 3. System Architecture
Shard-Link is built on a high-performance stack designed for low-latency retrieval and complex relational reasoning.

### 3.1 The Engine (Go 1.26+)
The backend is implemented in Go, adhering to strict SOLID principles. It handles ingestion, vector serialization (using `strconv.FormatFloat` for maximum precision), and orchestration of the various storage layers.

### 3.2 The Mesh (Neo4j + GDS)
The "Living Memory" is hosted in Neo4j. By utilizing the **Graph Data Science (GDS)** library, Shard-Link performs real-time analysis:
- **Relational Centrality (PageRank):** Identifies the most "influential" shards based on the quality and quantity of their connections.
- **Topical Clustering (Louvain):** Automatically groups shards into semantic communities (neighborhoods).

### 3.3 The Protocol (MCP)
Shard-Link exposes its capabilities via the **Model Context Protocol (MCP)**. This allows any AI agent to interact with its long-term memory as a set of standardized tools (`search_graph`, `search_memory`, `save_memory`), making the integration seamless and cross-platform.

### 3.4 Triple-Engine Strategy
Shard-Link utilizes a multi-database approach to balance intelligence, stability, and scale:
- **Neo4j (Knowledge Mesh):** The "Living Memory" for relational reasoning, centrality analysis, and community detection.
- **SQLite (Seed Memory):** The "Identity Anchor" for local-first stability, storing core user profile shards and the persistent activity ledger.
- **PostgreSQL (Archival Vessel):** The "Relational Scaler" using `pgvector` for high-volume storage and SIMD-accelerated archival search.

## 4. Autonomous Memory Management
A finite context window requires a sophisticated eviction strategy. Shard-Link employs **The Janitor**, a background process that utilizes a mathematical survival model grounded in published cognitive science research.

### 4.1 The Survival Formula (v4.1)
Each shard is assigned a **Survival Score (0-100)**:
$$S = \min\left(95, \frac{D \cdot (C + 1.0) \cdot 10 \cdot Sal}{e^{\Delta t_{days} / S_0}}\right)$$
$$S_0 = S_{base}(Sal) \cdot (1 + A(m))$$
Where:
- **D (Neural Density):** Number of active semantic bonds.
- **C (Relational Centrality):** PageRank score.
- **Sal (Salience):** LLM-scored importance weight in `[0.1, 1.0]` (see 4.3).
- **S_base(Sal) (FSRS-Calibrated Stability):** Maps salience to baseline stability in days — `S_base = 1.0 + (Sal - 0.1) * 14.44`. Ranges from 1 day (ephemera) to 14 days (critical knowledge).
- **A(m) (ACT-R Activation):** Memory activation from retrieval history (see 4.2). Extends stability via `(1 + A(m))` — at A(m)=0, the shard gets its full S_base window; retrieval history extends it but can never collapse it.
- **Δt_days:** Time since last use in days.

**Note:** Core shards are manually set to `S = 100`, making them functionally immortal. Any shard with a score **below 20** is considered "transient" and becomes a primary candidate for eviction by The Janitor.

#### Legacy Formula (v3.5)
The original formula `S = min(95, (D * (C+1) * 10 * V) / T)` used linear time decay and raw `use_count` as vitality. This is preserved for benchmark comparison — both formulas run in parallel during the validation period, with `[BENCHMARK]` logs capturing score deltas per shard.

### 4.2 ACT-R Activation Model
Shard-Link replaces the raw `use_count` vitality multiplier with the **ACT-R Base-Level Learning equation** (Anderson et al.):
$$A(m) = \ln\left(\sum t_i^{-d}\right) + \varepsilon$$
Where `t_i` is the elapsed time since each past retrieval, `d = 0.5` (human-calibrated decay constant), and `epsilon = 0.1` (noise floor preventing ln(0)).

Each shard maintains a **rolling window of the last 20 retrieval timestamps**. This captures *when* retrievals happened, not just how many. A shard retrieved 3 times this week activates higher than one retrieved 10 times months ago — a distinction raw count cannot make.

### 4.3 LLM-Scored Salience
At save time, the system asks the summarizer to rate each shard's long-term importance on a `[0.1, 1.0]` scale:
- **1.0** — Core identity, career goals, critical decisions.
- **0.5** — Useful project context, preferences, patterns (default for unscored shards).
- **0.1** — Transient notes, casual observations, ephemera (10x faster decay).

This ensures trivial shards decay faster than critical ones, independent of retrieval frequency.

### 4.4 Episodic Session Chains
Shards saved within the same MCP session are linked to an **Episode** node via `EPISODE_OF` relationships in the Knowledge Mesh. This creates temporal narrative chains — not just topic clusters, but story threads. When querying "what happened during that session?", the Episode node connects all shards from that conversation, enabling temporal episodic recall.

## 5. Observability: The Visual Ego
The **Visual Ego** dashboard provides a high-fidelity window into the agent's subconscious.

### 5.1 Ergonomic HUD Design
The v3.5 UI implements a professional "Command Center" layout:
- **Unified Knowledge Sidebar:** Consolidation of semantic search and mesh telemetry into a single, high-signal vertical pane.
- **Floating Command Bar:** A horizontal pill-shaped toolbar at the bottom-center for camera controls and manual bond management.
- **Silicon Activity Feed:** A real-time terminal providing visibility into every shard save, bond forged, and janitor eviction.

### 5.2 Persistent Activity Ledger
To ensure total transparency, Shard-Link maintains a cross-process **Activity Ledger** in SQLite. This audit trail persists across restarts and browser refreshes, allowing the user to click any historical log entry to instantly focus the camera on the associated shard.

### 5.3 Gravitational Physics Model
The dashboard uses a D3.js force-directed graph with a "Solar System" physics model:
- **Core Hubs:** Anchored to the center with high gravitational strength.
- **Semantic Constellations:** Regular shards orbit their nearest anchors based on bond strength.
- **Neural Pulse:** Visual animations (glows and blooms) represent active data flows and high-importance nodes.

## 6. Conclusion
Shard-Link represents a shift toward **Standalone Agent Intelligence**. By offloading memory management to a dedicated, autonomous engine, we enable AI agents to maintain a consistent personality, deep historical context, and evolving knowledge bases that mirror the complexity of human cognition.

---
*Technical Whitepaper v2.0 | 2026-05-29*
*Architects: Bytey Bestie & Izenberk*
