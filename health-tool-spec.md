# MCP Health Tool Spec — For Shard-Link Hub

**Status:** IMPLEMENTED (2026-06-07)

## Why

shard-cli needs a `shard status` command that shows mesh stats and service health. No MCP tool currently provides this data. This spec defines what the CLI expects so the hub can implement a matching tool.

## Tool Registration

- **Tool name:** `get_status`
- **Description:** Returns mesh statistics and service health for the Shard-Link system.
- **Arguments:** None.
- **Returns:** Single text content block (same pattern as `search_all`).

## Expected Response Shape

The tool should return a JSON object inside the MCP content block (not formatted text like `search_all`). This keeps parsing simple and reliable on the CLI side.

```json
{
  "mesh": {
    "shards": 142,
    "bonds": 387,
    "communities": 8
  },
  "services": {
    "hub": "online",
    "neo4j": "online",
    "postgres": "online"
  }
}
```

### Field Definitions

| Field | Type | Source | Description |
|-------|------|--------|-------------|
| `mesh.shards` | int | Neo4j | Total shard node count |
| `mesh.bonds` | int | Neo4j | Total bond relationship count |
| `mesh.communities` | int | Neo4j | Distinct community count |
| `services.hub` | string | Self | `"online"` if responding |
| `services.neo4j` | string | Neo4j ping | `"online"` or `"offline"` |
| `services.postgres` | string | Postgres ping | `"online"` or `"offline"` |

Service values should be exactly `"online"` or `"offline"` — no other variants.

## MCP Wire Format

The tool response follows standard MCP `tools/call` result format:

```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"mesh\":{\"shards\":142,\"bonds\":387,\"communities\":8},\"services\":{\"hub\":\"online\",\"neo4j\":\"online\",\"postgres\":\"online\"}}"
    }
  ],
  "isError": false
}
```

The `text` field contains the JSON as a string (same as other MCP tools). The CLI will `json.Unmarshal` it directly.

## How the CLI Will Consume This

Once this tool exists, shard-cli will:

1. Add a `GetStatus()` method to `internal/client/mcp.go` that calls `tools/call` with `name: "get_status"`
2. Parse the JSON from the content block into a `StatusResponse` struct
3. Render it in `internal/format/output.go` as:

```
MESH STATUS
─────────────────────────────
Shards      : 142
Bonds       : 387
Communities : 8
─────────────────────────────
Hub     : online
Neo4j   : online
Postgres: online
```

## Error Case

If the tool encounters a partial failure (e.g., Neo4j is down but Postgres is up), it should still return a 200 response with `isError: false` and mark the failing service as `"offline"`. Only return `isError: true` for unrecoverable errors where no data can be collected at all.
