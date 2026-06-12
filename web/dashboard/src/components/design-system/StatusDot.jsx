export default function StatusDot({ online = true }) {
  return <div className={`status-dot ${online ? '' : 'offline'}`} />
}
