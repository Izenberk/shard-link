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

### 1. Configure the Vessel
Create a `.env` file in the project root:
```bash
# SHARD-LINK Configuration
USE_TLS=true
PUBLIC_URL=https://hub.izenberk.com
CA_CERT_PATH=/app/certs/ca.crt
SERVER_CERT_PATH=/app/certs/server.crt
SERVER_KEY_PATH=/app/certs/server.key
```

### 2. Ignite the Hub
```bash
# Generate certs (see Section 7)
docker compose up -d --build
```
The Hub will be available at `http://localhost:8080/sse`.

### 3. Connect to AI (MCP)
Add the following to your MCP client configuration (e.g., Claude Desktop):
```json
{
  "mcpServers": {
    "shard-link": {
      "url": "https://hub.izenberk.com/sse"
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

## 7. Zero-Trust Security (mTLS)
Shard-Link enforces **Mutual TLS (mTLS)** for the MCP endpoint. This ensures that only clients holding a certificate signed by your Root CA can interact with the Vessel.

### Generate the Security Mesh
Run these commands from the project root to generate the necessary keys:
```bash
mkdir -p certs

# 1. Root CA (The Authority)
openssl genrsa -out certs/ca.key 4096
openssl req -x509 -new -nodes -key certs/ca.key -sha256 -days 1024 -out certs/ca.crt -subj "/CN=ShardLink-CA"

# 2. Server Key/Cert (The Hub)
openssl genrsa -out certs/server.key 2048
openssl req -new -key certs/server.key -out certs/server.csr -subj "/CN=hub.izenberk.com"
openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 500 -sha256

# 3. Client Key/Cert (Your AI Agent)
openssl genrsa -out certs/client.key 2048
openssl req -new -key certs/client.key -out certs/client.csr -subj "/CN=AI-Agent-Izenberk"
openssl x509 -req -in certs/client.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client.crt -days 500 -sha256
```

### Connect to AI (mTLS)
Add your **client certificates** to your AI agent's configuration. For `curl` testing:
```bash
curl --cert certs/client.crt --key certs/client.key --cacert certs/ca.crt https://localhost:8080/sse
```

---
*Status: MISSION COMPLETE (Security Mesh Deployed) | Date: 2026-04-16*
