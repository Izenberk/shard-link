package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"text/template"

	"github.com/izenberk/shard-link/internal/storage"
	"github.com/joho/godotenv"
)

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Shard-Link: Visual Ego 2.0</title>
    <script src="https://d3js.org/d3.v7.min.js"></script>
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&family=Inter:wght@400;600&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #06090f;
            --panel-bg: rgba(6, 9, 15, 0.9);
            --panel-border: rgba(88, 166, 255, 0.3);
            --text-main: #c9d1d9;
            --text-muted: #8b949e;
            --accent: #5ef3ff;
            --shard-glow: #00d4ff; 
            --doc-glow: #ff7b3a; /* Neural Orange (Positive/Central) */
            --core-color: #ff9b3a; /* Vibrant Gold/Orange for Core */
        }

        body { 
            font-family: 'JetBrains Mono', monospace; 
            background: var(--bg-color); 
            color: var(--text-main); 
            margin: 0; 
            overflow: hidden; 
        }

        /* SVG Elements */
        .node { 
            stroke-width: 0; 
            transition: fill 0.2s, stroke 0.2s, stroke-width 0.2s, r 0.2s, filter 0.2s; 
            cursor: pointer;
        }
        .node.core { filter: drop-shadow(0 0 12px var(--core-color)); }
        .node.doc { filter: drop-shadow(0 0 10px var(--doc-glow)); }
        .node.memory { filter: drop-shadow(0 0 6px rgba(0, 212, 255, 0.3)); }

        .link { 
            stroke: var(--accent); 
            stroke-opacity: 0.1; 
            stroke-width: 0.8px;
        }
        
        /* Selection HUD Overlay */
        .selection-box {
            fill: none;
            stroke: var(--accent);
            stroke-width: 1px;
            stroke-opacity: 0.6;
            pointer-events: none;
        }
        .callout-line {
            stroke: var(--accent);
            stroke-width: 1px;
            stroke-opacity: 0.8;
            pointer-events: none;
        }
        .callout-dot {
            fill: #fff;
            pointer-events: none;
        }
        .hud-text {
            fill: var(--accent);
            font-size: 11px;
            text-transform: uppercase;
            letter-spacing: 1px;
            pointer-events: none;
        }
        
        /* Layout */
        #controls { 
            position: absolute; top: 30px; left: 30px; width: 300px; padding: 0; 
            z-index: 10; background: none; border: none;
        }
        .control-section {
            background: var(--panel-bg);
            border: 1px solid var(--panel-border);
            padding: 20px;
            margin-bottom: 15px;
            backdrop-filter: blur(10px);
        }
        .search-container {
            display: flex;
            align-items: center;
            background: rgba(94, 243, 255, 0.05);
            border: 1px solid var(--panel-border);
            border-radius: 4px;
            padding: 0 10px;
        }
        .search-icon { color: var(--accent); font-size: 14px; margin-right: 10px; }
        input { 
            background: none; border: none; color: var(--text-main); 
            padding: 12px 0; width: 100%; 
            font-family: 'JetBrains Mono', monospace; font-size: 12px;
        }
        input:focus { outline: none; }
        
        h3 { margin: 0 0 15px 0; color: var(--accent); font-size: 11px; letter-spacing: 2px; border-bottom: 1px solid var(--panel-border); padding-bottom: 10px; text-transform: uppercase; }
        .legend-item { display: flex; align-items: center; margin-bottom: 8px; font-size: 11px; }
        .legend-color { width: 8px; height: 8px; border-radius: 50%; margin-right: 10px; }

        /* HUD Buttons */
        .btn-group { display: flex; gap: 10px; margin-top: 15px; }
        .hud-btn {
            background: rgba(94, 243, 255, 0.1);
            border: 1px solid var(--panel-border);
            color: var(--accent);
            padding: 8px 12px;
            border-radius: 4px;
            cursor: pointer;
            font-family: 'JetBrains Mono', monospace;
            font-size: 12px;
            transition: all 0.2s;
            flex-grow: 1;
            text-align: center;
        }
        .hud-btn:hover {
            background: rgba(94, 243, 255, 0.2);
            border-color: var(--accent);
            box-shadow: 0 0 10px rgba(94, 243, 255, 0.3);
        }

        /* Details Sidebar */
        #details { 
            position: absolute; top: 0; right: 0; width: 400px; height: 100vh; 
            padding: 0; display: none; z-index: 10;
            background: var(--panel-bg);
            backdrop-filter: blur(20px);
            border-left: 1px solid var(--panel-border);
            display: flex; flex-direction: column;
            transform: translateX(100%); transition: transform 0.3s ease;
        }
        #details.active { transform: translateX(0); display: flex; }
        
        .detail-header { 
            padding: 30px; border-bottom: 1px solid var(--panel-border);
            display: flex; justify-content: space-between; align-items: center;
        }
        .detail-header-title { color: var(--accent); font-weight: 700; font-size: 12px; letter-spacing: 2px; }
        
        .detail-body { padding: 30px; overflow-y: auto; flex-grow: 1; }
        .detail-field { margin-bottom: 25px; }
        .detail-label { color: var(--text-muted); font-size: 10px; text-transform: uppercase; letter-spacing: 1.5px; margin-bottom: 8px; display: block; }
        .detail-value { font-weight: 500; color: #fff; font-size: 13px; line-height: 1.5; }
        
        .content-box { 
            background: rgba(0,0,0,0.4); padding: 20px; border: 1px solid var(--panel-border);
            color: #d1d5db; white-space: pre-wrap; margin-top: 10px; 
            font-family: 'Inter', sans-serif; font-size: 14px;
        }
    </style>
</head>
<body>
    <div id="controls">
        <div class="control-section">
            <h3>SYSTEM_LOCATE</h3>
            <div class="search-container">
                <span class="search-icon">⌕</span>
                <input type="text" id="search" placeholder="FILTER_STREAM..." oninput="searchNodes()">
            </div>
        </div>
        <div class="control-section">
            <h3>MESH_TOPOLOGY</h3>
            <div class="legend-item" style="color: var(--core-color)"><div class="legend-color" style="background: var(--core-color); box-shadow: 0 0 8px var(--core-color)"></div> IDENTITY_ANCHOR</div>
            <div class="legend-item" style="color: var(--doc-glow)"><div class="legend-color" style="background: var(--doc-glow); box-shadow: 0 0 8px var(--doc-glow)"></div> SYSTEM_KNOWLEDGE</div>
            <div class="legend-item" style="color: #5ef3ff"><div class="legend-color" style="background: #5ef3ff; box-shadow: 0 0 8px #5ef3ff"></div> SEMANTIC_COMMUNITY</div>
            <div id="stat-container" style="font-size: 10px; color: var(--text-muted); margin-top: 15px; font-family: 'JetBrains Mono';">
                SHARDS: <span id="stat-n" style="color: var(--accent)">0</span> | 
                BONDS: <span id="stat-e" style="color: var(--accent)">0</span>
            </div>
        </div>
        <div class="control-section">
            <h3>VIEW_CONTROL</h3>
            <div class="btn-group">
                <div class="hud-btn" onclick="zoomIn()">ZOOM_IN (+)</div>
                <div class="hud-btn" onclick="zoomOut()">ZOOM_OUT (-)</div>
            </div>
            <div class="btn-group">
                <div class="hud-btn" onclick="resetView()">CENTRALIZE [⌖]</div>
            </div>
        </div>
    </div>

    <div id="details">
        <div class="detail-header">
            <span class="detail-header-title">ENTITY_INSPECTOR</span>
            <span class="close-btn" onclick="closeSidebar()" style="cursor:pointer; color: var(--accent)">×</span>
        </div>
        <div class="detail-body">
            <div class="detail-field"><span class="detail-label">SHARD_ID</span> <span class="detail-value" id="det-id" style="color: var(--accent);"></span></div>
            <div class="detail-field"><span class="detail-label">NEURAL_DENSITY</span> <span class="detail-value" id="det-density">--</span></div>
            <div class="detail-field"><span class="detail-label">TEMPORAL_MARK</span> <span class="detail-value" id="det-time"></span></div>
            <div class="detail-field"><span class="detail-label">RELATIONAL_COMMUNITY</span> <span class="detail-value" id="det-comm"></span></div>
            <div class="detail-field"><span class="detail-label">RAW_COGNITIVE_CONTENT</span> <div class="content-box" id="det-content"></div></div>
        </div>
    </div>

    <svg id="viz"></svg>

    <script>
        const data = {{.}};
        const width = window.innerWidth;
        const height = window.innerHeight;

        const zoom = d3.zoom().scaleExtent([0.1, 4]).on("zoom", (event) => {
            container.attr("transform", event.transform);
        });

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

        function closeSidebar() {
            document.getElementById('details').classList.remove('active');
            hudLayer.selectAll("*").remove();
            node.style("stroke", "none");
        }

        const svg = d3.select("#viz")
            .attr("width", width)
            .attr("height", height)
            .call(zoom);

        const container = svg.append("g");
        const hudLayer = container.append("g");

        const color = d3.scaleOrdinal(["#5ef3ff", "#00d4ff", "#70a1ff", "#a371f7", "#58a6ff"]);

        const degree = {};
        data.links.forEach(l => {
            degree[l.source] = (degree[l.source] || 0) + 1;
            degree[l.target] = (degree[l.target] || 0) + 1;
        });

        const simulation = d3.forceSimulation(data.nodes)
            .force("link", d3.forceLink(data.links).id(d => d.id).distance(150).strength(0.5))
            .force("charge", d3.forceManyBody().strength(-400))
            .force("center", d3.forceCenter(width / 2, height / 2))
            .force("collision", d3.forceCollide().radius(d => (degree[d.id] || 0) * 3 + 35))
            .force("coreX", d3.forceX(width / 2).strength(d => d.category === 'core' ? 0.4 : 0.01))
            .force("coreY", d3.forceY(height / 2).strength(d => d.category === 'core' ? 0.4 : 0.01));

        const link = container.append("g")
            .selectAll("line")
            .data(data.links)
            .join("line")
            .attr("class", "link");

        const node = container.append("g")
            .selectAll("circle")
            .data(data.nodes)
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
        }

        svg.on("click", () => closeSidebar());

        document.getElementById('stat-n').innerText = data.nodes.length;
        document.getElementById('stat-e').innerText = data.links.length;

        function searchNodes() {
            const term = document.getElementById('search').value.toLowerCase();
            node.style("opacity", d => d.content.toLowerCase().includes(term) ? 1 : 0.1);
            link.style("opacity", d => 0.05);
            if (term === "") {
                node.style("opacity", 1);
                link.style("opacity", 0.15);
            }
        }

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
    </script>
