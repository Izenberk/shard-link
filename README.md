# Shard-Link: Resonant Context Engine

Shard-Link is a high-performance "Memory Hub" designed to provide long-term memory for AI agents. It acts as a semantic gatekeeper, bridging raw data into LLM context windows using a fragmented, local-first storage model.

## 1. Project Objective
Maintain a persistent "Remembrance" across AI sessions through high-performance Go-based context routing, ensuring "Your Shards, Your Vessel" (Privacy & Safety).

## 2. Technical Stack: The Triple-Engine Architecture
Shard-Link utilizes a multi-vessel storage strategy to optimize for intelligence, stability, and scale:

- **Neo4j + GDS (The Knowledge Mesh):** Primary engine for relational reasoning, centrality analysis (PageRank), and community clustering.
- **SQLite (The Seed Memory):** Local-first anchor for Core Identity shards and the persistent activity ledger.
- **PostgreSQL + pgvector (The Archival Vessel):** High-volume relational scaler for deep memory archiving and SIMD-accelerated search.
- **Backend:** Go (Golang) 1.26+ (Strict SOLID standards).
- **Dashboard:** React 19 + Vite 6 + D3.js — "Neural Observatory" design system.
- **Protocol:** MCP (Model Context Protocol) over Streamable HTTP (primary) and SSE (legacy).

## 3. Getting Started

See the **[Setup & Deployment Guide (SETUP.md)](./SETUP.md)** for full instructions on configuration, deployment modes, security, and client integration.

Quick version:
```bash
cp .env.example .env   # Configure credentials
docker compose up -d --build
```

## 4. Documentation
- **[Setup & Deployment Guide (SETUP.md)](./SETUP.md):** Configuration, deployment modes, authentication, and client integration.
- **[Technical Whitepaper (WHITEPAPER.md)](./WHITEPAPER.md):** Architectural deep-dive into the Knowledge Mesh and mathematical models.
- **[Claude.ai Skill (skills/claude-ai/SKILL.md)](./skills/claude-ai/SKILL.md):** MCP connector setup and usage guide for Claude.ai integration.

## 5. Domain Language & Core Concepts
- **Shards:** Atomic contextual fragments (768-D vectors).
- **Knowledge Mesh:** A relational graph where shards are nodes and semantic similarities are edges.
- **Autonomous Memory (Phase 11):** Proactive link creation and mesh maintenance.
- **Community Summaries (GraphRAG):** LLM-generated paragraph-level summaries of shard clusters, enabling multi-resolution retrieval (micro = shard, macro = community).
- **The Janitor:** Background process for size management using the **Survival Formula (v4.2)**.
- **The Synthesizer:** Background service that autonomously bonds resonant shards, recalculates communities (Louvain), and generates community summaries via Gemini. Guarded by a **Dynamic Summarizer Gate** — defers synthesis until `SYNTHESIZER_MIN_DIRTY_SHARDS` (default 5) accumulate or `SYNTHESIZER_MAX_DEFERRAL_HOURS` (default 24h) elapse, whichever comes first.
- **Salience:** LLM-scored importance weight [0.1, 1.0] assigned at save time — trivial shards decay faster.
- **Episodic Sessions:** Shards saved in the same MCP session are linked to `Episode` nodes for temporal narrative recall.
- **Silicon Activity Feed:** A real-time terminal in the dashboard providing a persistent, interactive audit trail of all system actions.

### Survival Score (0-100):
The system calculates a "Probability of Retention" for every shard:
1. **Core Shards (100):** Immutable anchors representing user identity.
2. **Vital Memory (90-95):** Frequently used or highly connected "Knowledge Hubs."
3. **Transient Memory (<20):** Candidates for automated eviction (orphans or old data).

Formula (v4.2): `S = min(95, (D * (C+1) * 10 * Sal) / e^(Δt_days / S₀))` where `S₀ = S_base(Sal) * (1 + A(m))`
- **D (Neural Density)** = bond count, floored at 1 (v4.2) — prevents cold-start eviction of unbonded shards before The Synthesizer links them
- **S_base(Sal)** = FSRS-calibrated stability in days (0.1→1d, 0.5→~7d, 1.0→14d) — salience directly controls decay window
- **A(m)** = ACT-R activation from retrieval history — extends stability via `(1 + A(m))`, can never collapse it
- **Sal** = LLM-scored salience [0.1, 1.0] — trivial shards decay faster than critical ones

