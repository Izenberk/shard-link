export default function HudButton({ children, onClick, loading, style, className = '', title }) {
  return (
    <div
      className={`hud-btn ${loading ? 'loading' : ''} ${className}`}
      onClick={loading ? undefined : onClick}
      style={style}
      title={title}
    >
      {children}
    </div>
  )
}
