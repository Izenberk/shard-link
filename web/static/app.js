let data = { nodes: [], links: [] };
let simulation, svg, container, hudLayer, node, link;
let bondMode = false;
let selectedNode = null;
const width = window.innerWidth;
const height = window.innerHeight;
const color = d3.scaleOrdinal(["#5ef3ff", "#00d4ff", "#70a1ff", "#a371f7", "#58a6ff"]);
const degree = {};

const zoom = d3.zoom().scaleExtent([0.1, 4]).on("zoom", (event) => {
    container.attr("transform", event.transform);
});

function initViz() {
    svg = d3.select("#viz")
        .attr("width", width)
        .attr("height", height)
        .call(zoom);

    container = svg.append("g");
    hudLayer = container.append("g");
    
    link = container.append("g").selectAll("line");
    node = container.append("g").selectAll("circle");

    simulation = d3.forceSimulation()
        .force("link", d3.forceLink().id(d => d.id).distance(150).strength(0.5))
        .force("charge", d3.forceManyBody().strength(-400))
        .force("center", d3.forceCenter(width / 2, height / 2))
        .force("collision", d3.forceCollide().radius(d => (degree[d.id] || 0) * 3 + 35))
        .force("coreX", d3.forceX(width / 2).strength(d => d.category === 'core' ? 0.4 : 0.01))
        .force("coreY", d3.forceY(height / 2).strength(d => d.category === 'core' ? 0.4 : 0.01));

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

async function loadGraph() {
    try {
        const response = await fetch('/api/graph');
        const newData = await response.json();
        
        if (newData.nodes.length !== data.nodes.length || newData.links.length !== data.links.length) {
            console.log("[Sync] Mesh update detected.");
            data = newData;
            updateViz();
        } else if (!bondMode) {
             node.style("opacity", 1);
             link.style("stroke-opacity", d => Math.max(0.1, Math.pow(d.weight, 2) * 0.9));
        }
    } catch (err) {
        console.error("Failed to load graph:", err);
    }
}

function updateViz() {
    const newDegree = {};
    data.links.forEach(l => {
        const sourceID = l.source.id || l.source;
        const targetID = l.target.id || l.target;
        newDegree[sourceID] = (newDegree[sourceID] || 0) + 1;
        newDegree[targetID] = (newDegree[targetID] || 0) + 1;
    });
    Object.assign(degree, newDegree);

    // 2. Update Links
    link = link.data(data.links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)
        .join("line")
        .attr("class", "link")
        .style("stroke", d => d.weight >= 0.99 ? "var(--core-color)" : "var(--accent)")
        .style("stroke-width", d => Math.pow(d.weight, 5) * 5 + 0.5 + "px")
        .style("stroke-opacity", d => Math.max(0.1, Math.pow(d.weight, 2) * 0.9))
        .on("click", (event, d) => {
            if (bondMode) {
                event.stopPropagation();
                const sID = d.source.id || d.source;
                const tID = d.target.id || d.target;
                deleteBond(sID, tID);
            }
        });

    // 3. Update Nodes
    node = node.data(data.nodes, d => d.id)
        .join(
            enter => enter.append("circle")
                .attr("class", d => "node " + d.category + (d.category === 'core' ? " core" : ""))
                .attr("r", d => d.category === 'core' ? 12 : (degree[d.id] || 0) * 2 + 5)
                .attr("fill", d => {
                    if (d.category === 'core') return "var(--core-color)";
                    if (d.category.includes('doc')) return "var(--doc-glow)";
                    return color(d.community);
                })
                .call(d3.drag()
                    .on("start", dragstarted)
                    .on("drag", dragged)
                    .on("end", dragended))
                .on("click", (event, d) => {
                    event.stopPropagation();
                    selectNode(d);
                }),
            update => update
                .attr("r", d => d.category === 'core' ? 12 : (degree[d.id] || 0) * 2 + 5)
                .attr("fill", d => {
                    if (d.category === 'core') return "var(--core-color)";
                    if (d.category.includes('doc')) return "var(--doc-glow)";
                    return color(d.community);
                })
        );

    simulation.nodes(data.nodes);
    simulation.force("link").links(data.links);
    simulation.alpha(0.3).restart();

    document.getElementById('stat-n').innerText = data.nodes.length;
    document.getElementById('stat-e').innerText = data.links.length;
    updateNeighborhoods();
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
        btn.onclick = () => highlightCommunity(c);
        list.appendChild(btn);
    });
}

