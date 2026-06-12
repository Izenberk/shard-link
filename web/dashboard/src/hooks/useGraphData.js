import { useState, useEffect, useCallback, useRef } from 'react'
import { fetchGraph, fetchHealth } from '../lib/api'

// Change detection: mirrors vanilla app.js logic.
// Returns 'structure' | 'metrics' | 'none' so the graph knows
// whether to do a full re-layout, a gentle nudge, or nothing at all.
function detectChange(prev, next) {
  if (!prev) return 'structure'

  const structureChanged =
    next.nodes.length !== prev.nodes.length ||
    next.links.length !== prev.links.length

  const categoriesChanged =
    JSON.stringify(next.nodes.map(n => n.category)) !==
    JSON.stringify(prev.nodes.map(n => n.category))

  if (structureChanged || categoriesChanged) return 'structure'

  const metricsChanged =
    JSON.stringify(next.nodes.map(n => n.survival?.toFixed(1))) !==
    JSON.stringify(prev.nodes.map(n => n.survival?.toFixed(1)))

  if (metricsChanged) return 'metrics'

  return 'none'
}

export default function useGraphData(pauseRefresh = false) {
  const [data, setData] = useState(null)
  const [fullData, setFullData] = useState(null)
  const [health, setHealth] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  // 'structure' = full restart, 'metrics' = soft nudge, null = initial/search
  const [changeType, setChangeType] = useState(null)
  const searchActiveRef = useRef(false)
  const pauseRef = useRef(pauseRefresh)
  pauseRef.current = pauseRefresh

  const loadGraph = useCallback(async () => {
    try {
      const newData = await fetchGraph()

      setData(prev => {
        // Position preservation: copy x/y/vx/vy from old nodes
        if (prev?.nodes) {
          const nodeMap = new Map(prev.nodes.map(n => [n.id, n]))
          newData.nodes.forEach(n => {
            const old = nodeMap.get(n.id)
            if (old) {
              n.x = old.x; n.y = old.y
              n.vx = old.vx; n.vy = old.vy
              n.fx = old.fx; n.fy = old.fy
            }
          })
        }

        // Detect what changed
        const change = detectChange(prev, newData)
        if (change === 'none') {
          // Nothing meaningful changed — don't trigger a re-render
          setChangeType('none')
          return prev
        }

        setChangeType(change)
        return newData
      })

      setFullData({ nodes: [...newData.nodes], links: [...newData.links], threshold: newData.threshold })
      setError(null)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadHealth = useCallback(async () => {
    try {
      const h = await fetchHealth()
      setHealth(h)
    } catch { /* non-critical */ }
  }, [])

  // Replace data with search results — always full restart
  const setSearchResults = useCallback((searchData) => {
    searchActiveRef.current = true
    setChangeType('structure')
    setData(searchData)
    setFullData({ nodes: [...searchData.nodes], links: [...searchData.links], threshold: searchData.threshold })
  }, [])

  // Reset to full graph
  const resetGraph = useCallback(() => {
    searchActiveRef.current = false
    loadGraph()
    loadHealth()
  }, [loadGraph, loadHealth])

  // Apply community isolation — always full restart
  const isolateCommunity = useCallback((communityId) => {
    if (!fullData) return
    const communityNodes = fullData.nodes.filter(n => n.community === communityId)
    const nodeIds = new Set(communityNodes.map(n => n.id))
    const communityLinks = fullData.links.filter(l => {
      const sID = l.source.id || l.source
      const tID = l.target.id || l.target
      return nodeIds.has(sID) && nodeIds.has(tID)
    })
    setChangeType('structure')
    setData({ nodes: communityNodes, links: communityLinks, threshold: fullData.threshold })
  }, [fullData])

  // Restore from isolation — always full restart
  const restoreFullData = useCallback(() => {
    if (fullData) {
      setChangeType('structure')
      setData({ nodes: [...fullData.nodes], links: [...fullData.links], threshold: fullData.threshold })
    }
  }, [fullData])

  // Initial load
  useEffect(() => {
    loadGraph().then(() => loadHealth())
  }, [loadGraph, loadHealth])

  // Auto-refresh every 15 seconds — skip when inspector open, bond mode, or search active
  useEffect(() => {
    const interval = setInterval(() => {
      if (searchActiveRef.current || pauseRef.current) return
      loadGraph()
      loadHealth()
    }, 15000)
    return () => clearInterval(interval)
  }, [loadGraph, loadHealth])

  return {
    data,
    fullData,
    health,
    loading,
    error,
    changeType,
    refetch: loadGraph,
    refetchHealth: loadHealth,
    setSearchResults,
    resetGraph,
    isolateCommunity,
    restoreFullData,
  }
}
