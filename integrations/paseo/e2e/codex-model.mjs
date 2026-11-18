// A local Responses API for real Codex, with an optional scripted background command; no accounts or model traffic.
import { createServer } from 'node:http'

export async function startCodexModel({ backgroundCommand } = {}) {
  const requests = []
  let backgroundSent = false
  const server = createServer(async (req, res) => {
    try {
      const chunks = []
      for await (const chunk of req) chunks.push(chunk)
      if (req.method !== 'POST' || req.url !== '/v1/responses') {
        res.writeHead(404).end()
        return
      }
      const body = JSON.parse(Buffer.concat(chunks).toString())
      requests.push(body)
      const last = [...(body.input ?? [])].reverse().find((item) => item.role === 'user')
      const prompt = typeof last?.content === 'string' ? last.content : (last?.content ?? []).map((c) => c.text ?? '').join(' ')
      const marker = prompt.match(/E2E_CODEX_[A-Z]+/)?.[0] ?? 'E2E_CODEX_UNKNOWN'
      const text = `Scripted answer: ${marker}`
      const id = `resp_e2e_${requests.length}`
      let item = { id: `msg_e2e_${requests.length}`, type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text, annotations: [] }] }
      if (backgroundCommand && marker === 'E2E_CODEX_BACKGROUND' && !backgroundSent) {
        const findCommand = (tools, namespace) => {
          for (const tool of tools ?? []) {
            if (tool.type === 'namespace') {
              const nested = findCommand(tool.tools, tool.name)
              if (nested) return nested
            } else if (tool.name === 'exec_command') return { name: tool.name, ...(namespace ? { namespace } : {}) }
          }
        }
        const command = findCommand(body.tools)
        if (!command) throw new Error('The background fixture requires the exec_command tool')
        backgroundSent = true
        item = { id: `fc_e2e_${requests.length}`, type: 'function_call', status: 'completed', call_id: 'call_e2e_background', ...command,
          arguments: JSON.stringify({ cmd: backgroundCommand, yield_time_ms: 1000, max_output_tokens: 1000 }) }
      }
      const response = { id, object: 'response', created_at: Math.floor(Date.now() / 1000), status: 'completed', model: body.model, output: [item], usage: { input_tokens: 10, output_tokens: 10, total_tokens: 20 } }
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
      let sequence = 0
      const send = (type, data) => res.write(`event: ${type}\ndata: ${JSON.stringify({ type, sequence_number: sequence++, ...data })}\n\n`)
      send('response.created', { response: { ...response, status: 'in_progress', output: [] } })
      if (item.type === 'function_call') {
        send('response.output_item.added', { output_index: 0, item: { ...item, status: 'in_progress', arguments: '' } })
        send('response.function_call_arguments.delta', { output_index: 0, item_id: item.id, delta: item.arguments })
        send('response.function_call_arguments.done', { output_index: 0, item_id: item.id, arguments: item.arguments })
      } else {
        send('response.output_item.added', { output_index: 0, item: { ...item, status: 'in_progress', content: [] } })
        send('response.content_part.added', { output_index: 0, item_id: item.id, content_index: 0, part: { type: 'output_text', text: '', annotations: [] } })
        send('response.output_text.delta', { output_index: 0, item_id: item.id, content_index: 0, delta: text })
        send('response.output_text.done', { output_index: 0, item_id: item.id, content_index: 0, text })
        send('response.content_part.done', { output_index: 0, item_id: item.id, content_index: 0, part: item.content[0] })
      }
      send('response.output_item.done', { output_index: 0, item })
      send('response.completed', { response })
      res.end()
    } catch {
      // A cancelled request or malformed body must not reject the HTTP event handler.
      if (!res.headersSent) res.writeHead(400)
      res.end()
    }
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  return { requests, url: `http://127.0.0.1:${server.address().port}/v1`, close: () => new Promise((resolve) => { server.close(resolve); server.closeAllConnections() }) }
}
