import { ToolButton } from './design-system'

export default function CommandBar({ bondMode, onToggleBondMode, zoomControls }) {
  return (
    <div className="bottom-toolbar">
      <div className="toolbar-group">
        <ToolButton onClick={() => zoomControls?.zoomIn?.()} title="Zoom In">+</ToolButton>
        <ToolButton onClick={() => zoomControls?.zoomOut?.()} title="Zoom Out">-</ToolButton>
      </div>
      <div className="toolbar-divider" />
      <ToolButton
        onClick={onToggleBondMode}
        style={{
          fontSize: '10px',
          letterSpacing: '1px',
          borderColor: bondMode ? 'var(--core-color)' : 'transparent',
        }}
      >
        BOND_MODE: {bondMode ? 'ON' : 'OFF'}
      </ToolButton>
      <div className="toolbar-divider" />
      <ToolButton onClick={() => zoomControls?.resetView?.()} title="Recenter View">
        &#x2316;
      </ToolButton>
    </div>
  )
}
