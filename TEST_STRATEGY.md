# Shard-Link — Test Strategy

> Five-layer pyramid. Each layer answers a different question at a different cost.
> Layers 1–4 require zero external infrastructure. Layer 5 is deferred to publish time.

---

## Pyramid Overview

| Layer | What It Proves | Infra Required | Status |
|---|---|---|---|
| 1 — Unit | Pure functions produce correct output | None | ✓ Complete |
| 2 — Integration | Components wire together correctly | SQLite `:memory:` | ✓ Complete |
| 3 — Race | No data races under concurrency | None (`-race` flag) | ✓ Complete |
| 4 — Benchmark | Performance contracts hold after changes | None (`benchstat`) | ✓ Complete |
| 5 — E2E | Full stack boots and MCP tools respond | Docker Compose + Neo4j | Deferred |

---

## Layer 1 — Unit Tests

**What they cover:** Pure functions. No database, no network, no goroutines. Run in milliseconds and are the first line of defence against formula regressions.

### Current Coverage

| File | Covers | Quality |
|---|---|---|
| `cognitive_test.go` | `SurvivalScoreV4` — table-driven, NaN/Inf, zero-timestamp guards | Solid |
| `cognitive_bench_test.go` | `SurvivalScoreV4`, ACT-R, cosine, `DecodeVector`, MMR, RRF benchmarks | Solid |
| `validation_fuzz_test.go` | `validateSaveInput`, `validateQueryInput` — table-driven + fuzz | Solid |
| `server_test.go` | `MockRepository`, search fan-out, `WorkingMemory` cognitive biasing | Partial |
| `janitor_test.go` | Core shard eviction immunity, orphan targeting | Partial |

### Gaps to Fill

**`internal/storage/` — add to `cognitive_test.go` or new `shard_atomics_test.go`**

- `TestSalToStability` — boundary values: `0.1 → ~1d`, `0.5 → ~7d`, `1.0 → ~14d`; monotonicity assertion
- `TestCalculateACTRActivation` — empty history, single entry, full 20-entry window, comparative (recent > old)
- `TestRRF` — with overlap, without overlap, `k=60` bias proof
- `TestMMR` — `lambda=1.0` (pure relevance), `lambda=0.0` (pure diversity), mixed
- `TestVectorRoundTrip` — `EncodeVector` → `DecodeVector` → float32 equality within tolerance
- `TestDirtyShardCounter` — `MarkShardDirty` increments, `DirtyShardCount` reads, `ConsumeDirtyShards` zeroes
- `TestCommSummaryPrefixGate` — `comm-summary-*` prefix never increments `dirtyShardCount`
- `TestLastSynthesisTime` — `ConsumeDirtyShards` stamps `lastSynthesisNano`; `LastSynthesisTime()` reflects it

**`internal/synthesizer/` — new file `synthesizer_test.go`**

- `TestFilterSummarizable` — `contract` category filtered out, `memory`/`session` passes through, empty input safe
- `TestBuildSummaryPrompt` — character budget never exceeded (12 000 chars), correct header, member limit capped at 15
- `TestDynamicGate` — three branches: below threshold, threshold reached, max deferral exceeded

**`internal/mcp/` — add to `server_test.go`**

- `TestWorkingMemory_NoCentroid` — `Bias()` returns original vector unchanged on first query in session
- `TestCoreIntentGate_BlockedByDefault` — `save_memory` with `category=core`, no `allow_core` flag → error
- `TestCoreIntentGate_AllowedWithFlag` — same request with `allow_core=true` → accepted

**`internal/janitor/` — add to `janitor_test.go`**

- `TestJanitor_AtExactLimit` — `count == maxSize` → no eviction cycle triggered
- `TestJanitor_CommSummarySurvives` — `comm-summary-*` never appears in eviction candidates

### Run Commands

```bash
# All unit tests
go test ./...

# Specific package
go test ./internal/storage/
go test ./internal/synthesizer/
go test ./internal/mcp/

# Verbose
go test -v ./...
```

---

## Layer 2 — Integration Tests

**What they cover:** Component wiring through the real `Repository` interface. SQLite `:memory:` is the target — zero external services, sub-second startup, same pattern already used in `janitor_test.go` via `storage.NewVessel(":memory:")`.

Key distinction from unit tests: these exercise actual SQL queries, interface contracts, and atomic state transitions that mocks cannot verify.

