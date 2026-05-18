# Shard-Link: Improvements & Optimization Track

This document tracks the resolution of architectural bottlenecks and outlines future performance targets.

## ✅ Resolved: Vector Math Performance Bottleneck (2026-05-10)
**The Problem:** Calculating 1536-D vector cosine distances in the Go application layer was causing massive heap allocations (6KB per vector) and triggering "stop-the-world" Garbage Collector pauses.
**The Solution:** Migrated to **PostgreSQL + pgvector**. 
- Vector math is now offloaded to the database using native SQL SIMD operators (`<=>`).
- Go only receives the final IDs, drastically reducing heap pressure and stabilizing retrieval times to sub-5ms.

## ✅ Resolved: Flawed Janitor Eviction Logic (2026-05-10)
**The Problem:** Deterministic recency filtering was deleting "load-bearing" old shards and keeping irrelevant new "orphan" shards.
**The Solution:** Implemented the **Composite Survival Score**.
- `Score = (Links * Centrality) / (Time^Decay)`
- This protects foundational data (high links/centrality) regardless of age, while naturally fading irrelevant context.

## ✅ Resolved: Visual Ego Live Dashboard (2026-05-16)
**The Problem:** The `visual_ego` tool was a static HTML generator, requiring a manual rebuild to see Knowledge Mesh changes.
**The Solution:** Converted `cmd/visual_ego` into a live Go HTTP server.
- Extracted frontend to `web/static/`.
- Implemented `/api/graph` for real-time mesh data fetching with Louvain/PageRank analysis.
- Implemented `/api/search` for semantic "Multi-Hop" sub-graph exploration.

## ✅ Resolved: Local Embedding Pipeline (2026-05-16)
**The Problem:** Dependency on external APIs (Gemini) posed a long-term privacy and availability risk.
**The Solution:** Implemented the `OllamaEmbedder` provider.
- Added dynamic configuration via `EMBEDDING_MODE=local`.
- Supported local models (e.g., `nomic-embed-text`) via the Ollama REST API.

## ✅ Resolved: Janitor Refinement: Graph Centrality & Core Resonance (2026-05-16)
**The Problem:** Eviction logic was purely structural, potentially deleting disconnected but semantically vital personal context.
**The Solution:** Integrated **Core Resonance Protection** and **PageRank Centrality**.
- Shards with high similarity (>0.70) to Core Identity shards are now automatically protected.
- Eviction targets are now identified using PageRank (via GDS) to preserve "hub" shards that link disparate contexts.
- All thresholds externalized to `.env` for transparent tuning.

## ✅ Resolved: Enhanced Observability & Metric Definitions (2026-05-17)
**The Problem:** Dashboard metrics like `NEURAL_DENSITY` and `RELATIONAL_COMMUNITY` were conceptually dense and lacked immediate explanation for the user.
**The Solution:** Integrated context-aware tooltips and standardized terminology.
- Added `title` attributes to all UI labels in `visual_ego` to provide instant technical definitions on hover.
- Aligned `README.md` and `IMPROVEMENT.md` terminology with the UI (e.g., standardizing on "Neural Density").
- Clarified the meaning of `IDENTITY_ANCHORS` vs `SYSTEM_KNOWLEDGE` within the mesh topology.

## 🚀 Current Focus: Autonomous Memory & Relational Synthesis

### 1. Automatic Relational Synthesis (Phase 11)
- **Goal:** Enable the system to autonomously "think" and link disparate memories without manual triggers.
- **Requirement:** Refactor the `auto_link` tool into a periodic background service (similar to the Janitor) to maintain Knowledge Mesh density in real-time.

