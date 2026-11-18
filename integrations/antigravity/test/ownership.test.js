import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { startFakeNode } from '../../runtime-sdk/testing/fake-node.js'
import { createAgyRuntime } from '../lib/runtime.js'

const FAKE = fileURLToPath(new URL('./fake-acp-stream.mjs', import.meta.url))
const RAW = '11111111-2222-4333-8444-555555555555'
const SESSION = `agy-${RAW}`
const CLAUDE = '6f1c2d3e-4b5a-4c6d-8e7f-90a1b2c3d4e5' // a Claude Code session Persona just started in a terminal: no transcript yet

// The node asks every connected runtime about a session nobody listed (session.items, limit 1) and routes it to the
// first that answers: AGY must refuse what is not its own, or a new Claude session's first message goes to agy.
test('AGY refuses a session id it did not hand out and still answers for its own', async (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'agy-ownership-'))
  writeFileSync(join(directory, 'updates.jsonl'), '')
  writeFileSync(join(directory, 'calls.jsonl'), '')
  const binary = join(directory, 'agy')
  writeFileSync(binary, `#!/bin/sh\nexec '${process.execPath}' '${FAKE}' '${directory}' "$@"\n`)
  chmodSync(binary, 0o700)
  const logs = join(directory, 'config/brain', RAW, '.system_generated/logs')
  mkdirSync(logs, { recursive: true })
  writeFileSync(join(logs, 'transcript.jsonl'), [
    { step_index: 0, source: 'USER_EXPLICIT', type: 'USER_INPUT', status: 'DONE', content: 'Read the report.' },
    { step_index: 1, source: 'MODEL', type: 'PLANNER_RESPONSE', status: 'DONE', content: 'Done.' },
  ].map((row) => JSON.stringify(row)).join('\n') + '\n')
  const node = await startFakeNode(t)
  const runtime = createAgyRuntime({ agyBin: binary, agyDir: join(directory, 'config'), home: directory, socketPath: node.path, log: () => {} })
  t.after(async () => { await runtime.stop(); rmSync(directory, { recursive: true, force: true }) })
  runtime.start()
  await node.ready

  for (const [method, params] of [
    ['session.items', { session_id: CLAUDE, limit: 1 }],
    ['session.item', { session_id: CLAUDE, item_id: '1#0' }],
    ['session.send', { session_id: CLAUDE, text: 'hi', client_msg_id: 'claude-first' }],
    ['session.interrupt', { session_id: CLAUDE }],
  ]) {
    await assert.rejects(node.peer.call(method, params), (err) => err.code === 'not_found', `${method} refuses a Claude session`)
  }
  const calls = readFileSync(join(directory, 'calls.jsonl'), 'utf8')
  assert.ok(!calls.includes(CLAUDE), 'agy never hears of the Claude session')

  const page = await node.peer.call('session.items', { session_id: SESSION, limit: 1 })
  assert.equal(page.items.length, 1)
  assert.equal(page.to_seq, 2)
  const full = await node.peer.call('session.items', { session_id: SESSION })
  assert.deepEqual(full.items.map((item) => item.text), ['Read the report.', 'Done.'])
})
