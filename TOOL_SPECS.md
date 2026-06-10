# MCP Tool Specs: Query & CRUD Expansion

**Status:** Draft — pending review
**Date:** 2026-06-11
**Author:** Izenberk & BB

---

## Motivation

The current MCP toolset covers semantic search, keyword search, graph traversal, and exact ID lookup — but has gaps in **chronological awareness**, **categorical browsing**, **health inspection**, and **data correction**.

| Gap | Problem |
|-----|---------|
| No recency query | "What did I save recently?" requires searching with a vague keyword |
| No category listing | "Show me all contracts" requires `search_all` with `category` filter — but that needs a search query |
| No eviction visibility | No way to see what the Janitor is about to sweep — shards vanish silently |
| No update capability | A shard saved with the wrong category can only be fixed via direct Neo4j access |
| No delete capability | Garbage or sensitive shards persist until the Janitor naturally evicts them (if ever) |

---

## Current Toolset (for reference)

| Tool | Type | Query Method |
|------|------|-------------|
| `search_memory` | Read | Vector cosine similarity |
| `search_text` | Read | Keyword matching (SQL LIKE) |
| `search_graph` | Read | Vector + graph traversal (multi-hop) |
| `search_all` | Read | All three engines + optional category filter |
| `get_shard` | Read | Exact ID lookup |
| `get_core_shards` | Read | All core-category shards |
| `get_status` | Read | Mesh stats + service health |
| `save_memory` | Write | Create new shard with auto-embedding + salience scoring |

---

## New Tools

### 1. `get_recent_shards`

**Purpose:** Retrieve shards ordered by most recently updated (`last_used` DESC).

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `limit` | number | no | 10 | Max shards to return (capped at 100) |
| `category` | string | no | — | Optional category filter |

**Returns:** JSON array of `shardResponse` objects (same shape as `get_shard`: `id`, `content`, `category`, `created_at`, `updated_at`).

**Neo4j query:**
```cypher
MATCH (s:Shard)
WHERE s.category <> 'archived'
  AND ($category = '' OR s.category = $category)
RETURN s
ORDER BY s.last_used DESC
LIMIT $limit
```

**Side effects:** None. Pure read — no `ReinforceShards`, no `last_used` update.

---

### 2. `get_shards_by_category`

**Purpose:** List all shards in a specific category without needing a search query.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `category` | string | yes | — | Category to filter (e.g. `core`, `memory`, `session`, `contract`) |
| `limit` | number | no | 50 | Max shards to return (capped at 100) |

**Returns:** JSON array of `shardResponse` objects, ordered by `last_used` DESC.

**Neo4j query:**
```cypher
MATCH (s:Shard {category: $category})
WHERE s.category <> 'archived'
RETURN s
ORDER BY s.last_used DESC
LIMIT $limit
```

**Side effects:** None. Pure read.

**Note:** Overlaps with `get_recent_shards` when `category` is provided. The distinction is intent — `get_recent_shards` answers "what changed lately?" while `get_shards_by_category` answers "show me everything in this bucket."

---

### 3. `get_at_risk_shards`

**Purpose:** Inspect shards with the lowest survival scores — candidates the Janitor will evict next.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `limit` | number | no | 10 | Max shards to return (capped at 100) |
| `threshold` | number | no | 30 | Only return shards with survival score below this value (0-100) |

**Returns:** JSON array with fields: `id`, `content`, `category`, `survival_score`, `last_used`.

**Neo4j query:**
```cypher
MATCH (s:Shard)
WHERE s.category <> 'archived'
  AND s.category <> 'core'
  AND s.survival < $threshold
RETURN s
ORDER BY s.survival ASC
LIMIT $limit
```

**CRITICAL: NO side effects.** This is a read-only inspection tool. Observing at-risk shards must NOT trigger `ReinforceShards` or update `last_used` or append to `RetrievalHistory`. Reason: if inspection counted as retrieval, every time you checked what's dying you'd accidentally rescue it. The Janitor would never evict anything you looked at. Observing the system should not change it.

Core shards are excluded because they always have survival = 100 (immutable anchors).

---

### 4. `update_shard`

