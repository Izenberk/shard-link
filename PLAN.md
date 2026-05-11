# Shard-Link: Implementation Roadmap

This document serves as the step-by-step checklist for the Shard-Link "Memory Hub." Each phase follows the **Active Learning** mandate: I (Gemini) provide the "Why" and the scaffolds; you (Izenberk) implement the core logic.

## Phase 1: Environment & Tooling (Config Layer)
- [x] Initialize `go.mod` and add dependencies (`ncruces/go-sqlite3` v0.33+).
- [x] Setup Docker Compose for dual-boot (Ubuntu/Windows 11) using bind-mounts to the shared NTFS/ExFAT partition.
- [x] Verify manual `vec_distance_cosine` (Go) integration in `cmd/check_vec`.

## Phase 2: The Vessel (Storage Layer)
- [x] Define `Shard` and `ShardBond` structs with JSONB metadata support.
- [x] Create `schema.sql` with Tiered Storage support (`shards_archive`).
- [x] Implement `Vessel.SaveShard` with Upsert logic.
- [x] Implement `Vessel.FindResonant` using Go-based `vec_distance_cosine` function.
- [x] Implement `Vessel.ArchiveShard` for fading memories to the Basement.

## Phase 3: The Janitor (Resonance & Eviction)
- [x] Implement the `Scorer` interface for importance calculation.
- [x] Implement the **Standardized Eviction Hierarchy**:
    1. Skip `core` category.
    2. **Dependency Immunity**: Skip shards with `weight > 0.85` bonds.
    3. Sort by `LastUsed` (Oldest first).
    4. Sort by `LinkCount` (Least related/orphans first).
- [x] Implement `sync.Pool` for vector buffer optimization.
- [x] Setup a background worker to run the Janitor on a configurable interval.

## Phase 4: Model Context Protocol (The Bridge)
- [x] Setup the MCP JSON-RPC server over SSE (Server-Sent Events).
- [x] Implement `mcp.ListTools` (to expose memory search as a tool).
- [x] Implement `mcp.CallTool` to execute `FindResonant` searches.
- [x] Implement `mcp.ListResources` (to expose the "Core Shard" as a system resource).

## Phase 5: Deployment & Privacy (The Cloud)
- [x] Configure `cloudflared` (Cloudflare Tunnel) in Docker Compose.
- [x] Implement mutual TLS (mTLS) for the MCP endpoint. (Refactored to Token-Auth in Phase 6).
- [x] Final end-to-end testing between local Go container and local AI agent.

## Phase 6: Zero-Proxy Access (Authentication Refactor)
- [x] Implement **API Key / Token Middleware** in the MCP server.
- [x] Refactor `StartSSE` to remove mTLS debt in favor of **Defense in Depth** (Token + HTTPS).
- [x] Update `settings.json` with custom `headers` for direct, secure remote connection.

## Phase 7: High-Performance Scaling (PostgreSQL & pgvector)
- [x] Implement the **Repository Pattern** to abstract the storage layer.
- [x] Migrate from SQLite to **PostgreSQL + pgvector** to offload 1536-D math to SQL (SIMD).
- [x] Refactor Janitor to use a **Composite Survival Score**: `(Links * Centrality) / (Time^Decay)`.
- [x] Update the `Vessel` to support **Multi-Tenant** shard ownership (UserID field).

## Phase 8: Standalone Intelligence (Local Inference)
- [ ] Integrate a **Local Embedding Tool** (e.g., Ollama or a Go-native wrapper).
- [ ] Implement an `Embedder` interface to swap between local and cloud providers.
- [ ] Auto-generate embeddings during `save_memory` without external dependencies.

---
*Status: MISSION COMPLETE (Postgres Scaling Deployed) | Date: 2026-05-11*
