export default function DetailField({ label, hint, value, children }) {
  return (
    <div className="detail-field">
      <span className="detail-label" title={hint}>{label}</span>
      {value !== undefined && <span className="detail-value"> {value}</span>}
      {children}
    </div>
  )
}
