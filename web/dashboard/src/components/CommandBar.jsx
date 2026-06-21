import { useState } from 'react'
import { ToolButton } from './design-system'

export default function CommandBar({ bondMode, onToggleBondMode, zoomControls, onRunJanitor, onRunSynthesizer }) {
  const [janitorRunning, setJanitorRunning] = useState(false)
  const [synthRunning, setSynthRunning] = useState(false)

  async function handleJanitor() {
    if (janitorRunning) return
    setJanitorRunning(true)
    try {
      await onRunJanitor?.()
    } finally {
      setJanitorRunning(false)
    }
  }

  async function handleSynthesizer() {
    if (synthRunning) return
    setSynthRunning(true)
    try {
      await onRunSynthesizer?.()
    } finally {
      setSynthRunning(false)
    }
  }

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
      <ToolButton
        onClick={handleJanitor}
        title="Run Janitor — evict low-survival shards"
        style={{
          fontSize: '10px',
          letterSpacing: '1px',
          opacity: janitorRunning ? 0.5 : 1,
          borderColor: janitorRunning ? 'var(--accent)' : 'transparent',
        }}
      >
        {janitorRunning ? 'JANITOR...' : 'RUN_JANITOR'}
      </ToolButton>
      <div className="toolbar-divider" />
      <ToolButton
        onClick={handleSynthesizer}
        title="Run Synthesizer — forge semantic bonds immediately"
        style={{
          fontSize: '10px',
          letterSpacing: '1px',
          opacity: synthRunning ? 0.5 : 1,
          borderColor: synthRunning ? 'var(--accent)' : 'transparent',
        }}
      >
        {synthRunning ? 'SYNTH...' : 'RUN_SYNTH'}
      </ToolButton>
      <div className="toolbar-divider" />
      <ToolButton onClick={() => zoomControls?.resetView?.()} title="Recenter View">
        &#x2316;
      </ToolButton>
    </div>
  )
}
