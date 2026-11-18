import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs, { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { syncBuiltinESMExports } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { startFakeNode, until } from '../testing/fake-node.js'
import { createLoader } from '../lib/loader.js'
import { packageHash, readShipped, verifyPackage } from '../lib/package.js'

const sdk = join(dirname(fileURLToPath(import.meta.url)), '..')
const NAME = 'fixture-runtime'
const PATTERNS = ['impl/package.json', 'impl/*.js', 'runtime-sdk/package.json', 'runtime-sdk/index.js', 'runtime-sdk/lib/*.js']

// A runtime implementation for the loader, on the real SDK: its version says which copy runs. ctx.busy says whether a
// card is out (the loader waits for none); ctx.starts records every start.
const impl = (version, extra = '') => `import { createRuntime } from '../runtime-sdk/index.js'
export const api = 1
${extra}
export async function start(host) {
  await host.beforeStart?.('${version}')
  const rt = createRuntime({ listSessions: async () => [], items: async () => ({ items: [], has_more: false, to_seq: 0 }) },
    { runtime: 'fixture', version: '${version}', socketPath: host.socket, package: host.package, onUpdate: host.onUpdate, retryMs: 20 })
  rt.start()
  host.instances.push(rt)
  host.starts.push('${version}')
  await host.started?.('${version}')
  return { stop: async () => { host.stops.push('${version}'); await host.beforeStop?.('${version}'); return rt.stop() }, busy: () => host.busy(), fetchPackage: (name) => rt.fetchPackage(name) }
}
`

/** An integrations directory with the fixture runtime (impl/) beside a copy of this SDK. */
function shippedRoot(version, extra) {
  const root = mkdtempSync('/tmp/jrt-loader-')
  mkdirSync(join(root, 'impl'))
  writeFileSync(join(root, 'impl', 'package.json'), '{"type":"module"}\n')
  writeFileSync(join(root, 'impl', 'plugin.js'), impl(version, extra))
  mkdirSync(join(root, 'runtime-sdk', 'lib'), { recursive: true })
  for (const f of ['package.json', 'index.js']) cpSync(join(sdk, f), join(root, 'runtime-sdk', f))
  cpSync(join(sdk, 'lib'), join(root, 'runtime-sdk', 'lib'), { recursive: true })
  return root
}

/** The package a node would carry for a version: what runtime.package answers. */
function packageOf(version, extra) {
  const root = shippedRoot(version, extra)
  const files = readShipped(root, PATTERNS)
  rmSync(root, { recursive: true, force: true })
  return { name: NAME, hash: packageHash(files), version: `node-${version}`, files: files.map((f) => ({ path: f.path, data: f.data.toString('base64') })) }
}

async function setup(t, { version = 'v1', root, store, autoUpdate } = {}) {
  const n = await startFakeNode(t)
  root ??= shippedRoot(version)
  store ??= mkdtempSync('/tmp/jrt-store-')
  const host = { socket: n.path, starts: [], stops: [], instances: [], busy: () => false }
  const logs = []
  const loader = createLoader({ name: NAME, patterns: PATTERNS, root, entry: 'impl/plugin.js', store, autoUpdate, context: host,
    log: (m) => logs.push(m), idleEveryMs: 20 })
  t.after(async () => {
    await loader.stop()
    for (const rt of host.instances) rt.stop()
    rmSync(root, { recursive: true, force: true })
    rmSync(store, { recursive: true, force: true })
  })
  return { n, root, store, host, loader, logs }
}

test('the hash is the same function as personad\'s (server/internal/runtimepkg TestHashVector)', () => {
  const files = [{ path: 'b/c.js', data: Buffer.from('y') }, { path: 'a.js', data: Buffer.from('x') }]
  assert.equal(packageHash(files), 'd6f3031b914b7e5243281ea484c24fc9ef36488a09bf7fe78df53a1101f06b8f')
})

test('runs the shipped version and says its package in hello; the same package from the node changes nothing', async (t) => {
  const { n, root, loader } = await setup(t)
  n.offer = packageOf('v1') // the node carries what the runtime runs
  await loader.start()
  await n.ready
  const shipped = packageHash(readShipped(root, PATTERNS))
  assert.equal(n.offer.hash, shipped)
  assert.deepEqual(n.hello.package, { name: NAME, hash: shipped })
  assert.equal(n.hello.version, 'v1')
  assert.deepEqual(loader.running, { hash: shipped, shipped: true })
  await new Promise((r) => setTimeout(r, 100))
  assert.equal(n.packageCalls.length, 0)
})

test('takes the node\'s package: fetches, checks, stores, switches, and starts from the store next time', async (t) => {
  const { n, root, store, host, loader } = await setup(t)
  const v2 = packageOf('v2')
  n.offer = v2
  await loader.start()
  await until('the switch to v2', () => n.hello?.version === 'v2', 5000)
  assert.equal(n.hello.package.hash, v2.hash)
  assert.deepEqual(host.starts, ['v1', 'v2'])
  assert.equal(n.packageCalls.length, 1)
  const current = JSON.parse(readFileSync(join(store, 'current.json'), 'utf8'))
  assert.equal(current.hash, v2.hash)
  assert.equal(current.shipped, packageHash(readShipped(root, PATTERNS)))
  assert.equal(current.version, 'node-v2')
  assert.ok(existsSync(join(store, v2.hash, 'impl', 'plugin.js')))
  assert.deepEqual(loader.running, { hash: v2.hash, shipped: false })

  // Restarted (DSH restarts): the stored version runs at once, no new fetch.
  await loader.stop()
  const again = createLoader({ name: NAME, patterns: PATTERNS, root, entry: 'impl/plugin.js', store, context: host, log: () => {} })
  t.after(() => again.stop())
  const hellos = n.hellos
  await again.start()
  await until('the next hello', () => n.hellos > hellos)
  assert.equal(n.hello.version, 'v2')
  assert.equal(n.packageCalls.length, 1)
})

test('a copy installed by hand over the shipped files wins over the version the node sent before', async (t) => {
  const { n, root, store, host, loader } = await setup(t)
  n.offer = packageOf('v2')
  await loader.start()
  await until('the switch to v2', () => n.hello?.version === 'v2', 5000)
  await loader.stop()
  n.offer = undefined
  writeFileSync(join(root, 'impl', 'plugin.js'), impl('v3-by-hand'))
  const again = createLoader({ name: NAME, patterns: PATTERNS, root, entry: 'impl/plugin.js', store, context: host, log: () => {} })
  t.after(() => again.stop())
  await again.start()
  await until('the hand-installed copy', () => n.hello?.version === 'v3-by-hand')
  assert.equal(again.running.shipped, true)
})

test('files that do not hash to what the node says are refused, and not fetched again', async (t) => {
  const { n, host, loader, logs } = await setup(t)
  const v2 = packageOf('v2')
  n.offer = { ...v2, files: v2.files.map((f) => (f.path === 'impl/plugin.js' ? { ...f, data: Buffer.from(impl('evil')).toString('base64') } : f)) }
  await loader.start()
  await until('the refusal', () => logs.some((l) => /update to .* failed: .*hash to/.test(l)), 5000)
  assert.equal(loader.running.shipped, true)
  assert.deepEqual(host.starts, ['v1'])
  await loader.offer({ name: NAME, hash: v2.hash })
  assert.equal(n.packageCalls.length, 1)
  assert.throws(() => verifyPackage({ name: NAME, hash: v2.hash, files: [{ path: '../x.js', data: '' }] }, NAME), /bad file/)
})

test('a version that fails to start: back to the one that ran', async (t) => {
  const { n, host, loader, logs } = await setup(t)
  n.offer = packageOf('v2-broken', "throw new Error('broken on import')")
  await loader.start()
  await until('the rollback', () => logs.some((l) => /failed: broken on import/.test(l)), 5000)
  await until('v1 connected again', () => n.hello?.version === 'v1' && loader.running?.shipped === true)
  assert.deepEqual(host.starts, ['v1', 'v1'])
})

test('waits until no card is out on the phone before switching', async (t) => {
  const { n, host, loader } = await setup(t)
  let busy = true
  host.busy = () => busy
  n.offer = packageOf('v2')
  await loader.start()
  await until('the package fetched', () => n.packageCalls.length === 1)
  await new Promise((r) => setTimeout(r, 200))
  assert.deepEqual(host.starts, ['v1'], 'not while a card is out')
  busy = false
  await until('the switch', () => n.hello?.version === 'v2', 5000)
})

test('autoUpdate off (a daemon): reports its package, never switches', async (t) => {
  const { n, host, loader } = await setup(t, { autoUpdate: false })
  n.offer = packageOf('v2')
  await loader.start()
  await n.ready
  await new Promise((r) => setTimeout(r, 200))
  assert.equal(n.packageCalls.length, 0)
  assert.deepEqual(host.starts, ['v1'])
})

test('stopped while it switches: the version being started is stopped too, not left connected', async (t) => {
  const { n, loader } = await setup(t)
  n.offer = packageOf('v2', 'await new Promise((r) => setTimeout(r, 300))') // its import takes a while
  await loader.start()
  await until('the package fetched', () => n.packageCalls.length === 1)
  await new Promise((r) => setTimeout(r, 100)) // v1 stopped, v2 importing
  await loader.stop()
  await new Promise((r) => setTimeout(r, 600))
  assert.equal(loader.running, undefined)
  assert.ok(!n.peer || n.peer.closed, `still connected as ${n.hello?.version}`)
})

test('a failed version receipt rolls back to one connected previous version', async (t) => {
  const { n, root, store, host, loader, logs } = await setup(t)
  const v1 = packageHash(readShipped(root, PATTERNS))
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
  }
  n.offerUpdates = false
  n.offer = v2
  mkdirSync(join(store, 'current.json'))
  await loader.offer(v2)
  // v2 connected before the receipt failed. Wait for its close when it was stopped; on the defective loader, wait for
  // its leaked socket too so the assertion below pins the actual two connections rather than racing the second hello.
  await until('the receipt failure and rollback', () => n.hello?.version === 'v1' && loader.running?.hash === v1 &&
    logs.some((l) => /update to .* failed: .*current\.json/.test(l)) &&
    n.peers.size === (host.stops.includes('v2') ? 1 : 2), 5000)
  assert.equal(n.peers.size, 1)
  assert.deepEqual(loader.running, { hash: v1, shipped: true })

  const fetches = n.packageCalls.length
  await loader.offer(v2)
  assert.equal(n.packageCalls.length, fetches, 'the failed hash is not tried again')

  await loader.stop()
  await until('every runtime disconnected', () => n.peers.size === 0)
})

