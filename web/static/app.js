let data = { nodes: [], links: [] };
let fullData = null; // F.6: Unfiltered snapshot for community isolation reset
let simulation, svg, container, hudLayer, labelLayer, node, link;
let bondMode = false;
let selectedNode = null;
let focusMode = false;
let focusedNodeID = null;
let communityFilterState = null; // F.6: null | { id, phase: 'highlight'|'isolate' }
let searchActive = false; // True when viewing search results — blocks auto-refresh
let searchDebounceTimer = null; // F.7
const LOG_MAX_ENTRIES = 200; // L.7
const activeLogFilters = new Set(['success', 'bond', 'evict', 'info', 'warn', 'system', 'search', 'error']); // L.6
let width = window.innerWidth;
let height = window.innerHeight;
const color = d3.scaleOrdinal(["#5ef3ff", "#00d4ff", "#70a1ff", "#a371f7", "#58a6ff"]);
const degree = {};

const zoom = d3.zoom().scaleExtent([0.1, 4]).on("zoom", (event) => {
    container.attr("transform", event.transform);
    const pct = Math.round(event.transform.k * 100);
    document.getElementById('zoom-level').textContent = pct + '%';
});

function initViz() {
    svg = d3.select("#viz")
        .attr("width", width)
        .attr("height", height)
        .call(zoom)
        .on("click", (event) => {
            if (event.target.tagName === 'svg') {
                closeSidebar();
            }
        });

    container = svg.append("g");
    container.append("g").attr("class", "links-layer");
    container.append("g").attr("class", "nodes-layer");
    labelLayer = container.append("g").attr("class", "labels-layer");
    hudLayer = container.append("g").attr("class", "hud-layer");

    simulation = d3.forceSimulation()
        .force("link", d3.forceLink().id(d => d.id).distance(180).strength(0.3))
        .force("charge", d3.forceManyBody().strength(d => d.category === 'archived' ? 0 : -1600))
        .force("center", d3.forceCenter(width / 2, height / 2))
        .force("collision", d3.forceCollide().radius(d => {
            if (d.category === 'archived') return 10;
            return (degree[d.id] || 0) * 2 + 45;
        }))
        .force("cluster", forceCluster)
        .force("archival", forceArchival)
        .force("x", d3.forceX(width / 2).strength(d => {
            if (d.category === 'core') return 0.6;
            if (d.category === 'archived') return 0;
            return 0.03;
        }))
        .force("y", d3.forceY(height / 2).strength(d => {
            if (d.category === 'core') return 0.6;
            if (d.category === 'archived') return 0;
            return 0.03;
        }));

    simulation.on("tick", () => {
        if (!link || !node) return;
        link.attr("x1", d => d.source.x).attr("y1", d => d.source.y)
            .attr("x2", d => d.target.x).attr("y2", d => d.target.y);
        node.attr("cx", d => d.x).attr("cy", d => d.y);

        const activeIdNode = document.getElementById('det-id');
        if (activeIdNode && activeIdNode.innerText) {
            const d = data.nodes.find(n => n.id === activeIdNode.innerText);
            if (d) drawHUD(d);
        }
    });
}

function forceCluster(alpha) {
    const centroids = {};
    data.nodes.forEach(n => {
        if (!centroids[n.community]) centroids[n.community] = { x: 0, y: 0, count: 0 };
        centroids[n.community].x += n.x;
        centroids[n.community].y += n.y;
        centroids[n.community].count++;
    });
    for (let c in centroids) {
        centroids[c].x /= centroids[c].count;
        centroids[c].y /= centroids[c].count;
    }
    data.nodes.forEach(n => {
        const c = centroids[n.community];
        if (c) {
            n.vx += (c.x - n.x) * alpha * 0.15;
            n.vy += (c.y - n.y) * alpha * 0.15;
        }
    });
}

