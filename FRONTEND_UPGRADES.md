# Shard-Link: Visual Ego Dashboard — Frontend Upgrades

**Status:** PROPOSED — Candidates for next sprint
**Last Updated:** 2026-05-28
**Files:** `web/static/index.html`, `web/static/app.js`, `web/static/style.css`, `cmd/visual_ego/main.go`

---

## How to read this doc

Same structure as HARDENING.md. Each item is a checkbox. Items are ordered by value — work top-to-bottom. The "Why" is written before every item so the reason is always on record.

---

## High Value — Directly Useful

- [ ] **F.1 Community Summary Display**

**Why:** Phase 6.1 shipped GraphRAG community summaries — LLM-generated paragraph descriptions stored as `comm-summary-{communityID}` shards. The dashboard's ACTIVE_NEIGHBORHOODS buttons (N_0, N_1...) highlight clusters visually but provide zero context about what the cluster actually contains. The summary data already exists in Neo4j; it just needs to be surfaced.

**What:** When clicking a neighborhood button, fetch and display the corresponding `comm-summary-*` shard's content as a panel or expandable tooltip below the button. Falls back to "No summary available" if the community hasn't been summarized yet.

**Backend change:** New endpoint `GET /api/community?id={communityID}` that queries `MATCH (s:Shard {id: 'comm-summary-' + $id}) RETURN s.content` — or bundle summaries into the existing `/api/graph` response.

**Frontend change:** `highlightCommunity()` in `app.js` fetches and renders the summary in a floating panel anchored to the left sidebar.

---

- [ ] **F.2 Shard Category + Source Provenance in Entity Inspector**

**Why:** The sidebar shows ID, density, rank, survival, timestamp, community, and content — but omits `category`, `source_type`, and `source_ref`. These fields already exist on the `Shard` struct and are returned by the API via `packData()`. Without them, you can't tell whether a shard came from manual input, GitHub, chat, or web scrape — no provenance visibility.

**What:** Add three fields to the Entity Inspector sidebar:
- `SHARD_CATEGORY` — core, session, memory, archived
- `SOURCE_TYPE` — manual, github, chat, web_scrape
- `SOURCE_REF` — URI, file path, or ID

**Backend change:** Extend `VizNode` struct in `cmd/visual_ego/main.go` to include `Category` (already there), `SourceType`, and `SourceRef`. Wire them in `packData()`.

**Frontend change:** Add three `detail-field` divs in `index.html` after SHARD_ID. Populate in `selectNode()` in `app.js`.

---

- [ ] **F.3 SSE Auto-Reconnect**

**Why:** The activity feed connects via `EventSource('/api/activity')`. On network hiccup or server restart, `onerror` fires and sets the status dot to offline — but never reconnects. The feed stays dead until the user manually reloads the page. This is a silent failure that hides all Mesh activity.

**What:** Replace the single `EventSource` with a reconnect wrapper using exponential backoff (1s → 2s → 4s → max 30s). Reset backoff on successful `onopen`. Log reconnect attempts to the activity feed itself.

**Frontend change:** `initActivityFeed()` in `app.js` — wrap EventSource creation in a reconnect function. No backend changes needed.

---

## Medium Value — UX Polish

- [ ] **F.4 Search Debounce + Loading State**

**Why:** `semanticSearch()` fires immediately on Enter with no visual feedback. The Gemini API embedding call takes 200-500ms. During that window there's no indication anything is happening, and pressing Enter twice fires duplicate requests.

**What:**
- Add 300ms debounce on the search input's keypress handler
- Show a brief loading state (e.g., replace SEARCH button text with "..." or add a CSS spinner)
- Disable the search button during the fetch to prevent double-fire

**Frontend change:** `app.js` — debounce wrapper around `semanticSearch()`, toggle button state during fetch. `style.css` — optional spinner keyframe.

---

- [ ] **F.5 Node Tooltip on Hover**

**Why:** Currently you must click a node to open the full Entity Inspector sidebar. When exploring a large mesh, this click-per-node workflow is slow. A lightweight hover preview would let you scan nodes without committing to a full selection.

**What:** On `mouseenter` of a node circle, show a floating tooltip with:
- Shard ID (truncated to 30 chars)
- Category badge (color-coded)
- First ~80 characters of content

Tooltip follows cursor position. Disappears on `mouseleave`. Does NOT open the sidebar.

**Frontend change:** `app.js` — add `mouseenter`/`mouseleave` handlers in `updateViz()` node join. `style.css` — tooltip div styling. `index.html` — add hidden tooltip container div.

---

- [ ] **F.6 Bond Weight Display on Hover**

**Why:** Links render with visual thickness proportional to cosine similarity weight, but the actual numeric value (e.g., `0.82`) is never shown. When debugging why bonds exist or are missing, you have to check Neo4j directly. Surfacing the weight on hover gives instant diagnostic info.

**What:** On `mouseenter` of a link line, show a small floating label at the midpoint displaying the weight to 2 decimal places (e.g., `SIM: 0.82`). Disappears on `mouseleave`.

**Frontend change:** `app.js` — add hover handlers on link elements in `updateViz()`. Reuse the same tooltip div from F.5 or add a dedicated link-tooltip.

---

## Lower Priority — Nice to Have

- [ ] **F.7 Keyboard Shortcuts**

**Why:** Power users (us) interact with the dashboard frequently. Mouse-only navigation adds friction for common actions.

**What:**
- `Escape` — close sidebar / exit focus mode
- `/` — focus search input
- `r` — reset view (recenter + zoom identity)
- `b` — toggle bond mode

**Frontend change:** `app.js` — single `document.addEventListener('keydown', ...)` handler. No backend changes.

---

- [ ] **F.8 Export Graph Snapshot (PNG)**

**Why:** Useful for documentation, presentations, and sharing mesh state snapshots. Currently the only way to capture the graph is a manual screenshot.

**What:** Add an "EXPORT" button to the bottom toolbar. On click, serialize the SVG to a canvas, then trigger a PNG download. Exclude UI overlays (sidebar, controls, activity feed) from the capture — graph only.

**Frontend change:** `app.js` — new `exportGraph()` function using `XMLSerializer` + `canvas.toDataURL()`. `index.html` — add button to toolbar.

---

## Risk Register

| Item | Risk | Notes |
| :--- | :--- | :--- |
| F.1 | Low | Read-only query; summary shards already exist in Neo4j |
| F.2 | Low | Additive fields in sidebar; no data model changes |
| F.3 | Low | Client-side only; standard EventSource reconnect pattern |
| F.4 | Low | Client-side debounce; no API changes |
| F.5 | Low | DOM tooltip; no API changes |
| F.6 | Low | DOM tooltip; weight data already in link objects |
| F.7 | Low | Keyboard listener; must not conflict with search input focus |
| F.8 | Medium | SVG-to-canvas conversion can be tricky with external fonts and filters |

---

*Status: PROPOSED | Date: 2026-05-28* *Authors: BB & Brainy Bestie*