### `internal/storage/vessel_integration_test.go`

| Test Name | Contract Being Verified |
|---|---|
| `TestSaveAndRetrieve` | `SaveShard` persists all fields; `FindText` returns them intact |
| `TestTouchUpdatesLastUsed` | `FindText(shouldTouch=true)` updates `LastUsed` timestamp |
| `TestRetrievalHistoryRolling` | `ReinforceShards` 21× — history capped at 20 entries, oldest dropped |
| `TestArchiveShard` | `ArchiveShard` removes from active `FindResonant`; appears in `GetArchivedShards` |
| `TestGetEvictionCandidates` | Insert 5 shards with known scores; lowest 2 returned as candidates |
| `TestCoreShardNeverEvicted` | `Category=core` always excluded from `GetEvictionCandidates` output |

### `internal/synthesizer/synthesizer_integration_test.go`

Mock Embedder + mock Summarizer + real SQLite vessel.

| Test Name | Contract Being Verified |
|---|---|
| `TestGate_BelowThreshold` | `dirty=3`, `min=5`, deferral not exceeded → `performSynthesis` returns early, no `SyncBonds` call |
| `TestGate_ThresholdReached` | `dirty=5` → gate passes, `SyncBonds` called once |
| `TestGate_DeferralForced` | `dirty=1`, `lastSynthesis=25h ago` → gate forces synthesis regardless of count |
| `TestCommSummaryNotReArming` | `SaveShard(comm-summary-X)` → `dirtyShardCount` unchanged after save |

### `internal/mcp/server_integration_test.go`

Real HTTP handler wired to mock Repository.

| Test Name | Contract Being Verified |
|---|---|
| `TestSaveMemory_ReturnsOK` | POST `save_memory` → HTTP 200, shard ID in response |
| `TestSaveMemory_CoreBlocked` | `category=core` without `allow_core=true` → HTTP 400 before repo is touched |
| `TestSearchAll_DeduplicatedTouch` | Fan-out across 3 engines → `ReinforceShards` called exactly once |
| `TestValidation_RejectedByHandler` | Oversized ID → HTTP 400; `MockRepository.SaveShard` call count == 0 |

### Run Commands

```bash
# Integration tests (if using build tag)
go test -tags integration ./...

# Or run all — :memory: SQLite has no side effects
go test ./internal/storage/ ./internal/synthesizer/ ./internal/mcp/
```

---

## Layer 3 — Race Condition Tests

**What they cover:** The same test files, run with Go's built-in race detector. No new files required — just tests that exercise *concurrent access* to shared state.

This matters specifically for Shard-Link because three goroutines (Janitor, Synthesizer, MCP handlers) concurrently read and write: `dirtyShardCount`, `lastSynthesisNano`, `GlobalLogger`, and `communityCache`.

### Tests to Write

| Test Name | File | What It Exercises |
|---|---|---|
| `TestDirtyCounter_ConcurrentWrites` | `shard_atomics_test.go` | 50 goroutines call `MarkShardDirty()` — final count must == 50, no lost increments |
| `TestDirtyCounter_ConcurrentReadWrite` | `shard_atomics_test.go` | Writers increment while Synthesizer goroutine calls `DirtyShardCount()` — no torn reads |
| `TestConsume_ConcurrentWithMark` | `shard_atomics_test.go` | `ConsumeDirtyShards` racing with `MarkShardDirty` — counter never goes negative |
| `TestWorkingMemory_ConcurrentSessions` | `server_test.go` | 100 goroutines call `Update()` and `Bias()` with different session IDs simultaneously |
| `TestSessionTokens_ConcurrentAuth` | `server_test.go` | Concurrent `withAuth()` reads + `handleOAuthToken()` writes — no map panic |

### Standard Pattern

```go
func TestDirtyCounter_ConcurrentWrites(t *testing.T) {
    // Reset shared state
    storage.ConsumeDirtyShards(storage.DirtyShardCount())

    const goroutines = 50
    var wg sync.WaitGroup
    wg.Add(goroutines)

    for i := 0; i < goroutines; i++ {
        go func() {
            defer wg.Done()
            storage.MarkShardDirty()
        }()
    }
    wg.Wait()

    got := storage.DirtyShardCount()
    if got != goroutines {
        t.Errorf("expected %d, got %d — possible lost increment", goroutines, got)
    }
}
```

### Run Commands

