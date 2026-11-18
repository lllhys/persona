import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readlinkSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { PACKAGES, packageHash, readShipped } from '../../runtime-sdk/lib/package.js'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { installAntigravityPackage, PACKAGE_NAME } from '../install.mjs'
import { createAgyRuntime } from '../lib/runtime.js'

const INTEGRATIONS_ROOT = join(dirname(fileURLToPath(import.meta.url)), '../..')

const FAKE_AGY_SOURCE = `
import { existsSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { createInterface } from 'node:readline'

const stateDir = process.argv[2]
if (process.argv.includes('models')) {
  console.log('test-model\\tTest Model')
  process.exit(0)
}

const send = (obj) => process.stdout.write(JSON.stringify(obj) + '\\n')
let pendingPermId = null
let pendingPromptId = null

function maybeFinishPrompt() {
  if (pendingPromptId === null || pendingPermId !== null) return
  if (!existsSync(join(stateDir, 'release-turn'))) {
    setTimeout(maybeFinishPrompt, 15).unref?.()
    return
  }
  const id = pendingPromptId
  pendingPromptId = null
  send({ jsonrpc: '2.0', id, result: { stopReason: 'end_turn' } })
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
rl.on('line', (line) => {
  if (!line.trim()) return
  const msg = JSON.parse(line)
  if (msg.id !== undefined && !msg.method) {
    if (msg.id === pendingPermId) {
      pendingPermId = null
      writeFileSync(join(stateDir, 'perm-answered'), '1')
      maybeFinishPrompt()
    }
    return
  }
  if (msg.method === 'session/cancel') {
    if (pendingPromptId !== null) {
      const id = pendingPromptId
      pendingPromptId = null
      pendingPermId = null
      send({ jsonrpc: '2.0', id, result: { stopReason: 'cancelled' } })
    }
    return
  }
  switch (msg.method) {
    case 'initialize':
      send({ jsonrpc: '2.0', id: msg.id, result: { protocolVersion: 1, agentInfo: { name: 'fake-agy', version: '1.0.0' }, agentCapabilities: {} } })
      break
    case 'session/new':
      send({ jsonrpc: '2.0', id: msg.id, result: { sessionId: '22222222-3333-4444-8555-666666666666' } })
      break
    case 'session/resume':
    case 'session/set_model':
    case 'session/set_mode':
    case 'session/set_config_option':
    case 'session/close':
      send({ jsonrpc: '2.0', id: msg.id, result: {} })
      break
    case 'session/prompt': {
      const text = msg.params?.prompt?.find((b) => b.type === 'text')?.text?.trim() || ''
      if (text === 'hold-with-card') {
        pendingPromptId = msg.id
        pendingPermId = 'perm-1'
        send({
          jsonrpc: '2.0',
          id: pendingPermId,
          method: 'session/request_permission',
          params: {
            sessionId: msg.params.sessionId,
            toolCall: { toolCallId: 'call-1', title: 'run_command', rawInput: { command: 'echo ok' } },
            options: [
              { optionId: 'allow', name: 'Allow', kind: 'allow_once' },
              { optionId: 'reject', name: 'Reject', kind: 'reject_once' },
            ],
          },
        })
      } else {
        send({ jsonrpc: '2.0', id: msg.id, result: { stopReason: 'end_turn' } })
      }
      break
    }
    default:
      send({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: 'Method not found' } })
  }
})
rl.on('close', () => process.exit(0))
`

function makeEnv(t) {
  const dir = mkdtempSync(join(tmpdir(), 'agy-update-'))
  const fakeScript = join(dir, 'fake-agy.mjs')
  const binary = join(dir, 'agy')
  const agyDir = join(dir, 'agy-config')
  const store = join(dir, 'store')
  const socketPath = join(dir, 'runtime.sock')
  mkdirSync(agyDir, { recursive: true })
  writeFileSync(fakeScript, FAKE_AGY_SOURCE)
  writeFileSync(binary, `#!/bin/sh\nexec '${process.execPath}' '${fakeScript}' '${dir}' "$@"\n`)
  chmodSync(binary, 0o700)
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  return { dir, binary, agyDir, store, socketPath }
}

