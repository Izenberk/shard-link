export default function HudButton({ children, onClick, loading, style, className = '', title }) {
  return (
    <button
      className={`hud-btn ${loading ? 'loading' : ''} ${className}`}
      onClick={loading ? undefined : onClick}
      style={style}
      title={title}
      disabled={loading}
      type="button"
    >
      {children}
    </button>
  )
}