test('a cleanup failure keeps the new version connected and its receipt', async (t) => {
  const { n, store, host, loader, logs } = await setup(t)
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  const stale = join(store, '0'.repeat(64))
  mkdirSync(stale)
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
  }
  n.offerUpdates = false
  n.offer = v2
  const originalRmSync = fs.rmSync
  t.mock.method(fs, 'rmSync', (path, options) => {
    if (path === stale) throw new Error('cleanup blocked')
    return originalRmSync(path, options)
  })
  syncBuiltinESMExports()
  try {
    await loader.offer(v2)
  } finally {
    t.mock.restoreAll()
    syncBuiltinESMExports()
  }
  assert.deepEqual(host.starts, ['v1', 'v2'], 'nothing was rolled back')
  await until('v2 connected alone', () => n.hello?.version === 'v2' && n.peers.size === 1, 5000)
  assert.deepEqual(loader.running, { hash: v2.hash, shipped: false })
  assert.equal(JSON.parse(readFileSync(join(store, 'current.json'), 'utf8')).hash, v2.hash)
  assert.equal(logs.filter((l) => /cleanup after switching .* failed: cleanup blocked; keeping the new version/.test(l)).length, 1)
  await loader.stop()
  await until('every runtime disconnected', () => n.peers.size === 0)
})

