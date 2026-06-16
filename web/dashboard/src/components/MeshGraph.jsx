import { useEffect, useRef, useCallback } from 'react'
import { select } from 'd3-selection'
import { scaleOrdinal } from 'd3-scale'
import { zoom as d3Zoom, zoomIdentity } from 'd3-zoom'
import {
  forceSimulation, forceLink, forceManyBody,
  forceCenter, forceCollide, forceX, forceY,
} from 'd3-force'
import { drag as d3Drag } from 'd3-drag'
import 'd3-transition' // side-effect: enables .transition() on selections

// Community color palette — exported so other components can use it
export const COMM_COLORS = ['#5ef3ff', '#00d4ff', '#70a1ff', '#a371f7', '#58a6ff']
const color = scaleOrdinal(COMM_COLORS)

function nodeRadius(d) {
  if (d.category === 'core') return 12
  if (d.category === 'archived') return 4
  const baseRadius = 5
  const scaleFactor = 15
  return Math.min(baseRadius + (d.pagerank || 0) * scaleFactor, 20)
}

function survivalGlow(d) {
  if (d.category === 'core') return 'drop-shadow(0 0 10px var(--core-color))'
  if (d.category === 'archived') return 'drop-shadow(0 0 6px rgba(255,255,255,0.5))'
  const s = d.survival || 0
  const baseColor = color(d.community)
  const radius = Math.round((s / 100) * 12)
  if (radius <= 1) return 'none'
  return `drop-shadow(0 0 ${radius}px ${baseColor})`
}

function nodeFill(d) {
  if (d.category === 'core') return 'var(--core-color)'
  if (d.category === 'archived') return '#fff'
  return color(d.community)
}

function truncateID(id) {
  if (!id) return ''
  return id.length > 24 ? id.substring(0, 21) + '...' : id
}