function forceArchival(alpha) {
    let maxActiveDist = 400;
    data.nodes.forEach(n => {
        if (n.category !== 'archived') {
            const dx = n.x - width / 2;
            const dy = n.y - height / 2;
            const d = Math.sqrt(dx * dx + dy * dy);
            if (d > maxActiveDist) maxActiveDist = d;
        }
    });

    const orbitalRadius = maxActiveDist + 300;

    data.nodes.forEach(n => {
        if (n.category === 'archived') {
            const dx = n.x - width / 2;
            const dy = n.y - height / 2;
            const dist = Math.sqrt(dx * dx + dy * dy) || 1;
            const strength = 0.8;

            n.vx += (dx / dist) * (orbitalRadius - dist) * alpha * strength;
            n.vy += (dy / dist) * (orbitalRadius - dist) * alpha * strength;
        }
    });
}

// F.2: Calculate node radius based on PageRank centrality
function nodeRadius(d) {
    if (d.category === 'core') return 12;
    if (d.category === 'archived') return 4;
    const baseRadius = 5;
    const scaleFactor = 15;
    return Math.min(baseRadius + (d.pagerank || 0) * scaleFactor, 20);
}

// F.2: Encode survival as glow intensity instead of dimming fill color.
// All nodes stay full color (visible), but high-survival nodes radiate
// a bright halo while low-survival nodes sit flat with no glow.
function survivalGlow(d) {
    if (d.category === 'core') return `drop-shadow(0 0 10px var(--core-color))`;
    if (d.category === 'archived') return `drop-shadow(0 0 6px rgba(255,255,255,0.5))`;
    const s = d.survival || 0;
    const baseColor = color(d.community);
    // Map survival 0-100 to glow radius 0-12px
    const radius = Math.round((s / 100) * 12);
    if (radius <= 1) return 'none';
    return `drop-shadow(0 0 ${radius}px ${baseColor})`;
}

function nodeFill(d) {
    if (d.category === 'core') return "var(--core-color)";
    if (d.category === 'archived') return "#fff";
    return color(d.community);
}

async function loadGraph() {
    searchActive = false;
    try {
        const response = await fetch('/api/graph');
        const newData = await response.json();

        // --- Position Preservation Layer ---
        const nodeMap = new Map(data.nodes.map(n => [n.id, n]));
        newData.nodes.forEach(newNode => {
            const oldNode = nodeMap.get(newNode.id);
            if (oldNode) {
                newNode.x = oldNode.x;
                newNode.y = oldNode.y;
                newNode.vx = oldNode.vx;
                newNode.vy = oldNode.vy;
                newNode.fx = oldNode.fx;
                newNode.fy = oldNode.fy;
            }
        });

        const structureChanged = newData.nodes.length !== data.nodes.length || newData.links.length !== data.links.length;
        const categoriesChanged = JSON.stringify(newData.nodes.map(n => n.category)) !== JSON.stringify(data.nodes.map(n => n.category));
        const metricsChanged = JSON.stringify(newData.nodes.map(n => n.survival?.toFixed(1))) !== JSON.stringify(data.nodes.map(n => n.survival?.toFixed(1)));

        // F.6: Update fullData snapshot whenever we get fresh data
        fullData = { nodes: [...newData.nodes], links: [...newData.links], threshold: newData.threshold };

        if (structureChanged || categoriesChanged) {
            data = newData;
            updateViz(true);
        } else if (metricsChanged) {
            data = newData;
            updateViz(false);
        }

        const currentID = document.getElementById('det-id').innerText;
        if (currentID && currentID !== "UNKNOWN") {
            const updatedNode = data.nodes.find(n => n.id === currentID);
            if (updatedNode) {
                document.getElementById('det-survival').innerText = (updatedNode.survival || 0).toFixed(2);
                document.getElementById('det-rank').innerText = (updatedNode.pagerank || 0).toFixed(4);
            }
        }
    } catch (err) { console.error(err); }
}

