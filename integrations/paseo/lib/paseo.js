// The bridge's connection to the Paseo daemon (its WebSocket API, protocol 1): the hello, requests matched to their
// replies, the subscriptions this socket owns, the keepalive, and connecting again after the daemon restarts. Only
// what the bridge uses; the shapes are Paseo's (packages/protocol messages.ts), docs/integrations/paseo.md.

import { randomUUID } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { daemonTarget, localCredential, paseoHome } from './discover.js'
import { dialWebSocket } from './ws.js'

/** What the bridge asks of the daemon: replies and its own subscriptions only, no broadcast; no binary frames. */
export const CAPABILITIES = {
  owned_subscriptions: true,
  explicit_event_subscriptions: true,
  all_providers: true,
  hello_rejection: true,
  timeline_notifications: true,
  custom_mode_icons: true,
  reasoning_merge_enum: true,
}

const PING_EVERY = 10_000 // ms: as Paseo's own client
const SILENT_FOR = 25_000 // ms: no frame for this long (two pings unanswered) → the connection is dead (a sleep not counted)
const HELLO_WAIT = 15_000 // ms: the daemon answers a hello with server_info at once
const RETRY_MIN = 1000
const RETRY_MAX = 30_000

// Replies whose type is not the request's with _response / .response (Paseo's owned-subscriptions replies.ts).
const REPLY_TYPES = {
  archive_agent_request: 'agent_archived',
  delete_agent_request: 'agent_deleted',
}

/** An error the daemon answered with (rpc_error, a failed status), or the connection's end. */
export class PaseoError extends Error {
  constructor(message, code) {
    super(message)
    this.name = 'PaseoError'
    if (code) this.code = code
  }
}

/**
 * A connection to the daemon of a Paseo home that keeps itself up. Events: 'connected' (serverInfo) after each hello,
 * 'disconnected' (reason) when it ends; while disconnected, requests fail at once.
 */
export class PaseoClient extends EventEmitter {
  /**
   * @param {{ home?: string, env?: object, log?: (msg: string) => void, dial?: typeof dialWebSocket,
   *   clientId?: string, retryMin?: number, retryMax?: number, pingEvery?: number, silentFor?: number }} [opts]
   */
  constructor(opts = {}) {
    super()
    this.env = opts.env ?? process.env
    this.home = opts.home ?? paseoHome(this.env)
    this.log = opts.log ?? (() => {})
    this.dial = opts.dial ?? dialWebSocket
    // The same id across reconnects: the daemon takes it for this client coming back.
    this.clientId = opts.clientId ?? `cid_persona_${randomUUID().replaceAll('-', '')}`
    this.retryMin = opts.retryMin ?? RETRY_MIN
    this.retryMax = opts.retryMax ?? RETRY_MAX
    this.pingEvery = opts.pingEvery ?? PING_EVERY
    this.silentFor = opts.silentFor ?? SILENT_FOR
    this.ws = undefined
    this.serverInfo = undefined
    this.pending = new Map() // requestId → { resolve, reject, timer, expect }
    this.subs = new Map() // subscriptionId → onEvent
    this.stopped = true
    this.retry = this.retryMin
    this.lastError = ''
  }

  get connected() {
    return this.serverInfo !== undefined
  }

  /** Connect, and keep connecting again whenever the connection ends, until stop. */
  start() {
    if (!this.stopped) return
    this.stopped = false
    void this.connect()
  }

  stop() {
    this.stopped = true
    clearTimeout(this.retryTimer)
    this.ws?.close(1000, 'bye')
    this.drop(new PaseoError('the bridge stopped', 'stopped'))
  }