export default function MeshGraph({
  data,
  changeType,
  onSelectNode,
  onBondCreated,
  onBondDeleted,
  bondMode,
  focusedNodeId,
  onZoomReady,
}) {
  const svgRef = useRef(null)
  const containerRef = useRef(null)
  const simulationRef = useRef(null)
  const nodeRef = useRef(null)
  const linkRef = useRef(null)
  const hudLayerRef = useRef(null)
  const zoomRef = useRef(null)
  const degreeRef = useRef({})
  const adjMapRef = useRef(new Map())
  const bondSourceRef = useRef(null)
  const initialZoomApplied = useRef(false)
  const initialTransformRef = useRef(zoomIdentity)
  const focusedNodeIdRef = useRef(focusedNodeId)

  // Build adjacency map
  const buildAdjacencyMap = useCallback((links) => {
    const map = new Map()
    links.forEach(l => {
      const sID = l.source.id || l.source
      const tID = l.target.id || l.target
      if (!map.has(sID)) map.set(sID, new Set())
      if (!map.has(tID)) map.set(tID, new Set())
      map.get(sID).add(tID)
      map.get(tID).add(sID)
    })
    adjMapRef.current = map
  }, [])

  // Initialize SVG + simulation once
  useEffect(() => {
    const svg = select(svgRef.current)
    const width = window.innerWidth
    const height = window.innerHeight

    svg.attr('width', width).attr('height', height)

    const zoom = d3Zoom()
      .scaleExtent([0.1, 4])
      .on('zoom', (event) => {
        containerRef.current.attr('transform', event.transform)
      })
    zoomRef.current = zoom
    svg.call(zoom)

    // Click on empty SVG background to deselect
    svg.on('click', (event) => {
      if (event.target.tagName === 'svg') {
        onSelectNode(null)
      }
    })

    const container = svg.append('g')
    containerRef.current = container
    container.append('g').attr('class', 'links-layer')
    container.append('g').attr('class', 'nodes-layer')
    container.append('g').attr('class', 'hud-layer')
    hudLayerRef.current = container.select('g.hud-layer')

    function forceCluster(alpha) {
      const nodes = simulationRef.current?.nodes() || []
      const centroids = {}
      nodes.forEach(n => {
        if (!centroids[n.community]) centroids[n.community] = { x: 0, y: 0, count: 0 }
        centroids[n.community].x += n.x
        centroids[n.community].y += n.y
        centroids[n.community].count++
      })
      for (const c in centroids) {
        centroids[c].x /= centroids[c].count
        centroids[c].y /= centroids[c].count
      }
      nodes.forEach(n => {
        const c = centroids[n.community]
        if (c) {
          n.vx += (c.x - n.x) * alpha * 0.15
          n.vy += (c.y - n.y) * alpha * 0.15
        }
      })
    }

    function forceArchival(alpha) {
      const nodes = simulationRef.current?.nodes() || []
      let maxActiveDist = 400
      nodes.forEach(n => {
        if (n.category !== 'archived') {
          const dx = n.x - width / 2
          const dy = n.y - height / 2
          const d = Math.sqrt(dx * dx + dy * dy)
          if (d > maxActiveDist) maxActiveDist = d
        }
      })
      const orbitalRadius = maxActiveDist + 300
      nodes.forEach(n => {
        if (n.category === 'archived') {
          const dx = n.x - width / 2
          const dy = n.y - height / 2
          const dist = Math.sqrt(dx * dx + dy * dy) || 1
          n.vx += (dx / dist) * (orbitalRadius - dist) * alpha * 0.8
          n.vy += (dy / dist) * (orbitalRadius - dist) * alpha * 0.8
        }
      })
    }

    const simulation = forceSimulation()
      .force('link', forceLink()
        .id(d => d.id)
        .distance(d => {
          const w = d.weight || 0.5
          return 220 - (w * 80) // range: ~140 (strong bond) to ~220 (weak bond)
        })
        .strength(0.4))
      .force('charge', forceManyBody()
        .strength(d => {
          if (d.category === 'archived') return 0
          const bonds = degreeRef.current[d.id] || 0
          if (bonds === 0) return -300
          if (bonds <= 2) return -800
          return -1400
        }))
      .force('center', forceCenter(width / 2, height / 2).strength(0.08))
      .force('collision', forceCollide().radius(d => {
        if (d.category === 'archived') return 10
        return (degreeRef.current[d.id] || 0) * 2 + 38
      }))
      .force('cluster', forceCluster)
      .force('archival', forceArchival)
      .force('x', forceX(width / 2).strength(d => {
        if (d.category === 'core') return 0.6
        if (d.category === 'archived') return 0
        const bonds = degreeRef.current[d.id] || 0
        if (bonds === 0) return 0.15
        if (bonds <= 2) return 0.06
        return 0.03
      }))
      .force('y', forceY(height / 2).strength(d => {
        if (d.category === 'core') return 0.6
        if (d.category === 'archived') return 0
        const bonds = degreeRef.current[d.id] || 0
        if (bonds === 0) return 0.15
        if (bonds <= 2) return 0.06
        return 0.03
      }))

    simulation.on('tick', () => {
      if (!linkRef.current || !nodeRef.current) return
      linkRef.current
        .attr('x1', d => d.source.x).attr('y1', d => d.source.y)
        .attr('x2', d => d.target.x).attr('y2', d => d.target.y)
      nodeRef.current
        .attr('cx', d => d.x).attr('cy', d => d.y)

      // Track selection box with focused node on every tick
      if (hudLayerRef.current && focusedNodeIdRef.current) {
        const focused = simulation.nodes().find(n => n.id === focusedNodeIdRef.current)
        if (focused && !isNaN(focused.x)) {
          const boxSize = nodeRadius(focused) * 3.5
          hudLayerRef.current.select('rect.selection-box')
            .attr('x', focused.x - boxSize / 2)
            .attr('y', focused.y - boxSize / 2)
        }
      }
    })

    simulationRef.current = simulation

    // Handle resize
    function handleResize() {
      const w = window.innerWidth
      const h = window.innerHeight
      svg.attr('width', w).attr('height', h)
      simulation.force('center', forceCenter(w / 2, h / 2).strength(0.08))
      simulation.force('x', forceX(w / 2).strength(d => {
        if (d.category === 'core') return 0.6
        if (d.category === 'archived') return 0
        const bonds = degreeRef.current[d.id] || 0
        if (bonds === 0) return 0.15
        if (bonds <= 2) return 0.06
        return 0.03
      }))
      simulation.force('y', forceY(h / 2).strength(d => {
        if (d.category === 'core') return 0.6
        if (d.category === 'archived') return 0
        const bonds = degreeRef.current[d.id] || 0
        if (bonds === 0) return 0.15
        if (bonds <= 2) return 0.06
        return 0.03
      }))
      simulation.alpha(0.3).restart()
    }
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      simulation.stop()
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Update visualization when data changes
  useEffect(() => {
    if (!data || !simulationRef.current || !containerRef.current) return
    // Skip render entirely if nothing changed (matches vanilla logic)
    if (changeType === 'none') return

    const simulation = simulationRef.current
    const container = containerRef.current
    const degree = degreeRef.current

    // Compute degrees
    for (const key in degree) delete degree[key]
    data.links.forEach(l => {
      const sID = l.source.id || l.source
      const tID = l.target.id || l.target
      degree[sID] = (degree[sID] || 0) + 1
      degree[tID] = (degree[tID] || 0) + 1
    })

    buildAdjacencyMap(data.links)

    // Tooltip element
    const tooltipEl = document.querySelector('.node-tooltip')

    // Links
    const link = container.select('g.links-layer').selectAll('line')
      .data(data.links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)
      .join('line')
      .attr('class', 'link active')
      .style('stroke-width', d => Math.pow(d.weight, 2) * 2 + 1 + 'px')
      .style('stroke-opacity', d => Math.max(0.1, d.weight * 0.35))

    link.on('mouseenter', (event, d) => {
      if (!tooltipEl) return
      const sID = d.source.id || d.source
      const tID = d.target.id || d.target
      tooltipEl.querySelector('.tooltip-id').textContent = `${truncateID(sID)} <-> ${truncateID(tID)}`
      tooltipEl.querySelector('.tooltip-category').textContent = ''
      tooltipEl.querySelector('.tooltip-content').textContent = `SIM: ${d.weight.toFixed(2)}`
      tooltipEl.style.display = 'block'
    })
    .on('mouseleave', () => { if (tooltipEl) tooltipEl.style.display = 'none' })
    .on('mousemove', (event) => {
      if (!tooltipEl) return
      tooltipEl.style.left = (event.clientX + 14) + 'px'
      tooltipEl.style.top = (event.clientY - 10) + 'px'
    })
    .on('click', (event, d) => {
      if (!bondMode) return
      event.stopPropagation()
      const sID = d.source.id || d.source
      const tID = d.target.id || d.target
      onBondDeleted(sID, tID)
    })

    linkRef.current = link

    // Nodes
    const node = container.select('g.nodes-layer').selectAll('circle')
      .data(data.nodes, d => d.id)
      .join(
        enter => enter.append('circle')
          .attr('class', 'node')
          .attr('r', nodeRadius)
          .attr('fill', nodeFill)
          .style('filter', survivalGlow)
          .call(d3Drag()
            .on('start', (event) => {
              if (!event.active) simulation.alphaTarget(0.05).restart()
              event.subject.fx = event.subject.x
              event.subject.fy = event.subject.y
            })
            .on('drag', (event) => {
              event.subject.fx = event.x
              event.subject.fy = event.y
            })
            .on('end', (event) => {
              if (!event.active) simulation.alphaTarget(0).alpha(0.08)
              event.subject.fx = null
              event.subject.fy = null
            })
          ),
        update => update
          .attr('class', 'node')
          .attr('r', nodeRadius)
          .attr('fill', nodeFill)
          .style('filter', survivalGlow)
      )

    // Node click handler
    node.on('click', (event, d) => {
      event.stopPropagation()
      if (bondMode) {
        if (!bondSourceRef.current) {
          bondSourceRef.current = d
          node.style('stroke', n => n.id === d.id ? 'var(--core-color)' : 'none')
          node.style('stroke-width', n => n.id === d.id ? '4px' : '0')
        } else if (bondSourceRef.current.id !== d.id) {
          onBondCreated(bondSourceRef.current.id, d.id)
          bondSourceRef.current = null
        }
        return
      }
      onSelectNode(d)
    })

    // Node tooltips
    node.on('mouseenter', (event, d) => {
      if (!tooltipEl) return
      tooltipEl.querySelector('.tooltip-id').textContent = truncateID(d.id)
      const catEl = tooltipEl.querySelector('.tooltip-category')
      catEl.textContent = (d.category || 'unknown').toUpperCase()
      catEl.className = 'tooltip-category ' + (d.category || '')
      tooltipEl.querySelector('.tooltip-content').textContent =
        (d.content || '').substring(0, 120) + (d.content && d.content.length > 120 ? '...' : '')
      tooltipEl.style.display = 'block'
    })
    .on('mouseleave', () => { if (tooltipEl) tooltipEl.style.display = 'none' })
    .on('mousemove', (event) => {
      if (!tooltipEl) return
      tooltipEl.style.left = (event.clientX + 14) + 'px'
      tooltipEl.style.top = (event.clientY - 10) + 'px'
    })

    nodeRef.current = node

    // Update simulation — match vanilla logic:
    // 'structure' = full re-layout (alpha 0.5), 'metrics' = gentle nudge (alpha 0.01)
    // Pre-position nodes that have no position yet (first load) in a circle around
    // viewport center — prevents D3's default phyllotaxis near (0,0) causing a
    // visible "big bang" explosion from the top-left corner on page load.
    const cx = window.innerWidth / 2
    const cy = window.innerHeight / 2
    data.nodes.forEach((n, i) => {
      if (n.x == null) {
        const angle = (2 * Math.PI * i) / data.nodes.length
        n.x = cx + 220 * Math.cos(angle)
        n.y = cy + 220 * Math.sin(angle)
      }
    })

    simulation.nodes(data.nodes)
    simulation.force('link').links(data.links)

    // Apply initial zoom once on first data load, scaled to node count
    if (!initialZoomApplied.current && svgRef.current && containerRef.current) {
      initialZoomApplied.current = true
      const n = data.nodes.length
      // Scale shrinks as mesh grows: 0.55 at ~50 nodes, 0.4 at ~100, 0.25 at ~500+
      const scale = Math.max(0.2, Math.min(0.7, 4.5 / Math.sqrt(n)))
      const w = window.innerWidth
      const h = window.innerHeight
      const tx = (w / 2) * (1 - scale)
      const ty = (h / 2) * (1 - scale)
      const t = zoomIdentity.translate(tx, ty).scale(scale)
      initialTransformRef.current = t
      containerRef.current.attr('transform', t.toString())
      select(svgRef.current).property('__zoom', t)
    }

    if (changeType === 'structure' || !changeType) {
      simulation.alpha(0.5).restart()
    } else {
      simulation.alpha(0.01).restart()
    }

    // Re-apply focus styling after data refresh using ref (avoids restarting simulation on click)
    const fid = focusedNodeIdRef.current
    if (fid) {
      const adj = adjMapRef.current
      node.style('opacity', n => {
        if (n.id === fid || adj.get(fid)?.has(n.id)) return 1
        return 0.05
      })
      link.style('stroke-opacity', l => {
        const sID = l.source.id || l.source
        const tID = l.target.id || l.target
        return (sID === fid || tID === fid) ? 0.8 : 0.01
      })
      node.style('stroke', n => n.id === fid ? 'var(--accent)' : 'none')
      node.style('stroke-width', n => n.id === fid ? '3px' : '0')
    }
  }, [data, changeType, bondMode, buildAdjacencyMap, onSelectNode, onBondCreated])

  // Apply focus dimming when focusedNodeId changes
  useEffect(() => {
    if (!nodeRef.current || !linkRef.current) return
    const node = nodeRef.current
    const link = linkRef.current
    const hud = hudLayerRef.current

    if (!focusedNodeId) {
      node.style('opacity', 1).attr('fill', nodeFill).style('filter', survivalGlow)
      node.style('stroke', 'none')
      link.style('stroke-opacity', d => Math.max(0.1, d.weight * 0.35))
      if (hud) hud.selectAll('*').remove()
      return
    }

    const adj = adjMapRef.current
    node.style('opacity', n => {
      if (n.id === focusedNodeId || adj.get(focusedNodeId)?.has(n.id)) return 1
      return 0.05
    })
    link.style('stroke-opacity', l => {
      const sID = l.source.id || l.source
      const tID = l.target.id || l.target
      return (sID === focusedNodeId || tID === focusedNodeId) ? 0.8 : 0.01
    })
    node.style('stroke', n => n.id === focusedNodeId ? 'var(--accent)' : 'none')
    node.style('stroke-width', n => n.id === focusedNodeId ? '3px' : '0')

    // Draw HUD selection box
    if (hud) {
      hud.selectAll('*').remove()
      const d = data?.nodes.find(n => n.id === focusedNodeId)
      if (d && !isNaN(d.x) && !isNaN(d.y)) {
        const r = nodeRadius(d)
        const boxSize = r * 3.5
        hud.append('rect')
          .attr('class', 'selection-box')
          .attr('x', d.x - boxSize / 2)
          .attr('y', d.y - boxSize / 2)
          .attr('width', boxSize)
          .attr('height', boxSize)
      }
    }
  }, [focusedNodeId, data])

  // Keep ref in sync so tick handler can read focusedNodeId without stale closure
  useEffect(() => {
    focusedNodeIdRef.current = focusedNodeId
  }, [focusedNodeId])

  // Reset bond source when bond mode toggled off
  useEffect(() => {
    if (!bondMode) bondSourceRef.current = null
  }, [bondMode])

  // Expose zoom controls via ref methods
  const zoomIn = useCallback(() => {
    select(svgRef.current).transition().duration(300).call(zoomRef.current.scaleBy, 1.3)
  }, [])

  const zoomOut = useCallback(() => {
    select(svgRef.current).transition().duration(300).call(zoomRef.current.scaleBy, 0.7)
  }, [])

  const resetView = useCallback(() => {
    select(svgRef.current).transition().duration(750).call(zoomRef.current.transform, initialTransformRef.current)
  }, [])

  // Expose zoom controls to parent via callback
  useEffect(() => {
    if (onZoomReady) {
      onZoomReady({ zoomIn, zoomOut, resetView })
    }
  }, [zoomIn, zoomOut, resetView, onZoomReady])

  return (
    <>
      <svg
        ref={svgRef}
        className={bondMode ? 'bond-mode-active' : ''}
        style={{ position: 'absolute', top: 0, left: 0 }}
      />
      {/* Tooltip portal — positioned fixed, rendered once */}
      <div className="node-tooltip" style={{ display: 'none' }}>
        <div className="tooltip-id"></div>
        <div className="tooltip-category"></div>
        <div className="tooltip-content"></div>
      </div>
    </>
  )
}