function updateViz(restartSimulation = true) {
    buildAdjacencyMap();

    for (let key in degree) delete degree[key];
    data.links.forEach(l => {
        const sID = l.source.id || l.source;
        const tID = l.target.id || l.target;
        degree[sID] = (degree[sID] || 0) + 1;
        degree[tID] = (degree[tID] || 0) + 1;
    });

    link = container.select("g.links-layer").selectAll("line")
        .data(data.links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)
        .join("line")
        .attr("class", "link active")
        .style("stroke-width", d => Math.pow(d.weight, 2) * 2 + 1 + "px")
        .style("stroke-opacity", d => Math.max(0.1, d.weight * 0.35));

    // F.9: Tooltip handlers on links
    link.on("mouseenter", (event, d) => {
            const tooltip = document.getElementById('node-tooltip');
            const sID = d.source.id || d.source;
            const tID = d.target.id || d.target;
            tooltip.querySelector('.tooltip-id').textContent = `${truncateID(sID)} <-> ${truncateID(tID)}`;
            tooltip.querySelector('.tooltip-category').textContent = '';
            tooltip.querySelector('.tooltip-content').textContent = `SIM: ${d.weight.toFixed(2)}`;
            tooltip.style.display = 'block';
        })
        .on("mouseleave", () => { document.getElementById('node-tooltip').style.display = 'none'; })
        .on("mousemove", (event) => {
            const tooltip = document.getElementById('node-tooltip');
            tooltip.style.left = (event.clientX + 14) + 'px';
            tooltip.style.top = (event.clientY - 10) + 'px';
        });

    node = container.select("g.nodes-layer").selectAll("circle")
        .data(data.nodes, d => d.id)
        .join(
            enter => enter.append("circle")
                .attr("class", "node")
                .attr("r", nodeRadius)
                .attr("fill", nodeFill)
                .style("filter", survivalGlow) // F.2: Glow intensity = survival
                .on("click", (event, d) => {
                    event.stopPropagation();
                    selectNode(d);
                })
                .call(d3.drag().on("start", dragstarted).on("drag", dragged).on("end", dragended)),
            update => update
                .attr("class", "node")
                .attr("r", nodeRadius)
                .attr("fill", nodeFill)
                .style("filter", survivalGlow)
        );

    // F.8: Tooltip handlers on nodes
    node.on("mouseenter", (event, d) => {
            const tooltip = document.getElementById('node-tooltip');
            tooltip.querySelector('.tooltip-id').textContent = truncateID(d.id);
            const catEl = tooltip.querySelector('.tooltip-category');
            catEl.textContent = (d.category || 'unknown').toUpperCase();
            catEl.className = 'tooltip-category ' + (d.category || '');
            tooltip.querySelector('.tooltip-content').textContent = (d.content || '').substring(0, 120) + (d.content && d.content.length > 120 ? '...' : '');
            tooltip.style.display = 'block';
        })
        .on("mouseleave", () => { document.getElementById('node-tooltip').style.display = 'none'; })
        .on("mousemove", (event) => {
            const tooltip = document.getElementById('node-tooltip');
            tooltip.style.left = (event.clientX + 14) + 'px';
            tooltip.style.top = (event.clientY - 10) + 'px';
        });

    simulation.nodes(data.nodes);
    simulation.force("link").links(data.links);

    if (restartSimulation) {
        simulation.force("charge", d3.forceManyBody().strength(d => d.category === 'archived' ? 0 : -1600));
        simulation.force("center", d3.forceCenter(width / 2, height / 2));
        simulation.force("x", d3.forceX(width / 2).strength(d => {
            if (d.category === 'core') return 0.6;
            if (d.category === 'archived') return 0;
            return 0.03;
        }));
        simulation.force("y", d3.forceY(height / 2).strength(d => {
            if (d.category === 'core') return 0.6;
            if (d.category === 'archived') return 0;
            return 0.03;
        }));

        simulation.alpha(0.5).restart();
    } else {
        simulation.alpha(0.01).restart();
    }

    // Re-apply focus if a node is selected
    if (focusedNodeID) {
        focusNode(focusedNodeID);
    }

    if (data.threshold) {
        document.getElementById('stat-t').innerText = data.threshold.toFixed(2);
    }
    document.getElementById('stat-n').innerText = data.nodes.length;
    document.getElementById('stat-e').innerText = data.links.length;
    updateNeighborhoods();
}

// F.8/F.9: Truncate long shard IDs for tooltip display
function truncateID(id) {
    if (!id) return '';
    return id.length > 24 ? id.substring(0, 21) + '...' : id;
}

