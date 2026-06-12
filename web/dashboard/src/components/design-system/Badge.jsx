const CATEGORY_STYLES = {
  core: { background: 'rgba(255, 155, 58, 0.2)', color: 'var(--core-color)' },
  memory: { background: 'rgba(94, 243, 255, 0.15)', color: 'var(--accent)' },
  session: { background: 'rgba(163, 113, 247, 0.2)', color: 'var(--log-search)' },
  archived: { background: 'rgba(255, 255, 255, 0.15)', color: '#ffffff' },
}

export default function Badge({ category, children }) {
  const styles = CATEGORY_STYLES[category] || CATEGORY_STYLES.memory
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 6px',
        borderRadius: 'var(--radius-sm)',
        fontSize: 'var(--fs-sm)',
        letterSpacing: 'var(--ls-tight)',
        ...styles,
      }}
    >
      {children || (category || 'unknown').toUpperCase()}
    </span>
  )
}
