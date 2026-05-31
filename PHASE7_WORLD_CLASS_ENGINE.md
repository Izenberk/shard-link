# Phase 7 — Make the Engine World-Class

**Mandate:** Stop adding capabilities. Prove the ones we have.

Every claim in the README — sub-5ms retrieval, cognitive-science-backed memory, production-grade reliability — gets verified, measured, and made unbreakable.

---

## Context

Shard-Link's core engine is architecturally complete. The Triple-Engine storage model, Survival Formula v4.1, GraphRAG community summaries, cognitive biasing, and security hardening are all shipped and running.

Phase 7 is not about new features. It is about the delta between *impressive project* and *reference implementation* — tests, benchmarks, hardened concurrency, structured observability, and closing every documented gap in the Dependency Matrix.

---

## The Four Layers

Layer 1 — Close the Gaps        (Foundation integrity)

Layer 2 — Testing               (Credibility layer)

Layer 3 — Performance           (Prove the claims)

Layer 4 — Observability         (Make the invisible visible)

Each layer builds on the previous. Do not skip ahead.

---

## Layer 1 — Close the Gaps

### 1.1 Touch Completeness

**Problem:** SQLite and PostgreSQL only update `LastUsed` on shard touch. Neo4j receives the full cognitive treatment — `RetrievalHistory`, `UseCount`, `Salience`. The Survival Formula v4.1 reads from Neo4j for live shards, but archived shards in PostgreSQL are scored with hardcoded `salience=0.5`, `centrality=0.0`, `retrievalHistory=[]`. The Janitor makes eviction decisions on incomplete data.

**Fix:** Update `TouchShard()` in the PostgreSQL vessel to write back `RetrievalHistory` entries (append-only, cap at last 20 timestamps). This is not full cognitive parity — SQLite remains a stability anchor — but it ensures the Janitor is not flying blind on archived shards.

**Files:** `internal/storage/vessel_postgres.go`, `internal/storage/vessel.go`

**Acceptance:** A shard touched in PostgreSQL reflects updated `last_used` AND has its `retrieval_history` entry appended. `SurvivalScoreV4()` for that shard returns a non-hardcoded `A(m)` value.

---

### 1.2 Zero-Timestamp Guards on All Write Paths

**Problem:** The v4.1 migration patched 54 shards with `created_at = 0001-01-01` and added a CASE guard in `SaveShard`. However, `UpdateShard`, `TouchShard`, and bond creation paths do not have equivalent guards. One missed callsite produces a zero-epoch timestamp that collapses the Ebbinghaus decay denominator.

**Fix:** Audit every Cypher write that touches `created_at` or `last_used`. Add CASE guards consistently:

SET s.created\_at \= CASE

  WHEN s.created\_at IS NULL OR s.created\_at \= datetime('0001-01-01T00:00:00Z')

  THEN datetime()

  ELSE s.created\_at

END

**Files:** `internal/storage/vessel_graph.go` (all write methods)

**Acceptance:** `go test` with a shard that has zero-epoch timestamps passes `SurvivalScoreV4()` without returning `NaN`, `Inf`, or zero.

---

### 1.3 Graceful Shutdown — Context Chain \+ WaitGroup Drain

**Problem:** Synthesizer, Janitor, HygieneWorker, and WorkingMemory cleanup all run as background goroutines launched from `main.go`. None are wired to a proper cancellation chain. On `docker compose down`, goroutines are killed mid-cycle. For the Janitor, this risks a partial eviction state — some shards archived in PostgreSQL, Neo4j not yet updated, leaving the mesh in an inconsistent state.

**Fix:** Wire a `context.Context` cancellation chain from `main.go` through all background workers. Use a `sync.WaitGroup` to drain before process exit.

// main.go pattern

ctx, cancel := context.WithCancel(context.Background())

var wg sync.WaitGroup

wg.Add(1)

go func() {

    defer wg.Done()

    janitor.Run(ctx)

}()

// OS signal handler

sig := make(chan os.Signal, 1\)

signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

\<-sig

cancel()

wg.Wait()

Each background worker's loop checks `ctx.Done()` and exits cleanly, completing any in-progress cycle before returning.

**Files:** `main.go`, `internal/janitor/janitor.go`, `internal/synthesizer/synthesizer.go`, `internal/hygiene/hygiene.go`, `internal/mcp/working_memory.go`

**Acceptance:** `docker compose down` shows all goroutines completing their current cycle log entry before shutdown. No partial eviction state in Neo4j after restart.

---

## Layer 2 — Testing

### 2.1 Table-Driven Tests — cognitive.go

`SurvivalScoreV4()`, `ACTRActivation()`, `SalToStability()` are pure functions with no external dependencies. They are the most critical logic in the system and currently have zero test coverage.

**Pattern:**

func TestSurvivalScoreV4(t \*testing.T) {

    cases := \[\]struct {

        name     string

        shard    Shard

        now      time.Time

        wantMin  float64

        wantMax  float64

    }{

        {

            name:    "core shard always 100",

            shard:   Shard{Category: "core"},

            wantMin: 100, wantMax: 100,

        },

        {

            name:    "zero bonds orphan is eviction candidate",

            shard:   Shard{BondCount: 0, PageRank: 0, Salience: 0.5},

            wantMax: 20,

        },

        {

            name:    "high salience decays slower than low salience",

            // compare two shards identical except Salience

        },

        {

            name:    "result never exceeds 95 for non-core",

            shard:   Shard{BondCount: 100, PageRank: 5, Salience: 1.0},

            wantMax: 95,

        },

    }

    for \_, tc := range cases {

        t.Run(tc.name, func(t \*testing.T) {

            score := SurvivalScoreV4(tc.shard, tc.now)

            // assertions

        })

    }

}

**Coverage targets:**

- `SurvivalScoreV4` — all formula branches, edge cases (zero bonds, max salience, zero retrieval history)  
- `ACTRActivation` — empty history returns `ε`, recent timestamps weight higher than old  
- `SalToStability` — boundary values: `Sal=0.1 → ~1 day`, `Sal=1.0 → ~14 days`

**Files:** `internal/storage/cognitive_test.go` (new)

---

### 2.2 Integration Tests — Search Pipeline

Mock the three engines. Verify the search pipeline contracts hold regardless of backend behavior.

**Contracts to verify:**

- `search_all` deduplicates results across three engines (same shard ID appearing in Neo4j and Postgres returns only one result)  
- Anti-Double-Touching: `ReinforceShards()` is called exactly once per `search_all`, never per engine  
- Cognitive biasing: a biased query vector differs from the raw embedded query vector by the expected lambda blend  
- MMR re-ranking: results are more diverse than raw cosine-sorted results for the same query

**Pattern:** Use interface mocks (no real databases). The existing `Repository` interface makes this straightforward — implement `MockRepository` with controlled return values.

**Files:** `internal/mcp/server_test.go` (new), `internal/storage/mock_repository.go` (new)

---

### 2.3 Fuzz Testing — MCP Input Validation

The input validation in `server.go` (ID max 256 chars, content max 100KB, category whitelist, query max 10,000 chars) is exactly the kind of boundary surface that fuzz testing finds edge cases in.

func FuzzHandleSave(f \*testing.F) {

    f.Add("valid-id", "valid content", "memory")

    f.Fuzz(func(t \*testing.T, id, content, category string) {

        req := SaveRequest{ID: id, Content: content, Category: category}

        err := validateSaveRequest(req)

        // Must never panic, must return typed errors for invalid input

        if err \!= nil {

            var validationErr \*ValidationError

            if \!errors.As(err, \&validationErr) {

                t.Errorf("expected ValidationError, got %T", err)

            }

        }

    })

}

Run with: `go test -fuzz=FuzzHandleSave -fuzztime=60s`

**Files:** `internal/mcp/validation_fuzz_test.go` (new)

---

## Layer 3 — Performance

