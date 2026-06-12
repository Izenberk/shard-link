export default function HealthBar({ label, count, maxCount, color }) {
  const pct = maxCount > 0 ? Math.round((count / maxCount) * 100) : 0
  return (
    <div className="health-row">
      <span className="health-label">{label}</span>
      <div className="health-bar-bg">
        <div
          className="health-bar-fill"
          style={{ width: `${pct}%`, background: color }}
        />
      </div>
      <span className="health-count">{count}</span>
    </div>
  )
}
