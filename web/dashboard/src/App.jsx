import { useState, useCallback, useEffect, useRef } from 'react'
import BackgroundField from './components/BackgroundField'
import MeshGraph from './components/MeshGraph'
import CommandRail from './components/CommandRail'
import EntityInspector from './components/EntityInspector'
import CommandBar from './components/CommandBar'
import ActivityFeed from './components/ActivityFeed'
import useGraphData from './hooks/useGraphData'
import useActivityStream from './hooks/useActivityStream'
import { searchGraph, evictShard, createBond, deleteBond, fetchCommunity, fetchGraph, runJanitor } from './lib/api'

export default function App() {
  const [selectedNode, setSelectedNode] = useState(null)
  const [bondMode, setBondMode] = useState(false)
  const [searchLoading, setSearchLoading] = useState(false)
  const [commSummary, setCommSummary] = useState(null)

  // Pause auto-refresh when inspector is open, bond mode active, or node focused
  // Matches vanilla: !details.active && !bondMode && !focusMode
  const pauseRefresh = !!selectedNode || bondMode

  const {
    data,
    health,
    loading,
    changeType,
    refetch,
    refetchHealth,
    setSearchResults,
    resetGraph,
    isolateCommunity,
    restoreFullData,
  } = useGraphData(pauseRefresh)

  const { entries, online, clearEntries } = useActivityStream()
  const [communityFilter, setCommunityFilter] = useState(null) // { id, phase }
  const [zoomControls, setZoomControls] = useState(null)

  // Stable callback ref for MeshGraph zoom ready
  const handleZoomReady = useCallback((controls) => {
    setZoomControls(controls)
  }, [])

  // Compute degree for selected node
  const selectedDegree = selectedNode && data
    ? data.links.reduce((count, l) => {
        const sID = l.source.id || l.source
        const tID = l.target.id || l.target
        return count + (sID === selectedNode.id || tID === selectedNode.id ? 1 : 0)
      }, 0)
    : 0

  // --- Handlers ---

  const handleSelectNode = useCallback((node) => {
    setSelectedNode(node)
  }, [])

  const handleSearch = useCallback(async (query) => {
    setSearchLoading(true)
    try {
      const results = await searchGraph(query)
      setSearchResults(results)
      setSelectedNode(null)
      setCommunityFilter(null)
      setCommSummary(null)
    } catch (err) {
      console.error(err)
    } finally {
      setSearchLoading(false)
    }
  }, [setSearchResults])

  const handleReset = useCallback(() => {
    resetGraph()
    setSelectedNode(null)
    setCommunityFilter(null)
    setCommSummary(null)
  }, [resetGraph])

  const handleEvict = useCallback(async () => {
    if (!selectedNode) return
    if (!confirm(`Permanently evict shard ${selectedNode.id} from the Knowledge Mesh?`)) return
    try {
      await evictShard(selectedNode.id)
      setSelectedNode(null)
      setTimeout(() => { refetch(); refetchHealth() }, 500)
    } catch (err) {
      alert(err.message)
    }
  }, [selectedNode, refetch, refetchHealth])

  const handleRefreshMetrics = useCallback(async () => {
    if (!selectedNode) return
    try {
      const newData = await fetchGraph()
      const updated = newData.nodes.find(n => n.id === selectedNode.id)
      if (updated) setSelectedNode(updated)
    } catch (err) {
      console.error(err)
    }
  }, [selectedNode])

  const handleBondCreated = useCallback(async (fromId, toId) => {
    const fromNode = data?.nodes.find(n => n.id === fromId)
    const toNode = data?.nodes.find(n => n.id === toId)
    if (fromNode?.category === 'archived' || toNode?.category === 'archived') {
      alert('ACTION_DENIED: Cannot forge bonds with archived memory (White Dwarfs).')
      return
    }
    try {
      await createBond(fromId, toId)
      setBondMode(false)
      setTimeout(refetch, 500)
    } catch (err) {
      console.error(err)
    }
  }, [data, refetch])

  const handleBondDeleted = useCallback(async (fromId, toId) => {
    if (!confirm('Break bond?')) return
    try {
      await deleteBond(fromId, toId)
      setTimeout(refetch, 500)
    } catch (err) {
      console.error(err)
    }
  }, [refetch])

  // 3-state community toggle: highlight -> isolate -> reset
  const handleCommunityClick = useCallback(async (communityId) => {
    if (!communityFilter || communityFilter.id !== communityId) {
      setCommunityFilter({ id: communityId, phase: 'highlight' })
      try {
        const result = await fetchCommunity(communityId)
        setCommSummary(result.summary || 'No summary available for this community.')
      } catch {
        setCommSummary(null)
      }
    } else if (communityFilter.phase === 'highlight') {
      setCommunityFilter({ id: communityId, phase: 'isolate' })
      isolateCommunity(communityId)
    } else {
      setCommunityFilter(null)
      setCommSummary(null)
      restoreFullData()
    }
  }, [communityFilter, isolateCommunity, restoreFullData])

  const handleRunJanitor = useCallback(async () => {
    const result = await runJanitor()
    setTimeout(() => { refetch(); refetchHealth() }, 800)
    return result
  }, [refetch, refetchHealth])

  const handleClickShard = useCallback((shardId) => {
    if (!data) return
    const node = data.nodes.find(n => n.id === shardId)
    if (node) setSelectedNode(node)
  }, [data])

  // Escape key handler
  useEffect(() => {
    function handleKeyDown(e) {
      if (e.key === 'Escape') {
        if (communityFilter) {
          setCommunityFilter(null)
          setCommSummary(null)
          restoreFullData()
        }
        setSelectedNode(null)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [communityFilter, restoreFullData])

  if (loading && !data) {
    return (
      <div style={{
        background: 'var(--bg-color)',
        color: 'var(--accent)',
        height: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontFamily: 'var(--font-mono)',
        fontSize: '14px',
        letterSpacing: '2px',
      }}>
        INITIALIZING_MESH...
      </div>
    )
  }

  return (
    <>
      <BackgroundField />
      {/* D3 Graph */}
      <MeshGraph
        data={data}
        changeType={changeType}
        onSelectNode={handleSelectNode}
        onBondCreated={handleBondCreated}
        onBondDeleted={handleBondDeleted}
        bondMode={bondMode}
        focusedNodeId={selectedNode?.id || null}
        onZoomReady={handleZoomReady}
      />

      {/* Left Sidebar */}
      <CommandRail
        data={data}
        health={health}
        searchLoading={searchLoading}
        onSearch={handleSearch}
        onReset={handleReset}
        onCommunityClick={handleCommunityClick}
      />

      {/* Community Summary */}
      {commSummary && (
        <div className="community-summary" style={{ display: 'block' }}>
          <div className="comm-label">COMMUNITY_SUMMARY</div>
          <div>{commSummary}</div>
        </div>
      )}

      {/* Bond Mode Badge */}
      {bondMode && <div className="bond-mode-badge">&#x25CF; BOND_EDIT_MODE</div>}

      {/* Right Sidebar */}
      <EntityInspector
        node={selectedNode}
        degree={selectedDegree}
        links={data?.links ?? []}
        nodes={data?.nodes ?? []}
        onClose={() => setSelectedNode(null)}
        onEvict={handleEvict}
        onRefreshMetrics={handleRefreshMetrics}
        onSelectNode={handleSelectNode}
      />

      {/* Bottom Toolbar */}
      <CommandBar
        bondMode={bondMode}
        onToggleBondMode={() => { setBondMode(prev => !prev); setSelectedNode(null); }}
        zoomControls={zoomControls}
        onRunJanitor={handleRunJanitor}
      />

      {/* Activity Feed */}
      <ActivityFeed
        entries={entries}
        online={online}
        onClickShard={handleClickShard}
        onClear={clearEntries}
      />
    </>
  )
}