### 3.1 Benchmark the Sub-5ms Claim

The README states sub-5ms retrieval. This needs to be a measured number, not an aspiration.

func BenchmarkSearchAll(b \*testing.B) {

    // setup: real or realistic mock repo

    b.ResetTimer()

    for i := 0; i \< b.N; i++ {

        \_, err := server.SearchAll(ctx, "benchmark query", 10\)

        if err \!= nil {

            b.Fatal(err)

        }

    }

}

func BenchmarkSurvivalScoreV4(b \*testing.B) {

    shard := testShard()

    now := time.Now()

    b.ResetTimer()

    for i := 0; i \< b.N; i++ {

        \_ \= SurvivalScoreV4(shard, now)

    }

}

Run with: `go test -bench=. -benchmem -count=5 -cpuprofile=cpu.prof`

Analyze with: `go tool pprof cpu.prof`

**What to measure:**

- `search_all` end-to-end (including all three engines in parallel)  
- `SurvivalScoreV4` per shard (Janitor runs this on every shard in the mesh)  
- `SaveShard` including embedding call (or mock embedding for local benchmarks)  
- Neo4j round-trip latency isolated from application logic

**Goal:** Produce a `BENCHMARKS.md` with reproducible numbers. If sub-5ms holds — publish it. If it doesn't — fix it, then publish.

---

### 3.2 `sync.Pool` for Vector Slices

768-D float32 slices are allocated per search request in the embedding and MMR pipeline. Under load, this creates GC pressure as the runtime collects hundreds of short-lived 3KB allocations per second.

`sync.Pool` allows reuse:

var vectorPool \= sync.Pool{

    New: func() interface{} {

        s := make(\[\]float32, 768\)

        return \&s

    },

}

// In search hot path

vec := vectorPool.Get().(\*\[\]float32)

defer vectorPool.Put(vec)

// use \*vec

**Caveat:** `sync.Pool` objects can be collected by GC between GC cycles. This is fine for temporary computation buffers — do not use it for state that must persist.

The `vectorPool` global already exists in the codebase (documented in Dependency Matrix). This phase formalizes and optimizes its usage.

**Files:** `internal/storage/vessel_graph.go`, `internal/storage/mmr.go`

**Measurement:** Before/after `go test -bench=. -benchmem` should show reduction in `allocs/op` on search benchmarks.

---

### 3.3 `slog` Structured Logging Migration

Replace all `log.Printf` calls throughout the codebase with `slog`. This is not cosmetic — structured logs are machine-parseable, filterable by level and key, and composable with log aggregation pipelines.

// Before

log.Printf("Janitor: evicted shard %s, survival=%.2f", id, score)

// After

slog.Info("janitor eviction",

    "shard\_id", id,

    "survival\_score", score,

    "component", "janitor",

)

Wire a JSON handler for production (Docker) and a text handler for local development:

// main.go

if os.Getenv("LOG\_FORMAT") \== "json" {

    slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

} else {

    slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

}

**Files:** All Go files with `log.Printf` calls. Estimated \~15-20 call sites across `janitor.go`, `synthesizer.go`, `hygiene.go`, `server.go`, `main.go`.

---

## Layer 4 — Observability

### 4.1 `/metrics` Endpoint (Prometheus-Compatible)

Expose a `GET /metrics` endpoint in the Visual Ego dashboard server (`cmd/visual_ego/main.go`) with Prometheus-format metrics.

**Core metrics:**

\# Mesh state

shard\_link\_shards\_total{category="core|memory|session|archived"}

shard\_link\_bonds\_total

shard\_link\_communities\_total

\# Search performance

shard\_link\_search\_duration\_seconds{engine="neo4j|postgres|sqlite|all"}

shard\_link\_search\_requests\_total{tool="search\_all|search\_memory|search\_text|search\_graph"}

\# Cognitive engine

shard\_link\_janitor\_evictions\_total

shard\_link\_janitor\_cycle\_duration\_seconds

