import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { createAgyRuntime } from '../lib/runtime.js'
import { mapModels } from '../lib/models.js'

const entries = ['medium', 'high', 'low'].map((level) => ({ modelId: `gemini-future-${level}`, name: `Gemini Future (${level})` }))

test('native catalog preserves new IDs, defaults and only advertised thinking variants', () => {
  const models = mapModels([...entries, entries[0], { modelId: 'single', name: 'Single' }], entries[1].name)
  assert.equal(models.length, 4)
  assert.equal(models[1].default, true)
  assert.deepEqual(models[0].efforts, ['low', 'medium', 'high'])
  assert.deepEqual(models[3].efforts, [])
  assert.equal(models[0].default_effort, 'medium')
})

async function fixture(t) {
  const home = mkdtempSync(join(tmpdir(), 'agy-models-'))
  t.after(() => rmSync(home, { recursive: true, force: true }))
  const catalog = (value) => writeFileSync(join(home, 'catalog.json'), JSON.stringify(value))
  catalog({ models: entries })
  const agyDir = join(home, 'settings')
  mkdirSync(agyDir)
  const settings = (value) => writeFileSync(join(agyDir, 'settings.json'), JSON.stringify(value))
  settings({ model: entries[1].name, agentMode: 'auto' })
  const bin = join(home, 'agy')
  const fake = fileURLToPath(new URL('./fake-acp-models.mjs', import.meta.url))
  writeFileSync(bin, `#!/bin/sh\nexec '${process.execPath}' '${fake}' '${home}' "$@"\n`)
  chmodSync(bin, 0o755)
  const n = await startFakeNode(t)
  const runtime = createAgyRuntime({ home, agyDir, agyBin: bin, personadBin: '/missing', socketPath: n.path, log: () => {} })
  t.after(() => runtime.stop())
  runtime.start()
  await n.ready
  const { session } = await n.peer.call('session.start', { cwd: home })
  const sessionId = session.session_id
  const row = () => n.last('runtime.sessions')?.sessions.find((entry) => entry.session_id === sessionId)
  return { n, catalog, settings, sessionId, row, session, calls: () => readFileSync(join(home, 'calls.jsonl'), 'utf8').trim().split('\n').map(JSON.parse) }
}

test('catalog discovery and refresh use native CLI data and current settings', async (t) => {
  const { n, catalog, settings } = await fixture(t)
  let result = await n.peer.call('launchers.models', {})
  assert.equal(result.models.find((model) => model.default)?.id, entries[1].modelId)
  catalog({ models: [...entries, { modelId: 'new-native-model', name: 'New Native Model' }] })
  settings({ model: 'new-native-model' })
  result = await n.peer.call('launchers.models', { refresh: true })
  assert.equal(result.models.at(-1).default, true)
  await until('native configuration', () => n.last('runtime.launchers')?.launchers[0].model === 'New Native Model')
  catalog({ error: 'Please sign in' })
  await assert.rejects(n.peer.call('launchers.models', { refresh: true }), /Please sign in/)
})

test('session controls confirm native state and preserve acknowledged fields on failure', async (t) => {
  const { n, sessionId, row, session } = await fixture(t)
  assert.equal(n.hello.caps.model, true)
  assert.equal(n.hello.caps.models, true)
  assert.equal(session.model, entries[0].modelId)
  assert.equal(session.permission_mode, 'auto')
  assert.equal(session.effort, 'medium')
  await until('empty owned session remains listed', () => row()?.status === 'idle')
  await n.peer.call('session.modes', { session_id: sessionId, model: entries[1].modelId, permission_mode: 'auto' })
  await until('native configuration', () => row()?.model === entries[1].modelId && row()?.permission_mode === 'auto')
  await assert.rejects(n.peer.call('session.modes', { session_id: sessionId, model: 'missing' }), /Model unavailable/)
  assert.equal(row().model, entries[1].modelId)
  await n.peer.call('session.modes', { session_id: sessionId, effort: 'low' })
  await until('native configuration', () => row()?.model === entries[2].modelId && row()?.effort === 'low')
  await assert.rejects(n.peer.call('session.modes', { session_id: sessionId, model: entries[1].modelId, effort: 'max' }), /Thinking level unavailable/)
  await until('native configuration', () => row()?.model === entries[1].modelId && row()?.effort === 'high')
  await n.peer.call('session.modes', { session_id: sessionId, permission_mode: 'bypassPermissions' })
  await until('native configuration', () => row()?.permission_mode === 'bypassPermissions')
})

test('a rejected startup configuration closes the newly created native session', async (t) => {
  const { n, calls } = await fixture(t)
  await assert.rejects(n.peer.call('session.start', { model: 'missing' }), /Model unavailable/)
  assert.equal(calls().filter((call) => call.method === 'session/close').length, 1)
})