function highlightCommunity(communityID) {
    node.style("opacity", d => d.community === communityID ? 1 : 0.1);
    link.style("stroke-opacity", d => {
        const sComm = d.source.community !== undefined ? d.source.community : d.source;
        const tComm = d.target.community !== undefined ? d.target.community : d.target;
        return (sComm === communityID && tComm === communityID) ? 0.8 : 0.05;
    });
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
    try {
        const resp = await fetch('/api/bonds', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ from_id: fromID, to_id: toID, weight: 1.0 })
        });
        if (resp.ok) {
            console.log("[Mesh] Bond created.");
            setTimeout(loadGraph, 500);
        }
    } catch (err) { console.error(err); }
}

async function deleteBond(fromID, toID) {
    if (!confirm(`Break bond between ${fromID} and ${toID}?`)) return;
    try {
        const resp = await fetch(`/api/bonds?from=${encodeURIComponent(fromID)}&to=${encodeURIComponent(toID)}`, { method: 'DELETE' });
        if (resp.ok) {
            console.log("[Mesh] Bond deleted.");
            setTimeout(loadGraph, 500);
        }
    } catch (err) { console.error(err); }
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

    const sidebar = document.getElementById('details');
    sidebar.classList.add('active');
    document.getElementById('det-id').innerText = d.id;
    document.getElementById('det-density').innerText = (degree[d.id] || 0) + " BONDS";
    document.getElementById('det-time').innerText = d.created_at;
    document.getElementById('det-comm').innerText = "V_COMM_" + d.community;
    document.getElementById('det-content').innerText = d.content;

    node.style("stroke", n => n.id === d.id ? "var(--accent)" : "none");
    node.style("stroke-width", n => n.id === d.id ? "2px" : "0");
    drawHUD(d);
}

function drawHUD(d) {
    hudLayer.selectAll("*").remove();
    const r = d.category === 'core' ? 12 : (degree[d.id] || 0) * 2 + 5;
    const boxSize = r * 3.5;
    hudLayer.append("rect").attr("class", "selection-box").attr("x", d.x - boxSize/2).attr("y", d.y - boxSize/2).attr("width", boxSize).attr("height", boxSize);
    hudLayer.append("line").attr("class", "callout-line").attr("x1", d.x - boxSize/2).attr("y1", d.y).attr("x2", d.x - boxSize).attr("y2", d.y);
    hudLayer.append("text").attr("class", "hud-text").attr("x", d.x - boxSize - 5).attr("y", d.y).attr("text-anchor", "end").text("ADDR: " + d.id);
}

function closeSidebar() {
    document.getElementById('details').classList.remove('active');
    hudLayer.selectAll("*").remove();
    node.style("stroke", "none");
    document.getElementById('det-id').innerText = "";
}

function resetView() { svg.transition().duration(750).call(zoom.transform, d3.zoomIdentity); }
function zoomIn() { svg.transition().duration(300).call(zoom.scaleBy, 1.3); }
function zoomOut() { svg.transition().duration(300).call(zoom.scaleBy, 0.7); }

async function semanticSearch() {
    const term = document.getElementById('search').value;
    if (!term) return;
    try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(term)}`);
        data = await response.json();
        updateViz();
        node.style("opacity", 1);
        link.style("stroke-opacity", 0.3);
    } catch (err) { console.error(err); }
}

function dragstarted(event) { if (!event.active) simulation.alphaTarget(0.3).restart(); event.subject.fx = event.subject.x; event.subject.fy = event.subject.y; }
function dragged(event) { event.subject.fx = event.x; event.subject.fy = event.y; }
function dragended(event) { if (!event.active) simulation.alphaTarget(0); event.subject.fx = null; event.subject.fy = null; }

initViz();
loadGraph();

setInterval(() => {
    if (!document.getElementById('details').classList.contains('active') && !bondMode) {
        loadGraph();
    }
}, 15000);

window.addEventListener('resize', () => { svg.attr("width", window.innerWidth).attr("height", window.innerHeight); });
svg.on("click", () => closeSidebar());