test('stop while rollback stops the new version leaves nothing connected and does not restart the old one', async (t) => {
  const { n, store, host, loader } = await setup(t)
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
  }
  let releaseStop
  const stoppingV2 = new Promise((resolve) => { releaseStop = resolve })
  host.beforeStop = (version) => version === 'v2' ? stoppingV2 : undefined
  n.offerUpdates = false
  n.offer = v2
  mkdirSync(join(store, 'current.json'))
  const switching = loader.offer(v2)
  await until('rollback is stopping v2', () => host.stops.includes('v2'), 5000)
  const stopping = loader.stop()
  releaseStop()
  await Promise.all([switching, stopping])
  await until('every runtime disconnected', () => n.peers.size === 0)
  assert.equal(loader.running, undefined)
  assert.deepEqual(host.starts, ['v1', 'v2'], 'the previous version did not start after stop')
})

test('stop while rollback restarts the previous version stops its late start', async (t) => {
  const { n, store, host, loader } = await setup(t)
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  let releaseStart
  const startingV1 = new Promise((resolve) => { releaseStart = resolve })
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
    if (version === 'v1' && host.starts.join(',') === 'v1,v2,v1') await startingV1
  }
  n.offerUpdates = false
  n.offer = v2
  mkdirSync(join(store, 'current.json'))
  const switching = loader.offer(v2)
  await until('the previous version is restarting', () => host.starts.join(',') === 'v1,v2,v1' && n.hello?.version === 'v1', 5000)
  assert.equal(loader.running, undefined, 'the failed version is not reported while the previous one starts again')
  assert.equal(loader.instance, undefined, 'the failed version is not reported while the previous one starts again')
  const stopping = loader.stop()
  releaseStart()
  await Promise.all([switching, stopping])
  await until('every runtime disconnected', () => n.peers.size === 0)
  assert.equal(loader.running, undefined)
  assert.deepEqual(host.starts, ['v1', 'v2', 'v1'], 'no other version started after stop')
})

