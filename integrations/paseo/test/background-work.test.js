import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createBackgroundWork } from '../lib/background-work.js'
import { agentRow, nativeBackgroundOf } from '../lib/map.js'
import { agentSnapshot } from './fake-paseo.js'

const tick = () => new Promise((resolve) => setImmediate(resolve))
const job = (id) => ({ id, kind: 'shell', description: null, startedAt: '2026-11-16T00:00:00Z' })
function setup(request) {
  return createBackgroundWork({ request, changed() {}, log() {} })
}

test('native background events replace a pending older inventory reply, including completion', async () => {
  let resolve
  const inventory = setup(() => new Promise((done) => { resolve = done }))
  inventory.connect(true)
  inventory.observe({ id: 'a', status: 'idle', backgroundWork: { count: 1 } })
  await tick()
  assert.equal(inventory.pending('a'), true)
  inventory.update({ agentId: 'a', items: [] })
  resolve({ agentId: 'a', items: [job('old')], error: null })
  await tick()
  assert.deepEqual(inventory.items('a'), [])
  assert.equal(inventory.busy('a'), false)
})

test('disconnected replies cannot overwrite the inventory of a new connection', async () => {
  const answers = []
  const inventory = setup(() => new Promise((resolve) => answers.push(resolve)))
  const agent = { id: 'a', status: 'idle', backgroundWork: { count: 1 } }
  inventory.connect(true)
  inventory.observe(agent)
  await tick()
  inventory.disconnect()
  inventory.connect(true)
  inventory.observe(agent)
  await tick()
  answers[1]({ agentId: 'a', items: [job('new')], error: null })
  await tick()
  answers[0]({ agentId: 'a', items: [job('old')], error: null })
  await tick()
  assert.equal(inventory.items('a')[0].id, 'new')
})

test('legacy daemons and unloaded agents are never queried for native work', async () => {
  let reads = 0
  const inventory = setup(async (agentId) => { reads++; return { agentId, items: [], error: null } })
  inventory.connect(false)
  inventory.observe({ id: 'a', status: 'idle', backgroundWork: { count: 1 } })
  inventory.refresh()
  await tick()
  inventory.connect(true)
  inventory.observe({ id: 'a', status: 'closed', backgroundWork: { count: 1 } })
  inventory.observe({ id: 'b', status: 'idle', archivedAt: '2026-11-16T00:00:00Z', backgroundWork: { count: 1 } })
  await tick()
  assert.equal(reads, 0)
})

test('removal and an authoritative zero summary invalidate pending background replies', async () => {
  for (const remove of [true, false]) {
    let resolve
    const inventory = setup(() => new Promise((done) => { resolve = done }))
    inventory.connect(true)
    inventory.observe({ id: 'a', status: 'idle', backgroundWork: { count: 1 } })
    await tick()
    if (remove) inventory.forget('a')
    else inventory.observe({ id: 'a', status: 'idle', backgroundWork: { count: 0 } })
    resolve({ agentId: 'a', items: [job('old')], error: null })
    await tick()
    assert.equal(inventory.busy('a'), false)
    assert.deepEqual(inventory.items('a'), [])
  }
})

test('native jobs coexist with delegated tasks and never appear on an archived row', () => {
  const agent = agentSnapshot({ provider: 'codex', status: 'idle' })
  const child = agentSnapshot({ provider: 'codex', status: 'running' })
  const options = { roots: { codex: 'codex' }, nativeBackground: [job('native')], fleet: [child] }
  assert.deepEqual(agentRow(agent, options).background.map((t) => t.id), ['native', `paseo-${child.id}`])
  assert.equal(agentRow(agent, { ...options, archived: true }).background, undefined)
  assert.equal(nativeBackgroundOf({ id: '', startedAt: job('x').startedAt }), undefined)
  assert.equal(nativeBackgroundOf({ id: 'bad-time', startedAt: 'invalid' }), undefined)
})

test('native detail reads are bounded and failures retry only on the next refresh', async () => {
  const pending = []
  let reads = 0
  const inventory = setup((agentId) => { reads++; return new Promise((resolve) => pending.push(() => resolve({ agentId, items: [], error: 'Temporarily unavailable' }))) })
  inventory.connect(true)
  for (let n = 0; n < 8; n++) inventory.observe({ id: `a${n}`, status: 'idle', backgroundWork: { count: 1 } })
  await tick()
  assert.equal(reads, 4)
  pending.splice(0).forEach((done) => done())
  await tick()
  assert.equal(reads, 8)
  pending.splice(0).forEach((done) => done())
  await tick()
  assert.equal(reads, 8, 'failed reads do not busy-loop')
  assert.equal(inventory.pending('a0'), true, 'a failed read preserves known work')
  inventory.refresh()
  await tick()
  assert.equal(reads, 12)
  inventory.disconnect()
  pending.splice(0).forEach((done) => done())
  await tick()
})