function selectNode(d) {
    if (bondMode) {
        if (!selectedNode) {
            selectedNode = d;
            node.style("stroke", n => n.id === d.id ? "var(--core-color)" : "none");
            node.style("stroke-width", n => n.id === d.id ? "4px" : "0");
        } else if (selectedNode.id !== d.id) {
            createBond(selectedNode.id, d.id);
            toggleBondMode();
        }
        return;
    }

    focusNode(d.id);

    const sidebar = document.getElementById('details');
    sidebar.classList.add('active');
    document.getElementById('det-id').innerText = d.id || "UNKNOWN";
    document.getElementById('det-category').innerText = (d.category || "--").toUpperCase(); // F.3
    document.getElementById('det-source-type').innerText = d.source_type || "--"; // F.3
    document.getElementById('det-source-ref').innerText = d.source_ref || "--"; // F.3
    document.getElementById('det-density').innerText = (degree[d.id] || 0) + " BONDS";
    document.getElementById('det-rank').innerText = (d.pagerank || 0).toFixed(4);
    document.getElementById('det-survival').innerText = (d.survival || 0).toFixed(2);
    document.getElementById('det-time').innerText = d.created_at || "--";
    document.getElementById('det-comm').innerText = d.community !== undefined ? "N_" + d.community : "N_NONE";
    document.getElementById('det-content').innerText = d.content || "NO_CONTENT";

    const evictBtn = document.getElementById('evict-container');
    if (evictBtn) {
        if (d.category === 'core' || d.category === 'archived') {
            evictBtn.style.display = 'none';
        } else {
            evictBtn.style.display = 'block';
        }
    }

    drawHUD(d);
}

