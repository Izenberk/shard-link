# Shard-Link: Hardening & Upgrade Plan

**Status:** ACTIVE — Single Source of Truth **Author:** BB & Brainy Bestie **Last Updated:** 2026-05-22

**Absorbs and supersedes:**

- Notion: Docker Storage Cleanup Runbook  
- Google Doc: Storage Architecture Upgrade Plan  
- Google Doc: Next-Gen Architecture & Cognitive Upgrades (audited — 3/5 items already shipped)  
- Google Doc: Architectural Upgrades & Roadmap (SSE/Docker done — K8s absorbed here)  
- Repo: `GRAPH_IMPROVEMENT.md` (all items absorbed — retire this file after commit)

**Does NOT replace (frozen, append-only):**

- `PLAN.md` — feature phase history  
- `IMPROVEMENT.md` — resolved bottleneck log

---

## How to read this doc

Structured identically to `PLAN.md`. Each item is a checkbox. Work top-to-bottom — no phase starts until the previous one is fully checked. The "Why" is written before every implementation block so the reason is always on record.

---

## Phase 0: Storage Forensics & Emergency Cleanup

Before touching any code, confirm the actual disk culprit breakdown. A clean baseline means any post-upgrade anomaly is clearly new code, not legacy bloat. **Exception:** If Docker is actively crashing due to disk full, jump to item 0.4 (nuclear) first to restore stability, then return here.

- [ ] **0.1 Run forensics** — run from the `shard-link` project root on the Docker host:

\# Overall Docker storage summary

docker system df \-v

\# Actual bind mount sizes

du \-sh neo4j\_data/ neo4j\_logs/ neo4j\_plugins/ postgres\_data/ data/

\# Neo4j WAL vs store breakdown

du \-sh neo4j\_data/databases/ neo4j\_data/transactions/

\# Neo4j logs breakdown

du \-ah neo4j\_logs/ | sort \-rh | head \-20

\# Postgres WAL

du \-sh postgres\_data/pg\_wal/

\# Container log files

sudo du \-sh /var/lib/docker/containers/\*/\*-json.log | sort \-rh | head \-10

Expected culprit distribution for 50 GB:

| Directory | Suspected Size | Root Cause |
| :---- | :---- | :---- |
| `neo4j_data/transactions/` | 20–30 GB | GDS write-back WAL every 10 min, no checkpoint |
| `neo4j_data/databases/` | 5–10 GB | Graph store \+ 3072-D vector index |
| `neo4j_logs/` | 2–5 GB | No log rotation config |
| `postgres_data/` | 5–10 GB | Dead tuples, VACUUM never ran |
| Docker container logs | 2–5 GB | No log driver size limit |
| `data/shard-link.db` | \< 100 MB | SQLite activity ledger, no TTL |

- [ ] **0.2 Safe cleanup — zero downtime**

\# Docker prune (does NOT touch volumes)

docker system prune \-f

docker image prune \-f

docker builder prune \-f

\# Truncate container logs

sudo truncate \-s 0 /var/lib/docker/containers/\*/\*-json.log

\# Neo4j manual checkpoint — flush WAL to store (containers UP)

docker exec shard-link\_graph cypher-shell \\

  \-u neo4j \-p shardpass \\

  "CALL db.checkpoint();"

\# Verify WAL shrank

du \-sh neo4j\_data/transactions/

\# Delete old rotated Neo4j logs

find neo4j\_logs/ \-name "\*.log.\*" \-mtime \+3 \-delete

find neo4j\_logs/ \-name "\*.log.\[0-9\]\*" \-delete

\# Postgres VACUUM (containers UP)

docker exec shard-link\_db psql \-U sharduser \-d shardlink \\

  \-c "VACUUM ANALYZE shards;"

docker exec shard-link\_db psql \-U sharduser \-d shardlink \\

  \-c "VACUUM ANALYZE shards\_archive;"

\# Check dead tuple count before/after

docker exec shard-link\_db psql \-U sharduser \-d shardlink \\

  \-c "SELECT relname, n\_dead\_tup, n\_live\_tup,

      pg\_size\_pretty(pg\_total\_relation\_size(relid))

      FROM pg\_stat\_user\_tables ORDER BY n\_dead\_tup DESC;"

