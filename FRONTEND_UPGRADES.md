# Shard-Link: Visual Ego Dashboard — Frontend Upgrades

**Status:** IMPLEMENTED **Last Updated:** 2026-05-28 **Files:** `web/static/index.html`, `web/static/app.js`, `web/static/style.css`, `cmd/visual_ego/main.go` **Backend files (logging section):** `internal/janitor/janitor.go`, `internal/synthesizer/synthesizer.go`, `internal/hygiene/hygiene.go`, `main.go`

---

## Design Philosophy

Visual Ego's original intent is **visualization \+ light control** — see the mesh, inspect shards, manage bonds. It is not a full CRUD dashboard. The write path belongs to MCP tools.

This upgrade plan borrows patterns from established graph visualization tools (Neo4j Bloom, GraphRAG dashboards) where they directly serve that intent. Patterns that add complexity without improving mesh understanding are explicitly excluded.

**What we borrow and why:**

- **Multi-resolution view** (Neo4j Bloom, GraphRAG) — graph dashboards should support both node-level and cluster-level context. Right now Visual Ego is locked at node resolution. Community summaries from Phase 6.1 already exist in Neo4j but have no UI surface.  
- **Rule-based visual encoding** (Neo4j Bloom GDS integration) — node size and opacity should be bound to meaningful metrics (PageRank, survival score) so mesh health is readable at a glance without clicking anything.  
- **Explicit mode separation** (Neo4j Bloom's Explore/Inspect/Edit model) — when the user switches to bond edit mode, the UI should make that unmistakably clear. Silent mode changes cause errors.  
- **Filter vs highlight** (standard graph dashboard pattern) — dimming nodes to 5% opacity still creates visual noise. True isolation (hide everything outside a community) is a distinct and more useful operation.  
- **Provenance visibility** (GraphRAG dashboard pattern) — knowledge graph tools surface data lineage inline. `source_type`, `source_ref`, and `confidence` exist on every shard from Phase 9 but are invisible in the Inspector.

**What we explicitly do not borrow:**

- 3D visualization — overkill at personal exocortex scale  
- Time slider / temporal animation — not core to the use case  
- Full CRUD from the UI — MCP tools are the write path  
- Natural language search replacing semantic search — already have embedding-based search which is superior

**Decisions made and closed:**

- Category promotion/demotion (core ↔ memory) — NOT added to dashboard. MCP conversation handles this already. Dashboard stays as visualization \+ light control only.

---

## How to read this doc

Same structure as HARDENING.md. Each item is a checkbox. Items are ordered by value — work top-to-bottom within each section. The "Why" is written before every item so the reason is always on record.

Two item prefixes:

- **F.x** — Visual Ego graph UI changes (frontend-only unless noted)  
- **L.x** — Activity feed and logging changes (touches backend workers \+ frontend feed)

---

## Section 1: Graph Visualization Upgrades

### High Value — Directly Useful

- [x] **F.1 Community Summary Display**

**Why:** Phase 6.1 shipped GraphRAG community summaries — LLM-generated paragraph descriptions stored as `comm-summary-{communityID}` shards. The dashboard's ACTIVE\_NEIGHBORHOODS buttons (N\_0, N\_1...) highlight clusters visually but provide zero context about what the cluster actually contains. The summary data already exists in Neo4j; it just needs to be surfaced.

This directly addresses the **multi-resolution view** gap identified from Neo4j Bloom and GraphRAG dashboard patterns — users need both node-level detail and cluster-level orientation without clicking through every node individually.

**What:** When clicking a neighborhood button, fetch and display the corresponding `comm-summary-*` shard's content as a panel or expandable tooltip below the button. Falls back to "No summary available" if the community hasn't been summarized yet.

**Backend change:** New endpoint `GET /api/community?id={communityID}` that queries `MATCH (s:Shard {id: 'comm-summary-' + $id}) RETURN s.content` — or bundle summaries into the existing `/api/graph` response.

**Frontend change:** `highlightCommunity()` in `app.js` fetches and renders the summary in a floating panel anchored to the left sidebar.

---

- [x] **F.2 Rule-Based Visual Encoding (Node Size \+ Opacity)**

**Why:** Currently node size is hardcoded to bond count in `updateViz()`. PageRank and survival score exist on every shard but have no visual encoding beyond the sidebar — you have to click a node to know if it's important or dying.

Borrowed from **Neo4j Bloom's GDS integration pattern**: bind node visual properties to algorithm scores so mesh health is readable at a glance. High PageRank \= bigger node. Low survival score \= dimmer node. This transforms the graph from a connectivity map into a health dashboard.

**What:**

- Node **radius** \= `baseRadius + (pagerank * scaleFactor)` — PageRank-scaled size, capped to prevent runaway large nodes  
- Node **opacity** \= mapped from survival score (score \< 20 → 0.3 opacity, score \> 80 → 1.0 opacity) — visually flags eviction candidates without any click  
- Core shards remain at fixed max size and full opacity (immutable anchors)  
- Add a legend entry explaining the encoding: "SIZE \= CENTRALITY | BRIGHTNESS \= SURVIVAL"

**Frontend change:** `updateViz()` in `app.js` — update radius and opacity calculations. `index.html` — add legend entry. `style.css` — add transition for opacity changes.

---

- [x] **F.3 Shard Category \+ Source Provenance in Entity Inspector**

**Why:** The sidebar shows ID, density, rank, survival, timestamp, community, and content — but omits `category`, `source_type`, and `source_ref`. These fields already exist on the `Shard` struct and are returned by the API via `packData()`. Without them, you can't tell whether a shard came from manual input, GitHub, chat, or web scrape — no provenance visibility.

Borrowed from **GraphRAG dashboard pattern**: knowledge graph tools surface data lineage inline so you can answer "why is this here?" at a glance. This is especially important for Shard-Link because `confidence` score determines retrieval trustworthiness.

**What:** Add three fields to the Entity Inspector sidebar:

- `SHARD_CATEGORY` — core, session, memory, archived  
- `SOURCE_TYPE` — manual, github, chat, web\_scrape  
- `SOURCE_REF` — URI, file path, or ID

**Backend change:** Extend `VizNode` struct in `cmd/visual_ego/main.go` to include `Category` (already there), `SourceType`, and `SourceRef`. Wire them in `packData()`.

**Frontend change:** Add three `detail-field` divs in `index.html` after SHARD\_ID. Populate in `selectNode()` in `app.js`.

---

- [x] **F.4 SSE Auto-Reconnect**

**Why:** The activity feed connects via `EventSource('/api/activity')`. On network hiccup or server restart, `onerror` fires and sets the status dot to offline — but never reconnects. The feed stays dead until the user manually reloads the page. This is a silent failure that hides all Mesh activity.

**What:** Replace the single `EventSource` with a reconnect wrapper using exponential backoff (1s → 2s → 4s → max 30s). Reset backoff on successful `onopen`. Log reconnect attempts to the activity feed itself.

**Frontend change:** `initActivityFeed()` in `app.js` — wrap EventSource creation in a reconnect function. No backend changes needed.

---

- [x] **F.5 Explicit Mode Indicator for Bond Edit**

**Why:** Bond mode is toggled via a toolbar button, but there is no persistent visual signal that the canvas is in a different operating mode. If the user forgets they're in bond mode and clicks a node expecting inspection, they accidentally initiate a bond operation instead.

Borrowed from **Neo4j Bloom's Explore/Inspect/Edit model**: explicit mode separation prevents user errors. When you're in edit mode, the UI should make that unmistakable — not rely on a small button label the user might not notice.

**What:**

- When bond mode is active, apply a subtle colored border to the entire SVG canvas (e.g. `2px solid var(--core-color)`)  
- Add a persistent mode badge in the top-right corner of the canvas: `● BOND_EDIT_MODE` in the core color  
- Badge disappears when bond mode is off

**Frontend change:** `toggleBondMode()` in `app.js` — toggle a CSS class on the SVG or a wrapper div. `style.css` — add `.bond-mode-active` class with border and badge styles. `index.html` — add badge div.

---

### Medium Value — UX Polish

- [x] **F.6 Community Isolation Toggle (Filter vs Highlight)**

**Why:** Clicking a neighborhood button currently dims other nodes to 5% opacity — they are still rendered, still create visual noise, and still interfere with reading a specific community's structure. For dense meshes this makes community inspection difficult.

Borrowed from **standard graph dashboard interactive filtering pattern**: dimming is a highlight operation. Hiding is an isolation operation. They serve different purposes and both should be available.

**What:** Two-state toggle on neighborhood buttons:

- First click → **highlight** (current behavior — dim others to 5%)  
- Second click → **isolate** (remove non-community nodes from the rendered scene entirely, bonds only between community members visible)  
- Third click (or Escape) → reset to full mesh view

**Frontend change:** `highlightCommunity()` in `app.js` — add isolation state and toggle logic. Track current community filter state. No backend changes.

---

- [x] **F.7 Search Debounce \+ Loading State**

**Why:** `semanticSearch()` fires immediately on Enter with no visual feedback. The Gemini API embedding call takes 200-500ms. During that window there's no indication anything is happening, and pressing Enter twice fires duplicate requests.

**What:**

- Add 300ms debounce on the search input's keypress handler  
- Show a brief loading state (replace SEARCH button text with "..." or add a CSS spinner)  
- Disable the search button during the fetch to prevent double-fire

**Frontend change:** `app.js` — debounce wrapper around `semanticSearch()`, toggle button state during fetch. `style.css` — optional spinner keyframe.

---

- [x] **F.8 Node Tooltip on Hover**

**Why:** Currently you must click a node to open the full Entity Inspector sidebar. When exploring a large mesh, this click-per-node workflow is slow. A lightweight hover preview would let you scan nodes without committing to a full selection.

**What:** On `mouseenter` of a node circle, show a floating tooltip with:

- Shard ID (truncated to 30 chars)  
- Category badge (color-coded)  
- First \~80 characters of content

Tooltip follows cursor position. Disappears on `mouseleave`. Does NOT open the sidebar.

**Frontend change:** `app.js` — add `mouseenter`/`mouseleave` handlers in `updateViz()` node join. `style.css` — tooltip div styling. `index.html` — add hidden tooltip container div.

---

- [x] **F.9 Bond Weight Display on Hover**

**Why:** Links render with visual thickness proportional to cosine similarity weight, but the actual numeric value (e.g., `0.82`) is never shown. When debugging why bonds exist or are missing, you have to check Neo4j directly. Surfacing the weight on hover gives instant diagnostic info.

**What:** On `mouseenter` of a link line, show a small floating label at the midpoint displaying the weight to 2 decimal places (e.g., `SIM: 0.82`). Disappears on `mouseleave`.

**Frontend change:** `app.js` — add hover handlers on link elements in `updateViz()`. Reuse the same tooltip div from F.8 or add a dedicated link-tooltip.

---

## Section 2: Activity Feed & Logging Upgrades

### Root Cause Analysis

The activity feed has a coverage gap. `GlobalLogger` is a package-level var in `internal/storage` — only code that directly calls it emits to the feed. Background workers (Janitor, Synthesizer, HygieneWorker) live in separate packages and were never wired up. They use bare `log.Printf` which goes to Docker stdout only, invisible to the dashboard.

**Currently visible in the feed:**

| Event | Source |
| :---- | :---- |
| Shard saved | `vessel_graph.go → SaveShard()` |
| Bond forged | `vessel_graph.go → SaveBond()` |
| Shard evicted from Neo4j | `vessel_graph.go → ArchiveShard()` |
| Manual bond severed | `visual_ego/main.go → handleBonds DELETE` |

**Silently lost to Docker logs only:**

| Event | Source | Gap |
| :---- | :---- | :---- |
| Janitor cycle start / end | `janitor.go → performCleanup()` | `GlobalLogger` never injected |
| Janitor eviction candidates found | `janitor.go` | Same |
| Synthesizer bond sync count | `synthesizer.go → performSynthesis()` | No `GlobalLogger` reference |
| Synthesizer community refresh triggered | `synthesizer.go` | Same |
| Community summarized | `synthesizer.go → summarizeCommunities()` | Same |
| HygieneWorker cycle start / end | `hygiene.go → performHygiene()` | No `GlobalLogger` reference |
| Postgres VACUUM complete | `hygiene.go` | Same |
| Working memory session seeded / updated | `working_memory.go` | Same |
| Manual eviction initiated (Visual Ego) | `visual_ego/main.go → handleEvict()` | Partial — logs before Neo4j delete only |
| Manual bond created (Visual Ego) | `visual_ego/main.go → handleBonds POST` | No `GlobalLogger` call on success |
| Dashboard semantic search fired | `visual_ego/main.go → handleSearch()` | No log at all |

---

### Backend — Wire GlobalLogger into Workers

- [x] **L.1 Wire GlobalLogger into Janitor** — `internal/janitor/janitor.go`, `main.go`

**Why:** The Janitor is the most operationally significant background process. Every eviction decision it makes is currently invisible to the feed. You have no way to know from the dashboard whether the Janitor is running, what it found, or what it evicted — you have to tail Docker logs.

**What:** Add a `logger storage.LogFunc` field to the `Janitor` struct. Update `NewJanitor()` to accept it. Call `GlobalLogger` at:

- Cycle start: `type: system` — "Janitor cycle started. Shard count: {n}/{max}"  
- Overage detected: `type: warn` — "Overage detected: \+{n} shards above limit"  
- Each eviction: `type: evict` — already fires via `ArchiveShard()` in Neo4j, but Janitor-initiated evictions should add "Janitor evicted: {id}" with `shard_id` for clickability  
- Cycle complete: `type: system` — "Janitor cycle complete. Evicted: {n}"  
- No action needed: `type: system` — "Janitor: mesh within limits ({n}/{max})"

**Backend change:** `janitor.go` — add `logger` field, update constructor, add log calls in `performCleanup()`. `main.go` — pass `storage.GlobalLogger` to `NewJanitor()` after the logger is initialized (step 2.5).

---

- [x] **L.2 Wire GlobalLogger into Synthesizer** — `internal/synthesizer/synthesizer.go`, `main.go`

**Why:** The Synthesizer is the autonomous "thinking" process of the mesh — it forges bonds and generates community summaries. Its activity is the most interesting thing happening in the system and it's completely dark to the feed.

**What:** Add a `logger storage.LogFunc` field to the `Synthesizer` struct. Update `NewSynthesizer()` to accept it. Call `GlobalLogger` at:

- New bonds established: `type: bond` — "Synthesizer: {n} new semantic bonds forged autonomously"  
- No new bonds: `type: system` — "Synthesizer: no new relationships in this cycle"  
- Community refresh triggered: `type: info` — "Synthesizer: refreshing {n} changed communities"  
- Each community summarized: `type: success` — "Community {id} summarized → {shardID} ({n} members)" with `shard_id` for clickability  
- Summarization errors: `type: error` — "Synthesizer ERROR: community {id} summary failed: {err}"

**Backend change:** `synthesizer.go` — add `logger` field, update constructor, add log calls in `performSynthesis()` and `summarizeCommunities()`. `main.go` — pass `storage.GlobalLogger` to `NewSynthesizer()`.

---

- [x] **L.3 Wire GlobalLogger into HygieneWorker** — `internal/hygiene/hygiene.go`, `main.go`

**Why:** HygieneWorker runs every 24 hours and touches all three storage vessels. Currently completely silent to the feed. Lower urgency than Janitor/Synthesizer but still an operational blind spot.

**What:** Add a `logger storage.LogFunc` field to `HygieneWorker`. Update `NewHygieneWorker()` to accept it. Call `GlobalLogger` at:

- Cycle start: `type: system` — "Hygiene cycle started"  
- Each vessel complete: `type: system` — "Hygiene: Postgres VACUUM complete" / "Neo4j indexes verified" / "SQLite VACUUM complete"  
- Each vessel error: `type: error` — "Hygiene ERROR: {vessel}: {err}"  
- Cycle complete: `type: system` — "Hygiene cycle complete"

**Backend change:** `hygiene.go` — add `logger` field, update constructor, add log calls in `performHygiene()`. `main.go` — pass `storage.GlobalLogger` to `NewHygieneWorker()`.

---

- [x] **L.4 Fix Missing Handler Log Calls in Visual Ego** — `cmd/visual_ego/main.go`

**Why:** Three Visual Ego API handlers have incomplete or missing `GlobalLogger` calls. The feed shows evictions that come from the Neo4j delete step but misses the manual initiation context. Bond creation and dashboard search are completely silent.

**What:**

- `handleEvict()` — add `GlobalLogger` call after the core-shard guard passes and before archival begins: `type: warn` — "Manual eviction initiated: {id}" with `shard_id`. This gives the feed a clickable entry before the operation completes, not just after.  
- `handleBonds POST` — add `GlobalLogger` call after `SaveBond()` succeeds: `type: bond` — "Manual bond created: {fromID} ↔ {toID}" with `shard_id: fromID`. Currently `SaveBond()` in `vessel_graph.go` logs this but Visual Ego uses its own bond handler that bypasses it.  
- `handleSearch()` — add `GlobalLogger` call: `type: search` — "Dashboard search: '{query}' → {n} shards found"

**Backend change:** `cmd/visual_ego/main.go` — three additive `GlobalLogger` calls. No structural changes.

---

### Frontend — Feed UX Improvements

- [x] **L.5 Log Type Expansion** — `web/static/style.css`, `web/static/app.js`

**Why:** The current feed has 5 types: `success`, `bond`, `evict`, `info`, `warn`. With L.1–L.4 wiring in background worker events, the feed will gain significant volume. Background cycle events (Janitor running, Hygiene running) are operational noise compared to bond forges and evictions. They need a visually distinct treatment so signal doesn't get buried in noise.

**What:** Add three new log types with dedicated colors:

- `system` — muted gray (`#555e6b`) — for Janitor/Synthesizer/Hygiene cycle events. Low visual weight by design.  
- `search` — purple (`#a371f7`) — for MCP and dashboard search events.  
- `error` — bright red (`#ff2d2d`) with subtle background tint — for any worker error that bubbles up. Must be visually prominent.

**Frontend change:** `style.css` — add `.log-type-system`, `.log-type-search`, `.log-type-error` classes. `app.js` — no structural changes needed, `addLogEntry()` already uses the type for the CSS class.

---

- [x] **L.6 Log Filter Toggle** — `web/static/index.html`, `web/static/app.js`, `web/static/style.css`

**Why:** Once L.1–L.4 are wired, the feed will receive `system` events every 10–15 minutes from Synthesizer cycles and every 24 hours from Hygiene. During active mesh inspection these are noise. During debugging they are signal. A filter toggle lets you switch between the two modes without clearing the log.

**What:** Add a row of small toggle buttons above the log container, one per type:

\[✓ SUCCESS\] \[✓ BOND\] \[✓ EVICT\] \[✓ WARN\] \[✓ INFO\] \[✓ SYSTEM\] \[✓ SEARCH\] \[✓ ERROR\]

- All enabled by default  
- Clicking a button toggles that type's visibility in the DOM (CSS `display: none` on matching entries, not deletion)  
- Button renders dimmed when its type is hidden  
- Toggle state is session-only — resets on page reload

**Frontend change:** `index.html` — add filter bar div above `log-container`. `app.js` — `addLogEntry()` checks active filters before rendering; filter toggle handler updates a `Set` of active types and re-applies visibility to existing entries. `style.css` — filter button styles.

---

- [x] **L.7 Bump Log Hydration Limit** — `cmd/visual_ego/main.go`, `internal/storage/vessel.go`

**Why:** `GetRecentActivity()` fetches the last 50 rows from SQLite. `addLogEntry()` caps the DOM at 50 entries. With more event sources wired in, 50 entries covers a much shorter time window — potentially just the last Synthesizer cycle. The SQLite TTL already handles long-term storage so bumping the in-memory limit has no storage cost.

**What:**

- Bump `GetRecentActivity()` limit from 50 → 200  
- Bump DOM cap in `addLogEntry()` from 50 → 200  
- Make both configurable via a single constant at the top of `app.js`: `const LOG_MAX_ENTRIES = 200`

**Frontend change:** `app.js` — replace hardcoded `50` with `LOG_MAX_ENTRIES` constant. `cmd/visual_ego/main.go` — update `handleGetLogs()` to pass 200\. `vessel.go` — `GetRecentActivity()` already takes a `limit int` param, no signature change needed.

---

## Risk Register

| Item | Risk | Notes |
| :---- | :---- | :---- |
| F.1 | Low | Read-only query; summary shards already exist in Neo4j |
| F.2 | Low | Pure frontend — radius and opacity math in `updateViz()`; no API changes |
| F.3 | Low | Additive fields in sidebar; no data model changes |
| F.4 | Low | Client-side only; standard EventSource reconnect pattern |
| F.5 | Low | CSS class toggle \+ badge div; no logic changes |
| F.6 | Low | Additional state in `highlightCommunity()`; no API changes |
| F.7 | Low | Client-side debounce; no API changes |
| F.8 | Low | DOM tooltip; no API changes |
| F.9 | Low | DOM tooltip; weight data already in link objects |
| L.1 | Low | Additive logger field; workers already call `log.Printf` at same points |
| L.2 | Low | Same pattern as L.1; Synthesizer already has granular log points |
| L.3 | Low | Same pattern; HygieneWorker is simplest of the three |
| L.4 | Low | Three additive lines in Visual Ego handlers; no structural changes |
| L.5 | Low | CSS additions only; existing `addLogEntry()` already uses type for class |
| L.6 | Medium | Filter toggle mutates existing DOM entries; test with hydrated history |
| L.7 | Trivial | Limit bump; SQLite TTL already bounds total storage |

---

## Section 3: UX Polish (External Audit Follow-Up)

Source: External UX/UI audit of the Visual Ego dashboard. Items below were selected for implementation; rejected items are listed at the end with rationale.

### Implemented

- [x] **U.1 Timestamp Normalization** — `web/static/app.js`

**Why:** SSE live entries used `HH:MM:SS` while hydrated history entries used `YYYY-MM-DD HH:MM:SS`. Inconsistent formats in the same feed create confusion.

**What:** Added `normalizeTimestamp()` in `addLogEntry()` that extracts `HH:MM:SS` from any input format. All feed entries now display consistently.

---

- [x] **U.2 Log Entry Left-Border Color** — `web/static/style.css`

**Why:** Dense log streams are hard to scan when color only appears on the text. A left-border accent per type allows peripheral vision scanning without reading each line.

**What:** Added `border-left: 2px solid` per `data-logtype` attribute on `.log-entry`. Colors match existing type palette (green=success, red=evict/error, cyan=bond, purple=search, etc.).

---

- [x] **U.3 Filter Toggle Visual Clarity** — `web/static/style.css`

**Why:** The L.6 filter buttons looked decorative — active/inactive states were too subtle (just opacity difference). Users couldn't tell which types were filtered.

**What:** Active filters now have a visible background fill (`rgba(255,255,255,0.08)`). Inactive filters are dimmed to 25% opacity with `text-decoration: line-through`. No ambiguity about state.

---

- [x] **U.4 Neighborhood Shard Count Badges** — `web/static/app.js`

**Why:** Neighborhood buttons (`N_0`, `N_24`) provided no context about community size. You had to click and count visually.

**What:** `updateNeighborhoods()` now counts members per community and renders badges as `N_0 (12)` instead of `N_0`.

---

- [x] **U.5 Zoom Level Indicator** — `web/static/index.html`, `web/static/app.js`

**Why:** No visual feedback on current zoom level. The `+`/`-` buttons worked but users had no sense of scale or how far they'd zoomed.

**What:** Added a `100%` label between zoom buttons that updates in real-time via the D3 zoom handler.

---

### Rejected (with rationale)

| Suggestion | Reason for rejection |
|:-----------|:---------------------|
| Rename jargon labels (SYSTEM_LOCATE, NEURAL_DENSITY, etc.) | Intentional aesthetic. Personal exocortex tool, not a public SaaS product. Glossary exists for reference. |
| Collapsible panels / layout hierarchy rework | Over-engineering. Current layout works for single-user cockpit. |
| Move zoom to bottom-right "convention" | Conventions serve public apps. Personal tool — current placement is fine. |
| Full shard ID on hover / "View details" CTA on tooltip | IDs are 40+ chars, would bloat tooltip. Click-to-inspect already works. |
| WCAG contrast fixes / keyboard focus rings | Personal tool with intentional low-contrast sci-fi aesthetic. |
| First-run hint overlay | Single user — no onboarding needed. |
| Auto-pause feed scroll on hover | Low value — feed is small (180px) and entries are short. |

---

*Status: IMPLEMENTED | Date: 2026-05-28* *Authors: BB & Brainy Bestie* *Reference: Neo4j Bloom UX patterns, GraphRAG dashboard design, graph visualization best practices 2025*
