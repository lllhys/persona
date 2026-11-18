import assert from 'node:assert/strict'
import { test } from 'node:test'
import { GrokAcp } from '../lib/acp.js'

const SCRIPT = `
const { createInterface } = require('node:readline');
const send = (v) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...v }) + '\\n');
let pending;
createInterface({ input: process.stdin }).on('line', (line) => {
  const v = JSON.parse(line);
  if (v.method === 'initialize') send({ id: v.id, result: { agentCapabilities: { _meta: { 'x.ai/hooks': { blockingEvents: process.env.NO_HOOKS ? [] : ['pre_tool_use'] } } } } });
  else if (v.method === 'exit') process.exit(1);
  else if (v.method === 'permission') { pending = v.id; send({ id: 'reverse1', method: 'session/request_permission', params: { options: [{ kind: 'allow_once', optionId: 'yes' }] } }); }
  else if (v.id === 'reverse1') send({ id: pending, result: v });
  else if (v.id !== undefined) send({ id: v.id, result: v.params });
});
`

test('Grok transport rejects in-flight requests on exit and initializes a successor', async (t) => {
  const acp = new GrokAcp({ bin: process.execPath, args: ['-e', SCRIPT] })
  t.after(() => acp.stop())
  await acp.ready()
  assert.equal(acp.generation, 1)
  await assert.rejects(acp.call('exit', {}), /Grok exited/)
  assert.deepEqual(await acp.call('echo', { value: 1 }), { value: 1 })
  assert.equal(acp.generation, 2)
})

test('an unhandled reverse permission request never silently approves a tool', async (t) => {
  const acp = new GrokAcp({ bin: process.execPath, args: ['-e', SCRIPT] })
  t.after(() => acp.stop())
  const reply = await acp.call('permission', {})
  assert.equal(reply.result, undefined)
  assert.match(reply.error.message, /Unsupported Grok request/)
})

test('a Grok version lacking mandatory tool hooks is refused', async (t) => {
  const acp = new GrokAcp({ bin: process.execPath, args: ['-e', SCRIPT], env: { ...process.env, NO_HOOKS: '1' } })
  t.after(() => acp.stop())
  await assert.rejects(acp.ready(), /does not support the tool hooks/)
  assert.equal(acp.child, null)
})
