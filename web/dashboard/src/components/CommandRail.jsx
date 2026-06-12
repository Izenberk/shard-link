import { useState, useCallback } from 'react'
import { Panel, HudButton, HealthBar } from './design-system'
import { COMM_COLORS } from './MeshGraph'

const GLOSSARY = [
  { label: 'NEURAL_DENSITY', text: 'Connectivity count.\n• > 5: Dense Hub (Critical Context).\n• 0 - 1: Orphan Shard (Candidate for Eviction).' },
  { label: 'RELATIONAL_CENTRALITY', text: 'PageRank (Importance) Score.\n• > 1.0: Foundational Knowledge Node.\n• < 0.5: Peripheral / Transient detail.' },
  { label: 'SURVIVAL_SCORE', text: 'Probability of retention (0-100).\n• 90-100: Maximum safety (Core Shards).\n• < 20: Target for automated Janitor eviction.' },
  { label: 'RELATIONAL_COMMUNITY', text: 'Topical clustering ID (N_#). Shards in the same community share a distinct semantic domain.' },
  { label: 'IDENTITY_ANCHOR', text: 'Immutable "Core" fragments. These always maintain a Survival Score of 100.' },
]

const HEALTH_BUCKETS = [
  { label: '0-20', key: 'le_20', color: 'var(--health-critical)' },
  { label: '21-50', key: 'le_50', color: 'var(--health-low)' },
  { label: '51-80', key: 'le_80', color: 'var(--health-mid)' },
  { label: '81-95', key: 'le_95', color: 'var(--health-good)' },
  { label: '96-100', key: 'le_100', color: 'var(--health-excellent)' },
]

export default function CommandRail({
  data,
  health,
  searchLoading,
  onSearch,
  onReset,
  onCommunityClick,
}) {
  const [searchTerm, setSearchTerm] = useState('')
  const [glossaryOpen, setGlossaryOpen] = useState(false)

  const handleSearch = useCallback(() => {
    if (searchTerm.trim()) onSearch(searchTerm.trim())
  }, [searchTerm, onSearch])

  const handleKeyPress = useCallback((e) => {
    if (e.key === 'Enter') handleSearch()
  }, [handleSearch])

  // Compute communities from data
  const commCounts = {}
  if (data?.nodes) {
    data.nodes.forEach(n => {
      if (n.community !== undefined) {
        commCounts[n.community] = (commCounts[n.community] || 0) + 1
      }
    })
  }
  const communities = Object.keys(commCounts).sort((a, b) => a - b)

  const maxHealthCount = health
    ? Math.max(1, ...HEALTH_BUCKETS.map(b => health[b.key] || 0))
    : 1

  return (
    <div className="command-rail">
      {/* Search Panel */}
      <Panel title="SYSTEM_LOCATE" titleHint="The entry point for semantic and relational queries across the Knowledge Mesh.">
        <div className="search-container">
          <span className="search-icon">&#x2315;</span>
          <input
            type="text"
            placeholder="SEMANTIC_QUERY..."
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
            onKeyDown={handleKeyPress}
          />
        </div>
        <div className="btn-group">
          <HudButton
            onClick={handleSearch}
            loading={searchLoading}
            title="Execute a vector-similarity search to find resonant shards."
          >
            SEARCH
          </HudButton>
          <HudButton
            onClick={() => { setSearchTerm(''); onReset(); }}
            title="Reload the full Knowledge Mesh view."
          >
            RESET
          </HudButton>
        </div>
      </Panel>

      {/* Topology Panel */}
      <Panel title="MESH_TOPOLOGY" titleHint="Visual representation of shard relationships, communities, and centrality.">
        {/* Legend */}
        <div className="legend-item" style={{ color: 'var(--core-color)' }}>
          <div className="legend-color" style={{ background: 'var(--core-color)', boxShadow: '0 0 8px var(--core-color)' }} />
          IDENTITY_ANCHOR
        </div>
        <div className="legend-item" style={{ color: '#ffffff' }}>
          <div className="legend-color" style={{ background: '#ffffff', boxShadow: '0 0 8px rgba(255, 255, 255, 0.6)' }} />
          ARCHIVED_MEMORY
        </div>
        <div className="legend-item">
          <div style={{ display: 'flex', gap: '3px', marginRight: '10px' }}>
            {COMM_COLORS.map((c, i) => (
              <div key={i} className="legend-color" style={{ background: c, boxShadow: `0 0 6px ${c}` }} />
            ))}
          </div>
          <span style={{ color: 'var(--text-muted)' }}>COMMUNITY_COLORS</span>
        </div>
        <div className="legend-encoding">SIZE = CENTRALITY | GLOW = SURVIVAL</div>

        {/* Neighborhoods */}
        <div style={{ marginTop: '20px' }}>
          <h4 style={{ fontSize: '9px', color: 'var(--text-muted)', letterSpacing: '1px', marginBottom: '10px' }}>
            ACTIVE_NEIGHBORHOODS
          </h4>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '5px' }}>
            {communities.map(c => (
              <HudButton
                key={c}
                onClick={() => onCommunityClick(Number(c))}
                style={{ padding: '4px 8px', fontSize: '9px' }}
              >
                N_{c} ({commCounts[c]})
              </HudButton>
            ))}
          </div>
        </div>

        {/* Stats */}
        <div style={{ fontSize: '10px', color: 'var(--text-muted)', marginTop: '15px', fontFamily: 'var(--font-mono)' }}>
          MESH_SENSITIVITY: <span style={{ color: 'var(--accent)' }}>{(data?.threshold || 0).toFixed(2)}</span> |{' '}
          SHARDS: <span style={{ color: 'var(--accent)' }}>{data?.nodes?.length || 0}</span> |{' '}
          BONDS: <span style={{ color: 'var(--accent)' }}>{data?.links?.length || 0}</span>
        </div>

        {/* Health Distribution */}
        <div style={{ marginTop: '15px', borderTop: '1px solid var(--panel-border)', paddingTop: '12px' }}>
          <h4 style={{ fontSize: '9px', color: 'var(--text-muted)', letterSpacing: '1px', marginBottom: '8px' }}>
            SURVIVAL_DISTRIBUTION{' '}
            <span style={{ color: 'var(--accent)' }}>({health?.total || 0})</span>
          </h4>
          {HEALTH_BUCKETS.map(b => (
            <HealthBar
              key={b.label}
              label={b.label}
              count={health?.[b.key] || 0}
              maxCount={maxHealthCount}
              color={b.color}
            />
          ))}
        </div>

        {/* Glossary toggle */}
        <div style={{ marginTop: '20px', borderTop: '1px solid var(--panel-border)', paddingTop: '15px' }}>
          <HudButton onClick={() => setGlossaryOpen(!glossaryOpen)} style={{ fontSize: '10px' }}>
            {glossaryOpen ? 'HIDE_GLOSSARY' : 'SHOW_GLOSSARY'}
          </HudButton>
        </div>
      </Panel>

      {/* Glossary Overlay */}
      {glossaryOpen && (
        <div className="glossary-overlay active">
          <div className="detail-header">
            <span className="detail-header-title">MESH_GLOSSARY</span>
            <span
              style={{ cursor: 'pointer', color: 'var(--accent)' }}
              onClick={() => setGlossaryOpen(false)}
            >
              &times;
            </span>
          </div>
          <div style={{ padding: '30px' }}>
            {GLOSSARY.map(g => (
              <div className="detail-field" key={g.label}>
                <span className="detail-label">{g.label}</span>
                <div style={{ color: 'var(--text-main)', fontSize: '13px', whiteSpace: 'pre-line' }}>
                  {g.text}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
