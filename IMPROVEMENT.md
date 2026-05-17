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

---
*Last Updated: 2026-05-16*
