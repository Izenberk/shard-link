# Shard-Link: Core System Context

## 1. Project Essence
Shard-Link is a high-performance context engine designed to provide "long-term memory" for AI agents. It bridges raw data to LLM context windows using a fragmented storage model.

## 2. Knowledge Priority (Source of Truth)
- **Primary:** The **Neo4j Knowledge Mesh** is the "Living Memory." All project progress, technical decisions, and active session context must be retrieved via MCP tools (`search_graph`, `search_text`).
- **Anchor:** The SQLite file (`shard-link.db`) is the "Seed Memory." It contains Core Identity shards but is NOT the primary source for project-level status.
- **Legacy:** The PostgreSQL instance handles heavy relational scaling for high-volume shards.

## 3. Technical Stack
- **Backend:** Go (Golang) 1.26+ (Strict SOLID & Production standards).
- **Knowledge Mesh:** Neo4j 5.x + GDS + APOC (Relational & Graph reasoning).
- **Legacy Storage:** PostgreSQL + `pgvector` (Vector Search) + JSONB metadata.
- **Protocol:** MCP (Model Context Protocol) for tool-based orchestration.

## 3. High-Precision Vectors
- **Format:** Vectors must be handled as `[]float32` internally.
- **Serialization:** When converting to strings (for SQL/Cypher), use `strconv.FormatFloat(f, 'g', -1, 32)` to prevent precision loss. NEVER use `fmt.Sprintf("%f")`.
- **Dimensions:** Standardize on 1536-D (OpenAI/Gemini compatible).

## 4. Domain Language & Logic
- **Shards:** Atomic contextual fragments with vector embeddings.
- **Core Shards:** Immutable anchors for User Profile/Identity. **NEVER EVICT.**
- **The Janitor:** Background process for size management.
  - **Logic:** Eviction is based on **Resonance** (semantic similarity) and **Relational Centrality**.
  - **Constraint:** Prioritize keeping shards that act as "hubs" for multiple contexts.

## 4. Development Philosophy & Mentorship
- **Active Learning via Typing:** Provide the **full, complete code** for implementations. Do not play "puzzle games" by only providing scaffolding or boilerplate.
- **The "Why" First:** Always explain the architectural logic and trade-offs BEFORE providing the code.
- **Muscle Memory:** The developer will learn by typing out the full code you provide. Encourage them to type it rather than copy-pasting to build deep understanding.
- **Code Reviews:** When asked to review, focus on SOLID principles, Go idiomatic patterns, and potential edge cases.