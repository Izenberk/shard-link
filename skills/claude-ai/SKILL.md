---
name: shard-link
description: >-
  Shard-Link long-term memory interface — search and save knowledge to persistent
  AI memory via MCP. Use this skill whenever the user asks to remember something,
  recall past context, search their memory, save a decision or fact, look up
  previous conversations, or references "my memory", "what do you know about me",
  "remember this", "have we discussed", or any request involving long-term recall
  across sessions. Also use when the user says "shard", "/shard", or mentions
  Shard-Link by name.
---

# Shard-Link: Long-Term Memory Interface

You have access to Shard-Link, a persistent knowledge mesh that remembers context
across sessions. Think of it as your long-term memory — structured, searchable,
and always available via MCP tools.

## Tools

| Tool | Purpose | When to use |
|------|---------|-------------|
| `search_all` | Hybrid search (graph + text + vector) | **Always try this first.** Covers all three search engines in one call. |
| `search_memory` | Vector similarity search | When you need pure semantic matching (e.g., "things similar to X"). |
| `search_text` | Full-text index search | When you need exact keyword or phrase matching. |
| `search_graph` | Neo4j graph traversal | When you need relational context (e.g., "what's connected to X"). |
| `save_memory` | Persist a new fact | When the user wants to remember something for future sessions. |

## How to search

1. **Start with `search_all`** — it's the most efficient path, hitting all three engines simultaneously.
2. Summarize what you found in a structured, high-signal response.
3. If `search_all` returns nothing relevant, try `search_text` with different keywords or `search_graph` for relational context.
4. Never tell the user "I don't have memory" without searching first.

## How to save

When saving with `save_memory`, provide three fields:

- **id**: Descriptive kebab-case identifier (e.g., `project-auth-decision-2026-05`, `preference-go-backend`)
- **content**: Clear, self-contained text that will make sense months from now without additional context. Include the *why*, not just the *what*.
- **category**: One of:
  - `core` — Identity, values, immutable facts about the user
  - `memory` — Project progress, session context, decisions
  - `tech` — Technical knowledge, patterns, architecture
  - `arch` — System design decisions and trade-offs

After saving, confirm what was stored and the shard ID.

## Behavior guidelines

- When the user asks a question that might have a stored answer, **search before answering** from your general knowledge. Their personal context matters more than generic responses.
- When a conversation produces an important decision, insight, or preference, **proactively offer to save it** — e.g., "Want me to remember this for future sessions?"
- Keep saved content concise but complete. A shard should be a self-contained fragment that provides value on its own.
- Never fabricate memory results. If nothing is found, say so clearly.

---

## Setup Guide

### Prerequisites

- Claude.ai Pro, Max, Team, or Enterprise plan
- Shard-Link Hub running and publicly accessible (e.g., via Cloudflare Tunnel)

### 1. Connect the MCP Server

1. Go to **claude.ai** > Profile > **Customize** > **Connectors**
2. Click **"+"** > **"Add custom connector"**
3. Enter your MCP server URL (e.g., `https://hub.izenberk.com/mcp`)
4. Click **"Advanced settings"**:
   - **OAuth Client ID:** `shard-link`
   - **OAuth Client Secret:** your `HUB_API_KEY` value
5. Click **"Connect"**

The server implements OAuth 2.0 Authorization Code + PKCE — the browser will briefly
redirect to the Hub's `/authorize` endpoint (auto-approves for single-user) and return
with a Bearer token. No manual token management needed.

### 2. Install the Skill

1. Go to **claude.ai** > **Customize** > **Skills**
2. Create a new skill
3. Paste the contents of this file
4. Save

### 3. Use It

Toggle the Shard-Link connector on per conversation via the **"+"** button at the
bottom of the chat input. The skill triggers automatically when you ask anything
memory-related:

```
What do you remember about my tech stack?
Remember: I decided to use FSRS-calibrated decay for shard eviction.
What progress have I made on Shard-Link this week?
```
