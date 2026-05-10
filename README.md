# Shard-Link: Resonant Context Engine

Shard-Link is a high-performance "Memory Hub" designed to provide long-term memory for AI agents. It acts as a semantic gatekeeper, bridging raw data into LLM context windows using a fragmented, local-first storage model.

## 1. Project Objective
Maintain a persistent "Remembrance" across AI sessions through high-performance Go-based context routing, ensuring "Your Shards, Your Vessel" (Privacy & Safety).

## 2. Technical Stack
- **Backend:** Go (Golang) 1.26+ (Strict SOLID standards).
- **Database:** SQLite (via `ncruces/go-sqlite3` WASM/wasm2go driver).
- **Resonance Engine:** Manual Go-based implementation of `vec_distance_cosine` (Refactoring to SQL-SIMD).
- **Performance:** `sync.Pool` for vector buffer recycling (NTFS/ExFAT optimization).
- **Metadata:** JSONB (Binary JSON) for flexible "Ego" state.
- **Protocol:** MCP (Model Context Protocol) over SSE/JSON-RPC.
- **Deployment:** Multi-stage Distroless Docker image for Ubuntu/Windows 11 dual-boot.

## 3. Quick Start (Production)

### 1. Configure the Vessel
Create a `.env` file in the project root:
```bash
# SHARD-LINK Configuration
PUBLIC_URL=https://hub.izenberk.com
HUB_API_KEY=shl_live_your_secret_token  # Required for Zero-Proxy Access
CLOUDFLARE_TUNNEL_TOKEN=your_token_here
```

### 2. Ignite the Hub
```bash
docker compose up -d --build
```
The Hub will be available at `http://localhost:8080/sse` (Internal) and through your Cloudflare URL (External).

### 3. Connect to AI (MCP)
Add the following to your MCP client configuration (e.g., Gemini CLI, Claude Desktop):
```json
{
  "mcpServers": {
    "shard-link": {
      "url": "https://hub.izenberk.com/sse",
      "headers": {
        "X-API-Key": "shl_live_your_secret_token"
      }
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
The Janitor maintains memory density by "fading" shards to the Basement using a **Composite Survival Score**.

### Survival Formula:
Instead of rigid recency filtering, Shard-Link uses a weighted importance score:
`Score = (Links * Centrality) / (Time^Decay)`

1. **Category:** If `category == 'core'`, skip.
2. **Immunity:** If shard is strongly bonded (weight > 0.85), skip.
3. **Resonance Decay:** Shards with low survival scores are archived first, protecting foundational anchors even if they are old.

## 6. Development Philosophy
- **Active Learning:** Scaffolding provided by Gemini; core logic implemented by Izenberk.
- **Performance First:** Targeting sub-5ms retrieval via database-side SIMD vector math.

## 7. Security & Authentication Architecture

### Zero-Proxy Authentication
Shard-Link implements **Defense in Depth** to ensure security without requiring local sidecar proxies:
1. **The Edge (Cloudflare):** Tunnels protect the Hub from direct exposure.
2. **The App (Token Middleware):** A required `X-API-Key` header ensures only authorized agents can access the Vessel.
3. **The Transport (HTTPS):** Encryption-in-transit ensures that tokens cannot be sniffed.

---
*Status: MISSION COMPLETE (Zero-Proxy Access Deployed) | Date: 2026-04-17*
