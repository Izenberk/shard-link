# Shard-Link: Implementation Roadmap

This document serves as the step-by-step checklist for the Shard-Link "Memory Hub." Each phase follows the **Active Learning** mandate: I (Gemini) provide the "Why" and the scaffolds; you (Izenberk) implement the core logic.

## Phase 1: Environment & Tooling (Config Layer)
- [x] Initialize `go.mod` and add dependencies (`ncruces/go-sqlite3` or similar CGO-free driver).
- [x] Setup Docker Compose for dual-boot (Ubuntu/Windows 11) using bind-mounts to the shared NTFS/ExFAT partition.
- [ ] Verify `sqlite-vec` extension loading in a test environment.

## Phase 2: The Vessel (Storage Layer)
- [ ] Define `Shard` and `ShardBond` structs with JSONB metadata support.
- [ ] Create `schema.sql` (or Go migration logic) for:
    - `shards` table (id, category, resonance, last_used, vector BLOB, metadata JSONB).
    - `shard_bonds` table (from_id, to_id, weight).
- [ ] Implement `Vessel.SaveShard` logic.
- [ ] Implement `Vessel.FindResonant` using `sqlite-vec`'s cosine similarity.

## Phase 3: The Janitor (Resonance & Eviction)
- [ ] Implement the `Scorer` interface for calculating resonance and link count.
- [ ] Implement the **Deterministic Eviction Hierarchy**:
    1. Skip `core` category.
    2. Sort by `LastUsed` (Oldest first).
    3. Sort by `LinkCount` (Least related/orphans first).
    4. Sort by `DataSize` (Largest first).
- [ ] Setup a background worker to run the Janitor on a configurable interval.

## Phase 4: Model Context Protocol (The Bridge)
- [ ] Setup the MCP JSON-RPC server over SSE (Server-Sent Events).
- [ ] Implement `mcp.ListTools` (to expose memory search as a tool).
- [ ] Implement `mcp.CallTool` to execute `FindResonant` searches.
- [ ] Implement `mcp.ListResources` (to expose the "Core Shard" as a system resource).

## Phase 5: Deployment & Privacy (The Cloud)
- [ ] Configure `cloudflared` (Cloudflare Tunnel) in Docker Compose for secure, encrypted remote access.
- [ ] Implement mutual TLS (mTLS) for the MCP endpoint.
- [ ] Final end-to-end testing between local Go daemon and external AI (Gemini/ChatGPT).

---
*Status: Initial Plan Drafting | Date: 2026-04-04*
