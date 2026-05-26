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
- **Protocol:** MCP (Model Context Protocol) over Streamable HTTP (primary) and SSE (legacy).

## 3. Quick Start (Ready for Clone & Run)

### 1. Configure the Vessel
Create a `.env` file from `.env.example`:
```bash
# SHARD-LINK Configuration
HUB_API_KEY=shl_live_your_secret_token
GEMINI_API_KEY=your_gemini_key

# Mesh Configuration
NEO4J_URL=bolt://localhost:7687
NEO4J_USER=neo4j
NEO4J_PASS=shardpass
MESH_LINK_THRESHOLD=0.75
```

### 2. Ignite the Hub & Dashboard

#### **Option A: Secure Online (Default)**
Running this will start the Hub and the Cloudflare Tunnel automatically.
```bash
# Ensure CLOUDFLARE_TUNNEL_TOKEN is in your .env
docker compose up -d --build

# Start the Visual Ego Dashboard
go run cmd/visual_ego/main.go
```

#### **Option B: Pure Local (Offline)**
Use this if you want to explicitly disable external access.
```bash
docker compose --profile local up -d --build
```
- **Local Hub:** `http://localhost:8080/mcp` (Streamable HTTP) or `http://localhost:8080/sse` (Legacy SSE)
- **Visual Ego Dashboard:** `http://localhost:8081`
- **Neo4j Browser:** `http://localhost:7474` (Credentials: neo4j / shardpass)

## 4. Documentation & Research
- **[Technical Whitepaper (WHITEPAPER.md)](./WHITEPAPER.md):** Architectural deep-dive into the Knowledge Mesh and mathematical models.
- **[Implementation Roadmap (PLAN.md)](./PLAN.md):** Phase-by-phase development progress.
- **[Core Context (GEMINI.md)](./GEMINI.md):** Foundational mandates for AI agent integration.

## 5. Domain Language & Core Concepts
- **Shards:** Atomic contextual fragments (3072-D vectors).
- **Knowledge Mesh:** A relational graph where shards are nodes and semantic similarities are edges.
- **Autonomous Memory (Phase 11):** Proactive link creation and mesh maintenance.
- **Community Summaries (GraphRAG):** LLM-generated paragraph-level summaries of shard clusters, enabling multi-resolution retrieval (micro = shard, macro = community).
- **The Janitor:** Background process for size management using the **Survival Formula**.
- **The Synthesizer:** Background service that autonomously bonds resonant shards, recalculates communities (Louvain), and generates community summaries via Gemini.
- **Silicon Activity Feed:** A real-time terminal in the dashboard providing a persistent, interactive audit trail of all system actions.

### Survival Score (0-100):
The system calculates a "Probability of Retention" for every shard:
1. **Core Shards (100):** Immutable anchors representing user identity.
2. **Vital Memory (90-95):** Frequently used or highly connected "Knowledge Hubs."
3. **Transient Memory (<20):** Candidates for automated eviction (orphans or old data).

Formula: `S = (Density * Centrality * 10 * Vitality) / TimeDecay`
*Note: TimeDecay is based on **Last Used**, and Vitality increases with **Frequency**, meaning the system actively reinforces what you think about most.*

## 6. Development Philosophy
- **Active Learning:** Scaffolding provided by Gemini; core logic implemented by Izenberk.
- **Performance First:** Targeting sub-5ms retrieval via database-side SIMD vector math.

## 7. Deployment Modes

Shard-Link supports two primary deployment strategies to balance between maximum privacy and remote accessibility.

### Option A: Pure Local (Privacy First)
This is the default mode. It runs the entire stack—Neo4j, PostgreSQL, and the Shard-Link Hub—within your local network. No external traffic is permitted.
- **Connectivity:** All services are bound to `localhost`.
- **Security:** Naturally protected by your local firewall; no ports are exposed to the internet.
- **Usage:** Ideal for local-only agents or developers working with highly sensitive private data.
- **Command:** `docker compose up -d`

### Option B: Secure Online (Remote Hub)
This mode exposes your Hub to the internet via an encrypted **Cloudflare Tunnel**, allowing remote AI agents to connect to your Knowledge Mesh from anywhere in the world.
- **Connectivity:** Accessible via a custom domain (e.g., `hub.izenberk.com`).
- **Mechanism:** Uses an outbound-only connection to Cloudflare's edge; **no router port forwarding is required**.
- **Prerequisites:**
  - A Cloudflare-managed domain.
  - A `CLOUDFLARE_TUNNEL_TOKEN` (generated in the Cloudflare Zero Trust dashboard) added to your `.env`.
- **Command:** `docker compose --profile online up -d`

