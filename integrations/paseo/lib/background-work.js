// Paseo's native background inventory (0.16+). Summaries announce work before the optional detail read finishes;
// full-list events and newer snapshots supersede an in-flight reply. No timeline or provider-loading calls.
export function createBackgroundWork({ request, changed, log }) {
  const records = new Map()
  const queued = new Set()
  let enabled = false
  let connected = false
  let epoch = { active: 0 }

  function reset() {
    epoch = { active: 0 }
    queued.clear()
    for (const r of records.values()) {
      r.request = null
      r.dirty = true
      r.revision++
    }
  }

  function enqueue(id) {
    const r = records.get(id)
    if (!enabled || !connected || !r?.dirty || !r.count || r.request) return
    queued.add(id)
    pump()
  }

  function pump() {
    while (connected && epoch.active < 4 && queued.size) {
      const id = queued.values().next().value
      queued.delete(id)
      const r = records.get(id)
      if (!r?.dirty || !r.count || r.request) continue
      const current = epoch
      const revision = r.revision
      const token = {}
      r.request = token
      r.dirty = false
      current.active++
      let failed = false
      Promise.resolve().then(() => request(id)).then((p) => {
        if (current !== epoch || records.get(id) !== r || revision !== r.revision) return
        if (p?.error || !Array.isArray(p?.items) || p.agentId !== id) throw new Error(p?.error || 'Invalid background work inventory')
        r.items = p.items
        r.count = p.items.length
        changed()
      }).catch((error) => {
        if (current !== epoch || records.get(id) !== r || revision !== r.revision) return
        failed = true
        r.dirty = true // a failure says nothing; the next sweep retries it
        log(`paseo: reading background work: ${error.message}`)
      }).finally(() => {
        if (current !== epoch) return
        current.active--
        if (r.request === token) r.request = null
        if (!failed && records.get(id) === r) enqueue(id)
        pump()
      })
    }
  }

  return {
    connect(supported) {
      reset()
      enabled = supported
      connected = true
      if (!enabled) records.clear()
    },
    disconnect() {
      connected = false
      reset()
    },
    observe(agent) {
      if (!enabled) return
      const count = agent.backgroundWork?.count
      if (agent.archivedAt || agent.status === 'closed' || count === 0) {
        records.delete(agent.id)
        queued.delete(agent.id)
        return
      }
      if (!Number.isSafeInteger(count) || count < 0) return // unavailable is not empty
      let r = records.get(agent.id)
      if (!r) {
        r = { count, items: [], revision: 0, dirty: true, request: null }
        records.set(agent.id, r)
      } else if (r.count !== count) {
        r.count = count
        r.revision++
        r.dirty = true
      }
      enqueue(agent.id)
    },
    update({ agentId, items }) {
      if (!enabled || !Array.isArray(items)) return
      const r = records.get(agentId) ?? { revision: 0, request: null }
      r.revision++
      r.items = items
      r.count = items.length
      r.dirty = false
      records.set(agentId, r)
      queued.delete(agentId)
      changed()
    },
    refresh() {
      for (const [id, r] of records) {
        if (!r.request) r.dirty = true
        enqueue(id)
      }
    },
    forget(id) {
      records.delete(id)
      queued.delete(id)
    },
    items: (id) => records.get(id)?.items ?? [],
    pending: (id) => (records.get(id)?.count ?? 0) > (records.get(id)?.items.length ?? 0),
    busy: (id) => (records.get(id)?.count ?? 0) > 0,
  }
}
