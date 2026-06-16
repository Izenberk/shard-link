import { useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { Panel, HudButton, HealthBar } from './design-system'
import { COMM_COLORS } from './MeshGraph'

const GLOSSARY = [
  { label: 'SURVIVAL_SCORE', text: 'Retention probability (0-100), computed by the Survival Formula v4.1:\nS = min(95, (D × (C+1) × 10 × Sal) / e^(Δt / S₀))\nwhere S₀ = S_base(Sal) × (1 + A(m)).\n\n• 95+: Protected (cap for non-core).\n• 50-95: Healthy, actively bonded.\n• 20-50: At-risk, eviction candidate.\n• < 20: Immediate Janitor target.' },
  { label: 'SALIENCE', text: 'LLM-scored importance [0.1, 1.0] assigned at save time. Controls initial decay window via S_base.\n\n• 1.0: Core identity / critical decisions → 14-day window.\n• 0.5: Normal project context → ~7-day window.\n• 0.1: Ephemeral notes → 1-day window.\n\n10× decay difference between 0.1 and 1.0 shards.' },
  { label: 'NEURAL_DENSITY', text: 'Count of semantic bonds (CONNECTED_TO relationships). Multiplies the Survival numerator directly.\n\n• > 5: Dense Hub — critical contextual node.\n• 2-5: Well-connected, healthy.\n• 0-1: Orphan — eviction candidate unless high salience.' },
  { label: 'RELATIONAL_CENTRALITY', text: 'PageRank score from Neo4j GDS. Measures structural importance across the entire mesh, not just local bonds.\n\n• > 0.8: Central knowledge hub.\n• 0.2-0.8: Well-connected within community.\n• < 0.2: Peripheral / transient detail.' },
  { label: 'RETRIEVAL_ACTIVATION', text: 'ACT-R cognitive model: A(m) = ln(Σ tᵢ⁻⁰·⁵) + 0.1\nRolling window of last 20 retrieval timestamps. Recent retrievals contribute more than old ones.\n\nExtends survival window as S₀ × (1 + A(m)) — retrieval can never collapse survival, only extend it.\n\n• ~0.1: No retrieval history.\n• ~1.5: Strong recent use (2.5× window).\n• ~3.0: Very active (4× window).' },
  { label: 'RELATIONAL_COMMUNITY', text: 'Louvain clustering ID (N_#). Shards in the same community share a distinct semantic domain. Updated every ~10 min by The Synthesizer.\n\nCommunities > size threshold get LLM-synthesized summaries. Click a community badge to view its summary.' },
  { label: 'MESH_SENSITIVITY', text: 'Minimum cosine similarity for auto-bonding (MESH_LINK_THRESHOLD). Default: 0.75.\n\n• > 0.85: Strong semantic match.\n• 0.75-0.85: Bond created at threshold.\n• < 0.75: No automatic bond.\n\nLink tooltip shows SIM: the actual cosine similarity weight of each bond.' },
  { label: 'IDENTITY_ANCHOR', text: 'Immutable "Core" shards — survival locked at 100, immune to Janitor eviction. Represent foundational user identity.\n\nNon-core shards with cosine similarity ≥ 0.70 to any core shard receive resonance protection from eviction.' },
  { label: 'THE_JANITOR', text: 'Background eviction process. Scans for shards below the resonance threshold (default: 0.70) and with low survival scores.\n\nProtected from eviction:\n• Core shards (always).\n• Shards resonant to core (cosine ≥ 0.70).\n• Shards above survival threshold.' },
  { label: 'THE_SYNTHESIZER', text: 'Background linker that autonomously bonds resonant shards. Runs every ~10 minutes:\n\n1. Computes cosine similarity between shard embeddings.\n2. Creates CONNECTED_TO bonds above MESH_SENSITIVITY.\n3. Runs Louvain community detection.\n4. Runs PageRank for centrality scoring.\n5. Generates LLM summaries for large communities.' },
  { label: 'RESONANT_BONDS', text: 'Associative connections formed when two shards exceed the cosine similarity threshold.\n\n• Weight: Cosine similarity (0.00–1.00). Higher = stronger resonance.\n• Dots (●●●○○): Peer survival score. Fewer filled dots = peer is fading toward eviction.\n• Click any bond to follow the association to the connected shard.' },
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
  const [topologyOpen, setTopologyOpen] = useState(true)

  const handleSearch = useCallback(() => {
    if (searchTerm.trim()) onSearch(searchTerm.trim())
  }, [searchTerm, onSearch])

  const handleKeyPress = useCallback((e) => {
    if (e.key === 'Enter') handleSearch()
  }, [handleSearch])

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
      {/* Search — always visible */}
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

      {/* Topology toggle */}
      <button
        className="topology-toggle"
        onClick={() => setTopologyOpen(prev => !prev)}
        type="button"
        aria-label={topologyOpen ? 'Collapse topology panel' : 'Expand topology panel'}
      >
        <span className={`topology-toggle-chevron ${topologyOpen ? 'open' : ''}`}>&#x25B8;</span>
        <span className="topology-toggle-label">MESH_TOPOLOGY</span>
        <span className="topology-toggle-count">
          {data?.nodes?.length || 0} / {data?.links?.length || 0}
        </span>
      </button>

      {/* Topology — collapsible */}
      <div className={`topology-panel ${topologyOpen ? 'topology-panel--open' : ''}`}>
        <Panel titleHint="Visual representation of shard relationships, communities, and centrality.">
          {/* Legend */}
          <div className="legend-item legend-core">
            <div className="legend-color legend-color--core" />
            IDENTITY_ANCHOR
          </div>
          <div className="legend-item legend-archived">
            <div className="legend-color legend-color--archived" />
            ARCHIVED_MEMORY
          </div>
          <div className="legend-item">
            <div className="legend-color-group">
              {COMM_COLORS.map((c, i) => (
                <div key={i} className="legend-color" style={{ background: c, boxShadow: `0 0 6px ${c}` }} />
              ))}
            </div>
            <span className="legend-color-label">COMMUNITY_COLORS</span>
          </div>
          <div className="legend-encoding">SIZE = CENTRALITY | GLOW = SURVIVAL</div>

          {/* Neighborhoods */}
          <div className="rail-section">
            <h4 className="rail-section-title">ACTIVE_NEIGHBORHOODS</h4>
            <div className="neighborhood-grid">
              {communities.map(c => (
                <HudButton
                  key={c}
                  onClick={() => onCommunityClick(Number(c))}
                  className="neighborhood-btn"
                >
                  N_{c} ({commCounts[c]})
                </HudButton>
              ))}
            </div>
          </div>

          {/* Stats */}
          <div className="mesh-stats">
            MESH_SENSITIVITY: <span className="stat-value">{(data?.threshold || 0).toFixed(2)}</span> |{' '}
            SHARDS: <span className="stat-value">{data?.nodes?.length || 0}</span> |{' '}
            BONDS: <span className="stat-value">{data?.links?.length || 0}</span>
          </div>

          {/* Health Distribution */}
          <div className="rail-section rail-section--bordered">
            <h4 className="rail-section-title">
              SURVIVAL_DISTRIBUTION{' '}
              <span className="stat-value">({health?.total || 0})</span>
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
          <div className="rail-section rail-section--bordered">
            <HudButton onClick={() => setGlossaryOpen(!glossaryOpen)} className="glossary-toggle-btn">
              {glossaryOpen ? 'HIDE_GLOSSARY' : 'SHOW_GLOSSARY'}
            </HudButton>
          </div>
        </Panel>
      </div>

      {/* Glossary — portaled to body so it escapes the rail's transform context */}
      {glossaryOpen && createPortal(
        <>
          <div className="glossary-backdrop" onClick={() => setGlossaryOpen(false)} />
          <div className="glossary-overlay">
            <div className="detail-header">
              <span className="detail-header-title">MESH_GLOSSARY</span>
              <button
                className="panel-close-btn"
                onClick={() => setGlossaryOpen(false)}
                type="button"
                aria-label="Close glossary"
              >
                &times;
              </button>
            </div>
            <div className="glossary-body">
              {GLOSSARY.map(g => (
                <div className="detail-field" key={g.label}>
                  <span className="detail-label">{g.label}</span>
                  <div className="glossary-text">{g.text}</div>
                </div>
              ))}
            </div>
          </div>
        </>,
        document.body
      )}
    </div>
  )
}
