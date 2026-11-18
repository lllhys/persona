import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { request } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { randomBytes } from 'node:crypto'
import { until } from '../../runtime-sdk/testing/fake-node.js'

const fixture = fileURLToPath(new URL('./fixtures/token-file-listener.mjs', import.meta.url))

async function withTimeout(promise, ms, message) {
  let timer
  try {
    return await Promise.race([promise, new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), ms)
    })])
  } finally {
    clearTimeout(timer)
  }
}

function handshake(port, token) {
  return new Promise((resolve, reject) => {
    const req = request({
      host: '127.0.0.1', port, path: '/persona/runtime',
      headers: {
        connection: 'Upgrade', upgrade: 'websocket',
        'sec-websocket-version': '13', 'sec-websocket-key': randomBytes(16).toString('base64'),
        'sec-websocket-protocol': 'persona-runtime.v1',
        authorization: `Bearer ${token}`,
      },
    })
    const deadline = setTimeout(() => req.destroy(new Error('timed out waiting for the DSH handshake')), 3000)
    let settled = false
    const done = (error, status) => {
      if (settled) return
      settled = true
      clearTimeout(deadline)
      if (error) reject(error)
      else resolve(status)
    }
    req.on('response', (res) => {
      res.resume()
      done(null, res.statusCode)
    })
    req.on('upgrade', (res, socket) => {
      socket.destroy()
      done(null, res.statusCode)
    })
    req.on('error', (error) => done(error))
    req.end()
  })
}

test('a DSH token_file read failure refuses the upgrade, then a restored file authenticates later connections', async (t) => {
  const home = mkdtempSync(join(tmpdir(), 'persona-dsh-token-file-'))
  const tokenFile = join(home, 'missing-token')
  const fileContents = 'dsh-token-file-contents-8672'
  const wrongBearer = 'dsh-wrong-bearer-8672'
  const child = spawn(process.execPath, [fixture, tokenFile], { stdio: ['ignore', 'pipe', 'pipe'] })
  const exited = once(child, 'exit')
  let output = ''
  for (const stream of [child.stdout, child.stderr]) {
    stream.setEncoding('utf8')
    stream.on('data', (part) => { output += part })
  }
  const alive = () => child.exitCode === null && child.signalCode === null
  t.after(async () => {
    if (alive()) child.kill('SIGTERM')
    const stopped = await withTimeout(exited, 5000, 'the DSH child cleanup timed out').then(() => true, () => false)
    if (!stopped) {
      child.kill('SIGKILL')
      await exited
    }
    rmSync(home, { recursive: true, force: true })
  })

  await until('the DSH child listener or process exit', () => /daemon endpoint on /.test(output) || !alive(), 5000)
  assert.ok(alive(), `the DSH child exited before listening:\n${output}`)
  const port = Number(output.match(/daemon endpoint on 127\.0\.0\.1:(\d+)/)?.[1])
  assert.ok(Number.isInteger(port) && port > 0, `no DSH listener address in:\n${output}`)

  const missing = await handshake(port, wrongBearer).then((status) => ({ status }), (error) => ({ error }))
  if (missing.error) await Promise.race([exited, new Promise((resolve) => setTimeout(resolve, 100))])
  assert.ok(alive(), `the missing token_file killed the DSH child:\n${output}`)
  assert.equal(missing.status, 401, missing.error?.message)
  assert.ok(!output.includes('ENOENT'), 'the fixed refusal must not expose the token-file read error')

  writeFileSync(tokenFile, fileContents, { mode: 0o600 })
  assert.equal(await handshake(port, fileContents), 101)
  assert.ok(alive(), 'the DSH child must survive a successful upgrade')
  assert.equal(await handshake(port, wrongBearer), 401)
  assert.ok(alive(), 'a wrong bearer after recovery must not kill the DSH child')
  assert.ok(!output.includes(fileContents), 'the token-file contents must not enter the diagnostic')
  assert.ok(!output.includes(wrongBearer), 'the bearer token must not enter the diagnostic')

  child.kill('SIGTERM')
  const [code, signal] = await withTimeout(exited, 5000, 'the DSH child did not stop cleanly')
  assert.equal(code, 0)
  assert.equal(signal, null)
})
