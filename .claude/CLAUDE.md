# Shard-Link: Project Context

## Project Essence
Shard-Link is a high-performance context engine providing "long-term memory" for AI agents. It bridges raw data to LLM context windows using a fragmented storage model exposed via MCP (Model Context Protocol).

## Knowledge Priority (Source of Truth)
- **Primary:** Neo4j Knowledge Mesh — "Living Memory." All project progress, technical decisions, and active context retrieved via MCP tools.
- **Anchor:** SQLite (`shard-link.db`) — "Seed Memory." Core Identity shards and Activity Ledger.
- **Archival:** PostgreSQL + pgvector — high-volume relational scaling for archived shards.

## Technical Stack
- **Backend:** Go (Golang) — strict SOLID & production standards
- **Knowledge Mesh:** Neo4j 5.x + GDS + APOC
- **Archival Storage:** PostgreSQL + pgvector + JSONB metadata
- **Protocol:** MCP via Streamable HTTP (hub.izenberk.com/mcp)
- **Infra:** Docker Compose, Cloudflare Tunnel

## Vector Precision Rules
- **Internal format:** `[]float32`
- **Serialization:** Use `strconv.FormatFloat(f, 'g', -1, 32)` — NEVER `fmt.Sprintf("%f")`
- **Dimensions:** 3072-D (Gemini/Production compatible)

## Domain Language
- **Shards:** Atomic contextual fragments with vector embeddings
- **Core Shards:** Immutable identity anchors — NEVER evict
- **Bonds:** Semantic relationships between shards (cosine similarity > threshold)
- **The Janitor:** Background eviction process based on Resonance + Relational Centrality
- **The Synthesizer:** Background linker that autonomously bonds resonant shards
- **HygieneWorker:** Background maintenance (VACUUM, index integrity)

## Thresholds (env vars)
- `MESH_LINK_THRESHOLD` — min cosine similarity for auto-linking (default: 0.75)
- `JANITOR_RESONANCE_THRESHOLD` — min resonance to protect from eviction (default: 0.70)

## Active Plans
- **HARDENING.md** — single source of truth for all upgrades (Phases 0-6)
- **PLAN.md** — frozen phase history (append-only)
- **IMPROVEMENT.md** — frozen resolved bottleneck log (append-only)

## Development & Learning Philosophy
This project is a **learning vehicle**. The developer learns by building, not by reading.
- **Full code, always:** Provide complete, production-grade implementations — no scaffolding, no "fill in the blanks"
- **The "Why" first:** Explain architectural logic and trade-offs BEFORE showing code
- **Muscle memory via typing:** The developer types out all code manually to internalize patterns — never copy-paste
- **Code reviews:** Focus on SOLID principles, Go idiomatic patterns, and edge cases