**Purpose:** Update a shard's category and/or content. Fixes mistakes without requiring direct database access.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `id` | string | yes | — | Exact shard ID to update |
| `content` | string | no | — | New content (triggers re-embedding) |
| `category` | string | no | — | New category (must be in allowlist) |

**Behavior:**

- At least one of `content` or `category` must be provided — reject if both are empty
- If `content` changes: re-embed via `s.embedder.Embed()` and update the vector property. Without re-embedding, the vector would represent the old content and search results would drift from reality
- If only `category` changes: update the property only — no embedding cost
- `category` must pass the existing `allowedCategories` whitelist (`core`, `memory`, `session`, `tech`, `arch`, `contract`)
- **Blocked** for `comm-summary-*` shard IDs — these are system-managed by the Synthesizer. Manual edits would be overwritten on the next synthesis cycle anyway
- Logs the update to the Activity Ledger with old → new values

**Storage layer:**

```go
type ShardUpdate struct {
    Content  string // empty = no change
    Category string // empty = no change
    Vector   []byte // empty = no change (set by handler when content changes)
}
```

**Neo4j query:**
```cypher
MATCH (s:Shard {id: $id})
SET s.content = CASE WHEN $content <> '' THEN $content ELSE s.content END,
    s.category = CASE WHEN $category <> '' THEN $category ELSE s.category END,
    s.vector = CASE WHEN $vector IS NOT NULL THEN $vector ELSE s.vector END,
    s.last_used = datetime()
RETURN s
```

---

### 5. `delete_shard`

**Purpose:** Permanently remove a shard by exact ID.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `id` | string | yes | — | Exact shard ID to delete |

**Behavior:**

- Deletes the shard node **and all its relationships** (bonds, episode links) from Neo4j via `DETACH DELETE`
- **Blocked** for `comm-summary-*` shard IDs — system-managed by the Synthesizer
- **Blocked** for `core` category shards — core identity shards are immutable anchors. If you really need to delete one, use direct Neo4j access (intentional friction prevents accidental identity loss)
- Logs the deletion to the Activity Ledger

**Neo4j query:**
```cypher
MATCH (s:Shard {id: $id})
DETACH DELETE s
```

---

## Implementation Scope

### Repository Interface — 4 new methods

| Method | Signature |
|--------|-----------|
| `GetRecentShards` | `(ctx, limit int, category string) ([]Shard, error)` |
| `GetShardsByCategory` | `(ctx, category string, limit int) ([]Shard, error)` |
| `GetAtRiskShards` | `(ctx, limit int, threshold float64) ([]Shard, error)` |
| `UpdateShard` | `(ctx, id string, updates ShardUpdate) error` |
| `DeleteShard` | `(ctx, id string) error` |

### Files to Modify

| File | Change |
|------|--------|
| `internal/storage/repository.go` | Add 5 new methods to Repository interface |
| `internal/storage/shard.go` | Add `ShardUpdate` struct |
| `internal/storage/vessel_graph.go` | Neo4j implementations for all 5 new methods |
| `internal/storage/vessel.go` | SQLite no-op stubs |
| `internal/storage/vessel_postgres.go` | Postgres no-op stubs |
| `internal/mcp/server.go` | Register 5 new tools + 5 handler functions |

### What Does NOT Change

- Existing search tools — untouched
- Existing read tools (`get_shard`, `get_core_shards`, `get_status`) — untouched
- `save_memory` — untouched
- Janitor, Synthesizer, HygieneWorker — untouched
- OAuth, auth middleware — untouched

---

## Verification Plan

1. `go build ./...` + `go vet ./...` — clean compilation
2. `docker compose up -d --build`
3. Test each tool:
   - `get_recent_shards` with `limit=5` → returns 5 most recently touched shards
   - `get_shards_by_category` with `category=contract` → returns only contract shards
   - `get_at_risk_shards` with `threshold=30` → returns low-survival shards; verify `last_used` is NOT updated after the call
   - `update_shard` — fix a wrong category, verify change persists in Neo4j
   - `update_shard` — change content, verify vector is re-embedded (cosine sim with old vector should differ)
   - `delete_shard` on a test shard → verify removed from Neo4j
   - `delete_shard` on a `core` shard → verify rejected with error
   - `delete_shard` on a `comm-summary-*` shard → verify rejected with error
4. Verify existing tools still work (regression check)
