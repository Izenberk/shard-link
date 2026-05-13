# Shard-Link Improvement Plan

Personal memory system for AI agents — Go \+ PostgreSQL/pgvector \+ MCP Drafted: 2026-05-13 — trimmed against project's actual state

## Design Principles (preserve)

- **Local-first** — user owns binary, container, data  
- **Portable across AI clients** — MCP-native, not tied to one vendor  
- **Personal exocortex** — memory as extension of self, not shared knowledge base  
- **Sovereignty over convenience** — never trade privacy for features

---

## Context

- **Sole user, 32 commits, hobby-scale**  
- **README status:** "MISSION COMPLETE (Postgres Scaling Deployed) | 2026-05-11"  
- **Sub-5ms retrieval already met** — performance is not a real problem  
- **Janitor formula** `Score = (Links × Centrality) / (Time^Decay)` already in place  
- **Phase 8 (Neo4j migration) and Phase 9 (local Ollama embeddings)** already on official PLAN.md

This document covers only what's missing from existing plans and what's worth the effort at this scale.

---

## The Shortlist (7 items, \~6 days total)

### 1\. Source Provenance — \~1 day

Every shard should know where it came from.

**Add to schema:**

- `source_type`: enum (`user_direct` | `agent_inferred` | `document_ingest` | `chat_summary` | `system`)  
- `source_ref`: text (conversation ID, URL, file path)  
- `confidence`: float 0–1 (default 1.0 for user\_direct)

**Why:** Protects core memory from LLM hallucination contamination. Enables confidence-filtered retrieval. Makes the system debuggable ("why is this here?").

---

### 2\. Hybrid Retrieval (Vector \+ BM25) — \~2 days

Cosine similarity is weak on exact tokens — names, IDs, error codes, numbers.

**Example failure:** Query `"error 1000"` returns shards about `"error 1001"`, `"error 1042"` because they're semantically adjacent.

**Implementation:**

- Add PostgreSQL FTS (`tsvector` column) or `pg_trgm` alongside `pgvector`  
- Reciprocal Rank Fusion (RRF) to merge results — no training required  
- Expose `retrieval_mode` param: `vector` | `lexical` | `hybrid` (default `hybrid`)

**Win:** Large quality improvement on factual queries.

---

### 3\. MMR Retrieval — \~1 day

Default kNN returns shards close to query — but often close to each other too. Wastes context window on redundant info.

**Maximal Marginal Relevance:**

score(d) \= λ · sim(query, d) − (1 − λ) · max\_{d' in selected} sim(d, d')

Tunable `λ` (default 0.7) trades relevance vs diversity.

**Win:** Broader context in same token budget.

---

### 4\. HNSW Index Tuning — ½ day

pgvector defaults are conservative for general use — personal memory is a specific workload.

**Tunable params:**

- `m` (graph connectivity, default 16\) — bump to 24–32 for better recall on small datasets  
- `ef_construction` (build quality, default 64\) — bump to 128–200; one-time cost  
- `ef_search` (runtime accuracy, default 40\) — tune per query; expose as retrieval param

**Win:** 2–3× speedup or noticeably better recall — pick.

---

### 5\. Partial Indexes (Active vs Basement) — ½ day

Basement shards are queried rarely but still occupy the same index, slowing kNN.

CREATE INDEX shards\_active\_hnsw ON shards

  USING hnsw (embedding vector\_cosine\_ops)

  WHERE status \= 'active';

CREATE INDEX shards\_archive\_hnsw ON shards\_archive

  USING hnsw (embedding vector\_cosine\_ops);

Default queries hit active only. Explicit "search basement too" flag scans both.

**Win:** Active index stays small and fast even as Basement grows unboundedly.

---

### 6\. Storage Hygiene — \~1 day

Boring but important.

- **JSONB compression** — Postgres 14+ supports `lz4` (`SET default_toast_compression = 'lz4'`) — 30% smaller, faster decompress than default `pglz`  
- **Scheduled `VACUUM ANALYZE`** — pgvector indexes degrade with updates; weekly cron  
- **Periodic `REINDEX CONCURRENTLY`** — quarterly, when index bloat \> 30%