## 6. MCP Tool Suite

Shard-Link exposes 13 MCP tools for AI agent integration:

**Search Tools:**
| Tool | Purpose |
|------|---------|
| `search_memory` | Vector similarity search with MMR diversity re-ranking |
| `search_text` | Full-text search across shard content |
| `search_graph` | Multi-hop graph traversal from nearest shard |
| `search_all` | Unified fan-out across all three engines (RRF-ranked) |

**Observation Tools (metadata-only, no touch/reinforcement):**
| Tool | Purpose |
|------|---------|
| `get_status` | Mesh statistics + service health (hub, neo4j, postgres) |
| `get_shard` | Retrieve a single shard by ID |
| `get_core_shards` | List all identity anchor shards |
| `get_recent_shards` | List recently saved shards (with optional category filter) |
| `get_shards_by_category` | List shards filtered by category |
| `get_at_risk_shards` | List shards below a survival score threshold |

**CRUD Tools:**
| Tool | Purpose |
|------|---------|
| `save_memory` | Persist new shards (with LLM salience scoring + auto-embedding) |
| `update_shard` | Modify content/category/metadata of existing shards |
| `delete_shard` | Permanently remove a shard from all backends |

## 7. Development Philosophy
- **Active Learning:** Scaffolding provided by Gemini; core logic implemented by Izenberk.
- **Performance First:** Targeting sub-5ms retrieval via database-side SIMD vector math.

## 8. High-Resiliency Design
Shard-Link is built for production-grade stability across disparate environments:
- **Startup Resilience:** The Hub implements a 150-second "Ignition Loop," allowing it to wait for the Knowledge Mesh to finish intensive plugin installations (APOC/GDS) after a system reboot.
- **Tunnel Stability:** MCP connections are protected by aggressive 10-second heartbeats (`server.WithHeartbeatInterval`), preventing Cloudflare and other edge proxies from dropping idle sessions. Both Streamable HTTP (`/mcp`) and legacy SSE (`/sse`) transports benefit from this.
- **Asynchronous Thinking:** Intensive graph operations (Louvain communities) are offloaded to background goroutines, ensuring the semantic search tools remain responsive under load.

## 9. Community Summaries (GraphRAG)

The Synthesizer generates macro-level context for shard clusters using LLM summarization:

1. **Dynamic Gate:** The Synthesizer defers work until enough shards accumulate (`SYNTHESIZER_MIN_DIRTY_SHARDS`, default 5) or a max deferral window elapses (`SYNTHESIZER_MAX_DEFERRAL_HOURS`, default 24h). This prevents expensive graph + LLM work from firing on every single shard save.
2. **Bond Detection:** Every 30 minutes (if gate passes), `SyncBonds` discovers new semantic relationships (cosine similarity > threshold).
3. **Community Detection:** Louvain clustering (Neo4j GDS) groups bonded shards into neighborhoods. A delta cache ensures only changed communities trigger work.
4. **Summarization:** For each changed community with 3+ members, the top 15 shards (by PageRank) are sent to Gemini 2.5 Flash Lite, which produces a cohesive paragraph summary.
5. **Embedding & Storage:** The summary is embedded (768-D vector) and saved as a `core` shard with a deterministic ID (`comm-summary-{communityID}`). MERGE upsert ensures old summaries are overwritten when communities evolve.

**Self-trigger prevention:** `comm-summary-*` shards are prefix-gated in `SaveShard()` — they do not increment the dirty counter, preventing the Synthesizer's own writes from re-arming the accumulation gate.

**Feedback loop prevention:** `GetShardsByCommunity` excludes `comm-summary-*` shards from the prompt input, so summaries can never feed into their own regeneration.

**Fault isolation:** Summarization runs in a detached goroutine with a separate 5-minute timeout. Gemini API failures are logged and skipped — the MCP server is never affected. Rate limiting (2s between calls) respects the Gemini free tier (15 RPM).

---
*Status: PHASE 7 COMPLETE (World-Class Engine) | Visual Ego: React 19 + Vite 6 (Neural Observatory) | Formula: v4.2 | 13 MCP Tools | Transport: Streamable HTTP (MCP 2024-11-05) | Date: 2026-06-13*
