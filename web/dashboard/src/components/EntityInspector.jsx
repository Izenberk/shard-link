import { DetailField, HudButton } from './design-system'

export default function EntityInspector({ node, degree, onClose, onEvict, onRefreshMetrics }) {
  if (!node) return null

  const isEvictable = node.category !== 'core' && node.category !== 'archived'

  return (
    <div className={`entity-inspector ${node ? 'active' : ''}`}>
      <div className="detail-header">
        <span className="detail-header-title">ENTITY_INSPECTOR</span>
        <span style={{ cursor: 'pointer', color: 'var(--accent)' }} onClick={onClose}>
          &times;
        </span>
      </div>
      <div className="detail-body">
        <DetailField
          label="SHARD_ID"
          hint="Unique identifier for this specific shard."
          value={<span style={{ color: 'var(--accent)' }}>{node.id || 'UNKNOWN'}</span>}
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

        {isEvictable && (
          <div style={{ marginTop: '15px', borderTop: '1px solid var(--panel-border)', paddingTop: '15px' }}>
            <HudButton
              onClick={onRefreshMetrics}
              style={{ marginBottom: '10px', background: 'rgba(94, 243, 255, 0.1)', borderColor: 'var(--core-color)', color: 'var(--core-color)', textAlign: 'center' }}
            >
              REFRESH_METRICS
            </HudButton>
            <HudButton
              onClick={onEvict}
              style={{ background: 'rgba(255, 82, 82, 0.1)', borderColor: '#ff5252', color: '#ff5252', textAlign: 'center' }}
            >
              EVICT_SHARD
            </HudButton>
          </div>
        )}

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
