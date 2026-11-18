import type { EngineInterface, Register } from 'claude-code'
import {
  BASE,
  POLL_WAIT_MS,
  homeDir,
  nextBackoff,
  readPoll,
  socketPath,
  verdict,
  type Message,
  type PollAnswer,
} from './bridge.ts'

// Persona's plugin (README.md): while this session lives it asks the local Persona node for the owner's own messages
// from the Persona apps, and submits each one as the person's words (`asUser`), so the session reads it as typed
// input rather than as another session's. It submits text and nothing else: it runs no command, reads no file but
// its own manifest, and keeps quiet (the debug log only) when no node answers.

type Engine = EngineInterface

export const register: Register = on => {
  // One poll loop per load: a reload runs session.start again in a fresh environment, and the old loop goes with
  // its environment. A /clear keeps the loop: each poll names the session open now.
  let loop = 0

  on('session.start', async ($, e, next) => {
    const started = await next(e)
    const mine = ++loop
    void run($, () => mine === loop)
    return started
  })
}

async function run($: Engine, current: () => boolean): Promise<void> {
  const home = homeDir(await $.env.get('USERPROFILE'), await $.env.get('HOME'))
  const sock = socketPath(home, await $.env.get('PERSONA_HOME'))
  if (sock === undefined) return
  const pluginVersion = await manifestVersion($)
  const engineVersion = await $.session
    .version()
    .then(v => v.version)
    .catch(() => '')
  let backoff = 0
  let reachable: boolean | undefined
  while (current()) {
    const session = await $.session.id()
    let answer: PollAnswer
    try {
      const res = await $.http.fetch(`${BASE}/poll`, post(sock, {
        session_id: session,
        plugin_version: pluginVersion,
        engine_version: engineVersion,
        wait_ms: POLL_WAIT_MS,
      }))
      answer = readPoll(res.status, res.text)
    } catch {
      answer = { kind: 'unavailable' }
    }
    if (!current()) return
    const now = answer.kind === 'messages'
    if (now !== reachable) {
      reachable = now
      $.ui.log(now ? 'Persona node reached' : `Persona node not reachable (${answer.kind})`, { to: 'debug' })
    }
    if (answer.kind !== 'messages') {
      backoff = answer.kind === 'refused' ? answer.retryMs : nextBackoff(backoff)
      await sleep($, backoff)
      continue
    }
    backoff = 0
    for (const m of answer.messages) await deliver($, sock, session, m)
    if (answer.retryMs > 0) await sleep($, answer.retryMs)
  }
}

/** Submits one message as the person's words, if it may go in now, and tells the node what became of it. */
async function deliver($: Engine, sock: string, session: string, m: Message): Promise<void> {
  let outcome: string = verdict(m, await $.clock.now())
  if (outcome === 'ok' && typeof m.text === 'string') {
    try {
      const submitted = await $.prompt.submit({ text: m.text, asUser: true })
      if (submitted.drop !== undefined) outcome = 'dropped'
    } catch {
      outcome = 'failed'
    }
  }
  const ack = outcome === 'ok' ? { submitted: true } : { submitted: false, reason: outcome }
  try {
    await $.http.fetch(`${BASE}/ack`, post(sock, { session_id: session, id: m.id, ...ack }))
  } catch {
    // The node went away: it reads the transcript to learn what became of the message.
  }
}

async function manifestVersion($: Engine): Promise<string> {
  try {
    const manifest: unknown = JSON.parse(await $.fs.read(`${$.plugin.root}/.claude-plugin/plugin.json`))
    const version = (manifest as { version?: unknown }).version
    return typeof version === 'string' ? version : ''
  } catch {
    return ''
  }
}

function post(socket: string, body: Record<string, unknown>) {
  return {
    method: 'POST',
    socketPath: socket,
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  }
}

function sleep($: Engine, ms: number): Promise<void> {
  return new Promise(resolve => {
    $.clock.after(ms, resolve)
  })
}
