import type { On } from 'claude-code'
import { describe, expect, mock, test } from 'claude-code/testing'
import { homeDir, nextBackoff, readPoll, socketPath, verdict } from '../hooks/bridge.ts'

const SESSION = '11111111-2222-3333-4444-555555555555'

type Answer = { status: number; body: unknown } | { fail: true }

// The engine beneath the plugin: a session, its manifest, and a node that answers polls in the order given (then
// none: a node that went away), recording every request and every prompt the plugin submits.
function world(on: On, polls: Answer[], env: Record<string, string> = { HOME: '/home/me' }) {
  const requests: { url: string; socketPath?: string; body: Record<string, unknown> }[] = []
  const submitted: { text: string; origin: unknown }[] = []
  mock.env(on, env)
  const clock = mock.clock(on, { now: 1_000 })
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('session.id', () => ({ value: SESSION }))
  on('session.version', () => ({ value: { version: '2.1.308', base: '2.1.308' } }))
  on('fs.read', () => ({ value: '{"name":"persona","version":"0.1.0"}' }))
  on('ui.log', () => ({ value: undefined }))
  on('prompt.submit', (_$, e) => {
    submitted.push({ text: e.text, origin: e.origin })
    return { text: e.text }
  })
  on('http.fetch', (_$, e) => {
    requests.push({ url: e.url, socketPath: e.init?.socketPath, body: JSON.parse(e.init?.body ?? '{}') })
    if (e.url.endsWith('/ack')) return { value: { status: 200, ok: true, headers: {}, text: '{"ok":true}' } }
    const next = polls.shift() ?? { fail: true }
    if ('fail' in next) throw new Error('connection refused')
    return { value: { status: next.status, ok: next.status === 200, headers: {}, text: JSON.stringify(next.body) } }
  })
  return { requests, submitted, clock }
}

const start = { cwd: '/work', surface: 'desktop', isInteractive: true } as const

describe('persona plugin', () => {
  test('submits the node’s message as the person’s words and says so', async ($, on) => {
    const w = world(on, [{ status: 200, body: { messages: [{ id: 'm1', text: 'go ahead and merge', deadline_ms: 9_000 }] } }])
    await $.session.start(start)
    await w.clock.settle()

    expect(w.submitted).toEqual([{ text: 'go ahead and merge', origin: { kind: 'plugin', name: 'persona', asUser: true } }])
    const [poll, ack] = w.requests
    expect(poll?.url).toBe('http://persona/claude-plugin/v1/poll')
    expect(poll?.socketPath).toBe('/home/me/.persona/node.sock')
    expect(poll?.body).toEqual({ session_id: SESSION, plugin_version: '0.1.0', engine_version: '2.1.308', wait_ms: 25_000 })
    expect(ack?.url).toBe('http://persona/claude-plugin/v1/ack')
    expect(ack?.body).toEqual({ session_id: SESSION, id: 'm1', submitted: true })
  })

  test('submits nothing late, empty or a slash command, and tells the node why', async ($, on) => {
    const w = world(on, [{
      status: 200,
      body: {
        messages: [
          { id: 'late', text: 'too late', deadline_ms: 500 },
          { id: 'cmd', text: ' /exit', deadline_ms: 9_000 },
          { id: 'empty', text: '  ', deadline_ms: 9_000 },
          { id: 'odd', text: 42, deadline_ms: 9_000 },
          { id: 'bad id!', text: 'no id', deadline_ms: 9_000 },
        ],
      },
    }])
    await $.session.start(start)
    await w.clock.settle()

    expect(w.submitted).toEqual([])
    expect(w.requests.filter(r => r.url.endsWith('/ack')).map(r => r.body)).toEqual([
      { session_id: SESSION, id: 'late', submitted: false, reason: 'late' },
      { session_id: SESSION, id: 'cmd', submitted: false, reason: 'command' },
      { session_id: SESSION, id: 'empty', submitted: false, reason: 'invalid' },
      { session_id: SESSION, id: 'odd', submitted: false, reason: 'invalid' },
    ])
  })

  test('backs off quietly while no node answers, and polls again once one does', async ($, on) => {
    const w = world(on, [{ fail: true }, { status: 404, body: {} }, { status: 200, body: { messages: [] } }])
    await $.session.start(start)
    await w.clock.settle()
    expect(w.requests.length).toBe(1)
    await w.clock.advance(2_000) // the first backoff: an old node answers 404
    expect(w.requests.length).toBe(2)
    await w.clock.advance(3_999) // the second is twice the first
    expect(w.requests.length).toBe(2)
    await w.clock.advance(1)
    expect(w.requests.length).toBe(4) // answered empty: the next poll goes at once (and finds no node)
    expect(w.submitted).toEqual([])
  })

  test('waits as long as the node asks when it does not serve this session', async ($, on) => {
    const w = world(on, [{ status: 403, body: { error: 'not_this_session', retry_ms: 300_000 } }])
    await $.session.start(start)
    await w.clock.settle()
    await w.clock.advance(299_999)
    expect(w.requests.length).toBe(1)
    await w.clock.advance(1)
    expect(w.requests.length).toBe(2)
  })

  test('finds the node where the node itself looks', async ($, on) => {
    const w = world(on, [], { USERPROFILE: 'C:\\Users\\me', HOME: '/c/Users/me', PERSONA_HOME: 'persona-data' })
    await $.session.start(start)
    await w.clock.settle()
    expect(w.requests[0]?.socketPath).toBe('C:\\Users\\me\\persona-data\\node.sock')
  })
})

describe('protocol helpers', () => {
  test('socket path', () => {
    expect(socketPath('/home/me', undefined)).toBe('/home/me/.persona/node.sock')
    expect(socketPath('/home/me', '~')).toBe('/home/me/node.sock')
    expect(socketPath('/home/me', '~/state')).toBe('/home/me/state/node.sock')
    expect(socketPath('/home/me', '/srv/persona')).toBe('/srv/persona/node.sock')
    expect(socketPath('C:\\Users\\me', undefined)).toBe('C:\\Users\\me\\.persona\\node.sock')
    expect(socketPath('C:\\Users\\me', 'D:\\persona')).toBe('D:\\persona\\node.sock')
    expect(socketPath(undefined, '/srv/persona')).toBe(undefined)
    expect(homeDir('C:\\Users\\me', '/c/Users/me')).toBe('C:\\Users\\me')
    expect(homeDir(undefined, '/Users/me')).toBe('/Users/me')
  })

  test('poll answers', () => {
    expect(readPoll(200, '{"messages":[{"id":"a","text":"hi","deadline_ms":5}],"retry_ms":10}')).toEqual({
      kind: 'messages',
      messages: [{ id: 'a', text: 'hi', deadlineMs: 5 }],
      retryMs: 10,
    })
    expect(readPoll(200, 'not json')).toEqual({ kind: 'unavailable' })
    expect(readPoll(404, '')).toEqual({ kind: 'unavailable' })
    expect(readPoll(403, '{}')).toEqual({ kind: 'refused', retryMs: 60_000 })
    expect(verdict({ id: 'a', text: 'x'.repeat(200_001), deadlineMs: 10 }, 0)).toBe('invalid')
    expect(nextBackoff(0)).toBe(2_000)
    expect(nextBackoff(40_000)).toBe(60_000)
  })
})
