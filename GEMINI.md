# Shard-Link: Core System Context

## 1. Project Essence
Shard-Link is a high-performance context engine designed to provide "long-term memory" for AI agents. It bridges raw data to LLM context windows using a fragmented storage model.

## 2. Technical Stack
- **Backend:** Go (Golang) 1.22+ (Strict SOLID & Production standards).
- **Database:** SQLite + `sqlite-vec` (Vector Search) + JSONB metadata.
- **Protocol:** MCP (Model Context Protocol) for tool-based orchestration.

## 3. Domain Language & Logic
- **Shards:** Atomic contextual fragments with vector embeddings.
- **Core Shards:** Immutable anchors for User Profile/Identity. **NEVER EVICT.**
- **The Janitor:** Background process for size management.
  - **Logic:** Eviction is based on **Resonance** (semantic similarity) and **Relational Centrality**.
  - **Constraint:** Prioritize keeping shards that act as "hubs" for multiple contexts.

## 4. Development Philosophy & Mentorship
- **Active Learning:** Do NOT provide complete implementation for core logic (e.g., the Janitor's scoring algorithm or Shard linking).
- **The "Why" First:** Always explain the architectural logic and trade-offs BEFORE suggesting any code.
- **Scaffold, Don't Build:** Provide interfaces, function signatures, or boilerplate (e.g., imports/structs), but guide the developer to write the internal logic.
- **Code Reviews:** When asked to review, focus on SOLID principles, Go idiomatic patterns, and potential edge cases rather than just fixing syntax.
- **No Copy-Paste:** Encourage the developer to type out the logic to build muscle memory and deep understanding.