shard\_link\_synthesizer\_bonds\_created\_total

shard\_link\_synthesizer\_summaries\_generated\_total

\# Survival score distribution

shard\_link\_survival\_score\_bucket{le="20|50|80|95|100"}

Use `github.com/prometheus/client_golang` or implement the text format manually (straightforward for this metric count).

**Why this matters:** Operational insight today, Kubernetes readiness tomorrow. Any production deployment pattern — whether personal or shared infra — needs metrics.

---

### 4.2 Survival Score Distribution in Visual Ego

The current dashboard shows individual shard survival scores on hover. Phase 7 adds a mesh-level health view: a histogram or distribution panel showing how survival scores are distributed across the entire mesh.

**What to look for:**

- Scores clustered near 100 → Janitor threshold may be too conservative, mesh growing unbounded  
- Scores clustered near 20 → Janitor too aggressive, valuable memory being evicted prematurely  
- Bimodal distribution (many at 100, many at \<20) → healthy — core anchors stable, transient data cycling

**Implementation:** New `/api/health` endpoint in Visual Ego returning survival score buckets. Small panel in the dashboard sidebar next to the neighborhood list.

---

## Go Mastery Alignment

Every task in Phase 7 maps directly to the 2026 Go learning roadmap:

| Go Topic | Phase 7 Task |
| :---- | :---- |
| `context` propagation | Layer 1.3 — graceful shutdown context chain |
| `sync.WaitGroup` | Layer 1.3 — drain background goroutines on exit |
| `sync.Pool` | Layer 3.2 — vector slice reuse |
| `pprof` \+ benchmarks | Layer 3.1 — sub-5ms benchmark \+ profiling session |
| Table-driven tests | Layer 2.1 — cognitive.go test suite |
| Fuzz testing | Layer 2.3 — MCP input validation |
| `slog` structured logging | Layer 3.3 — replace all log.Printf calls |
| Error wrapping | Consistent `fmt.Errorf("%w", err)` across all vessels |
| Graceful shutdown | Layer 1.3 — OS signal handling in main.go |
| Escape analysis | Layer 3.1 — pprof reveals heap allocation hotspots |

Phase 7 is not separate from learning Go. It *is* the Go learning path — applied to a real system, end-to-end, with real consequences.

---

## Execution Sequence

Phase 7A — Close the Gaps         \~1-2 weeks

  → 1.1 Touch completeness (Postgres RetrievalHistory writes)

  → 1.2 Zero-timestamp guards on all write paths

  → 1.3 Graceful shutdown: context chain \+ WaitGroup drain

Phase 7B — Testing                \~2-3 weeks

  → 2.1 Table-driven tests: cognitive.go

  → 2.2 Integration tests: search pipeline \+ dedup \+ anti-double-touch

  → 2.3 Fuzz tests: MCP input validation

Phase 7C — Performance            \~2 weeks

  → 3.1 Benchmarks: search\_all, SaveShard hot paths

  → 3.1 pprof profiling session — find the real bottlenecks

  → 3.2 sync.Pool for vector slices

  → 3.3 slog migration

Phase 7D — Observability          \~1 week

  → 4.1 /metrics endpoint (Prometheus-compatible)

  → 4.2 Survival score distribution panel in Visual Ego

**Total estimated duration:** 6-8 weeks of focused engineering.

**Definition of done:** Every claim in the README is backed by a test, a benchmark, or a metric. The codebase shuts down cleanly, logs structurally, and fails loudly with typed errors. The Dependency Matrix has no undocumented gaps.

---

## What Phase 7 Is Not

- It is not a feature phase. No new MCP tools, no new storage engines, no UI redesigns.  
- It is not a refactor-for-refactor's-sake phase. Every change has a measurable outcome.  
- It is not a blocker on using Shard-Link. The system runs today. Phase 7 makes it trustworthy at depth.

---

*Architect: Brainy Bestie & BB* *Status: PLANNED — Phase 6.x COMPLETE* *Date: 2026-05-30*  