function offerOf(marker) {
  const files = readShipped(INTEGRATIONS_ROOT, PACKAGES[PACKAGE_NAME]).map((f) =>
    f.path === 'antigravity/companion.mjs'
      ? { path: f.path, data: Buffer.concat([f.data, Buffer.from(`\n// ${marker}\n`)]) }
      : f,
  )
  return {
    name: PACKAGE_NAME,
    hash: packageHash(files),
    version: 'node-next',
    files: files.map((f) => ({ path: f.path, data: f.data.toString('base64') })),
  }
}

test('a non-store launch sends no package hash and ignores node update offers', async (t) => {
  const { dir, binary, agyDir, socketPath } = makeEnv(t)
  const node = await startFakeNode(t, { path: socketPath })
  node.offer = offerOf('v2')
  const runtime = createAgyRuntime({
    agyBin: binary,
    agyDir,
    home: dir,
    socketPath: node.path,
    retryMs: 20,
    log: () => {},
  })
  t.after(() => runtime.stop())
  runtime.start()
  await node.ready
  assert.equal(node.hello.package, undefined)
  assert.equal(node.packageCalls.length, 0)
  assert.equal(runtime.idle(), true)
})

test('an idle store runtime installs the offered package as current and exits for its supervisor', async (t) => {
  const { dir, binary, agyDir, store, socketPath } = makeEnv(t)
  const exits = []
  const node = await startFakeNode(t, { path: socketPath })
  const v2 = offerOf('v2')
  node.offer = v2
  const runtime = createAgyRuntime({
    agyBin: binary,
    agyDir,
    home: dir,
    socketPath: node.path,
    retryMs: 20,
    log: () => {},
    update: { name: PACKAGE_NAME, hash: 'running-v1', store, exit: (h) => exits.push(h) },
  })
  t.after(() => runtime.stop())
  runtime.start()
  await until('exit called', () => exits.length === 1)
  assert.deepEqual(node.hello.package, { name: PACKAGE_NAME, hash: 'running-v1' })
  assert.equal(exits[0], v2.hash)
  assert.equal(readlinkSync(join(store, 'current')), v2.hash)
})

test('waits for an open permission card and active ACP turn to settle before exiting onto the installed version', async (t) => {
  const { dir, binary, agyDir, store, socketPath } = makeEnv(t)
  const exits = []
  let answerCard
  const node = await startFakeNode(t, { path: socketPath })
  node.resolve = () => new Promise((r) => { answerCard = r })
  const runtime = createAgyRuntime({
    agyBin: binary,
    agyDir,
    home: dir,
    socketPath: node.path,
    retryMs: 20,
    idleCheckMs: 25,
    log: () => {},
    update: { name: PACKAGE_NAME, hash: 'running-v1', store, exit: (h) => exits.push(h) },
  })
  t.after(() => runtime.stop())
  runtime.start()
  await node.ready

  await node.peer.call('session.start', { cwd: dir, prompt: 'hold-with-card' })
  await until('permission card reaches phone', () => node.asked.length === 1 && typeof answerCard === 'function')
  assert.equal(runtime.idle(), false)

  // Node upgrades and reconnects while both the card and ACP prompt are in flight.
  const v2 = offerOf('v2-while-busy')
  node.offer = v2
  node.peer.close(new Error('node upgraded'))
  await until('new version installed in store', () => {
    try { return readlinkSync(join(store, 'current')) === v2.hash } catch { return false }
  })
  assert.equal(exits.length, 0, 'does not exit while permission card and turn are active')

  // Answering the card on the reconnected node leaves the ACP turn still running until release-turn is written.
  await until('card re-asked on upgraded node', () => node.asked.length >= 2)
  answerCard({ allow: true })
  await until('agy received permission answer', () => existsSync(join(dir, 'perm-answered')))
  assert.equal(runtime.idle(), false)
  assert.equal(exits.length, 0, 'does not exit while ACP turn is still executing')

  writeFileSync(join(dir, 'release-turn'), '1')
  await until('exits once turn finishes', () => exits.length === 1)
  assert.equal(exits[0], v2.hash)
})

