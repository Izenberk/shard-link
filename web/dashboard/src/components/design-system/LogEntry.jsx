export default function LogEntry({ timestamp, type = 'info', message, onClick, hidden }) {
  return (
    <div
      className="log-entry"
      data-logtype={type}
      onClick={onClick}
      style={hidden ? { display: 'none' } : undefined}
    >
      <span className="log-time">[{timestamp}]</span>
      <span className={`log-type-${type}`}>{message}</span>
    </div>
  )
}
