const BASE = '' // Vite proxy handles /api/* in dev; same-origin in production

export async function fetchGraph() {
  const resp = await fetch(`${BASE}/api/graph`)
  if (!resp.ok) throw new Error(`Graph fetch failed: ${resp.status}`)
  return resp.json()
}

export async function searchGraph(query) {
  const resp = await fetch(`${BASE}/api/search?q=${encodeURIComponent(query)}`)
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(`Search failed (${resp.status}): ${text}`)
  }
  return resp.json()
}

export async function evictShard(id) {
  const resp = await fetch(`${BASE}/api/evict?id=${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(`Eviction failed: ${text}`)
  }
}

export async function createBond(fromId, toId) {
  const resp = await fetch(`${BASE}/api/bonds`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from_id: fromId, to_id: toId, weight: 1.0 }),
  })
  if (!resp.ok) throw new Error(`Bond creation failed: ${resp.status}`)
}

export async function deleteBond(fromId, toId) {
  const resp = await fetch(
    `${BASE}/api/bonds?from=${encodeURIComponent(fromId)}&to=${encodeURIComponent(toId)}`,
    { method: 'DELETE' }
  )
  if (!resp.ok) throw new Error(`Bond deletion failed: ${resp.status}`)
}

export async function fetchCommunity(communityId) {
  const resp = await fetch(`${BASE}/api/community?id=${communityId}`)
  if (!resp.ok) throw new Error(`Community fetch failed: ${resp.status}`)
  return resp.json()
}

export async function fetchHealth() {
  const resp = await fetch(`${BASE}/api/health`)
  if (!resp.ok) throw new Error(`Health fetch failed: ${resp.status}`)
  return resp.json()
}

export async function fetchLogs() {
  const resp = await fetch(`${BASE}/api/logs`)
  if (!resp.ok) throw new Error(`Logs fetch failed: ${resp.status}`)
  return resp.json()
}