window.evictShard = async function() {
    const id = document.getElementById('det-id').innerText;
    if (!id) return;
    if (!confirm(`Permanently evict shard ${id} from the Knowledge Mesh?`)) return;

    try {
        const resp = await fetch(`/api/evict?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
        if (resp.ok) {
            closeSidebar();
            setTimeout(loadGraph, 500);
        } else {
            const err = await resp.text();
            alert(`Eviction failed: ${err}`);
        }
    } catch (err) { console.error(err); }
}

const adjMap = new Map();

function buildAdjacencyMap() {
    adjMap.clear();
    data.links.forEach(l => {
        const sID = l.source.id || l.source;
        const tID = l.target.id || l.target;
        if (!adjMap.has(sID)) adjMap.set(sID, new Set());
        if (!adjMap.has(tID)) adjMap.set(tID, new Set());
        adjMap.get(sID).add(tID);
        adjMap.get(tID).add(sID);
    });
}

function focusNode(id) {
    if (!id || id === "UNKNOWN") {
        resetFocus();
        return;
    }
    focusedNodeID = id;
    focusMode = true;

    node.style("opacity", n => {
        if (n.id === id || isNeighbor(id, n.id)) return 1;
        return 0.05;
    });
    link.style("stroke-opacity", l => {
        const sID = l.source.id || l.source;
        const tID = l.target.id || l.target;
        return (sID === id || tID === id) ? 0.8 : 0.01;
    });
    node.style("stroke", n => n.id === id ? "var(--accent)" : "none");
    node.style("stroke-width", n => n.id === id ? "3px" : "0");
}

function resetFocus() {
    focusedNodeID = null;
    focusMode = false;
    if (node) node.style("opacity", 1).attr("fill", nodeFill).style("filter", survivalGlow);
    if (node) node.style("stroke", "none");
    if (link) link.style("stroke-opacity", 0.1);
    if (labelLayer) labelLayer.selectAll("*").remove();
}

function isNeighbor(idA, idB) {
    return adjMap.get(idA)?.has(idB) || false;
}

function updateNeighborhoods() {
    const list = document.getElementById('neighborhoods-list');
    list.innerHTML = "";
    // Count members per community
    const commCounts = {};
    data.nodes.forEach(n => {
        if (n.community === undefined) return;
        commCounts[n.community] = (commCounts[n.community] || 0) + 1;
    });
    const communities = Object.keys(commCounts).sort((a, b) => a - b);
    communities.forEach(c => {
        const btn = document.createElement("div");
        btn.className = "hud-btn";
        btn.style.padding = "4px 8px";
        btn.style.fontSize = "9px";
        btn.innerText = `N_${c} (${commCounts[c]})`;
        btn.onclick = (event) => {
            event.stopPropagation();
            toggleCommunityFilter(Number(c)); // F.6: 3-state toggle
        };
        list.appendChild(btn);
    });
}

// F.6: 3-state community toggle: highlight -> isolate -> reset
function toggleCommunityFilter(communityID) {
    if (!communityFilterState || communityFilterState.id !== communityID) {
        // First click (or different community): highlight
        communityFilterState = { id: communityID, phase: 'highlight' };
        highlightCommunity(communityID);
        fetchCommunitySummary(communityID); // F.1
    } else if (communityFilterState.phase === 'highlight') {
        // Second click: isolate
        communityFilterState.phase = 'isolate';
        isolateCommunity(communityID);
    } else {
        // Third click: reset
        resetCommunityFilter();
    }
}

function highlightCommunity(communityID) {
    focusMode = true;
    node.style("opacity", d => d.community === communityID ? 1 : 0.05);
    link.style("stroke-opacity", l => (l.source.community === communityID && l.target.community === communityID) ? 0.8 : 0.01);
}

// F.6: Filter data to only community members, re-render
function isolateCommunity(communityID) {
    if (!fullData) return;
    const communityNodes = fullData.nodes.filter(n => n.community === communityID);
    const nodeIDs = new Set(communityNodes.map(n => n.id));
    const communityLinks = fullData.links.filter(l => {
        const sID = l.source.id || l.source;
        const tID = l.target.id || l.target;
        return nodeIDs.has(sID) && nodeIDs.has(tID);
    });

    data = { nodes: communityNodes, links: communityLinks, threshold: fullData.threshold };
    updateViz(true);
}

// F.6: Restore full data
function resetCommunityFilter() {
    communityFilterState = null;
    hideCommunityPanel();
    if (fullData) {
        data = { nodes: [...fullData.nodes], links: [...fullData.links], threshold: fullData.threshold };
        updateViz(true);
    }
    resetFocus();
}

// F.1: Fetch and display community summary
async function fetchCommunitySummary(communityID) {
    const panel = document.getElementById('community-summary');
    const textEl = document.getElementById('comm-summary-text');
    try {
        const resp = await fetch(`/api/community?id=${communityID}`);
        if (resp.ok) {
            const result = await resp.json();
            if (result.summary) {
                textEl.textContent = result.summary;
                panel.style.display = 'block';
            } else {
                textEl.textContent = 'No summary available for this community.';
                panel.style.display = 'block';
            }
        }
    } catch (err) {
        console.error("Community summary fetch failed:", err);
    }
}

function hideCommunityPanel() {
    document.getElementById('community-summary').style.display = 'none';
}

// F.5: Enhanced bond mode toggle with visual indicator
function toggleBondMode() {
    bondMode = !bondMode;
    selectedNode = null;
    const btn = document.getElementById('bond-mode-btn');
    const badge = document.getElementById('bond-mode-badge');
    btn.innerText = bondMode ? "BOND_MODE: ON" : "BOND_MODE: OFF";
    btn.style.borderColor = bondMode ? "var(--core-color)" : "var(--panel-border)";
    node.style("stroke", "none");

    // F.5: Orange border on SVG + badge
    if (bondMode) {
        svg.classed('bond-mode-active', true);
        badge.style.display = 'block';
    } else {
        svg.classed('bond-mode-active', false);
        badge.style.display = 'none';
    }
}

async function createBond(fromID, toID) {
    const fromShard = data.nodes.find(n => n.id === fromID);
    const targetShard = data.nodes.find(n => n.id === toID);

    if (fromShard?.category === 'archived' || targetShard?.category === 'archived') {
        alert("ACTION_DENIED: Cannot forge bonds with archived memory (White Dwarfs).");
        return;
    }

    try {
        const resp = await fetch('/api/bonds', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ from_id: fromID, to_id: toID, weight: 1.0 })
        });
        if (resp.ok) setTimeout(loadGraph, 500);
    } catch (err) { console.error(err); }
}

async function deleteBond(fromID, toID) {
    if (!confirm(`Break bond?`)) return;
    try {
        const resp = await fetch(`/api/bonds?from=${encodeURIComponent(fromID)}&to=${encodeURIComponent(toID)}`, { method: 'DELETE' });
        if (resp.ok) setTimeout(loadGraph, 500);
    } catch (err) { console.error(err); }
}

function drawHUD(d) {
    hudLayer.selectAll("*").remove();
    if (!d || isNaN(d.x) || isNaN(d.y)) return;
    const r = nodeRadius(d);
    const boxSize = r * 3.5;
    hudLayer.append("rect")
        .attr("class", "selection-box")
        .attr("x", d.x - boxSize/2)
        .attr("y", d.y - boxSize/2)
        .attr("width", boxSize)
        .attr("height", boxSize);
}

function closeSidebar() {
    document.getElementById('details').classList.remove('active');
    hudLayer.selectAll("*").remove();
    document.getElementById('det-id').innerText = "";
    resetFocus();
}

function resetView() {
    resetFocus();
    svg.transition().duration(750).call(zoom.transform, d3.zoomIdentity);
}
function zoomIn() { svg.transition().duration(300).call(zoom.scaleBy, 1.3); }
function zoomOut() { svg.transition().duration(300).call(zoom.scaleBy, 0.7); }

function toggleGlossary() {
    const glossary = document.getElementById('mesh-glossary');
    glossary.classList.toggle('active');
    const btn = document.getElementById('glossary-btn');
    btn.innerText = glossary.classList.contains('active') ? 'HIDE_GLOSSARY' : 'SHOW_GLOSSARY';
}

// F.7: Debounced search (300ms) with loading state
function debouncedSearch() {
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
    const searchBtn = document.getElementById('search-btn');
    searchBtn.classList.add('loading');
    searchDebounceTimer = setTimeout(() => {
        semanticSearch().finally(() => {
            searchBtn.classList.remove('loading');
        });
    }, 300);
}

async function semanticSearch() {
    const term = document.getElementById('search').value;
    if (!term) return;
    try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(term)}`);
        if (!response.ok) {
            const errText = await response.text();
            console.error(`Search failed (${response.status}): ${errText}`);
            addLogEntry({ timestamp: new Date().toLocaleTimeString('en-GB'), type: 'error', message: `Search failed: ${errText}` });
            return;
        }
        data = await response.json();
        fullData = { nodes: [...data.nodes], links: [...data.links], threshold: data.threshold };
        communityFilterState = null;
        searchActive = true;
        updateViz();
        resetFocus();
    } catch (err) { console.error(err); }
}

