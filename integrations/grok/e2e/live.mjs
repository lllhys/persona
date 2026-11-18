// Opt-in real-provider check. Real: the Grok CLI (its model, tools and ACP turns) and this companion. Simulated: the
// Persona side (the runtime SDK's fake node stands in for the node, its Persona tool and the phone's permission
// answers), so no phone, app UI or hub is exercised. No user files or production hub are used; Grok keeps the
// temporary conversation in its own history.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { mkdir, mkdtemp, rm, access } from 'node:fs/promises'
import { homedir, tmpdir } from 'node:os'
import { join } from 'node:path'
import { startFakeNode, until } from '../../runtime-sdk/testing/fake-node.js'
import { GrokAcp } from '../lib/acp.js'
import { createGrokRuntime } from '../lib/runtime.js'

if (process.env.GROK_LIVE !== '1') throw new Error('Set GROK_LIVE=1 to make real Grok requests')
const home = await mkdtemp(join(tmpdir(), 'persona-grok-live-'))
const cwd = join(home, '.persona', 'assistant')
await mkdir(cwd, { recursive: true })
const nonce = randomUUID()
const node = await startFakeNode(undefined, { strict: true, tools: [{ name: 'integration_probe', description: 'Return the integration nonce supplied as value.', input_schema: { type: 'object', properties: { value: { type: 'string' } }, required: ['value'] } }] })
node.callTool = ({ args }) => ({ content: [{ type: 'text', text: `PROBE_OK:${args.value}` }] })
const options = { home, env: {}, socketPath: node.path, bin: process.env.GROK_BIN || 'grok',
  grokHome: process.env.GROK_HOME || join(homedir(), '.grok'), model: process.env.GROK_MODEL, effort: process.env.GROK_EFFORT,
  displayName: 'Grok', assistantFor: 'grok',
  log: (msg) => process.stderr.write(`${msg}\n`) }