**Effort:** \~1 day setup, then automated.

---

### 7\. Embedding Dimension Right-sizing — rolls into Phase 9

1536-dim is overkill for personal memory (\~10k–100k shards).

When you do the Phase 9 Ollama migration, go to 512-dim or 768-dim instead of 1536:

- **`bge-small-en-v1.5` at 384-dim** — local model, surprisingly strong  
- **`nomic-embed-text-v1.5` Matryoshka at 512-dim** — explicit dimension param  
- **`gte-small` at 384-dim** — alternative

**Memory savings:** 1536 → 512 \= 3× less RAM for index, 3× faster kNN. Combines with Phase 9 work at no extra effort cost.

---

## Suggested Order

1. **Provenance \+ Hybrid retrieval together** — touch the same code paths (write \+ retrieve)  
2. **HNSW tuning \+ Partial indexes \+ Storage hygiene** — pure config, no architectural risk, do anytime  
3. **MMR** — single algorithm change, layers on top of \#1  
4. **Dimension right-sizing** — fold into Phase 9 when it happens

---

## Deferred (Speculative — defer until evidence)

These are interesting but unproven at current scale. Revisit when there's a real failure mode pointing at them.

### Working Memory / Active Context Bias

Track recently retrieved/mentioned shards within current session; bias new queries toward that centroid. Mimics "thinking about X → things related to X come to mind easier." Try after MMR is in.

### Dream Replay (Basement Resurrection)

Background job samples Basement shards, checks cosine vs active context, promotes if relevant. Build only if Basement gets crowded with relevant-but-archived shards.

### Soft Delete \+ Quarantine Tier

Useful if/when ingesting from untrusted sources (e.g., document import). Skip while writes are user-direct.

---

## Already Handled — Do Not Duplicate

These appeared in earlier drafts but are redundant with existing project state:

| Item | Status |
| :---- | :---- |
| Local embeddings | Phase 9 in official PLAN.md |
| Temporal decay scoring | Janitor formula already does it |
| Typed bonds | Phase 8 Neo4j migration covers it |
| Reflection / cluster summaries | Phase 8 GraphRAG plans this |
| Encryption at rest | OS disk encryption already covers threat |
| Export/import | `pg_dump` already works |

---

## Rejected as Over-Engineering

For sole-user hobby scale, these were considered and rejected:

- **Write-time dedup** — manual cleanup is faster than building it  
- **Matryoshka two-stage retrieval** — sub-5ms already met  
- **Adaptive K** — fixed K=5 is fine  
- **Tiered retrieval detail / summary shards** — LLM call per write, no payoff at this scale  
- **Query / result caches** — near-zero hit rate in real agent use  
- **Batch / async embedding** — organic writes, no batch opportunity  
- **Streaming retrieval early termination** — agent slices locally anyway  
- **Web UI** — pgAdmin/DBeaver inspects Postgres directly  
- **Eval harness** — gut-feel testing fine at this scale

---

## What NOT to Build (Scope Creep Guards)

- **Multi-user / team sharing** — breaks "personal exocortex" framing  
- **Cloud-hosted SaaS** — contradicts local-first  
- **Mobile native apps** — web UI \+ MCP from mobile client is enough  
- **Agent-side reasoning / planning** — Shard-Link is memory, not cognition

---

## Open Questions

1. **Phase 9 migration path** — if existing shards are on OpenAI-1536, switching to local 768-dim requires re-embedding. Dual-write during window, or lazy re-embed on access?  
2. **Bond threshold tuning** — fixed 0.85 vs adaptive (per-shard, per-cluster, learned)?  
3. **Core shard designation** — user-marked, or auto-detected by bond centrality (high-degree nodes)?  
4. **MCP tool surface scope** — minimal (store/retrieve) vs rich (forget/strengthen/annotate/link)?

---

## Recommended First Patch

**Source Provenance \+ Hybrid Retrieval together** — they touch the same code paths, unlock measurable quality wins, don't require any architectural decisions. \~3 days. Builds momentum for the rest.  