# Shard-Link: Graph Database Integration & Implementation Plan

This document outlines the strategic implementation plan to transition Shard-Link from its current **PostgreSQL + pgvector** architecture to a native **Graph Database (Neo4j)**. This migration natively resolves the "Janitor" relational centrality bottlenecks and leverages the Model Context Protocol (MCP) for multi-hop agentic reasoning.

### 1. Architectural Paradigm Shift: The "Brain" Model
The core concepts of Shard-Link will migrate from a relational-vector storage to a native knowledge mesh:
*   **Shards to Nodes:** Atomic context fragments (currently rows in Postgres) will become Graph Nodes. Metadata and raw content will be stored as node properties.
*   **Vector to Graph-Vector:** 1536-D embeddings will be indexed directly in Neo4j using its native Vector Search capabilities, allowing for hybrid **Semantic + Structural** queries.
*   **Shard Bonds to Typed Edges:** The similarity-based links will be replaced by native Graph Relationships. Phase 2 will introduce explicit typing:
    *   `REFINES`: One shard updates or clarifies a previous one.
    *   `DEPENDS_ON`: One project's context requires another.
    *   `EGO_ANCHOR`: Links Core Shards to the active memory mesh.

### 2. Implementation Phases

#### Phase 1: Infrastructure & The Neo4j Stack
*   **Action:** Update `docker-compose.yml` to replace (or supplement) Postgres with **Neo4j Community Edition**.
*   **Plugins:** Ensure **APOC** (Awesome Procedures on Cypher) and **GDS** (Graph Data Science) are enabled for advanced graph algorithms.
*   **Goal:** Preserve the local-first mandate while enabling networked Cypher access.

#### Phase 2: Repository Refactor (Full Migration)
*   **Action:** Implement `GraphRepository` in Go using the `neo4j-go-driver`.
*   **Logic:** Move from SQL-based Repository Pattern to Cypher-based persistence. 
*   **Janitor Update:** Replace the "Standardized Eviction Hierarchy" with **Native Centrality Scoring**. The Janitor will query `gds.degree.stream` to identify "Hub" shards that must be protected and "Orphan" shards for archival.

#### Phase 3: Multi-Hop Retrieval via MCP
*   **Tooling:** Implement the `search_graph` MCP tool.
*   **Capabilities:**
    *   **Vector Similarity:** Find shards semantically close to a query.
    *   **Path Traversal:** Traverse edges from a hit to find its "ancestry" or "siblings" in the context mesh.
    *   **Text2Cypher:** Allow the AI agent to write dynamic graph queries for exploratory research.

#### Phase 4: GraphRAG & Hierarchical Communities
*   **Logic:** Use GraphRAG principles to organize Shards into "communities" (projects, topics, sessions).
*   **Benefit:** Provides the AI with a summary of entire clusters of thought rather than just individual fragments, reducing context window noise.

### 3. Migration Strategy: The "Clean Break"
Given the conceptual leap from rows to nodes, we will implement a migration utility in `cmd/migrate/graph.go` to:
1.  Read current Shards from PostgreSQL.
2.  `MERGE` them into Neo4j as Nodes.
3.  Re-calculate Bonds and create `CONNECTED_TO` edges.
4.  Decommission the Postgres container once verification is complete.

---
*Status: PLAN APPROVED | Target: Phase 8 (The Knowledge Mesh) | Date: 2026-05-11*