  async connect() {
    if (this.stopped) return
    const { target, listen, running } = daemonTarget(this.home)
    if (!target) {
      return this.again(running ? `Paseo in ${this.home} is starting (no address in its paseo.pid yet)` : `Paseo is not running (no paseo.pid in ${this.home})`, running)
    }
    let ws
    try {
      ws = await this.dial({ ...target, path: '/ws', timeoutMs: 10_000 })
    } catch (err) {
      return this.again(`Paseo at ${listen}: ${err.message}`, running)
    }
    if (this.stopped) {
      ws.close(1000, 'bye')
      return
    }
    this.ws = ws
    let heard = Date.now()
    let ticked = heard
    let helloTimer
    const pinger = setInterval(() => {
      const now = Date.now()
      // A tick long after the last one: the machine slept (its timers stopped, its clock did not) or this process was
      // held up, and nothing was read meanwhile. That silence is not Paseo's: it is measured from now, so the connection
      // is judged by the answer to the ping sent now, as Paseo's own client waits for its ping before it gives up.
      if (now - ticked > 2 * this.pingEvery) heard = now
      ticked = now
      if (now - heard > this.silentFor) {
        this.log('paseo: no answer to pings; connecting again')
        ws.close(4000, 'keepalive timeout')
        return
      }
      if (this.connected) ws.send('{"type":"ping"}')
    }, this.pingEvery)
    pinger.unref?.()
    ws.on('message', (text) => {
      heard = Date.now()
      let m
      try {
        m = JSON.parse(text)
      } catch {
        return
      }
      if (m?.type === 'hello.rejected') {
        this.lastError = `Paseo refused the connection (${m.reason ?? 'rejected'})`
        return
      }
      if (m?.type !== 'session' || !m.message) return
      const inner = m.message
      if (!this.connected) {
        if (inner.type === 'status' && inner.payload?.status === 'server_info') {
          clearTimeout(helloTimer)
          this.serverInfo = inner.payload
          this.retry = this.retryMin
          this.lastError = ''
          this.emit('connected', this.serverInfo)
        }
        return
      }
      this.route(inner)
    })
    ws.on('close', (code, reason) => {
      clearInterval(pinger)
      clearTimeout(helloTimer)
      if (this.ws === ws) this.ws = undefined
      const was = this.connected
      const what = `${code}${reason ? ` ${reason}` : ''}`
      this.drop(new PaseoError('Paseo 的连接断了', 'disconnected'))
      if (was) this.emit('disconnected', what)
      if (code === 4401 || code === 4003) this.lastError ||= `Paseo refused the connection (${code} ${reason})`
      const refused = code === 4401 || code === 4003 || code === 4002
      // Who ended it: the bridge (no answer to its pings, no server_info, its setup failed, a frame it refused), Paseo
      // (its close frame), or nobody said: a socket that ended without a close frame (1006) dropped, as when Paseo is
      // killed.
      const ended = ws.local ? `the bridge closed the connection (${what})`
        : code === 1006 ? 'the connection to Paseo dropped (1006)' : `Paseo closed the connection (${what})`
      this.again(this.lastError || ended, !refused && daemonTarget(this.home).running)
    })
    ws.on('error', () => {}) // 'close' follows
    helloTimer = setTimeout(() => ws.close(4000, 'no server_info'), HELLO_WAIT)
    helloTimer.unref?.()
    // No appVersion: Paseo reads one as a version of its own app (session.ts isAppVersionAtLeast) and decides what the
    // client gets by it. The bridge is no Paseo app; the capabilities say what it takes.
    const hello = { type: 'hello', clientId: this.clientId, clientType: 'cli', protocolVersion: 1, capabilities: CAPABILITIES }
    // Checked only when the daemon has a password; then the local credential is what Paseo's own CLI shows.
    const token = localCredential(this.home)
    if (token) hello.auth = { kind: 'localCredential', token }
    ws.send(JSON.stringify(hello))
  }

  /**
   * Connect again later: soon while the home has a paseo.pid (Paseo restarting: back in seconds), backing off to
   * retryMax while it has none (Paseo not running).
   */
  again(why, soon = false) {
    if (this.stopped) return
    if (why !== this.lastLogged) this.log(`paseo: ${why}; trying again`)
    this.lastLogged = why
    clearTimeout(this.retryTimer)
    this.retryTimer = setTimeout(() => void this.connect(), soon ? this.retryMin : this.retry)
    this.retryTimer.unref?.()
    this.retry = soon ? this.retryMin : Math.min(this.retry * 2, this.retryMax)
  }

