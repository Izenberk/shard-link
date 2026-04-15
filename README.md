# Shard-Link: Resonant Context Engine

Shard-Link is a high-performance "Memory Hub" designed to provide long-term memory for AI agents. It acts as a semantic gatekeeper, bridging raw data into LLM context windows using a fragmented, local-first storage model.

## 1. Project Objective
Maintain a persistent "Remembrance" across AI sessions through high-performance Go-based context routing, ensuring "Your Shards, Your Vessel" (Privacy & Safety).

## 2. Technical Stack
- **Backend:** Go (Golang) 1.26+ (Strict SOLID standards).
- **Database:** SQLite (via `ncruces/go-sqlite3` WASM/wasm2go driver).
- **Resonance Engine:** Manual Go-based implementation of `vec_distance_cosine` (CGO-free).
- **Performance:** `sync.Pool` for vector buffer recycling (NTFS/ExFAT optimization).
- **Metadata:** JSONB (Binary JSON) for flexible "Ego" state.
- **Protocol:** MCP (Model Context Protocol) over SSE/JSON-RPC.
- **Deployment:** Multi-stage Distroless Docker image for Ubuntu/Windows 11 dual-boot.

## 3. Quick Start (Production)

### Run with Docker
```bash
docker compose up -d
```
The Hub will be available at `http://localhost:8080/sse`.

### Connect to AI (MCP)
Add the following to your MCP client configuration (e.g., Claude Desktop):
```json
{
  "mcpServers": {
    "shard-link": {
      "url": "http://localhost:8080/sse"
    }
  }
}
```

## 4. Domain Language & Core Concepts
- **Shards:** Atomic contextual fragments with 1536-dimensional embeddings.
- **Core Shards (Ego Anchors):** Immutable fragments defining user identity. **NEVER EVICTED.**
- **The Basement:** Tiered archival storage (`shards_archive`) for evicted memories.
- **Shard Bonds:** A relational "Knowledge Mesh" where shards are linked by cosine similarity (> 0.85).
- **Dependency Immunity:** Shards strongly bonded to active context are protected from archival.
- **The Vessel:** A hybrid relational-document storage model in a single SQLite file.

## 5. The Janitor (Size Management)
The Janitor is a background process that maintains memory density by "fading" shards to the Basement based on **Resonance** and **Relational Centrality**.

### Eviction Hierarchy (Deterministic):
1. **Category:** If `category == 'core'`, skip.
2. **Immunity:** If shard is strongly bonded (weight > 0.85), skip.
3. **Recency:** Sort by `LastUsed` (Oldest first).
4. **Connectivity:** Sort by `LinkCount` (Least related/orphans first).

## 6. Development Philosophy
- **Active Learning:** Scaffolding provided by Gemini; core logic implemented by Izenberk.
- **Standardized & Optimized:** Production-grade tiered storage and memory pooling.

---
*Status: MISSION COMPLETE | Date: 2026-04-15*