test('rejects a tampered package without touching current or retrying the bad hash', async (t) => {
  const { dir, binary, agyDir, store, socketPath } = makeEnv(t)
  const exits = []
  const node = await startFakeNode(t, { path: socketPath })
  const v2 = offerOf('tampered')
  node.offer = {
    ...v2,
    files: v2.files.map((f) =>
      f.path === 'antigravity/companion.mjs' ? { ...f, data: Buffer.from('corrupted').toString('base64') } : f,
    ),
  }
  const runtime = createAgyRuntime({
    agyBin: binary,
    agyDir,
    home: dir,
    socketPath: node.path,
    retryMs: 20,
    log: () => {},
    update: { name: PACKAGE_NAME, hash: 'running-v1', store, exit: (h) => exits.push(h) },
  })
  t.after(() => runtime.stop())
  runtime.start()
  await until('package fetched once', () => node.packageCalls.length === 1)
  const hellosBefore = node.hellos
  node.peer.close(new Error('reconnect'))
  await until('reconnected', () => node.hellos > hellosBefore)
  assert.equal(node.packageCalls.length, 1, 'bad hash is not fetched again')
  assert.throws(() => readlinkSync(join(store, 'current')))
  assert.equal(exits.length, 0)
})

test('install.mjs seeds the store and companion.mjs self-updates unless --no-update is passed', async (t) => {
  const { binary, agyDir } = makeEnv(t)
  const socketDir = mkdtempSync(join(tmpdir(), 'jrt-agy-'))
  t.after(() => rmSync(socketDir, { recursive: true, force: true }))
  const socketPath = join(socketDir, 'runtime.sock')
  const node = await startFakeNode(t, { path: socketPath })
  const v2 = offerOf('v2-companion-e2e')
  node.offer = v2

  const { store, current, hash: v1Hash } = installAntigravityPackage({ socketPath })
  assert.equal(readlinkSync(current), v1Hash)
  const entry = join(current, 'antigravity', 'companion.mjs')

  // With --no-update, the store companion omits hello.package and keeps running.
  const noUpdateProc = spawn(process.execPath, [entry, '--socket', socketPath, '--agy-bin', binary, '--agy-dir', agyDir, '--no-update'], {
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  t.after(() => { try { noUpdateProc.kill('SIGKILL') } catch {} })
  await until('connected with --no-update', () => node.hellos === 1)
  assert.equal(node.hello.package, undefined)
  assert.equal(node.packageCalls.length, 0)
  assert.equal(readlinkSync(current), v1Hash)
  const stoppedNoUpdate = once(noUpdateProc, 'exit')
  noUpdateProc.kill('SIGTERM')
  const [noUpdateCode] = await stoppedNoUpdate
  assert.equal(noUpdateCode, 0)

  // Without --no-update, the store companion installs v2 as current and exits 0; restarting runs v2.
  const updateProc = spawn(process.execPath, [entry, '--socket', socketPath, '--agy-bin', binary, '--agy-dir', agyDir], {
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  t.after(() => { try { updateProc.kill('SIGKILL') } catch {} })
  const [updateCode] = await once(updateProc, 'exit')
  assert.equal(updateCode, 0)
  assert.deepEqual(node.hello.package, { name: PACKAGE_NAME, hash: v1Hash })
  assert.equal(node.packageCalls.length, 1)
  assert.equal(readlinkSync(current), v2.hash)
  assert.ok(existsSync(join(store, v1Hash)), 'previous version stays alongside current')

  const restartedProc = spawn(process.execPath, [entry, '--socket', socketPath, '--agy-bin', binary, '--agy-dir', agyDir], {
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  t.after(() => { try { restartedProc.kill('SIGKILL') } catch {} })
  await until('restarted companion connected on v2', () => node.hellos >= 3 && node.hello?.package?.hash === v2.hash)
  assert.equal(node.packageCalls.length, 1, 'v2 matches node offer so no further package fetch occurs')
  const stoppedRestarted = once(restartedProc, 'exit')
  restartedProc.kill('SIGTERM')
  const [restartedCode] = await stoppedRestarted
  assert.equal(restartedCode, 0)
})
