export default function Panel({ title, titleHint, children, className = '', style }) {
  return (
    <div className={`panel ${className}`} style={style}>
      {title && (
        <h3 className="panel-title" title={titleHint}>
          {title}
        </h3>
      )}
      {children}
    </div>
  )
}