## ✅ Resolved: Usage Reinforcement & Long-Term Potentiation (2026-05-17)
**The Problem:** Memory survival was only based on the creation date, meaning ancient but vital knowledge would eventually fade even if frequently used.
**The Solution:** Implemented **LTP (Long-Term Potentiation)** logic.
- Survival score is now calculated using `LastUsed` rather than `CreatedAt`.
- Implemented **"Touch-on-Search"**: Every retrieval resets the decay clock and increments a `use_count`.
- Added **Vitality Boost**: Frequent usage provides a multiplicative boost (+10% per hit) to survival probability, mimicking neural pathway strengthening.

## ✅ Resolved: Persistent Activity Ledger & Cross-Process Auditing (2026-05-17)
**The Problem:** Dashboard logs were in-memory and vanished on refresh; furthermore, actions taken via MCP tools were invisible to the local dashboard.
**The Solution:** Implemented a shared **SQLite Activity Ledger**.
- Created an `activity_logs` table in the 'Seed Memory' vessel.
- Unified the Hub (Docker) and Dashboard (Local) processes to log all actions to this shared table.
- Implemented **Log Hydration**: The dashboard now fetches historical logs on startup, providing a durable, clickable audit trail.

## ✅ Resolved: HUD Redesign & Ergonomic Command Bar (2026-05-17)
**The Problem:** As more monitoring panels were added (Activity Feed, Glossary), the UI became cluttered and control panels began to overlap.
**The Solution:** Completely refactored the Visual Ego layout.
- Moved camera and bond controls to a floating horizontal **Command Bar** at the bottom-center.
- Unified search and topology stats into a single **Knowledge Sidebar**.
- Integrated a dedicated, scrollable **Activity Terminal** in the bottom-left.

## ✅ Resolved: MCP Stability & Startup Resilience (2026-05-17)
**The Problem:** The Hub service often crashed or failed to initialize if Neo4j was still in its slow plugin installation phase (especially after a system restart). Furthermore, Cloudflare tunnels frequently timed out long-lived SSE connections due to idle activity.
**The Solution:** Implemented a multi-layered stability patch.
- **Hub Ignition Retry Loop:** Added a 15-attempt (150s) retry loop for database connections in `main.go`.
- **Healthcheck Orchestration:** Updated `docker-compose.yml` to wait for a `service_healthy` signal from Neo4j before starting the Hub.
- **Aggressive Heartbeats:** Reduced the MCP SSE keep-alive interval from 15s to 10s to maintain stable tunnel connectivity.
- **Non-Blocking Synthesis:** Refactored the `Synthesizer` to execute intensive Louvain community refreshes asynchronously, preventing main-loop blocking and session timeouts.

## ✅ Resolved: Surgical Archival & Dynamic Perimeter (2026-05-19)
**The Problem:** Manual eviction was a "destructive" process that permanently deleted data, and there was no architectural protection for Core Identity shards. Furthermore, the dashboard lacked a visual distinction between active knowledge and cold-storage fragments.
**The Solution:** Implemented the **"White Dwarf" Archival Model**.
- **Multi-Vessel Migration:** Eviction now moves shards from the Neo4j "Living Mesh" to the PostgreSQL "Archival Vessel," severing high-cost bonds while preserving the underlying data.
- **Double-Lock Protection:** Implemented UI-level button hiding and API-level 403 Forbidden rejection to prevent accidental eviction of `core` identity shards.
- **Dynamic Orbital Physics:** The dashboard now calculates the "Galactic Perimeter" dynamically (Farthest Shard + 300px), ensuring archived White Dwarfs always orbit in a clear, non-overlapping outer system.
- **Architectural Dashboard Hardening:** Implemented a global **Adjacency Map** and **Position Preservation Layer** in Visual Ego. This eliminates race conditions during rapid selection, provides $O(1)$ neighbor lookups, and ensures the mesh remains stationary during background metric updates, behaving exactly like a manual refresh.
- **Manual Metric Control:** Introduced the `REFRESH_METRICS` protocol, allowing the user to sync cognitive stats (Survival/PageRank) on-demand while maintaining a stable, pulse-free environment during active inspection.

---
*Last Updated: 2026-05-19*
