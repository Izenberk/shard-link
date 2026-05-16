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

## 🚀 Current Focus: Multi-Tenancy, Local Inference & UI

### 1. Visual Ego Live Dashboard (Planned UI Upgrade)
- **Goal:** Upgrade the `visual_ego` tool from a static HTML generator to the primary live-serving frontend, replacing the Neo4j query UI.
- **Requirements:**
  - Convert `cmd/visual_ego` into a live Go `http.Server`.
  - Extract the D3.js HTML/CSS/JS into a separate `web/static/` directory.
  - Implement an `/api/graph` endpoint for live data fetching without page reloads.
  - Implement an `/api/search` endpoint utilizing `SearchGraph` for real-time "Multi-Hop" sub-graph exploration.
  - Future: Add interactive hooks in the D3 UI to edit or delete shards directly from the graph view.

### 2. Multi-Tenant Isolation
- **Goal:** Support isolated memory vessels for multiple users within the same Postgres instance.
- **Requirement:** Add `UserID` indexing to the `shards` table and enforce ownership checks in the Repository layer.

### 3. Phase 8: Local Embedding Pipeline
- **Goal:** Eliminate the dependency on external embedding APIs.
- **Requirement:** Integrate a local embedder (e.g., Ollama or Go-native `local-embed`) to handle the 1536-D generation during `save_memory`.

### 4. Janitor Refinement: Graph Centrality
- **Goal:** More accurately identify "hub" shards.
- **Requirement:** Implement a lightweight PageRank-inspired centrality check during the scoring phase to ensure the Knowledge Mesh remains structurally sound.

---
*Last Updated: 2026-05-14*