```bash
# Full suite with race detector
go test -race ./...

# Specific package
go test -race ./internal/storage/

# Verbose — shows which test triggers the detector
go test -race -v ./...
```

If the detector fires, output identifies the exact goroutines and line numbers involved.

---

## Layer 4 — Benchmark Validation

**What they cover:** Turning benchmarks from observation tools into pass/fail gates. A code change that regresses performance is caught before merging, not discovered in production.

### Current Benchmark Coverage

| Benchmark | What It Measures | Status |
|---|---|---|
| `BenchmarkSurvivalScoreV4` | Per-shard Janitor cost — empty, typical, full 20-entry history | ✓ Written |
| `BenchmarkACTRActivation` | ACT-R inner loop — 20 retrieval timestamps worst case | ✓ Written |
| `BenchmarkCosineSimilarity` | Hot inner loop — O(candidates²) in MMR, called per search | ✓ Written |
| `BenchmarkDecodeVector` | Pool-backed 768-D vector decode — called per shard per search | ✓ Written |
| `BenchmarkMMR` | Full MMR re-ranking pipeline — most allocation-heavy search path | ✓ Written |
| `BenchmarkRRF` | Reciprocal Rank Fusion across 3 result lists | ✓ Written |
| `BenchmarkSearchPipeline_Sub5ms` | End-to-end: embed → fan-out → dedup → RRF → MMR | Planned |

### The benchstat Workflow

```bash
# Install
go install golang.org/x/perf/cmd/benchstat@latest

# Step 1: Capture baseline before your change
go test -bench=. -benchmem -count=10 ./internal/storage/ > before.txt

# Step 2: Make your change

# Step 3: Capture after
go test -bench=. -benchmem -count=10 ./internal/storage/ > after.txt

# Step 4: Compare
benchstat before.txt after.txt
```

Example output:

```
name                    old time/op   new time/op   delta
SurvivalScoreV4-8       1.23µs ± 2%   2.41µs ± 3%  +95.93%  (p=0.000 n=10)
CosineSimilarity-8       890ns ± 1%    880ns ± 2%   ~        (p=0.420 n=10)
```

`+95.93%` on `SurvivalScoreV4` blocks the merge. `~` on `CosineSimilarity` means within noise — acceptable.

### pprof Workflow

Use when benchstat shows a regression and you need to find the cause.

```bash
# CPU profile
go test -bench=BenchmarkMMR -cpuprofile=cpu.prof ./internal/storage/
go tool pprof -http=:8090 cpu.prof

# Memory/allocation profile
go test -bench=BenchmarkMMR -memprofile=mem.prof ./internal/storage/
go tool pprof -http=:8090 mem.prof
```

### Performance Contracts

| Contract | Target | Benchmark |
|---|---|---|
| `search_all` end-to-end latency (p99) | < 5ms | `BenchmarkSearchPipeline_Sub5ms` (planned) |
| `SurvivalScoreV4` per-shard cost | < 2µs | `BenchmarkSurvivalScoreV4` |
| `CosineSimilarity` inner loop | < 1µs | `BenchmarkCosineSimilarity` |
| `DecodeVector` (768-D pool-backed) | < 500ns | `BenchmarkDecodeVector` |
| MMR re-ranking (20 candidates, k=10) | < 50µs | `BenchmarkMMR` |

### Run Commands

```bash
# All benchmarks
go test -bench=. -benchmem ./...

# Specific benchmark, 5 iterations for stability
go test -bench=BenchmarkSurvivalScoreV4 -benchmem -count=5 ./internal/storage/

# Benchmarks with race detector (confirms no races under load)
go test -bench=. -race ./...
```

---

## Layer 5 — E2E Tests (Deferred)

**What they cover:** Full stack validation — Docker Compose up, MCP server responds, all three engines behave together under real conditions.

### Why Deferred

- Real production usage has been running continuously with organic, semantically diverse inputs across all three engines. That is a better E2E harness than any script.
- Scripted inputs (`save "test content"` → `search "test content"`) are synthetically simple compared to real session traffic.
- Layers 1–4 catch the same bug classes at lower cost and faster feedback.
- No CI pipeline exists yet — E2E scripts have no automated runner.

### Recommended Path Forward (decided 2026-06-17)

