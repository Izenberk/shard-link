# Shard-Link: Resonant Context Engine

Shard-Link is a high-performance "Memory Hub" designed to provide long-term memory for AI agents. It acts as a semantic gatekeeper, bridging raw data into LLM context windows using a fragmented, local-first storage model.

## 1. Project Objective
Maintain a persistent "Remembrance" across AI sessions through high-performance Go-based context routing, ensuring "Your Shards, Your Vessel" (Privacy & Safety).

## 2. Technical Stack
- **Backend:** Go (Golang) 1.26+ (Strict SOLID standards).
- **Database:** PostgreSQL + `pgvector` (Production-grade vector search).
- **Resonance Engine:** Native SQL SIMD operators (`<=>`) for cosine distance.
- **Performance:** Repository Pattern for storage abstraction; SIMD-accelerated retrieval.
- **Metadata:** JSONB (Binary JSON) for flexible "Ego" state.
- **Protocol:** MCP (Model Context Protocol) over SSE/JSON-RPC.
- **Deployment:** Multi-stage Distroless Docker image for Ubuntu/Windows 11 dual-boot.

## 3. Quick Start (Production)

### 1. Configure the Vessel
Create a `.env` file in the project root:
```bash
# SHARD-LINK Configuration
PUBLIC_URL=https://hub.izenberk.com
HUB_API_KEY=shl_live_your_secret_token  
GEMINI_API_KEY=your_gemini_key
NEO4J_URL=bolt://localhost:7687
MESH_LINK_THRESHOLD=0.70
```

### 2. Ignite the Hub & Dashboard
```bash
# Start the Core Hub
docker compose up -d --build

# Start the Visual Ego Dashboard (Live View)
go run cmd/visual_ego/main.go
```
The Hub will be available at `:8080/sse`, and your **Live Knowledge Mesh** is viewable at `http://localhost:8081`.

## 4. Domain Language & Core Concepts
- **Shards:** Atomic contextual fragments with 3072-dimensional embeddings.
- **Knowledge Mesh:** A relational graph (Neo4j) where shards are nodes and semantic similarities are edges.
- **Autonomous Memory (Phase 11):** The system proactively links resonant shards on-save and via background synthesis.
- **Shard Bonds:** Relationships established when cosine similarity exceeds `MESH_LINK_THRESHOLD` (default 0.70).
- **Core Resonance Protection:** Shards with high similarity to your core profile are autonomously protected from eviction.
- **Visual Ego:** A real-time, interactive D3.js dashboard for monitoring memory clusters, topical neighborhoods, and manual bond management.


## 5. The Janitor (Size Management)
The Janitor maintains memory density by "fading" shards to the Basement using a **Composite Survival Score**.

### Survival Formula:
Instead of rigid recency filtering, Shard-Link uses a weighted importance score:
`Score = (Links * Centrality) / (Time^Decay)`

1. **Category:** If `category == 'core'`, skip.
2. **Immunity:** If shard is strongly bonded (weight > 0.85), skip.
3. **Resonance Decay:** Shards with low survival scores are archived first, protecting foundational anchors even if they are old.
4. **Storage Hygiene:** The Janitor automatically runs DB-specific tuning (VACUUM ANALYZE, HNSW tuning, PRAGMA optimize) after evictions.

## 6. Development Philosophy
- **Active Learning:** Scaffolding provided by Gemini; core logic implemented by Izenberk.
- **Performance First:** Targeting sub-5ms retrieval via database-side SIMD vector math.

## 7. Security & Authentication Architecture

### Zero-Proxy Authentication
Shard-Link implements **Defense in Depth** to ensure security without requiring local sidecar proxies:
1. **The Edge (Cloudflare):** Tunnels protect the Hub from direct exposure.
2. **The App (Token Middleware):** A required `X-API-Key` header ensures only authorized agents can access the Vessel.
3. **The Transport (HTTPS):** Encryption-in-transit ensures that tokens cannot be sniffed.

## 8. Documentation & Roadmap
- **[Implementation Roadmap (PLAN.md)](./PLAN.md):** Track the phase-by-phase progress of the Hub.
- **[Improvements Track (IMPROVEMENT.md)](./IMPROVEMENT.md):** Architectural bottlenecks and performance optimization logs.
- **[Core Context (GEMINI.md)](./GEMINI.md):** Foundational mandates and domain logic for AI agents.

---
*Status: PHASE 11 COMPLETE (Autonomous Memory Active) | Date: 2026-05-16*