</body>
</html>
`

type VizNode struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Content   string  `json:"content"`
	Community int64   `json:"community"`
	PageRank  float64 `json:"pagerank"`
	CreatedAt string  `json:"created_at"`
}

type VizLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

type VizData struct {
	Nodes []VizNode `json:"nodes"`
	Links []VizLink `json:"links"`
}

func main() {
	_ = godotenv.Load()
	ctx := context.Background()

	v, err := storage.NewVesselGraph(os.Getenv("NEO4J_URL"), os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASS"), "neo4j")
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	log.Println("Analyzing Knowledge Mesh (Louvain + PageRank)...")
	commCount, err := v.CalculateCommunities(ctx)
	if err != nil {
		log.Printf("Warning: Mesh analysis failed: %v", err)
	} else {
		log.Printf("Analysis complete. %d communities mapped.", commCount)
	}

	shards, bonds, err := v.GetGraphData(ctx)
	if err != nil {
		log.Fatal(err)
	}

	data := VizData{
		Nodes: make([]VizNode, len(shards)),
		Links: make([]VizLink, len(bonds)),
	}

	for i, s := range shards {
		data.Nodes[i] = VizNode{
			ID:        s.ID,
			Category:  s.Category,
			Content:   s.Content,
			Community: s.CommunityID,
			PageRank:  s.PageRank,
			CreatedAt: s.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	for i, b := range bonds {
		data.Links[i] = VizLink{Source: b.FromID, Target: b.ToID, Weight: b.Weight}
	}

	jsonBytes, _ := json.Marshal(data)
	f, _ := os.Create("visual_ego.html")
	defer f.Close()

	tmpl := template.Must(template.New("viz").Parse(htmlTemplate))
	tmpl.Execute(f, string(jsonBytes))

	fmt.Println("MISSION COMPLETE: visual_ego.html updated with real centrality and redesigned search.")
}
