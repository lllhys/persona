// Protocol 1 between this plugin and the local Persona node (README.md): where the node's socket is, what a poll's
// answer holds, and which messages may go in as the person's words. Pure functions; register.ts does the I/O.

/** The base of the node's endpoints; the host name is not used (the request goes over the socket). */
export const BASE = 'http://persona/claude-plugin/v1'

/** How long the node may hold a poll: under the 30 s after which Claude Code's fetch gives up. */
export const POLL_WAIT_MS = 25_000

/** Backoff while no node answers: from the first to the most, doubling. */
export const BACKOFF_FIRST_MS = 2_000
export const BACKOFF_MOST_MS = 60_000

/** The longest text submitted, in UTF-16 units; the node never sends more (its inbox line holds 1 MiB). */
export const TEXT_MOST = 200_000

const ID = /^[A-Za-z0-9_-]{1,128}$/

/**
 * The node's socket, as the node resolves it: `$PERSONA_HOME/node.sock` when that is set (`~`, `~/…` and a relative
 * path are taken from the home directory), else `<home>/.persona/node.sock`. `undefined` without a home directory.
 */
export function socketPath(home: string | undefined, personaHome: string | undefined): string | undefined {
  if (!home) return undefined
  const sep = /^[A-Za-z]:[\\/]/.test(home) || home.includes('\\') ? '\\' : '/'
  const join = (a: string, b: string) => (a.endsWith('/') || a.endsWith('\\') ? a + b : a + sep + b)
  let root = join(home, '.persona')
  const p = personaHome?.trim()
  if (p) {
    if (p === '~') root = home
    else if (p.startsWith('~/') || p.startsWith('~\\')) root = join(home, p.slice(2))
    else if (p.startsWith('/') || /^[A-Za-z]:[\\/]/.test(p) || p.startsWith('\\\\')) root = p
    else root = join(home, p)
  }
  return join(root, 'node.sock')
}

/** The home directory: `USERPROFILE` on Windows (what the node's Go reads there), else `HOME`. */
export function homeDir(userProfile: string | undefined, home: string | undefined): string | undefined {
  if (userProfile && /^[A-Za-z]:[\\/]/.test(userProfile)) return userProfile
  return home || undefined
}

/** One message the node handed over: its id, the text, and the time after which it must not be submitted. */
export type Message = { id: string; text: unknown; deadlineMs: number }

/** What a poll came to. */
export type PollAnswer =
  | { kind: 'messages'; messages: Message[]; retryMs: number }
  | { kind: 'refused'; retryMs: number } // the node does not take this process for this session
  | { kind: 'unavailable' } // no node, an old node, or no answer

/** Reads a poll's answer: status and body as `$.http.fetch` resolved them. */
export function readPoll(status: number, body: string): PollAnswer {
  if (status === 403) return { kind: 'refused', retryMs: retryOf(parse(body), BACKOFF_MOST_MS) }
  if (status !== 200) return { kind: 'unavailable' }
  const answer = parse(body)
  if (answer === undefined) return { kind: 'unavailable' }
  const list = Array.isArray(answer.messages) ? answer.messages : []
  const messages: Message[] = []
  for (const m of list) {
    if (typeof m !== 'object' || m === null) continue
    const { id, text, deadline_ms: deadline } = m as Record<string, unknown>
    if (typeof id !== 'string' || !ID.test(id) || typeof deadline !== 'number') continue
    messages.push({ id, text, deadlineMs: deadline })
  }
  return { kind: 'messages', messages, retryMs: retryOf(answer, 0) }
}

/** Why a message is not submitted, or `ok`. */
export type Verdict = 'ok' | 'invalid' | 'late' | 'command'

/**
 * Whether the message may go in as the person's words now (`now`, epoch ms): text that is a string with something in
 * it and not too long, before its deadline, and no slash command (the node sends none; a command would run, not be
 * read).
 */
export function verdict(m: Message, now: number): Verdict {
  if (typeof m.text !== 'string' || m.text.trim() === '' || m.text.length > TEXT_MOST) return 'invalid'
  if (now > m.deadlineMs) return 'late'
  if (m.text.trimStart().startsWith('/')) return 'command'
  return 'ok'
}

/** The wait after a failed poll: the first backoff, then twice the last, up to the most. */
export function nextBackoff(last: number): number {
  return last <= 0 ? BACKOFF_FIRST_MS : Math.min(last * 2, BACKOFF_MOST_MS)
}

function parse(body: string): Record<string, unknown> | undefined {
  try {
    const v: unknown = JSON.parse(body)
    return typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined
  } catch {
    return undefined
  }
}

function retryOf(answer: Record<string, unknown> | undefined, fallback: number): number {
  const r = answer?.retry_ms
  return typeof r === 'number' && r >= 0 && r <= 3_600_000 ? r : fallback
}