  drop(err) {
    this.serverInfo = undefined
    for (const p of this.pending.values()) {
      clearTimeout(p.timer)
      p.reject(err)
    }
    this.pending.clear()
    this.subs.clear()
  }

  /**
   * A session message from the daemon: a reply to a request, a subscription's event, both, or nothing the bridge
   * waits for. Both: Paseo tells a permission's resolution to the subscriptions under the permission's own id, which
   * the bridge answers it under, so that event settles the answer and reaches its subscription too.
   */
  route(inner) {
    const p = inner.payload
    const id = p?.requestId
    const w = typeof id === 'string' ? this.pending.get(id) : undefined
    // Read before a reply settles: a subscription's own reply carries its new subscriptionId, and is no event of it.
    const on = p && typeof p.subscriptionId === 'string' ? this.subs.get(p.subscriptionId) : undefined
    if (w) {
      if (inner.type === 'rpc_error') return this.settle(id, w, new PaseoError(p.error || 'Paseo 出错了', p.code))
      if (inner.type === 'status' && p.status === 'agent_create_failed') {
        return this.settle(id, w, new PaseoError(p.error || 'Paseo 没能建出这个 Agent', p.errorCode ?? 'create_failed'))
      }
      const type = inner.type === 'status' ? `status:${p.status}` : inner.type
      if (!w.expect || w.expect === type) this.settle(id, w, undefined, p)
    }
    if (!on) return
    try {
      on(inner)
    } catch (err) {
      this.log(`paseo: ${inner.type}: ${err.stack ?? err.message}`)
    }
  }

  settle(id, w, err, payload) {
    clearTimeout(w.timer)
    this.pending.delete(id)
    if (err) return w.reject(err)
    // A subscription's events may come in the same read as its reply: it takes them from now on, not once the
    // caller's await resumes.
    if (w.onEvent && typeof payload?.subscriptionId === 'string') this.subs.set(payload.subscriptionId, w.onEvent)
    w.resolve(payload)
  }

  /**
   * Send a session request and wait for its reply's payload. expect: the reply's type when it is not the usual
   * (`status:<name>` for status replies); requestId: answer to one the caller names (a permission's id).
   * @returns {Promise<any>}
   */
  request(type, fields = {}, { timeoutMs = 30_000, expect, requestId, onEvent } = {}) {
    if (!this.connected || !this.ws) return Promise.reject(new PaseoError('Paseo 没有连上', 'disconnected'))
    const id = requestId ?? randomUUID()
    const want = expect ?? REPLY_TYPES[type] ?? replyType(type)
    return new Promise((resolve, reject) => {
      const w = { resolve, reject, expect: want, timer: undefined, onEvent }
      if (timeoutMs > 0) {
        w.timer = setTimeout(() => {
          if (this.pending.get(id) === w) this.pending.delete(id)
          reject(new PaseoError(`Paseo 没有回答（${type}，${Math.round(timeoutMs / 1000)} 秒）`, 'timeout'))
        }, timeoutMs)
        w.timer.unref?.()
      }
      this.pending.set(id, w)
      this.ws.send(JSON.stringify({ type: 'session', message: { ...fields, type, requestId: id } }))
    })
  }

  /**
   * Make a subscription this socket owns: the request (it answers with a subscriptionId), then every message tagged
   * with that id goes to onEvent. Returns the reply's payload. Subscriptions end with the connection: subscribe again
   * on 'connected'.
   */
  subscribe(type, fields, onEvent, opts) {
    return this.request(type, fields, { ...opts, onEvent })
  }

  /** End a subscription (no wait: the daemon always answers, and nothing depends on it). */
  release(subscriptionId) {
    if (!subscriptionId || !this.subs.delete(subscriptionId)) return
    this.request('subscription.release.request', { subscriptionId }, { timeoutMs: 10_000 }).catch(() => {})
  }
}

/** The reply type of a request type: x.request → x.response, x_request → x_response, foo → foo_response. */
export function replyType(type) {
  if (type.endsWith('.request')) return `${type.slice(0, -'.request'.length)}.response`
  if (type.endsWith('_request')) return `${type.slice(0, -'_request'.length)}_response`
  return `${type}_response`
}
