import { DetailField, HudButton } from './design-system'

export default function EntityInspector({ node, degree, onClose, onEvict, onRefreshMetrics }) {
  if (!node) return null

  const isEvictable = node.category !== 'core' && node.category !== 'archived'

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
      </div>
    </div>
  )
}
