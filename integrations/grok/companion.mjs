#!/usr/bin/env node
import { parseArgs } from 'node:util'
import { createGrokRuntime } from './lib/runtime.js'

const { values } = parseArgs({ options: {
  socket: { type: 'string' }, 'grok-bin': { type: 'string' }, 'grok-home': { type: 'string' },
  model: { type: 'string' }, effort: { type: 'string' }, launcher: { type: 'string' }, 'permission-mode': { type: 'string' },
  'display-name': { type: 'string' }, 'assistant-for': { type: 'string' },
} })
const runtime = createGrokRuntime({ socketPath: values.socket, bin: values['grok-bin'], grokHome: values['grok-home'],
  model: values.model, effort: values.effort, mode: values['permission-mode'], launcher: values.launcher,
  displayName: values['display-name'], assistantFor: values['assistant-for'], log: (message) => process.stderr.write(`persona-grok: ${message}\n`) })
runtime.start()
const alive = setInterval(() => {}, 1 << 30)
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => {
  clearInterval(alive)
  void runtime.stop().finally(() => process.exit(0))
})
