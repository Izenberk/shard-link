export default function ToolButton({ children, onClick, title, style }) {
  return (
    <button
      className="tool-btn"
      onClick={onClick}
      title={title}
      style={style}
      type="button"
    >
      {children}
    </button>
  )
}