## 8. Security & Authentication Architecture

### Zero-Proxy Authentication
Shard-Link implements **Defense in Depth** to ensure security without requiring local sidecar proxies:
1. **The Edge (Cloudflare):** Tunnels protect the Hub from direct exposure.
2. **The App (Token Middleware):** A required `X-API-Key` header ensures only authorized agents can access the Vessel.
3. **The Transport (HTTPS):** Encryption-in-transit ensures that tokens cannot be sniffed.

## 9. Client Integration

Shard-Link communicates over the standard **Model Context Protocol (MCP)** using the **Streamable HTTP** transport (MCP spec 2024-11-05). Legacy SSE transport is kept at `/sse` for backward compatibility.

### Supported Transports

| Transport | Endpoint | Status | Spec |
|-----------|----------|--------|------|
| **Streamable HTTP** | `/mcp` | Primary (recommended) | MCP 2024-11-05 |
| **SSE** | `/sse` | Legacy (backward compat) | Deprecated |

### Claude Code (Recommended)

Add Shard-Link to your user-scoped MCP configuration:

```bash
claude mcp add --transport http shard-link https://hub.izenberk.com/mcp \
  --header "X-API-Key: YOUR_HUB_API_KEY_HERE" \
  --scope user
```

Verify the connection:
```bash
claude mcp get shard-link
```

### Claude Code Skill (`/shard`)

Create a global skill at `~/.claude/skills/shard/SKILL.md`:

```markdown
---
name: shard
description: Shard-Link memory interface - search and save to long-term AI memory via remote MCP
disable-model-invocation: false
argument-hint: [query or command]
---

# Shard-Link: Resonant Memory Interface

You are the Shard-Link architect. Your ONLY job is to fulfill the following request
using the registered MCP tools: **$ARGUMENTS**

## Available MCP Tools
- `shard-link:search_all` → PRIMARY. Searches Neo4j, Text Index, and Vector embeddings.
- `shard-link:save_memory` → Persist new facts.
```

Use it in any Claude Code session:
```text
/shard What is my favorite programming language?
/shard Remember: I prefer Go for backend development
```

---

## 10. Documentation & Roadmap
- **[Implementation Roadmap (PLAN.md)](./PLAN.md):** Track the phase-by-phase progress of the Hub.
- **[Improvements Track (IMPROVEMENT.md)](./IMPROVEMENT.md):** Architectural bottlenecks and performance optimization logs.
- **[Core Context (GEMINI.md)](./GEMINI.md):** Foundational mandates and domain logic for AI agents.

## 11. High-Resiliency Design
Shard-Link is built for production-grade stability across disparate environments:
- **Startup Resilience:** The Hub implements a 150-second "Ignition Loop," allowing it to wait for the Knowledge Mesh to finish intensive plugin installations (APOC/GDS) after a system reboot.
- **Tunnel Stability:** MCP connections are protected by aggressive 10-second heartbeats (`server.WithHeartbeatInterval`), preventing Cloudflare and other edge proxies from dropping idle sessions. Both Streamable HTTP (`/mcp`) and legacy SSE (`/sse`) transports benefit from this.
- **Asynchronous Thinking:** Intensive graph operations (Louvain communities) are offloaded to background goroutines, ensuring the semantic search tools remain responsive under load.

## 12. Community Summaries (GraphRAG)

The Synthesizer generates macro-level context for shard clusters using LLM summarization:

1. **Bond Detection:** Every 10 minutes, `SyncBonds` discovers new semantic relationships (cosine similarity > threshold).
2. **Community Detection:** Louvain clustering (Neo4j GDS) groups bonded shards into neighborhoods. A delta cache ensures only changed communities trigger work.
3. **Summarization:** For each changed community with 2+ members, the top 15 shards (by PageRank) are sent to Gemini 2.5 Flash, which produces a cohesive paragraph summary.
4. **Embedding & Storage:** The summary is embedded (3072-D vector) and saved as a `core` shard with a deterministic ID (`comm-summary-{communityID}`). MERGE upsert ensures old summaries are overwritten when communities evolve.

**Feedback loop prevention:** `GetShardsByCommunity` excludes `comm-summary-*` shards from the prompt input, so summaries can never feed into their own regeneration.

**Fault isolation:** Summarization runs in a detached goroutine with a separate 5-minute timeout. Gemini API failures are logged and skipped — the MCP server is never affected. Rate limiting (2s between calls) respects the Gemini free tier (15 RPM).

---
*Status: PHASE 6.1 COMPLETE (GraphRAG Community Summaries) | Transport: Streamable HTTP (MCP 2024-11-05) | Date: 2026-05-27*
