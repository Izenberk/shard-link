import { useState, useEffect, useRef, useCallback } from 'react'
import { fetchLogs } from '../lib/api'

const MAX_ENTRIES = 200

function normalizeTimestamp(ts) {
  if (!ts) return new Date().toLocaleTimeString('en-GB')
  // YYYY-MM-DD HH:MM:SS (UTC from SQLite) — parse and convert to local
  if (/^\d{4}-\d{2}-\d{2}/.test(ts)) {
    const d = new Date(ts + 'Z')
    if (!isNaN(d)) return d.toLocaleTimeString('en-GB')
  }
  // Already HH:MM:SS — pass through
  if (/^\d{2}:\d{2}:\d{2}$/.test(ts)) return ts
  return ts
}

let keyCounter = 0

export default function useActivityStream() {
  const [entries, setEntries] = useState([])
  const [online, setOnline] = useState(false)
  const lastLogTimestampRef = useRef('')

  const addEntry = useCallback((entry) => {
    const normalized = {
      ...entry,
      timestamp: normalizeTimestamp(entry.timestamp),
      _key: `log-${++keyCounter}`,
    }
    setEntries(prev => {
      const next = [normalized, ...prev]
      if (next.length > MAX_ENTRIES) next.length = MAX_ENTRIES
      return next
    })
  }, [])

  const clearEntries = useCallback(() => setEntries([]), [])

  // SSE connection with exponential backoff
  useEffect(() => {
    let retryDelay = 1000
    const maxRetryDelay = 30000
    let evtSource = null
    let retryTimeout = null
    let cancelled = false

    function connect() {
      if (cancelled) return
      evtSource = new EventSource('/api/activity')

      evtSource.onmessage = (event) => {
        const entry = JSON.parse(event.data)
        addEntry(entry)
      }

      evtSource.onopen = () => {
        setOnline(true)
        retryDelay = 1000
      }

      evtSource.onerror = () => {
        setOnline(false)
        evtSource.close()
        addEntry({
          timestamp: new Date().toLocaleTimeString('en-GB'),
          type: 'system',
          message: `SSE disconnected. Reconnecting in ${retryDelay / 1000}s...`,
        })
        retryTimeout = setTimeout(() => {
          connect()
          retryDelay = Math.min(retryDelay * 2, maxRetryDelay)
        }, retryDelay)
      }
    }

    connect()

    return () => {
      cancelled = true
      if (evtSource) evtSource.close()
      if (retryTimeout) clearTimeout(retryTimeout)
    }
  }, [addEntry])

  // Load persistent logs on mount
  useEffect(() => {
    async function hydrate() {
      try {
        const history = await fetchLogs()
        if (history.length > 0) {
          lastLogTimestampRef.current = history[0].timestamp || ''
        }
        // Reverse to add oldest first (newest ends up at top)
        const reversed = [...history].reverse()
        reversed.forEach(entry => addEntry(entry))
      } catch { /* silent */ }
    }
    hydrate()
  }, [addEntry])

  // Poll for Hub-written logs every 5 seconds
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const logs = await fetchLogs()
        if (logs.length === 0) return
        const newestTs = logs[0].timestamp || ''
        if (newestTs === lastLogTimestampRef.current) return

        const newEntries = []
        for (const entry of logs) {
          if ((entry.timestamp || '') <= lastLogTimestampRef.current) break
          newEntries.push(entry)
        }
        newEntries.reverse().forEach(entry => addEntry(entry))
        lastLogTimestampRef.current = newestTs
      } catch { /* silent */ }
    }, 5000)

    return () => clearInterval(interval)
  }, [addEntry])

  return { entries, online, clearEntries }
}
