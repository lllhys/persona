import { spawn } from 'node:child_process'
import { EventEmitter } from 'node:events'
import { createInterface } from 'node:readline'

// Grok uses ACP over newline-delimited JSON. Requests from a former process must never be answered on its successor.
export class GrokAcp extends EventEmitter {
  constructor({ bin = 'grok', args = ['agent', '--no-leader', 'stdio'], env = process.env, log = () => {}, request } = {}) {
    super()
    Object.assign(this, { bin, args, env, log, request })
    this.pending = new Map()
    this.nextId = 0
    this.generation = 0
  }

  async ready() {
    if (this.stopped) throw new Error('Grok runtime is stopped')
    if (this.starting) return this.starting
    const child = spawn(this.bin, this.args, { env: this.env, stdio: ['pipe', 'pipe', 'pipe'] })
    this.child = child
    this.generation++
    const lines = createInterface({ input: child.stdout, crlfDelay: Infinity })
    lines.on('line', (line) => {
      let msg
      try { msg = JSON.parse(line) } catch { return }
      if (child !== this.child) return
      if (msg.method) {
        if (msg.id === undefined) this.emit('notification', msg.method, msg.params)
        else void this.answer(child, msg)
      } else if (msg.id !== undefined) {
        const pending = this.pending.get(msg.id)
        if (!pending) return
        this.pending.delete(msg.id)
        clearTimeout(pending.timer)
        if (msg.error) pending.reject(Object.assign(new Error(msg.error.message || 'Grok ACP request failed'), { code: msg.error.code }))
        else pending.resolve(msg.result)
      }
    })
    // stderr can contain provider credentials or user content. Keep it drained; failures name the protocol operation.
    child.stderr.resume()
    child.on('error', (error) => this.lost(child, error))
    child.stdin.on('error', (error) => { this.lost(child, error); child.kill('SIGTERM') })
    child.on('exit', (code, signal) => this.lost(child, new Error(`Grok exited (${signal || code})`)))
    this.starting = this.raw('initialize', {
      protocolVersion: 1, clientCapabilities: {}, clientInfo: { name: 'persona', version: '0.1.0' },
    }, 30_000).then((value) => {
      this.initialized = value
      if (!value.agentCapabilities?._meta?.['x.ai/hooks']?.blockingEvents?.includes('pre_tool_use')) {
        throw new Error('This Grok version does not support the tool hooks required by Persona')
      }
      return value
    }).catch((error) => { this.lost(child, error); child.kill('SIGTERM'); throw error })
    return this.starting
  }

  lost(child, error) {
    if (this.child !== child) return
    this.child = null
    this.starting = null
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(error) }
    this.pending.clear()
    this.emit('gone', error)
  }

  async answer(child, msg) {
    let response
    try {
      if (!this.request) throw new Error(`Unsupported Grok request: ${msg.method}`)
      response = { result: await this.request(msg.method, msg.params) }
    } catch (error) {
      response = { error: { code: -32000, message: error.message } }
    }
    if (this.child === child && child.stdin.writable) child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: msg.id, ...response })}\n`)
  }

  raw(method, params, timeout = 30_000) {
    if (!this.child?.stdin.writable) return Promise.reject(new Error('Grok is not connected'))
    const id = ++this.nextId
    return new Promise((resolve, reject) => {
      const timer = timeout ? setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`Grok ${method} timed out`))
      }, timeout) : null
      this.pending.set(id, { resolve, reject, timer })
      this.child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`)
    })
  }

  async call(method, params, timeout) { await this.ready(); return this.raw(method, params, timeout) }

  notify(method, params) {
    if (this.child?.stdin.writable) this.child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', method, params })}\n`)
  }

  async stop() {
    this.stopped = true
    const child = this.child
    if (!child) return
    this.lost(child, new Error('Grok runtime stopped'))
    if (!child.pid || child.exitCode !== null || child.signalCode) return
    await new Promise((resolve) => {
      const timer = setTimeout(() => child.kill('SIGKILL'), 3000)
      child.once('exit', () => { clearTimeout(timer); resolve() })
      child.kill('SIGTERM')
    })
  }
}
