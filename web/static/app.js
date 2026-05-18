let data = { nodes: [], links: [] };
let simulation, svg, container, hudLayer, labelLayer, node, link;
let bondMode = false;
let selectedNode = null;
let focusMode = false;
let width = window.innerWidth;
let height = window.innerHeight;
const color = d3.scaleOrdinal(["#5ef3ff", "#00d4ff", "#70a1ff", "#a371f7", "#58a6ff"]);
const degree = {};

const zoom = d3.zoom().scaleExtent([0.1, 4]).on("zoom", (event) => {
    container.attr("transform", event.transform);
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
        .force("archival", forceArchival) // Orbit outer system
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
    // Dynamic Orbital Radius: Find the farthest ACTIVE shard and add padding
    let maxActiveDist = 400; // Minimum baseline
    data.nodes.forEach(n => {
        if (n.category !== 'archived') {
            const dx = n.x - width / 2;
            const dy = n.y - height / 2;
            const d = Math.sqrt(dx * dx + dy * dy);
            if (d > maxActiveDist) maxActiveDist = d;
        }
    });

    const orbitalRadius = maxActiveDist + 300; // Padding to ensure clear separation
    
    data.nodes.forEach(n => {
        if (n.category === 'archived') {
            const dx = n.x - width / 2;
            const dy = n.y - height / 2;
            const dist = Math.sqrt(dx * dx + dy * dy) || 1;
            const strength = 0.8; // High precision pull
            
            n.vx += (dx / dist) * (orbitalRadius - dist) * alpha * strength;
            n.vy += (dy / dist) * (orbitalRadius - dist) * alpha * strength;
        }
    });
}

async function loadGraph() {
    try {
        const response = await fetch('/api/graph');
        const newData = await response.json();
        
        // --- Position Preservation Layer ---
        // Map existing positions to incoming data to prevent simulation 'reset'
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
        
        // 1. Check for Structural Changes (Requires full updateViz)
        const structureChanged = newData.nodes.length !== data.nodes.length || newData.links.length !== data.links.length;
        const categoriesChanged = JSON.stringify(newData.nodes.map(n => n.category)) !== JSON.stringify(data.nodes.map(n => n.category));
        
        // 2. Check for Metric Changes (Sidebar & Visual properties only)
        const metricsChanged = JSON.stringify(newData.nodes.map(n => n.survival?.toFixed(1))) !== JSON.stringify(data.nodes.map(n => n.survival?.toFixed(1)));

        if (structureChanged || categoriesChanged) {
            data = newData;
            updateViz(true); // Structural change: Allow pulse to settle new nodes
        } else if (metricsChanged) {
            data = newData;
            updateViz(false); // Metric change: Visual update only, keep mesh stationary
        }

        // 3. Independent Sidebar Sync
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
    
    // Reset and recalculate degrees
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

    node = container.select("g.nodes-layer").selectAll("circle")
        .data(data.nodes, d => d.id)
        .join(
            enter => enter.append("circle")
                .attr("class", d => "node " + d.category + (d.category === 'core' ? " core" : ""))
                .attr("r", d => d.category === 'core' ? 12 : (d.category === 'archived' ? 4 : (degree[d.id] || 0) * 1.5 + 6))
                .attr("fill", d => d.category === 'core' ? "var(--core-color)" : (d.category === 'archived' ? "#fff" : color(d.community)))
                .on("click", (event, d) => {
                    event.stopPropagation();
                    selectNode(d);
                })
                .call(d3.drag().on("start", dragstarted).on("drag", dragged).on("end", dragended)),
            update => update
                .attr("class", d => "node " + d.category + (d.category === 'core' ? " core" : ""))
                .attr("r", d => d.category === 'core' ? 12 : (d.category === 'archived' ? 4 : (degree[d.id] || 0) * 1.5 + 6))
                .attr("fill", d => d.category === 'core' ? "var(--core-color)" : (d.category === 'archived' ? "#fff" : color(d.community)))
        );

    // ALWAYS sync simulation with current data to prevent divergence
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
        // Just update positions based on the current alpha without a full restart pulse
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
    document.getElementById('det-density').innerText = (degree[d.id] || 0) + " BONDS";
    document.getElementById('det-rank').innerText = (d.pagerank || 0).toFixed(4);
    document.getElementById('det-survival').innerText = (d.survival || 0).toFixed(2);
    document.getElementById('det-time').innerText = d.created_at || "--";
    document.getElementById('det-comm').innerText = d.community !== undefined ? "N_" + d.community : "N_NONE";
    document.getElementById('det-content').innerText = d.content || "NO_CONTENT";

    // Core and Archived shards cannot be manually evicted
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
    
    // Apply visibility based on the ID and Adjacency Map
    node.style("opacity", n => n.id === id || isNeighbor(id, n.id) ? 1 : 0.05);
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
    node.style("opacity", 1);
    node.style("stroke", "none");
    link.style("stroke-opacity", 0.1);
    labelLayer.selectAll("*").remove();
}

function isNeighbor(idA, idB) {
    return adjMap.get(idA)?.has(idB) || false;
}
function updateNeighborhoods() {
    const list = document.getElementById('neighborhoods-list');
    list.innerHTML = "";
    const communities = [...new Set(data.nodes.map(n => n.community))].sort();
    communities.forEach(c => {
        if (c === undefined) return;
        const btn = document.createElement("div");
        btn.className = "hud-btn";
        btn.style.padding = "4px 8px";
        btn.style.fontSize = "9px";
        btn.innerText = "N_" + c;
        btn.onclick = (event) => {
            event.stopPropagation();
            highlightCommunity(c);
        };
        list.appendChild(btn);
    });
}

function highlightCommunity(communityID) {
    focusMode = true;
    node.style("opacity", d => d.community === communityID ? 1 : 0.05);
    link.style("stroke-opacity", l => (l.source.community === communityID && l.target.community === communityID) ? 0.8 : 0.01);
}

function toggleBondMode() {
    bondMode = !bondMode;
    selectedNode = null;
    const btn = document.getElementById('bond-mode-btn');
    btn.innerText = bondMode ? "BOND_MODE: ON" : "BOND_MODE: OFF";
    btn.style.borderColor = bondMode ? "var(--core-color)" : "var(--panel-border)";
    node.style("stroke", "none");
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
    if (!d || isNaN(d.x) || isNaN(d.y)) return; // Prevent NaN errors
    const r = d.category === 'core' ? 12 : (degree[d.id] || 0) * 1.5 + 6;
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

async function semanticSearch() {
    const term = document.getElementById('search').value;
    if (!term) return;
    try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(term)}`);
        data = await response.json();
        updateViz();
        resetFocus();
    } catch (err) { console.error(err); }
}

function dragstarted(event) { if (!event.active) simulation.alphaTarget(0.3).restart(); event.subject.fx = event.subject.x; event.subject.fy = event.subject.y; }
function dragged(event) { event.subject.fx = event.x; event.subject.fy = event.y; }
function dragended(event) { if (!event.active) simulation.alphaTarget(0); event.subject.fx = null; event.subject.fy = null; }

// --- Activity Feed & Logging ---

function initActivityFeed() {
    const statusDot = document.getElementById('status-dot');
    const evtSource = new EventSource('/api/activity');

    evtSource.onmessage = (event) => {
        const entry = JSON.parse(event.data);
        addLogEntry(entry);
    };

    evtSource.onerror = () => statusDot.classList.add('offline');
    evtSource.onopen = () => statusDot.classList.remove('offline');
    
    // Hydrate history
    loadPersistentLogs();
}

async function loadPersistentLogs() {
    try {
        const resp = await fetch('/api/logs');
        if (resp.ok) {
            const history = await resp.json();
            // history is DESC (newest first). Add oldest first to preserve order.
            history.reverse().forEach(entry => addLogEntry(entry));
        }
    } catch (err) { console.error("Log hydration failed:", err); }
}

function addLogEntry(entry) {
    const container = document.getElementById('log-container');
    if (!container) return;
    const div = document.createElement('div');
    div.className = 'log-entry';
    div.innerHTML = `<span class="log-time">[${entry.timestamp}]</span> <span class="log-type-${entry.type}">${entry.message}</span>`;
    
    if (entry.shard_id) {
        div.onclick = () => focusOnShard(entry.shard_id);
    }

    container.prepend(div);
    if (container.children.length > 50) container.removeChild(container.lastChild);
}

function focusOnShard(id) {
    const d = data.nodes.find(n => n.id === id);
    if (d) {
        selectNode(d);
        const transform = d3.zoomIdentity.translate(width / 2 - d.x, height / 2 - d.y).scale(1.5);
        d3.select('#viz').transition().duration(750).call(zoom.transform, transform);
    } else {
        document.getElementById('search').value = id;
        semanticSearch();
    }
}

function clearLogs() { document.getElementById('log-container').innerHTML = ''; }

// Ignite
initViz();
loadGraph();
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
        data = newData; // Fully update local state
        updateViz();
        
        const updatedNode = data.nodes.find(n => n.id === id);
        if (updatedNode) {
            document.getElementById('det-survival').innerText = (updatedNode.survival || 0).toFixed(2);
            document.getElementById('det-rank').innerText = (updatedNode.pagerank || 0).toFixed(4);
            
            // Temporary flash effect to confirm refresh
            const scoreEl = document.getElementById('det-survival');
            scoreEl.style.color = "var(--core-color)";
            setTimeout(() => scoreEl.style.color = "", 500);
        }
    } catch (err) { console.error(err); }
}

setInterval(() => {
    if (!document.getElementById('details').classList.contains('active') && !bondMode && !focusMode) loadGraph();
}, 15000);