**Do not write raw E2E test scripts.** Build `shard-cli` instead — it covers all four planned scenarios as a daily-use tool, not a test-only artifact. A CLI command that gets used every day is a better harness than a script that runs once during setup. GitHub Actions CI is the second priority: it makes Layers 1–4 run automatically on every push so enforcement doesn't rely on manual discipline.

Priority order:
1. `shard-cli` (Cobra + Viper, hits live MCP server) — natural E2E harness with real utility
2. GitHub Actions CI — automated enforcement for Layers 1–4
3. Dedicated E2E scripts — only if a bug slips through that `shard-cli` usage wouldn't catch

### Trigger Conditions — When to Revisit

| Trigger | Why It Changes the Calculus |
|---|---|
| Publishing Shard-Link | Others need to validate their own install without manual walkthrough |
| Adding GitHub Actions CI | Pipeline needs a smoke test to confirm deployment correctness on every PR |
| Real usage regression detected | A bug slips through Layers 1–4 — write the E2E scenario that would have caught it |
| `shard-cli` is built | CLI hitting MCP directly is a natural E2E harness with daily real-world use |

### Planned Scenarios

**Scenario 1 — Save → Search Round Trip**
```
1. POST save_memory("test content", category="memory")
2. POST search_all("test content", limit=5)
3. Assert: shard appears in results with correct ID and category
4. Assert: LastUsed updated, ReinforceShards called once
```

**Scenario 2 — Synthesizer Gate End-to-End**
```
1. Save 5 shards with varied content
2. Assert: dirtyShardCount == 5
3. Trigger performSynthesis (or wait for interval)
4. Assert: dirtyShardCount == 0 after ConsumeDirtyShards
5. Assert: bonds created in Neo4j (GetAllBonds count increases)
6. Assert: comm-summary-* shard saved if community >= 3 members
```

**Scenario 3 — Janitor Eviction Chain**
```
1. Save maxSize+1 shards with known salience values
2. Trigger Janitor cycle
3. Assert: count returns to maxSize
4. Assert: lowest-survival shard evicted (appears in GetArchivedShards)
5. Assert: comm-summary-* and core shards were NOT evicted
```

**Scenario 4 — OAuth + MCP Handshake**
```
1. Request authorization code via /oauth/authorize
2. Exchange code for token via /oauth/token
3. Call search_all with Bearer token → Assert: HTTP 200
4. Repeat with invalid token → Assert: HTTP 401
```

### shard-cli as Natural E2E Harness

The planned `shard-cli` (Cobra + Viper, hitting MCP server directly) covers the same scenarios while serving as a daily-use tool — a better investment than a dedicated test script.

| CLI Command | E2E Scenario It Covers |
|---|---|
| `shard save "content" --category memory` | MCP `save_memory` → Neo4j write path |
| `shard search "query" --limit 5` | MCP `search_all` → fan-out → dedup → RRF → MMR |
| `shard get <id>` | `GetShardByID` → correct field retrieval |
| `shard status` | `GetCount` → survival buckets → `/api/health` response |

---

## Quick Reference

| Goal | Command |
|---|---|
| Run all tests | `go test ./...` |
| Run with race detector | `go test -race ./...` |
| Run verbose | `go test -v ./...` |
| Run specific package | `go test ./internal/storage/` |
| Run specific test | `go test -run TestSurvivalScoreV4 ./internal/storage/` |
| Run all benchmarks | `go test -bench=. -benchmem ./...` |
| Run benchmark, 5 iterations | `go test -bench=BenchmarkMMR -count=5 ./internal/storage/` |
| Capture benchmark baseline | `go test -bench=. -count=10 ./internal/storage/ > before.txt` |
| Compare benchmark runs | `benchstat before.txt after.txt` |
| CPU profile | `go test -bench=BenchmarkMMR -cpuprofile=cpu.prof ./internal/storage/` |
| Open pprof web UI | `go tool pprof -http=:8090 cpu.prof` |
| Run fuzz test (60s) | `go test -fuzz=FuzzValidateSaveInput -fuzztime=60s ./internal/mcp/` |

### Recommended Order Per Session

1. `go test ./...` — confirm all units pass before any work
2. `go test -race ./...` — after any change touching shared state (atomics, maps, channels)
3. `go test -bench=. -benchmem -count=5 ./internal/storage/` — after any change to the search pipeline or survival formula
4. `benchstat before.txt after.txt` — when benchmarks show a surprising delta
5. `go tool pprof -http=:8090 cpu.prof` — when benchstat shows a regression and you need to find why