- [ ] **0.3 WAL truncation — if 0.2 checkpoint did not sufficiently shrink `transactions/`** (brief downtime):

\# Stop hub and tunnel only — keep neo4j running

docker compose stop hub tunnel

\# Force checkpoint again

docker exec shard-link\_graph cypher-shell \\

  \-u neo4j \-p shardpass \\

  "CALL db.checkpoint();"

\# Clean shutdown of Neo4j — flushes remaining WAL to store

docker compose stop neo4j

\# Check residual size

du \-sh neo4j\_data/transactions/

\# Delete old applied transaction logs (safe after clean shutdown)

find neo4j\_data/transactions/ \-name "\*.logX.\*" \-mtime \+1 \-delete 2\>/dev/null

\# Bring everything back up

docker compose up \-d

- [ ] **0.4 Nuclear option — last resort only**

Deletes the entire Knowledge Mesh (Neo4j) and Archival Vessel (Postgres). Only use if 0.2 \+ 0.3 still leave \> 20 GB AND core shards are confirmed safe in SQLite.

docker compose down

\# Backup SQLite seed memory first

cp data/shard-link.db data/shard-link.db.backup.$(date \+%Y%m%d)

\# Wipe Neo4j and Postgres

rm \-rf neo4j\_data/ neo4j\_logs/

mkdir neo4j\_data neo4j\_logs neo4j\_plugins

rm \-rf postgres\_data/

mkdir postgres\_data

\# Restart and rebuild

docker compose up \-d \--build

\# Re-migrate shards from SQLite seed memory

docker exec shard-link\_hub /migrate

- [ ] **0.5 Confirm baseline** — storage must be \< 10 GB total before proceeding to Phase 1:

docker system df \-v

du \-sh neo4j\_data/ neo4j\_logs/ postgres\_data/ data/

---

## Phase 1: Emergency Prevention (docker-compose only)

One code change (`vessel.go` TTL) plus `docker-compose.yml` and `.env.example`. Stops the bleeding permanently. Can be done in the same session as Phase 0\.

- [x] **1.1 Container log rotation** — add `logging` blocks to `docker-compose.yml` under hub, db, and neo4j services:

**Note:** Neo4j 5.26 does NOT accept internal log rotation settings (`server.logs.*.rotation.*`) via environment variables — strict config validation rejects them. Docker's `json-file` driver handles container-level log capping instead. The `neo4j_logs/` bind mount (debug.log, neo4j.log) is < 30 MB and not a concern.

neo4j:

  logging:

    driver: "json-file"

    options:

      max-size: "50m"

      max-file: "3"

hub:

  logging:

    driver: "json-file"

    options:

      max-size: "20m"

      max-file: "3"

db:

  logging:

    driver: "json-file"

    options:

      max-size: "20m"

      max-file: "3"

docker compose up \-d

- [x] **1.2 SQLite activity ledger TTL** — `internal/storage/vessel.go`

**Why:** `activity_logs` has no TTL. Inserts on every shard save, bond forge, eviction, and Synthesizer cycle. `GetRecentActivity()` reads the last 50 rows but never deletes. Grows at \~100+ rows/hour indefinitely.