function dragstarted(event) { if (!event.active) simulation.alphaTarget(0.3).restart(); event.subject.fx = event.subject.x; event.subject.fy = event.subject.y; }
function dragged(event) { event.subject.fx = event.x; event.subject.fy = event.y; }
function dragended(event) { if (!event.active) simulation.alphaTarget(0); event.subject.fx = null; event.subject.fy = null; }

// --- Activity Feed & Logging ---

// F.4: SSE with exponential backoff reconnect
function initActivityFeed() {
    const statusDot = document.getElementById('status-dot');
    let retryDelay = 1000;
    const maxRetryDelay = 30000;

    function connect() {
        const evtSource = new EventSource('/api/activity');

        evtSource.onmessage = (event) => {
            const entry = JSON.parse(event.data);
            addLogEntry(entry);
        };

        evtSource.onopen = () => {
            statusDot.classList.remove('offline');
            retryDelay = 1000; // Reset backoff on successful connection
        };

        evtSource.onerror = () => {
            statusDot.classList.add('offline');
            evtSource.close();
            addLogEntry({ timestamp: new Date().toLocaleTimeString('en-GB'), type: 'system', message: `SSE disconnected. Reconnecting in ${retryDelay / 1000}s...` });
            setTimeout(() => {
                connect();
                retryDelay = Math.min(retryDelay * 2, maxRetryDelay);
            }, retryDelay);
        };
    }

    connect();
    loadPersistentLogs();
    startLogPolling();
    initLogFilters(); // L.6
}

// Track the newest log timestamp so polling can detect new entries.
// The Hub and Visual Ego are separate processes — Hub writes to SQLite
// but doesn't push to SSE, so we poll /api/logs to catch Hub events.
let lastLogTimestamp = '';

