# Shard-Link: Resonant Context Engine

Shard-Link is a high-performance "Memory Hub" designed to provide long-term memory for AI agents. It acts as a semantic gatekeeper, bridging raw data into LLM context windows using a fragmented, local-first storage model.

## 1. Project Objective
Maintain a persistent "Remembrance" across AI sessions through high-performance Go-based context routing, ensuring "Your Shards, Your Vessel" (Privacy & Safety).

## 2. Technical Stack
- **Backend:** Go (Golang) 1.22+ (Strict SOLID standards).
- **Database:** SQLite + `sqlite-vec` (SIMD-accelerated Vector Search).
- **Metadata:** JSONB (Binary JSON) for flexible "Ego" state.
- **Protocol:** MCP (Model Context Protocol) over SSE/JSON-RPC.
- **Environment:** Docker-native for Ubuntu/Windows 11 dual-boot via shared partitions.

## 3. Domain Language & Core Concepts
- **Shards:** Atomic contextual fragments with 1536-dimensional embeddings.
- **Core Shards (Ego Anchors):** Immutable fragments defining user identity, philosophy, and standards. **NEVER EVICTED.**
- **Shard Bonds:** A relational "Knowledge Mesh" where shards are linked based on cosine similarity (> 0.85) or explicit refinement.
- **The Vessel:** A hybrid relational-document storage model within a single SQLite file.

## 4. The Janitor (Size Management)
The Janitor is a background process that maintains memory density by evicting shards based on **Resonance** and **Relational Centrality**.

### Eviction Hierarchy (Deterministic):
1. **Category:** If `category == 'core'`, skip eviction.
2. **Recency:** Sort by `LastUsed` (Oldest first).
3. **Connectivity (Tie-Breaker):** Sort by `LinkCount` (Least related/orphans first).
4. **Density:** If still tied, sort by `DataSize` (Largest first).

## 5. Development Philosophy
- **Active Learning:** Implementations focus on interfaces and scaffolds; core logic (scoring/linking) is built by the developer to ensure deep understanding.
- **"Why" First:** Architectural rationale precedes code implementation.
- **Surgical Design:** Prioritize performance and "Saver" principles (Token Gatekeeping).

---
*Status: Architecture Confirmed | Date: 2026-04-04*