func (v \*Vessel) SaveActivity(ctx context.Context, entry ShardActivity) error {

    const insert \= \`INSERT INTO activity\_logs (type, message, shard\_id) VALUES (?, ?, ?)\`

    stmt, \_, err := v.conn.Prepare(insert)

    if err \!= nil {

        return err

    }

    defer stmt.Close()

    stmt.BindText(1, entry.Type)

    stmt.BindText(2, entry.Message)

    stmt.BindText(3, entry.ShardID)

    if err := stmt.Exec(); err \!= nil {

        return err

    }

    // TTL purge — keep last N days only

    retentionDays := os.Getenv("ACTIVITY\_LOG\_RETENTION\_DAYS")

    if retentionDays \== "" {

        retentionDays \= "7"

    }

    purge, \_, \_ := v.conn.Prepare(

        \`DELETE FROM activity\_logs WHERE timestamp \< datetime('now', '-' || ? || ' days')\`,

    )

    if purge \!= nil {

        defer purge.Close()

        purge.BindText(1, retentionDays)

        \_ \= purge.Exec()

    }

    return nil

}

- [x] **1.3 Add to `.env.example`:**

\# Activity ledger retention window (days). Default: 7

ACTIVITY\_LOG\_RETENTION\_DAYS=7

- [x] **1.4 Verify Phase 1** — confirm log cap is applied after restart:

docker inspect shard-link\_graph | grep \-A5 LogConfig

docker inspect shard-link\_hub   | grep \-A5 LogConfig

---

## Phase 2: Core Storage Architecture

Architectural changes to eliminate the root causes permanently. Requires careful testing against Neo4j GDS API after each sub-phase. Do not start until Phase 1 is fully checked and `docker system df` confirms stable baseline.

- [ ] **2.1 GDS stream mode refactor** — `internal/storage/vessel_graph.go`

**Why:** Every Synthesizer cycle (10 min) calls `CalculateCommunities()`, which runs `gds.louvain.write` \+ `gds.pageRank.write`. Both rewrite properties on EVERY Shard node every cycle, generating a WAL entry per node per cycle — O(all nodes) WAL growth regardless of whether anything changed. Fix: stream results into Go memory, delta-write only changed nodes.

**Add new internal types at package level:**

type CommunityMetrics struct {

    CommunityID int64

    PageRank    float64

}

var (

    communityCache   map\[string\]CommunityMetrics

    communityCacheMu sync.RWMutex

)

**Replace `CalculateCommunities()` body:**

func (v \*VesselGraph) CalculateCommunities(ctx context.Context) (int, error) {

    const cleanupQuery  \= \`CALL gds.graph.drop('communityGraph', false)\`

    const projectQuery  \= \`

        CALL gds.graph.project('communityGraph', 'Shard', 'CONNECTED\_TO',

            {relationshipProperties: 'weight'}) YIELD graphName\`

    const louvainQuery  \= \`

        CALL gds.louvain.stream('communityGraph')

        YIELD nodeId, communityId

        RETURN gds.util.asNode(nodeId).id AS shardID, communityId\`

    const pageRankQuery \= \`

        CALL gds.pageRank.stream('communityGraph')

        YIELD nodeId, score

        RETURN gds.util.asNode(nodeId).id AS shardID, score\`

    const dropQuery     \= \`CALL gds.graph.drop('communityGraph', false)\`

    session := v.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: v.dbName})

    defer session.Close(ctx)

    newCache := make(map\[string\]CommunityMetrics)

    \_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {

        \_, \_ \= tx.Run(ctx, cleanupQuery, nil)

        if \_, err := tx.Run(ctx, projectQuery, nil); err \!= nil {

            return nil, err

        }

        lRes, err := tx.Run(ctx, louvainQuery, nil)

        if err \!= nil {

            return nil, err

        }

        for lRes.Next(ctx) {

            id, \_   := lRes.Record().Get("shardID")

            comm, \_ := lRes.Record().Get("communityId")

            newCache\[id.(string)\] \= CommunityMetrics{CommunityID: comm.(int64)}

        }

        prRes, err := tx.Run(ctx, pageRankQuery, nil)

        if err \!= nil {

            return nil, err

        }

        for prRes.Next(ctx) {

            id, \_    := prRes.Record().Get("shardID")

            score, \_ := prRes.Record().Get("score")

            if m, ok := newCache\[id.(string)\]; ok {

                m.PageRank \= score.(float64)

                newCache\[id.(string)\] \= m

            }

        }

        \_, \_ \= tx.Run(ctx, dropQuery, nil)

        return nil, nil

    })

    if err \!= nil {

        return 0, err

    }

    // Delta write — only nodes whose values changed

    communityCacheMu.RLock()

    old := communityCache

    communityCacheMu.RUnlock()

    var updates \[\]map\[string\]any

    for id, newM := range newCache {

        if old \== nil || old\[id\] \!= newM {

            updates \= append(updates, map\[string\]any{

                "id":        id,

                "community": newM.CommunityID,

                "pagerank":  newM.PageRank,

            })

        }

    }

    if len(updates) \> 0 {

        writeQuery := \`

            UNWIND $updates AS u

            MATCH (s:Shard {id: u.id})

            SET s.community \= u.community, s.pagerank \= u.pagerank\`

        \_, \_ \= neo4j.ExecuteQuery(ctx, v.driver, writeQuery,

            map\[string\]any{"updates": updates},

            neo4j.EagerResultTransformer,

            neo4j.ExecuteQueryWithDatabase(v.dbName))

        log.Printf("\[VesselGraph\] Community delta-write: %d nodes updated", len(updates))

    } else {

        log.Println("\[VesselGraph\] Community delta-write: no changes — WAL untouched")

    }

    communityCacheMu.Lock()

    communityCache \= newCache

    communityCacheMu.Unlock()

    return len(newCache), nil

}

**Add cache-aware shard builder — replace `nodeToShard()` calls in `GetAllShards()` and `GetGraphData()`:**

func nodeToShardWithCache(node neo4j.Node) Shard {

    s := nodeToShard(node)

    communityCacheMu.RLock()

    if m, ok := communityCache\[s.ID\]; ok {

        s.CommunityID \= m.CommunityID

        s.PageRank    \= m.PageRank

    }

    communityCacheMu.RUnlock()

    return s

}

- [ ] **2.2 Validate GDS stream mode** — run after 2.1 is deployed:

\# Watch logs for delta-write lines — should show "WAL untouched" on stable mesh

docker compose logs hub \--follow | grep "delta-write"

\# Run mesh audit to confirm no integrity issues

go run cmd/check\_mesh/main.go

- [ ] **2.3 Storage HygieneWorker** — new file `internal/hygiene/hygiene.go`

**Why:** The Janitor holds `VesselGraph` as its `Repository`. When eviction occurs, `j.vessel.Optimize()` only calls `VesselGraph.Optimize()` which re-runs `ensureIndexes()`. `PostgresVessel.Optimize()` — containing `VACUUM ANALYZE shards` — is never invoked. Dead tuple accumulation is unbounded.

package hygiene

import (

    "context"

    "log"

    "time"

    "github.com/izenberk/shard-link/internal/storage"

)

type HygieneWorker struct {

    graphVessel \*storage.VesselGraph

    pgVessel    \*storage.PostgresVessel

    localVessel \*storage.Vessel

    interval    time.Duration

}

func NewHygieneWorker(

    g  \*storage.VesselGraph,

    pg \*storage.PostgresVessel,

    lv \*storage.Vessel,

    interval time.Duration,

) \*HygieneWorker {

    return \&HygieneWorker{

        graphVessel: g,

        pgVessel:    pg,

        localVessel: lv,

        interval:    interval,

    }

}

func (h \*HygieneWorker) Run(ctx context.Context) {

    log.Printf("\[Hygiene\] Service ignited. Interval: %v", h.interval)

    ticker := time.NewTicker(h.interval)

    defer ticker.Stop()

    for {

        select {

        case \<-ctx.Done():

            log.Println("\[Hygiene\] Shutting down...")

            return

        case \<-ticker.C:

            h.performHygiene(ctx)

        }

    }

}

func (h \*HygieneWorker) performHygiene(ctx context.Context) {

    log.Println("\[Hygiene\] Starting maintenance cycle...")

    if h.pgVessel \!= nil {

        if err := h.pgVessel.Optimize(ctx); err \!= nil {

            log.Printf("\[Hygiene ERROR\] Postgres: %v", err)

        } else {

            log.Println("\[Hygiene\] Postgres: VACUUM ANALYZE complete.")

        }

    }

    if h.graphVessel \!= nil {

        if err := h.graphVessel.Optimize(ctx); err \!= nil {

            log.Printf("\[Hygiene ERROR\] Neo4j: %v", err)

        } else {

            log.Println("\[Hygiene\] Neo4j: Index integrity verified.")

        }

    }

    if h.localVessel \!= nil {

        if err := h.localVessel.Optimize(ctx); err \!= nil {

            log.Printf("\[Hygiene ERROR\] SQLite: %v", err)

        } else {

            log.Println("\[Hygiene\] SQLite: VACUUM complete.")

        }

    }

    log.Println("\[Hygiene\] Maintenance cycle complete.")

}

- [ ] **2.4 Wire HygieneWorker into `main.go`** — add after vessel ignition block, before MCP server launch:

import "strconv"

// Cast to concrete types for HygieneWorker

graphV := v.(\*storage.VesselGraph)

hygieneInterval := 24 \* time.Hour

if h := os.Getenv("HYGIENE\_INTERVAL\_HOURS"); h \!= "" {

    if hours, err := strconv.Atoi(h); err \== nil {

        hygieneInterval \= time.Duration(hours) \* time.Hour

    }

}

hygieneWorker := hygiene.NewHygieneWorker(graphV, av, lv, hygieneInterval)

go hygieneWorker.Run(ctx)

- [ ] **2.5 Add to `.env.example`:**

\# Storage hygiene interval (hours). Default: 24

HYGIENE\_INTERVAL\_HOURS=24

- [ ] **2.6 Verify Phase 2** — confirm HygieneWorker is logging after first cycle:

docker compose logs hub | grep "\\\[Hygiene\\\]"

---

## Phase 3: Infrastructure Hardening

Optional but required before declaring the storage issue fully resolved. Do not start until Phase 2 is verified stable for at least 24 hours.

- [ ] **3.1 Named Docker volumes for Neo4j** — replace bind mounts in `docker-compose.yml`:

**Why:** Bind mounts have no size governance at the Docker layer. Named volumes enable `docker system df` tracking and future size cap policies.

Back up first if `./neo4j_data/` has content:

cp \-r neo4j\_data/ neo4j\_data\_backup\_$(date \+%Y%m%d)

\# Add at bottom of docker-compose.yml

volumes:

  neo4j\_data:

    driver: local

  neo4j\_plugins:

    driver: local

\# Update neo4j service volumes section

neo4j:

  volumes:

    \- neo4j\_data:/data         \# named volume

    \- ./neo4j\_logs:/logs       \# bind mount (easy host log access)

    \- neo4j\_plugins:/plugins   \# named volume

docker compose down

docker volume create shard-link\_neo4j\_data

docker volume create shard-link\_neo4j\_plugins

docker compose up \-d \--build

\# Re-migrate if fresh volume (neo4j\_data was wiped)

docker exec shard-link\_hub /migrate

- [ ] **3.2 Full `.env.example` audit** — confirm all tunables are documented:

\# ─── Hub ────────────────────────────────────────────────────────

HUB\_API\_KEY=your\_mcp\_api\_key\_here

PUBLIC\_URL=https://your-hub.example.com

\# ─── Intelligence ───────────────────────────────────────────────

EMBEDDING\_MODE=none

EMBEDDING\_MODEL=gemini-embedding-001

GEMINI\_API\_KEY=your\_actual\_key\_here

OLLAMA\_URL=http://localhost:11434

OLLAMA\_MODEL=nomic-embed-text

MMR\_LAMBDA=0.7

\# ─── Knowledge Mesh (Neo4j) ─────────────────────────────────────

NEO4J\_URL=bolt://neo4j:7687

NEO4J\_USER=neo4j

NEO4J\_PASS=shardpass

MESH\_LINK\_THRESHOLD=0.75

JANITOR\_RESONANCE\_THRESHOLD=0.70

\# ─── Archival Vessel (PostgreSQL) ───────────────────────────────

DATABASE\_URL=postgres://sharduser:shardpass@db:5432/shardlink?sslmode=disable

\# ─── Seed Memory (SQLite) ───────────────────────────────────────

DATABASE\_PATH=/app/data/shard-link.db

\# ─── Storage Hygiene ────────────────────────────────────────────

ACTIVITY\_LOG\_RETENTION\_DAYS=7

HYGIENE\_INTERVAL\_HOURS=24

\# ─── Cloudflare Tunnel ──────────────────────────────────────────

CLOUDFLARE\_TUNNEL\_TOKEN=your\_token\_here

- [ ] **3.3 Verify Phase 3 success criteria:**

\# Neo4j logs capped

du \-sh neo4j\_logs/

\# → must be \< 150 MB

\# WAL bounded

du \-sh neo4j\_data/transactions/

\# → must be \< 500 MB after checkpoint

\# Postgres dead tuples under control

docker exec shard-link\_db psql \-U sharduser \-d shardlink \\

  \-c "SELECT relname, n\_dead\_tup FROM pg\_stat\_user\_tables ORDER BY n\_dead\_tup DESC;"

\# → dead tuple ratio \< 20%

\# Activity log bounded

docker exec shard-link\_hub sqlite3 /app/data/shard-link.db \\

  "SELECT COUNT(\*) FROM activity\_logs;"

\# → must be ≤ 10,080

\# No uncontrolled volume growth

docker system df

---

## Phase 4: Correctness Fixes

These fix actual bugs — wrong data returned or wrong shards evicted. All are single-file surgical changes. Start after Phase 3 success criteria are met.

- [ ] **4.1 Janitor GDS fallback — resonance filter missing** — `internal/storage/vessel_graph.go`

**Why:** When the GDS projection fails (e.g. no relationships exist yet), `GetEvictionCandidates()` falls through to a degree-centrality fallback. That fallback does NOT apply the resonance threshold filter — a shard with high cosine similarity to a core shard can be evicted silently.

// BEFORE — broken fallback, no resonance protection

fallbackQuery := \`

MATCH (s:Shard) WHERE s.category \<\> 'core'

OPTIONAL MATCH (s)-\[r\]-()

WITH s, count(r) AS links

ORDER BY links ASC, s.last\_used ASC

LIMIT $limit

RETURN s.id

\`

// AFTER — resonance protection preserved in fallback

fallbackQuery := \`

MATCH (core:Shard {category: 'core'})

MATCH (s:Shard) WHERE s.category \<\> 'core'

WITH s, core, gds.similarity.cosine(s.embedding, core.embedding) AS sim

WITH s, max(sim) AS maxResonance

WHERE maxResonance \< parseFloat($threshold)

OPTIONAL MATCH (s)-\[r\]-()

WITH s, count(r) AS links

ORDER BY links ASC, s.last\_used ASC

LIMIT $limit

RETURN s.id

\`

// Pass "threshold" param same as primary GDS path

params := map\[string\]any{"limit": limit, "threshold": threshold}

- [ ] **4.2 Neighbor BondCount unpopulated in SearchGraph** — `internal/storage/vessel_graph.go`

**Why:** In `SearchGraph()`, the center shard gets `BondCount` via `centerDegree` but neighbor shards are built from `collect({node: neighbor, weight: r.weight})` with no degree info. Dashboard survival scores treat all neighbors as orphans.

// BEFORE — no neighbor degree

query \= \`

...

OPTIONAL MATCH (center)-\[r:CONNECTED\_TO\]-(neighbor:Shard)

WITH center, neighbor, r

RETURN center, count(r) AS centerDegree,

       collect({node: neighbor, weight: r.weight}) AS neighbors

\`

// AFTER — neighbor degree included via subquery

query \= \`

...

OPTIONAL MATCH (center)-\[r:CONNECTED\_TO\]-(neighbor:Shard)

WITH center, neighbor, r

OPTIONAL MATCH (neighbor)-\[nr:CONNECTED\_TO\]-()

WITH center, neighbor, r, count(nr) AS neighborDegree

RETURN center,

       count(r) AS centerDegree,

       collect({node: neighbor, weight: r.weight, degree: neighborDegree}) AS neighbors

\`

In Go result parsing, add:

neighborShard.BondCount \= int(m\["degree"\].(int64))

- [ ] **4.3 Verify Phase 4** — run janitor manually and confirm resonant shards are protected:

\# Trigger a Janitor cycle via log inspection

docker compose logs hub | grep "\\\[Janitor\\\]"

\# Run mesh audit — should show zero invalid evictions

go run cmd/check\_mesh/main.go

---

## Phase 5: Quality Ceiling Lifts

Fully shipped features with known constraints. No correctness risk. Start after Phase 4 is verified.

- [ ] **5.1 search\_all parallel engine calls** — `internal/mcp/server.go`

**Why:** Text, vector, and graph calls in `handleSearchAll()` run sequentially. Latency \= sum of all three. Fix: fan out via `sync.WaitGroup`, merge under mutex.

var (

    mu         sync.Mutex

    seenShards \= make(map\[string\]storage.Shard)

    allBonds   \[\]storage.ShardBond

    wg         sync.WaitGroup

)

wg.Add(1)

go func() {

    defer wg.Done()

    results, \_ := s.vessel.FindText(ctx, query, limit, false)

    mu.Lock()

    for \_, sh := range results { seenShards\[sh.ID\] \= sh }

    mu.Unlock()

}()

if queryVec \!= nil {

    wg.Add(1)

    go func() {

        defer wg.Done()

        results, \_ := s.vessel.FindResonant(ctx, queryVec, limit, false)

        mu.Lock()

        for \_, sh := range results { seenShards\[sh.ID\] \= sh }

        mu.Unlock()

    }()

    wg.Add(1)

    go func() {

        defer wg.Done()

        gShards, gBonds, \_ := s.vessel.SearchGraph(ctx, queryVec, limit, false)

        mu.Lock()

        for \_, sh := range gShards { seenShards\[sh.ID\] \= sh }

        allBonds \= append(allBonds, gBonds...)

        mu.Unlock()

    }()

}

wg.Wait()

// Unified reinforcement — touch all deduplicated IDs exactly once

ids := make(\[\]string, 0, len(seenShards))

for id := range seenShards { ids \= append(ids, id) }

\_ \= s.vessel.ReinforceShards(ctx, ids)

- [ ] **5.2 Confidence field wired through RRF** — `internal/storage/rrf.go`

**Why:** `Confidence` exists on the `Shard` struct and schema but the RRF output never populates it. MCP callers receive `Confidence: 0` on all `search_all` results — useless for confidence-filtered retrieval downstream.

// In the final result collection loop, add:

shard := shardMap\[sorted\[i\].id\]

shard.Confidence \= sorted\[i\].score  // wire fusion score → Confidence

result \= append(result, shard)

- [ ] **5.3 MMR lambda as MCP param and env var** — `internal/mcp/server.go` \+ `main.go`

**Why:** Lambda is hardcoded to 0.7. Cannot be tuned per-query or per-deployment without a code change.

Add optional param to `search_memory` tool definition:

mcp.WithNumber("lambda", mcp.Description(

    "MMR diversity tuning (0.0=max diversity, 1.0=max relevance). Default: 0.7")),

In `handleSearch()`, read with fallback:

lambda := request.GetFloat("lambda", 0.7)

if envL := os.Getenv("MMR\_LAMBDA"); lambda \== 0.7 && envL \!= "" {

    if parsed, err := strconv.ParseFloat(envL, 64); err \== nil {

        lambda \= parsed

    }

}

results \= storage.MaximalMarginalRelevance(queryVec, results, limit, lambda)

Add to `.env.example`:

MMR\_LAMBDA=0.7

- [ ] **5.4 Auth middleware rate limiting** — `internal/mcp/server.go`

**Why:** `withAuth()` validates `X-API-Key` but has no per-key request rate cap. Cloudflare provides edge-level DDoS protection but nothing at the application layer.

import "golang.org/x/time/rate"

var (

    rateLimiters   \= make(map\[string\]\*rate.Limiter)

    rateLimitersMu sync.Mutex

)

func getLimiter(key string) \*rate.Limiter {

    rateLimitersMu.Lock()

    defer rateLimitersMu.Unlock()

    if lim, ok := rateLimiters\[key\]; ok {

        return lim

    }

    lim := rate.NewLimiter(rate.Every(time.Minute/60), 10\) // 60 req/min, burst 10

    rateLimiters\[key\] \= lim

    return lim

}

Add rate check inside `withAuth()` after key validation:

key := r.Header.Get("X-API-Key")

if key \== "" { key \= r.RemoteAddr }

if \!getLimiter(key).Allow() {

    http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)

    return

}

---

## Phase 6: New Features

Genuine new feature work. Not bug fixes, not hardening. Do not start until Phases 1–5 are complete and stable. Items here are candidates for the next PLAN.md phase entry (Phase 12).

- [ ] **6.1 GraphRAG community summaries** \[Phase 12 candidate\]

**What:** After Louvain clustering, synthesize an LLM-generated paragraph summary for each community cluster. Store the summary as a `core`\-category shard anchored to that community. Gives the AI agent a neighborhood-level orientation rather than just fragment-level retrieval.

**Entry point:** Synthesizer background worker, after `CalculateCommunities()` completes and delta cache shows which communities changed.

**Dependency:** Phase 2.1 (delta-write cache) is a prerequisite — it tells us which communities actually changed and avoids regenerating summaries for stable clusters.

**Rough effort:** 3–4 days.

- [ ] **6.2 Working memory / cognitive biasing** \[Phase 12 candidate\]

**What:** Track recently retrieved shards within a session as a rolling centroid. Bias new queries toward that centroid — mimicking "thinking about X → related things come to mind more easily."

**Sketch:** Per-session `[]float32` centroid updated on each retrieval. Blend query vector with centroid: `q' = λ·q + (1-λ)·centroid`. Session state lives in MCP request context — no persistence required.

**Dependency:** Phase 5.1 (search\_all parallelism) should ship first so centroid update doesn't block fan-out.

**Rough effort:** 2 days.

- [ ] **6.3 Dream Replay — Basement resurrection** \[Phase 12 candidate\]

**What:** Background job that samples archived (White Dwarf) shards from Postgres, checks cosine similarity against the current active mesh centroid, and promotes resonant archived shards back to Neo4j living memory.

**When to build:** Only when the Basement actually fills up with relevant-but-archived shards. Defer until there is a real failure mode pointing at it.

**Rough effort:** 2 days.

- [ ] **6.4 Embedding dimension right-sizing** \[Phase 9 continuation\]

**What:** Migrate from 3072-D (Gemini) to 512-D or 768-D local models when Ollama is the confirmed primary embedder. Candidate models: `bge-small-en-v1.5` (384-D), `nomic-embed-text-v1.5` Matryoshka (512-D).

**Blocker:** Requires re-embedding all existing shards. Dual-write window or lazy re-embed on access. Non-trivial migration tooling.

**Rough effort:** 2–3 days including migration tooling.

- [ ] **6.5 Kubernetes deployment** \[H2 2026 roadmap\]

**What:** Transition the containerised stack into a local K8s cluster (`minikube` or `k3s`) as the Platform Engineering learning milestone.

**Mechanics:**

- `Deployment` manifest for the Hub service  
- `Service` manifest (ClusterIP/NodePort) to expose MCP endpoint  
- `StatefulSet` for Neo4j with persistent volume claims  
- `ConfigMap` and `Secret` for environment variables  
- `CronJob` for HygieneWorker (replaces goroutine)

**Dependency:** All Phases 1–5 must be stable in Docker first. Do not start until mid-H2 2026\.

**Rough effort:** 1–2 weeks including StatefulSet learning curve.

---

## Risk Register

| Phase | Change | Risk | Mitigation |
| :---- | :---- | :---- | :---- |
| 1.1 | Container log rotation | Low | SHIPPED. Neo4j 5.26 rejects internal log rotation env vars — Docker json-file driver used instead. |
| 1.2 | SQLite TTL purge | Low | SHIPPED. Scoped to `activity_logs`. Core shards unaffected. |
| 2.1 | GDS stream mode | Medium | GDS `stream` stable in Neo4j 5.x. Validate with `cmd/check_mesh` post-deploy. |
| 2.3 | HygieneWorker | Low | Additive goroutine. No existing logic changed. |
| 3.1 | Named Docker volumes | Medium | Requires data migration. Back up `neo4j_data/` first. |
| 4.1 | Janitor fallback fix | Low | Single query change. No interface changes. |
| 4.2 | SearchGraph degree fix | Low | Query extension only. Additive. |
| 5.1 | search\_all goroutines | Low | Isolated to `handleSearchAll()`. No shared state risk. |
| 5.2 | RRF Confidence wire | Low | Single return value addition in `rrf.go`. |
| 5.3 | MMR lambda param | Low | Additive MCP param with fallback default. |
| 5.4 | Rate limiting | Low | Additive middleware. Remove cleanly if issues arise. |

---

## Archived docs — do not update

| Doc | Location | Status |
| :---- | :---- | :---- |
| Docker Storage Cleanup Runbook | Notion | Archived — fully absorbed into Phase 0 |
| Storage Architecture Upgrade Plan | Google Docs | Archived — absorbed into Phases 1–3 |
| Next-Gen Architecture & Cognitive Upgrades | Google Docs | Archived — 3/5 items shipped in Phase 9, 2 items in Phase 6 |
| Architectural Upgrades & Roadmap | Google Docs | Archived — SSE/Docker done, K8s in Phase 6.5 |
| `GRAPH_IMPROVEMENT.md` | Repo | Retire — delete after this commit |

---

*Status: PHASE 1 COMPLETE — PHASE 2 NEXT | Date: 2026-05-24* *Authors: BB & Brainy Bestie*  
