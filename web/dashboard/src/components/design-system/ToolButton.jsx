export default function ToolButton({ children, onClick, title, style }) {
  return (
    <div className="tool-btn" onClick={onClick} title={title} style={style}>
      {children}
    </div>
  )
}