async function loadPersistentLogs() {
    try {
        const resp = await fetch('/api/logs');
        if (resp.ok) {
            const history = await resp.json();
            if (history.length > 0) {
                lastLogTimestamp = history[0].timestamp || '';
            }
            history.reverse().forEach(entry => addLogEntry(entry));
        }
    } catch (err) { console.error("Log hydration failed:", err); }
}

// Poll /api/logs every 5 seconds for new entries written by the Hub process.
// Compares against lastLogTimestamp to only add entries we haven't seen yet.
function startLogPolling() {
    setInterval(async () => {
        try {
            const resp = await fetch('/api/logs');
            if (!resp.ok) return;
            const logs = await resp.json();
            if (logs.length === 0) return;

            // logs[0] is the newest (ORDER BY timestamp DESC)
            const newestTs = logs[0].timestamp || '';
            if (newestTs === lastLogTimestamp) return; // No new entries

            // Collect only entries newer than what we've already shown
            const newEntries = [];
            for (const entry of logs) {
                if ((entry.timestamp || '') <= lastLogTimestamp) break;
                newEntries.push(entry);
            }

            // Add in chronological order (oldest first → prepend pushes newest to top)
            newEntries.reverse().forEach(entry => addLogEntry(entry));
            lastLogTimestamp = newestTs;
        } catch (err) { /* silent — SSE handles connectivity status */ }
    }, 5000);
}

// Normalize timestamps to HH:MM:SS in the browser's local timezone.
// SQLite stores CURRENT_TIMESTAMP in UTC — we parse it into a Date object
// which auto-converts to local time, then format back to HH:MM:SS.
function normalizeTimestamp(ts) {
    if (!ts) return new Date().toLocaleTimeString('en-GB');
    // YYYY-MM-DD HH:MM:SS (UTC from SQLite) — parse and convert to local
    if (/^\d{4}-\d{2}-\d{2}/.test(ts)) {
        const d = new Date(ts + 'Z'); // Append Z to signal UTC
        if (!isNaN(d)) return d.toLocaleTimeString('en-GB');
    }
    // Already HH:MM:SS (from SSE, already local server time) — pass through
    if (/^\d{2}:\d{2}:\d{2}$/.test(ts)) return ts;
    return ts;
}

function addLogEntry(entry) {
    const container = document.getElementById('log-container');
    if (!container) return;
    const ts = normalizeTimestamp(entry.timestamp);
    const div = document.createElement('div');
    div.className = 'log-entry';
    div.dataset.logtype = entry.type || 'info';
    div.innerHTML = `<span class="log-time">[${ts}]</span> <span class="log-type-${entry.type}">${entry.message}</span>`;

    if (entry.shard_id) {
        div.onclick = () => focusOnShard(entry.shard_id);
    }

    // L.6: Respect active filters
    if (!activeLogFilters.has(entry.type || 'info')) {
        div.style.display = 'none';
    }

    container.prepend(div);
    while (container.children.length > LOG_MAX_ENTRIES) container.removeChild(container.lastChild); // L.7
}

function focusOnShard(id) {
    const d = data.nodes.find(n => n.id === id);
    if (d) {
        selectNode(d);
        const scale = 1.5;
        // Compute visible center between left controls and right sidebar
        const controls = document.getElementById('controls');
        const sidebar = document.getElementById('details');
        const leftEdge = controls ? controls.offsetLeft + controls.offsetWidth : 0;
        const rightEdge = sidebar.classList.contains('active') ? width - sidebar.offsetWidth : width;
        const cx = (leftEdge + rightEdge) / 2;
        const cy = height / 2;
        const transform = d3.zoomIdentity.translate(cx - d.x * scale, cy - d.y * scale).scale(scale);
        d3.select('#viz').transition().duration(750).call(zoom.transform, transform);
    } else {
        document.getElementById('search').value = id;
        semanticSearch();
    }
}

function clearLogs() { document.getElementById('log-container').innerHTML = ''; }

