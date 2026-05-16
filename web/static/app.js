let data = { nodes: [], links: [] };
let simulation, svg, container, hudLayer, node, link;
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
        
        const activeId = document.getElementById('det-id').innerText;
        if (activeId) {
            const d = data.nodes.find(n => n.id === activeId);
            if (d) drawHUD(d);
        }
    });
}

async function loadGraph() {
    try {
        const response = await fetch('/api/graph');
        data = await response.json();
        updateViz();
    } catch (err) {
        console.error("Failed to load graph:", err);
    }
}

function updateViz() {
    // Calculate degrees
    data.links.forEach(l => {
        degree[l.source] = (degree[l.source] || 0) + 1;
        degree[l.target] = (degree[l.target] || 0) + 1;
    });

    link = link.data(data.links)
        .join("line")
        .attr("class", "link");

    node = node.data(data.nodes)
        .join("circle")
        .attr("class", d => {
            let cls = "node " + d.category;
            if (d.category === 'core') return cls + " core";
            if (d.category.includes('doc') || d.category.includes('guideline') || d.category.includes('plan')) cls += " doc";
            return cls;
        })
        .attr("r", d => d.category === 'core' ? 12 : (degree[d.id] || 0) * 2 + 5)
        .attr("fill", d => {
            if (d.category === 'core') return "var(--core-color)";
            if (d.category.includes('doc') || d.category.includes('guideline') || d.category.includes('plan')) return "var(--doc-glow)";
            return color(d.community);
        })
        .on("click", (event, d) => {
            event.stopPropagation();
            selectNode(d);
        })
        .call(d3.drag()
            .on("start", dragstarted)
            .on("drag", dragged)
            .on("end", dragended));

    simulation.nodes(data.nodes);
    simulation.force("link").links(data.links);
    simulation.alpha(1).restart();

    document.getElementById('stat-n').innerText = data.nodes.length;
    document.getElementById('stat-e').innerText = data.links.length;
}

function selectNode(d) {
    const sidebar = document.getElementById('details');
    sidebar.classList.add('active');
    document.getElementById('det-id').innerText = d.id;
    document.getElementById('det-density').innerText = (degree[d.id] || 0) + " ACTIVE_BONDS";
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
    
    hudLayer.append("rect")
        .attr("class", "selection-box")
        .attr("x", d.x - boxSize/2)
        .attr("y", d.y - boxSize/2)
        .attr("width", boxSize)
        .attr("height", boxSize);

    hudLayer.append("line")
        .attr("class", "callout-line")
        .attr("x1", d.x - boxSize/2)
        .attr("y1", d.y)
        .attr("x2", d.x - boxSize)
        .attr("y2", d.y);

    hudLayer.append("circle")
        .attr("class", "callout-dot")
        .attr("cx", d.x - boxSize)
        .attr("cy", d.y)
        .attr("r", 2);

    hudLayer.append("text")
        .attr("class", "hud-text")
        .attr("x", d.x - boxSize - 5)
        .attr("y", d.y - 15)
        .attr("text-anchor", "end")
        .text("NEURAL_DENSITY: " + (degree[d.id] || 0));

    hudLayer.append("text")
        .attr("class", "hud-text")
        .attr("x", d.x - boxSize - 5)
        .attr("y", d.y)
        .attr("text-anchor", "end")
        .text("ADDR: " + d.id);
}

function closeSidebar() {
    document.getElementById('details').classList.remove('active');
    hudLayer.selectAll("*").remove();
    node.style("stroke", "none");
    document.getElementById('det-id').innerText = "";
}

function resetView() {
    svg.transition().duration(750).call(
        zoom.transform,
        d3.zoomIdentity
    );
}

function zoomIn() {
    svg.transition().duration(300).call(zoom.scaleBy, 1.3);
}

function zoomOut() {
    svg.transition().duration(300).call(zoom.scaleBy, 0.7);
}

async function semanticSearch() {
    const term = document.getElementById('search').value;
    if (!term) return;
    
    try {
        const response = await fetch(`/api/search?q=${encodeURIComponent(term)}`);
        data = await response.json();
        updateViz();
    } catch (err) {
        console.error("Semantic search failed:", err);
    }
}

function dragstarted(event) {
    if (!event.active) simulation.alphaTarget(0.3).restart();
    event.subject.fx = event.subject.x;
    event.subject.fy = event.subject.y;
}
function dragged(event) {
    event.subject.fx = event.x;
    event.subject.fy = event.y;
}
function dragended(event) {
    if (!event.active) simulation.alphaTarget(0);
    event.subject.fx = null;
    event.subject.fy = null;
}

// Initial Setup
initViz();
loadGraph();

// Handle resizing
window.addEventListener('resize', () => {
    svg.attr("width", window.innerWidth).attr("height", window.innerHeight);
});

svg.on("click", () => closeSidebar());
