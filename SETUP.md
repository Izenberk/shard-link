# Shard-Link: Setup & Deployment Guide

This guide covers installation, configuration, deployment modes, authentication, and client integration for Shard-Link.

---

## 1. Prerequisites

- **Docker** and **Docker Compose** (v2+)
- **Go** 1.26+ (for the Visual Ego dashboard server and local development)
- **Node.js** 18+ and **npm** (for building the Visual Ego React frontend)
- A **Gemini API key** for embedding and summarization
- (Optional) A **Cloudflare-managed domain** and tunnel token for remote access

## 2. Configuration

### Environment Variables

Create a `.env` file in the project root (use `.env.example` as a template):

```bash
# Authentication
HUB_API_KEY=shl_live_your_secret_token

# OAuth Client Credentials (required for Claude.ai connector)
OAUTH_CLIENT_ID=claude-ai-connector
OAUTH_CLIENT_SECRET=your_oauth_client_secret   # generate: openssl rand -base64 32

# Gemini (Embedding + Summarization)
GEMINI_API_KEY=your_gemini_key
EMBEDDING_MODE=gemini
EMBEDDING_MODEL=gemini-embedding-001

# Neo4j Knowledge Mesh
NEO4J_PASS=your_neo4j_password

# PostgreSQL Archival Vessel
DB_USER=sharduser
DB_PASSWORD=your_postgres_password

# Mesh Tuning
MESH_LINK_THRESHOLD=0.75
JANITOR_RESONANCE_THRESHOLD=0.70

# Remote Access (optional)
CLOUDFLARE_TUNNEL_TOKEN=your_tunnel_token
```

> **Security note:** `.env` is gitignored and must never be committed. All database credentials are injected via environment variables — no passwords are hardcoded in `docker-compose.yml`.

### Port Bindings

All database ports are bound to `127.0.0.1` (localhost only) by default:

| Service | Port | Access |
|---------|------|--------|
| Hub (MCP) | `8080` | Exposed (via Cloudflare Tunnel or direct) |
| Neo4j Browser | `127.0.0.1:7474` | Localhost only |
| Neo4j Bolt | `127.0.0.1:7687` | Localhost only |
| PostgreSQL | `127.0.0.1:5434` | Localhost only |
| Visual Ego | `127.0.0.1:8081` | Localhost only |

## 3. Deployment Modes

### Option A: Secure Online (Default)

Exposes the Hub via an encrypted **Cloudflare Tunnel** — no router port forwarding required. Remote AI agents connect through your custom domain (e.g., `your-hub.example.com`).

```bash
# Ensure CLOUDFLARE_TUNNEL_TOKEN is in your .env
docker compose up -d --build

# Build the Visual Ego React frontend
cd web/dashboard && npm install && npm run build && cd ../..

# Start the Visual Ego Dashboard (localhost only)
go run cmd/visual_ego/main.go
```

**Prerequisites:**
- A Cloudflare-managed domain
- A `CLOUDFLARE_TUNNEL_TOKEN` from the Cloudflare Zero Trust dashboard

### Option B: Pure Local (Offline)

Runs the entire stack within your local network. No external traffic is permitted.

```bash
docker compose --profile local up -d --build
```

**Local endpoints:**
- **Hub:** `http://localhost:8080/mcp` (Streamable HTTP) or `http://localhost:8080/sse` (Legacy SSE)
- **Visual Ego Dashboard:** `http://127.0.0.1:8081`
- **Neo4j Browser:** `http://127.0.0.1:7474`

## 4. Security & Authentication

### Defense in Depth

Shard-Link implements layered security without requiring sidecar proxies:

1. **The Edge (Cloudflare):** Outbound-only tunnels protect the Hub from direct internet exposure.
2. **The App (Token Middleware):** All MCP endpoints require authentication via `X-API-Key` or `Authorization: Bearer`.
3. **The Transport (HTTPS):** Encryption-in-transit via Cloudflare Tunnel ensures tokens cannot be sniffed.
4. **OAuth Hardening:** Confidential client credentials (`client_id` + `client_secret`), redirect URI whitelist, mandatory S256 PKCE, stateless JWT tokens, per-IP rate limiting, and security headers on all OAuth responses.

### Authentication Methods

The Hub accepts two auth methods:

| Method | Header | Used by |
|--------|--------|---------|
| API Key | `X-API-Key: <key>` | Claude Code CLI, curl, direct clients |
| Bearer Token | `Authorization: Bearer <token>` | Claude.ai (via OAuth), other OAuth clients |

**Important:** OAuth clients receive **stateless JWTs** (30-day TTL, HS256-signed with `HUB_API_KEY`). JWTs survive container restarts — no server-side token state required. If a token leaks, only that session is compromised — the master key is never exposed.