// L.6: Log filter toggle system
function initLogFilters() {
    const bar = document.getElementById('log-filter-bar');
    const types = ['success', 'bond', 'evict', 'info', 'warn', 'system', 'search', 'error'];
    types.forEach(type => {
        const btn = document.createElement('div');
        btn.className = 'log-filter-btn active';
        btn.dataset.type = type;
        btn.textContent = type.toUpperCase();
        btn.onclick = () => toggleLogFilter(type, btn);
        bar.appendChild(btn);
    });
}

function toggleLogFilter(type, btn) {
    if (activeLogFilters.has(type)) {
        activeLogFilters.delete(type);
        btn.classList.remove('active');
        btn.classList.add('inactive');
    } else {
        activeLogFilters.add(type);
        btn.classList.remove('inactive');
        btn.classList.add('active');
    }

    // Re-apply visibility on all existing entries
    const entries = document.querySelectorAll('#log-container .log-entry');
    entries.forEach(entry => {
        const entryType = entry.dataset.logtype;
        entry.style.display = activeLogFilters.has(entryType) ? '' : 'none';
    });
}

// F.6: Escape key resets community filter
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && communityFilterState) {
        resetCommunityFilter();
    }
});

// --- Survival Distribution Panel ---

async function loadHealthDistribution() {
    try {
        const resp = await fetch('/api/health');
        if (!resp.ok) return;
        const sb = await resp.json();
        renderHealthPanel(sb);
    } catch (err) { /* silent — non-critical */ }
}

function renderHealthPanel(sb) {
    const panel = document.getElementById('health-bars');
    if (!panel) return;

    const buckets = [
        { label: '0-20', count: sb.le_20, color: '#ff5252' },
        { label: '21-50', count: sb.le_50, color: '#ff9800' },
        { label: '51-80', count: sb.le_80, color: '#ffd740' },
        { label: '81-95', count: sb.le_95, color: '#69f0ae' },
        { label: '96-100', count: sb.le_100, color: '#5ef3ff' },
    ];

    const maxCount = Math.max(1, ...buckets.map(b => b.count));
    panel.innerHTML = buckets.map(b => {
        const pct = Math.round((b.count / maxCount) * 100);
        return `<div class="health-row">
            <span class="health-label">${b.label}</span>
            <div class="health-bar-bg">
                <div class="health-bar-fill" style="width:${pct}%;background:${b.color}"></div>
            </div>
            <span class="health-count">${b.count}</span>
        </div>`;
    }).join('');

    document.getElementById('health-total').textContent = sb.total;
}

// Ignite
initViz();
loadGraph().then(() => loadHealthDistribution());
initActivityFeed();

window.addEventListener('resize', () => {
    width = window.innerWidth;
    height = window.innerHeight;
    svg.attr("width", width).attr("height", height);
    simulation.force("center", d3.forceCenter(width / 2, height / 2));
    simulation.force("x", d3.forceX(width / 2).strength(d => d.category === 'core' ? 0.6 : 0.03));
    simulation.force("y", d3.forceY(height / 2).strength(d => d.category === 'core' ? 0.6 : 0.03));
    simulation.alpha(0.3).restart();
});

window.refreshCurrentMetrics = async function() {
    const id = document.getElementById('det-id').innerText;
    if (!id || id === "UNKNOWN") return;

    try {
        const response = await fetch('/api/graph');
        const newData = await response.json();
        data = newData;
        fullData = { nodes: [...newData.nodes], links: [...newData.links], threshold: newData.threshold };
        updateViz();

        const updatedNode = data.nodes.find(n => n.id === id);
        if (updatedNode) {
            document.getElementById('det-survival').innerText = (updatedNode.survival || 0).toFixed(2);
            document.getElementById('det-rank').innerText = (updatedNode.pagerank || 0).toFixed(4);

            const scoreEl = document.getElementById('det-survival');
            scoreEl.style.color = "var(--core-color)";
            setTimeout(() => scoreEl.style.color = "", 500);
        }
    } catch (err) { console.error(err); }
}

setInterval(async () => {
    if (searchActive) return; // Don't overwrite search results — user clicks RESET to exit
    if (!document.getElementById('details').classList.contains('active') && !bondMode && !focusMode) {
        await loadGraph();
        loadHealthDistribution();
    }
}, 15000);