// Each companion gets its own Grok ACP process, tapped for the stop reason Grok gives each turn (session/prompt).
const stopReasons = []
function companion(extra = {}) {
  const acp = new GrokAcp({ bin: options.bin, env: { ...process.env, GROK_HOME: options.grokHome }, log: options.log })
  const call = acp.call.bind(acp)
  acp.call = async (method, params, timeout) => {
    const result = await call(method, params, timeout)
    if (method === 'session/prompt') stopReasons.push(result?.stopReason)
    return result
  }
  return createGrokRuntime({ ...options, ...extra, acp })
}
let runtime = companion()
let id
async function send(text, extra = {}) {
  await node.peer.call('session.send', { session_id: id, text, mode: 'bypassPermissions', ...extra })
  const s = runtime.sessions.get(id)
  await s.tail
  const result = await node.peer.call('session.items', { session_id: id })
  const last = result.items.filter((i) => i.role === 'assistant' && i.kind === 'text').at(-1)?.text || ''
  assert.ok(!last.startsWith('API Error:'), last)
  return { result, last }
}
try {
  runtime.start()
  await node.ready
  const start = await node.peer.call('session.start', { cwd, assistant: true, model: process.env.GROK_MODEL, effort: process.env.GROK_EFFORT, permission_mode: 'bypassPermissions' })
  id = start.session.session_id
  console.log(JSON.stringify({ stage: 'created', model: start.session.model, effort: start.session.effort }))
  assert.equal(node.hello.caps.rename, true)
  assert.deepEqual(node.hello.launchers.map((l) => [l.name, l.display_name, l.assistant_for]), [['grok-chat', 'Grok', 'grok']])
  console.log(JSON.stringify({ stage: 'launcher_declared', success: true }))
  await node.peer.call('session.watch', { session_id: id, since_seq: 0 })
  await node.peer.call('session.rename', { session_id: id, title: 'Initial integration title' })
  const title = 'Renamed integration conversation'
  await node.peer.call('session.rename', { session_id: id, title })
  assert.equal((await runtime.driver.listSessions()).find((s) => s.session_id === id)?.title, title)
  console.log(JSON.stringify({ stage: 'native_rename', success: true }))
  const first = await send('Call the Persona integration_probe tool once with the nonce from the private context. Discover it with search_tool if needed. Reply with exactly its result. Do not use shell or other tools.', { context: `The integration nonce is ${nonce}.` })
  assert.ok(node.toolCalls.some((c) => c.name === 'integration_probe' && c.args.value === nonce), 'Grok did not call the scoped Persona tool with private context')
  assert.ok(first.last.includes(`PROBE_OK:${nonce}`), first.last)
  assert.ok(!first.result.items.some((i) => i.role === 'user' && i.text?.includes('The integration nonce is')), 'Private context leaked into the rendered transcript')
  console.log(JSON.stringify({ stage: 'assistant_mcp_context', success: true }))
  const deltas = node.all('session.delta').filter((d) => d.session_id === id && d.text)
  assert.ok(deltas.length > 0, 'no streamed reply text reached the node')
  console.log(JSON.stringify({ stage: 'streaming', success: true, deltas: deltas.length }))
  const follow = await send('Without using any tools, repeat exactly the PROBE_OK result from your previous answer.')
  assert.ok(follow.last.includes(`PROBE_OK:${nonce}`), follow.last)
  console.log(JSON.stringify({ stage: 'continuation', success: true }))
  await runtime.stop()
  runtime = companion()
  runtime.start()
  await until('companion reconnect', () => node.hellos >= 2, 10_000)
  await until('session rediscovery', async () => (await runtime.driver.listSessions()).some((s) => s.session_id === id), 10_000)
  assert.equal((await runtime.driver.listSessions()).find((s) => s.session_id === id)?.title, title)
  const resumed = await send('Without using any tools, repeat exactly the PROBE_OK result from this conversation.')
  assert.ok(resumed.last.includes(`PROBE_OK:${nonce}`), resumed.last)
  assert.equal((await runtime.driver.listSessions()).find((s) => s.session_id === id)?.title, title)
  console.log(JSON.stringify({ stage: 'restart_resume', success: true }))
  const target = join(cwd, 'must-not-exist')
  const plan = await send(`Integration permission test. Attempt to create the file ${target} with the text forbidden using a write tool once. If the hook denies it, do not retry and reply DENIED.`, { mode: 'plan' })
  assert.ok(plan.last.includes('DENIED'), plan.last)
  await assert.rejects(access(target))
  console.log(JSON.stringify({ stage: 'readonly_hook', success: true }))
  await runtime.stop()
  runtime = companion({ mode: 'bypassPermissions' })
  runtime.start()
  await until('second companion reconnect', () => node.hellos >= 3, 10_000)
  const restored = await until('restored plan mode', async () => (await runtime.driver.listSessions()).find((s) => s.session_id === id), 10_000)
  assert.equal(restored.permission_mode, 'plan')
  console.log(JSON.stringify({ stage: 'restart_keeps_readonly', success: true }))
  // Default mode: the companion turns a write into a permission interaction for the node before Grok may run it. The
  // fake node answers it as the phone would (refuse, then approve); the phone and its card UI are not part of this check.
  for (const allow of [false, true]) {
    const file = join(cwd, `approval-${allow ? 'allowed' : 'denied'}.txt`)
    const before = node.asked.length
    node.resolve = (ix) => (ix.kind === 'permission' ? { allow } : { dismiss: true }) // the simulated phone's answer
    const turn = await send(`Integration approval test. Create the file ${file} containing the single word ${allow ? 'ALLOWED' : 'DENIED'} using a file write tool once. If the write is refused, do not retry and reply REFUSED. Otherwise reply WRITTEN.`, { mode: 'default' })
    const asked = node.asked.slice(before).filter((ix) => ix.kind === 'permission' && ix.session_id === id)
    assert.ok(asked.length > 0, 'the write raised no permission interaction to the node')
    if (allow) {
      await access(file)
      assert.ok(turn.last.includes('WRITTEN'), turn.last)
    } else {
      await assert.rejects(access(file))
      assert.ok(turn.last.includes('REFUSED'), turn.last)
    }
    console.log(JSON.stringify({ stage: allow ? 'approval_allowed' : 'approval_denied', success: true, answeredBy: 'fake node', interactions: asked.length, tool: asked[0].tool_name }))
  }
  // Stop: a reply far too long to finish within the wait is interrupted while it is seen running, and Grok ends that turn
  // as cancelled. A turn that had already ended by itself would fail here, not pass as a stop.
  await node.peer.call('session.watch', { session_id: id, since_seq: 0 })
  const streamed = node.all('session.delta').length
  const turns = stopReasons.length
  await node.peer.call('session.send', { session_id: id, text: 'Without using tools, write every number from 1 to 5000 in English words, one per line, and nothing else.', mode: 'bypassPermissions' })
  const sending = await until('the long reply streaming', () => node.all('session.delta').slice(streamed).filter((d) => d.session_id === id && d.text).length >= 3, 120_000)
  const live = runtime.sessions.get(id)
  const listed = (await runtime.driver.listSessions()).find((r) => r.session_id === id)
  assert.equal(live.running, true, 'the reply had ended before the stop was sent')
  assert.equal(stopReasons.length, turns, 'Grok had already ended the turn before the stop was sent')
  const before = { running: live.running, listed_status: listed?.status, deltas: node.all('session.delta').slice(streamed).filter((d) => d.session_id === id && d.text).length,
    streamed_chars: live.segment?.kind === 'text' ? live.segment.text.length : null }
  const stopped = await node.peer.call('session.interrupt', { session_id: id })
  assert.ok(sending)
  assert.equal(stopped.settled, true)
  assert.equal(runtime.sessions.get(id).running, false)
  assert.equal(stopReasons.length, turns + 1)
  assert.equal(stopReasons.at(-1), 'cancelled', `Grok ended the turn with ${stopReasons.at(-1)}`)
  const partial = (await node.peer.call('session.items', { session_id: id })).items.filter((i) => i.role === 'assistant' && i.kind === 'text').at(-1)?.text || ''
  assert.ok(!/five thousand/i.test(partial), 'the reply reached its end: it was not stopped')
  console.log(JSON.stringify({ stage: 'interrupt', success: true, before, settled: stopped.settled, running_after: runtime.sessions.get(id).running,
    stop_reason: stopReasons.at(-1), reply_chars_kept: partial.length }))
  const after = await send('Without using any tools, reply exactly AFTER_STOP_OK.')
  assert.ok(after.last.includes('AFTER_STOP_OK'), after.last)
  assert.equal(stopReasons.at(-1), 'end_turn')
  console.log(JSON.stringify({ stage: 'after_interrupt', success: true, stop_reason: stopReasons.at(-1), reply: after.last.trim() }))
  const probeDir = join(home, 'launcher-check')
  await mkdir(probeDir)
  const checked = await node.peer.call('launchers.check', { cwd: probeDir, prompt: 'Reply exactly CHECK_OK. Do not use tools.' })
  assert.equal(checked.ok, true, checked.error)
  assert.equal(checked.model, process.env.GROK_MODEL || start.session.model)
  assert.equal(checked.answer, 'CHECK_OK')
  console.log(JSON.stringify({ stage: 'manual_launcher_check', success: true, model: checked.model }))
} finally {
  await runtime.stop()
  node.close()
  await rm(home, { recursive: true, force: true })
}
