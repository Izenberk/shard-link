import { DetailField, HudButton } from './design-system'

function truncateID(id) {
  if (!id) return ''
  return id.length > 24 ? id.substring(0, 21) + '...' : id
}

function survivalDots(score) {
  const filled = Math.round((score / 100) * 5)
  return '●'.repeat(filled) + '○'.repeat(5 - filled)
}

function survivalDotColor(score) {
  if (score >= 80) return '#00ff88'
  if (score >= 50) return '#5ef3ff'
  if (score >= 20) return '#ffaa00'
  return '#ff4444'
}

export default function EntityInspector({ node, degree, links, nodes, onClose, onEvict, onRefreshMetrics, onSelectNode }) {
  if (!node) return null

  const isEvictable = node.category !== 'core' && node.category !== 'community' && node.category !== 'archived'

  const resonantLinks = links
    ? links
        .filter(l => {
          const src = typeof l.source === 'object' ? l.source.id : l.source
          const tgt = typeof l.target === 'object' ? l.target.id : l.target
          return src === node.id || tgt === node.id
        })
        .sort((a, b) => (b.weight || 0) - (a.weight || 0))
        .filter((l, _, arr) => {
          const src = typeof l.source === 'object' ? l.source.id : l.source
          const tgt = typeof l.target === 'object' ? l.target.id : l.target
          const peerId = src === node.id ? tgt : src
          return arr.findIndex(x => {
            const xSrc = typeof x.source === 'object' ? x.source.id : x.source
            const xTgt = typeof x.target === 'object' ? x.target.id : x.target
            return (xSrc === node.id ? xTgt : xSrc) === peerId
          }) === arr.indexOf(l)
        })
    : []

  return (
    <div className={`entity-inspector ${node ? 'active' : ''}`}>
      <div className="detail-header">
        <span className="detail-header-title">ENTITY_INSPECTOR</span>
        <button
          className="panel-close-btn"
          onClick={onClose}
          type="button"
          aria-label="Close inspector"
        >
          &times;
        </button>
      </div>
      <div className="detail-body">
        {/* Identity group */}
        <div className="inspector-group">
          <div className="inspector-group-label">IDENTITY</div>
          <DetailField
            label="SHARD_ID"
            hint="Unique identifier for this specific shard."
            value={<span className="value-accent">{node.id || 'UNKNOWN'}</span>}
          />
          <DetailField
            label="SHARD_CATEGORY"
            hint="Classification category: core, memory, session, or archived."
            value={(node.category || '--').toUpperCase()}
          />
          <DetailField
            label="SOURCE_TYPE"
            hint="Ingestion source type: manual, github, chat, web_scrape."
            value={node.source_type || '--'}
          />
          <DetailField
            label="SOURCE_REF"
            hint="Source reference URI, file path, or identifier."
            value={node.source_ref || '--'}
          />
        </div>

        {/* Metrics group */}
        <div className="inspector-group">
          <div className="inspector-group-label">METRICS</div>
          <DetailField
            label="NEURAL_DENSITY"
            hint="Total number of semantic bonds connected to this shard."
            value={`${degree || 0} BONDS`}
          />
          <DetailField
            label="RELATIONAL_CENTRALITY"
            hint="Relational Centrality (PageRank) of this shard."
            value={(node.pagerank || 0).toFixed(4)}
          />
          <DetailField
            label="SURVIVAL_SCORE"
            hint="Estimated survival score based on centrality, density, and age."
            value={(node.survival || 0).toFixed(2)}
          />
        </div>

        {/* Context group */}
        <div className="inspector-group">
          <div className="inspector-group-label">CONTEXT</div>
          <DetailField
            label="TEMPORAL_MARK"
            hint="The precise timestamp when this shard was first committed to the Knowledge Mesh."
            value={node.created_at || '--'}
          />
          <DetailField
            label="RELATIONAL_COMMUNITY"
            hint="The community ID (neighborhood) this shard belongs to."
            value={node.community !== undefined ? `N_${node.community}` : 'N_NONE'}
          />
        </div>

        {/* Actions */}
        {isEvictable && (
          <div className="inspector-actions">
            <HudButton onClick={onRefreshMetrics} className="action-btn action-btn--refresh">
              REFRESH_METRICS
            </HudButton>
            <HudButton onClick={onEvict} className="action-btn action-btn--evict">
              EVICT_SHARD
            </HudButton>
          </div>
        )}

        {/* Raw content */}
        <DetailField
          label="RAW_COGNITIVE_CONTENT"
          hint="The original text content stored within this shard."
        >
          <div className="content-box">{node.content || 'NO_CONTENT'}</div>
        </DetailField>

        {/* RESONANT_BONDS */}
        <div className="resonant-bonds-section">
          <div className="resonant-bonds-header">
            <span
              className="detail-label"
              title="Associative bonds — other shards that resonated above the similarity threshold. Sorted by bond strength. Peer survival indicates whether the connected memory is thriving or fading."
            >
              RESONANT_BONDS
            </span>
            <span className="resonant-bonds-count">{resonantLinks.length}</span>
          </div>
          <div className="resonant-bonds-list">
            {resonantLinks.length === 0 ? (
              <div className="bond-empty">NO_RESONANCE_DETECTED</div>
            ) : (
              resonantLinks.map((l, i) => {
                const src = typeof l.source === 'object' ? l.source.id : l.source
                const tgt = typeof l.target === 'object' ? l.target.id : l.target
                const peerId = src === node.id ? tgt : src
                const peerNode = nodes ? nodes.find(n => n.id === peerId) : null
                const peerSurvival = peerNode ? (peerNode.survival || 0) : 0
                const peerCategory = peerNode ? (peerNode.category || '') : ''
                const weight = (l.weight || 0).toFixed(2)
                const displaySurvival = peerCategory === 'core' ? 100 : peerSurvival
                const dots = survivalDots(displaySurvival)
                const dotColor = survivalDotColor(displaySurvival)

                return (
                  <div
                    key={`${peerId}-${i}`}
                    className={`bond-item${peerCategory ? ` bond-peer-${peerCategory}` : ''}`}
                    onClick={() => { if (peerNode && onSelectNode) onSelectNode(peerNode) }}
                  >
                    <span className="bond-arrow">→</span>
                    <span className="bond-peer-id" title={peerId}>{truncateID(peerId)}</span>
                    <span className="bond-meta">
                      <span className="bond-weight">{weight}</span>
                      <span
                        className="bond-dots"
                        style={{ color: dotColor }}
                        title={`Peer survival: ${displaySurvival.toFixed(0)}`}
                      >
                        {dots}
                      </span>
                    </span>
                  </div>
                )
              })
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
