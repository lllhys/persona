import { createServer } from 'node:http'
import { randomBytes, timingSafeEqual } from 'node:crypto'

// The runtime owns this loopback MCP endpoint; no hub credentials are passed to Grok. Each path belongs to one chat.
export function createMcpProxy({ tools, call }) {
  const token = randomBytes(32).toString('hex')
  const sessions = new Map()
  const server = createServer(async (req, res) => {
    const supplied = Buffer.from(String(req.headers.authorization || ''))
    const expected = Buffer.from(`Bearer ${token}`)
    if (supplied.length !== expected.length || !timingSafeEqual(supplied, expected)) { res.writeHead(401).end(); return }
    const session = sessions.get(req.url)
    if (!session) { res.writeHead(404).end(); return }
    if (req.method === 'DELETE') { res.writeHead(204).end(); return }
    if (req.method !== 'POST') { res.writeHead(405).end(); return }
    let size = 0
    const chunks = []
    try {
      for await (const chunk of req) {
        size += chunk.length
        if (size > 2 * 1024 * 1024) { res.writeHead(413).end(); return }
        chunks.push(chunk)
      }
      const msg = JSON.parse(Buffer.concat(chunks).toString())
      if (msg.id === undefined) { res.writeHead(202).end(); return }
      let result
      if (msg.method === 'initialize') result = { protocolVersion: '2024-11-05', capabilities: { tools: {} }, serverInfo: { name: 'persona', version: '0.1.0' } }
      else if (msg.method === 'ping') result = {}
      else if (msg.method === 'tools/list') result = { tools: (await tools(session)).map((t) => ({ name: t.name, description: t.description, inputSchema: t.input_schema || { type: 'object', properties: {} } })) }
      else if (msg.method === 'tools/call') {
        const value = await call(session, msg.params?.name, msg.params?.arguments || {})
        result = { content: (value.content || []).map((b) => b.type === 'image' ? { type: 'image', data: b.data, mimeType: b.mimeType || b.mime_type || 'image/png' } : b), isError: Boolean(value.is_error) }
      } else {
        res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: 'Method not found' } }))
        return
      }
      res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: msg.id, result }))
    } catch {
      if (!res.headersSent) res.writeHead(500, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: 'Persona tool request failed' }))
      else res.end()
    }
  })
  let starting
  return {
    async forSession(session) {
      starting ??= new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve) })
      await starting
      const path = `/mcp/${session.id}`
      sessions.set(path, session)
      return [{ type: 'http', name: 'persona', url: `http://127.0.0.1:${server.address().port}${path}`, headers: [{ name: 'Authorization', value: `Bearer ${token}` }] }]
    },
    stop() { server.closeAllConnections(); server.close(); sessions.clear() },
  }
}