test('the previous version failing to restart after a receipt failure leaves nothing connected', async (t) => {
  const { n, store, host, loader, logs } = await setup(t)
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  host.beforeStart = (version) => {
    if (version === 'v1' && host.starts.length === 2) throw new Error('v1 cannot restart')
  }
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
  }
  n.offerUpdates = false
  n.offer = v2
  mkdirSync(join(store, 'current.json'))
  await loader.offer(v2)
  await until('the failed restart is logged and everything disconnected', () => n.peers.size === 0 &&
    logs.some((l) => /did not start again: v1 cannot restart/.test(l)), 5000)
  assert.equal(loader.running, undefined)
  assert.deepEqual(host.starts, ['v1', 'v2'])
  assert.equal(logs.filter((l) => /did not start again: v1 cannot restart/.test(l)).length, 1)
})

test('a valid offer after a receipt rollback switches and writes its receipt', async (t) => {
  const { n, root, store, host, loader } = await setup(t)
  const v1 = packageHash(readShipped(root, PATTERNS))
  const v2 = packageOf('v2')
  await loader.start()
  await n.ready
  host.started = async (version) => {
    if (version === 'v2') await until('v2 connected', () => n.hello?.version === 'v2')
  }
  n.offerUpdates = false
  n.offer = v2
  mkdirSync(join(store, 'current.json'))
  await loader.offer(v2)
  await until('the previous version connected after rollback', () => n.hello?.version === 'v1' && loader.running?.hash === v1 &&
    n.peers.size === 1, 5000)

  rmSync(join(store, 'current.json'), { recursive: true })
  const v3 = packageOf('v3')
  n.offer = v3
  await loader.offer(v3)
  await until('v3 connected alone', () => n.hello?.version === 'v3' && n.peers.size === 1, 5000)
  assert.deepEqual(loader.running, { hash: v3.hash, shipped: false })
  assert.equal(JSON.parse(readFileSync(join(store, 'current.json'), 'utf8')).hash, v3.hash)
  assert.deepEqual(host.starts, ['v1', 'v2', 'v1', 'v3'])
  await loader.stop()
  await until('every runtime disconnected', () => n.peers.size === 0)
})
