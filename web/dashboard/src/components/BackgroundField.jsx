import { useEffect, useRef } from 'react'

const COLORS = [
  [94, 243, 255],   // cyan
  [88, 166, 255],   // blue
  [163, 113, 247],  // purple
]

export default function BackgroundField() {
  const canvasRef = useRef(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const ctx = canvas.getContext('2d')
    let animId
    let w = window.innerWidth
    let h = window.innerHeight

    function resize() {
      w = window.innerWidth
      h = window.innerHeight
      canvas.width = w
      canvas.height = h
    }
    resize()
    window.addEventListener('resize', resize)

    const particles = Array.from({ length: 90 }, () => {
      const color = COLORS[Math.floor(Math.random() * COLORS.length)]
      return {
        x: Math.random() * w,
        y: Math.random() * h,
        vx: (Math.random() - 0.5) * 0.22,
        vy: (Math.random() - 0.5) * 0.22,
        r: Math.random() * 1.0 + 0.3,
        opacity: Math.random() * 0.10 + 0.03,
        color,
      }
    })

    function draw() {
      ctx.clearRect(0, 0, w, h)
      particles.forEach(p => {
        p.x = (p.x + p.vx + w) % w
        p.y = (p.y + p.vy + h) % h
        ctx.beginPath()
        ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2)
        ctx.fillStyle = `rgba(${p.color.join(',')}, ${p.opacity})`
        ctx.fill()
      })
      animId = requestAnimationFrame(draw)
    }

    draw()

    // Pause animation when tab is hidden — no point burning GPU for invisible frames
    function onVisibilityChange() {
      if (document.hidden) {
        cancelAnimationFrame(animId)
      } else {
        draw()
      }
    }
    document.addEventListener('visibilitychange', onVisibilityChange)

    return () => {
      cancelAnimationFrame(animId)
      window.removeEventListener('resize', resize)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [])

  return (
    <>
      {/* Nebula blobs */}
      <div className="bg-nebula" aria-hidden="true">
        <div className="bg-nebula-blob bg-nebula-blob--1" />
        <div className="bg-nebula-blob bg-nebula-blob--2" />
        <div className="bg-nebula-blob bg-nebula-blob--3" />
      </div>
      {/* Star particle canvas */}
      <canvas
        ref={canvasRef}
        style={{ position: 'fixed', inset: 0, zIndex: 1, pointerEvents: 'none' }}
      />
    </>
  )
}
