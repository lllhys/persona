import { start } from '../../lib/plugin.js'

const tokenFile = process.argv[2]
if (!tokenFile) throw new Error('token file argument required')

const scope = {
  sessionController: {},
  on: () => () => {},
  logger: { info: (message) => console.log(message) },
}
const runtime = start({
  ctx: scope,
  config: {
    assistant: false,
    phoneTools: false,
    listen: { port: 0, token_file: tokenFile },
  },
  log: (message) => console.log(message),
})

process.on('SIGTERM', () => {
  runtime.stop()
})