### OAuth 2.0 Flow (Claude.ai)

The OAuth implementation follows RFC 7636 (Authorization Code + PKCE) with confidential client authentication:

1. Claude.ai redirects the browser to `/authorize` with `client_id` and `code_challenge` (S256)
2. The server validates `client_id` against the registered client, then validates `redirect_uri` against a trusted host whitelist (`claude.ai`)
3. A one-time authorization code is issued and redirected back
4. Claude.ai exchanges the code + `code_verifier` + `client_id` + `client_secret` at `/token`
5. The server validates client credentials (constant-time), then PKCE, and returns a stateless JWT

**Protections:**
- `client_id` must match the registered `OAUTH_CLIENT_ID` at both `/authorize` and `/token`
- `client_secret` is validated at `/token` using `crypto/subtle.ConstantTimeCompare` (prevents timing attacks)
- `redirect_uri` must be HTTPS and on a whitelisted host (prevents open redirect attacks)
- `code_challenge_method` must be `S256` (plain method rejected)
- `code_verifier` is mandatory at `/token` (PKCE cannot be bypassed)
- PKCE comparison uses `crypto/subtle.ConstantTimeCompare` (prevents timing attacks)
- OAuth endpoints are rate-limited per IP (5 req/sec, burst 3)
- Pending authorization codes are capped at 100 and auto-cleaned every minute
- All OAuth responses include `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and `Strict-Transport-Security`

### Input Validation

MCP tool calls enforce the following limits:

| Field | Max Size | Behavior |
|-------|----------|----------|
| Shard ID | 256 chars | Reject with error |
| Content | 100 KB | Reject with error |
| Category | 50 chars | Reject with error |
| Query | 10,000 chars | Reject with error |
| Result limit | 100 | Clamped silently |

Allowed categories: `core`, `memory`, `session`, `tech`, `arch`, `contract`. Mutating or deleting `core`-category shards via `update_shard` / `delete_shard` requires `confirm_core=true`.

## 5. Client Integration

### Supported Transports

| Transport | Endpoint | Status | Spec |
|-----------|----------|--------|------|
| **Streamable HTTP** | `/mcp` | Primary (recommended) | MCP 2024-11-05 |
| **SSE** | `/sse` | Legacy (backward compat) | Deprecated |

### Claude Code (CLI)

Add Shard-Link to your user-scoped MCP configuration:

```bash
claude mcp add --transport http shard-link https://your-hub.example.com/mcp \
  --header "X-API-Key: YOUR_HUB_API_KEY_HERE" \
  --scope user
```

Verify the connection:
```bash
claude mcp get shard-link
```

### Claude.ai (Web)

Shard-Link connects as a custom MCP connector on Claude.ai (Pro/Max/Team/Enterprise plans):

1. Go to **Customize > Connectors > Add custom connector**
2. URL: `https://your-hub.example.com/mcp`
3. Advanced settings:
   - **OAuth Client ID:** your `OAUTH_CLIENT_ID` value (e.g. `claude-ai-connector`)
   - **OAuth Client Secret:** your `OAUTH_CLIENT_SECRET` value
4. Click **Connect** — the server validates credentials, auto-approves, and issues a stateless JWT

Once connected, toggle Shard-Link per conversation via the **"+"** button at the bottom of the chat input.

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

## 6. Verification Checklist

After deployment, verify the security hardening:

| Test | Command / Action | Expected |
|------|-----------------|----------|
| OAuth redirect blocked | `curl "https://your-hub.example.com/authorize?redirect_uri=https://evil.com&code_challenge=test&code_challenge_method=S256"` | 400 — host not in allowlist |
| PKCE required | POST `/token` without `code_verifier` | 400 — code_verifier is required |
| Category blocked | `save_memory` with `category=core` | Error — category not allowed |
| Content limit | `save_memory` with 200KB content | Error — exceeds 100KB |
| DB ports local-only | `curl http://<host-ip>:7474` from another machine | Connection refused |
| Visual Ego local-only | `curl http://<host-ip>:8081` from another machine | Connection refused |
| Client credentials | POST `/token` with wrong `client_secret` | 401 — `{"error": "invalid_client"}` |
| Unknown client_id | GET `/authorize?client_id=unknown&...` | 401 — Invalid client_id |
| Claude.ai connector | Remove and re-add connector with correct credentials | OAuth flow completes, JWT issued |
| Claude Code CLI | `claude mcp get shard-link` | Tools listed, X-API-Key auth works |

---
*See [README.md](./README.md) for project overview, architecture, and domain concepts.*
