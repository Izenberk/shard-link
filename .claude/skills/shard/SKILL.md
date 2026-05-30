---
name: shard
description: Shard-Link development and testing - interact with remote MCP server via Cloudflare tunnel
disable-model-invocation: false
allowed-tools: Read Bash(docker *) Bash(go *) Bash(curl *)
argument-hint: [query or command]
---

# Shard-Link: Development Interface

Execute shard operation: **$ARGUMENTS**

## Context

This is the Shard-Link project workspace. You have access to:
- **Remote MCP Hub:** Via Cloudflare tunnel (see .env PUBLIC_URL)
- **Local MCP endpoint:** `http://localhost:8080/sse` (direct Docker access)
- **Neo4j Browser:** `http://localhost:7474` (credentials in .env)
- **Source code:** `internal/mcp/server.go`
- **Infrastructure:** Docker Compose stack

## Available MCP Tools

- `shard-link:search_all` → Search across all indexes
- `shard-link:save_memory` → Persist memories
- `shard-link:get_activities` → View activity feed

## Development Commands

For development tasks, you can use:
- `docker compose logs hub` - View Hub logs
- `docker compose ps` - Check service status
- `go run cmd/visual_ego/main.go` - Start dashboard

## Response Format

Provide structured responses based on MCP tool results or development context.
