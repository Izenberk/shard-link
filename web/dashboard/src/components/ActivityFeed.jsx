import { useState, useCallback } from 'react'
import { StatusDot, LogEntry } from './design-system'

const LOG_TYPES = ['success', 'bond', 'evict', 'info', 'warn', 'system', 'search', 'error']

export default function ActivityFeed({ entries, online, onClickShard, onClear }) {
  const [activeFilters, setActiveFilters] = useState(() => new Set(LOG_TYPES))

  const toggleFilter = useCallback((type) => {
    setActiveFilters(prev => {
      const next = new Set(prev)
      if (next.has(type)) next.delete(type)
      else next.add(type)
      return next
    })
  }, [])

  return (
    <div className="activity-feed">
      <div className="activity-header">
        <span>SILICON_ACTIVITY_FEED</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <StatusDot online={online} />
          <span
            onClick={onClear}
            style={{ cursor: 'pointer', color: 'var(--accent)', fontSize: '9px', letterSpacing: '1px' }}
          >
            CLEAR
          </span>
        </div>
      </div>

      {/* Filter bar */}
      <div className="log-filter-bar">
        {LOG_TYPES.map(type => (
          <div
            key={type}
            className={`log-filter-btn ${activeFilters.has(type) ? 'active' : 'inactive'}`}
            data-type={type}
            onClick={() => toggleFilter(type)}
          >
            {type.toUpperCase()}
          </div>
        ))}
      </div>

      {/* Log entries */}
      <div className="log-container">
        {entries.map((entry, i) => (
          <LogEntry
            key={entry._key || i}
            timestamp={entry.timestamp}
            type={entry.type || 'info'}
            message={entry.message}
            hidden={!activeFilters.has(entry.type || 'info')}
            onClick={entry.shard_id ? () => onClickShard(entry.shard_id) : undefined}
          />
        ))}
      </div>
    </div>
  )
}
