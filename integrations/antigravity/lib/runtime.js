import { daemonBinary, mcpServers } from './compat.js'
import { contextWrapped } from '../../runtime-sdk/lib/compat.js'
// Antigravity (agy) runtime for Persona (docs/integrations/runtimes.md).
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { homedir } from 'node:os'
import { basename, join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { createRuntime, interactionId, page, snippet, summaryOf } from '../../runtime-sdk/index.js'
import { isAssistantDir } from '../../runtime-sdk/lib/identity.js'
import { AgyAcpClient } from './acp.js'
import { discoverModels, mapModels } from './models.js'
import {
  agyModeToPersona,
  LABEL,
  mapToolInput,
  mapToolName,
  nativeWorking,
  personaModeToAgy,
  readTranscriptSteps,
  RUNTIME,
  stepsToItems,
  transcriptStatus,
  toAgyId,
  toPersonaId,
  workspacePathOf,
} from './map.js'

export function createAgyRuntime(options = {}) {
  const home = options.home || homedir()
  const agyDir = options.agyDir || process.env.ANTIGRAVITY_CLI_CONFIG_DIR || join(home, '.gemini', 'antigravity-cli')
  const personadBin = daemonBinary(home, options)
  const agyBin = options.agyBin || join(home, '.local', 'bin', 'agy')
  const log = options.log || ((m) => console.error(m))
  const listDays = options.listDays ?? 14
  const maxSessions = options.maxSessions ?? 80

  const activeTurns = new Map() // personaId -> { streamingText, startedAt }
  const createdAt = new Map() // owned sessions remain discoverable before their first transcript export
  const completedTurns = new Set() // owned ACP turns that ended before a native transcript was available
  const sessionTitles = new Map() // agyId -> title
  const sessionCwds = new Map() // agyId -> cwd
  const sessionModels = new Map() // agyId -> modelId
  const sessionModes = new Map() // agyId -> { mode, effort }
  const archived = new Set() // agyId
  const lastPushedSeq = new Map() // personaId -> number
  const assistants = new Set() // personaId
  const permissionAsks = new Map() // agyId -> Set of AbortControllers, one per permission card still on the phone
  const cancelling = new Set() // agyId: interrupted, its turn has not ended yet

  const acp = new AgyAcpClient({
    bin: existsSync(agyBin) ? agyBin : 'agy',
    log,
    onPermissionRequest: async (params) => handlePermissionRequest(params),
  })

  let catalog = []
  let catalogAt = 0
  let catalogFlight
  let catalogTimer
  const catalogAbort = new AbortController()
  let stopped = false
  const launchers = [{ name: 'agy', agent: RUNTIME, models: [] }]

  function publishModels(entries = catalog) {
    catalog = mapModels(entries, readAgySettings().model)
    const selected = catalog.find((model) => model.default)
    rt.launchersChanged([{ name: 'agy', agent: RUNTIME, model: selected?.name || readAgySettings().model || '', models: catalog }])
  }

  async function models(params = {}) {
    if (!params.refresh && catalog.length && Date.now() - catalogAt < 300_000) {
      publishModels()
      return { agent: RUNTIME, models: catalog }
    }
    if (!catalogFlight) {
      catalogFlight = discoverModels(acp.bin, { signal: catalogAbort.signal }).then((entries) => {
        if (!stopped) {
          catalogAt = Date.now()
          publishModels(entries)
        }
        return { agent: RUNTIME, models: catalog }
      }).finally(() => { catalogFlight = undefined })
    }
    return catalogFlight
  }

  function sessionConfiguration(rawId, result) {
    if (!result) return
    if (result.models?.availableModels) publishModels(result.models.availableModels)
    if (result.models?.currentModelId) sessionModels.set(rawId, result.models.currentModelId)
    if (result.modes?.currentModeId) {
      const cur = sessionModes.get(rawId) || {}
      const initialBypass = !cur.mode && configuredMode() === 'bypassPermissions'
      sessionModes.set(rawId, { ...cur, mode: initialBypass ? 'bypassPermissions' : agyModeToPersona(result.modes.currentModeId) })
    }
    const config = result.configOptions
    if (Array.isArray(config)) {
      const model = config.find((option) => option.category === 'model' || option.id === 'model')?.currentValue
      const effort = config.find((option) => option.id === 'thought_level')?.currentValue
      const mode = config.find((option) => option.id === 'mode')?.currentValue
      const cur = sessionModes.get(rawId) || {}
      sessionModes.set(rawId, { ...cur, ...(effort ? { effort } : {}), ...(mode && !(cur.mode === 'bypassPermissions' && ['accept-edits', 'default'].includes(mode)) ? { mode: agyModeToPersona(mode) } : {}) })
      if (model) {
        const variant = catalog.find((entry) => entry.id === `${model}-${effort}`) || catalog.find((entry) => entry.id === model)
        if (variant) sessionModels.set(rawId, variant.id)
      }
    }
    rt.sessionsChanged()
  }

  const rt = createRuntime(
    {
      listSessions,
      items,
      cursor,
      item,
      send,
      interrupt,
      start,
      rename,
      close,
      modes,
      models,
    },
    {
      runtime: RUNTIME,
      name: LABEL,
      agent: RUNTIME,
      version: '0.1.0',
      launcher: 'agy',
      modelSwitch: true,
      launchers,
      assistant: true,
      socketPath: options.socketPath,
      log,
    },
  )

  acp.on('update', (params) => {
    const rawId = params?.sessionId
    const upd = params?.update
    if (!rawId || !upd) return
    const pid = toPersonaId(rawId)
    switch (upd.sessionUpdate) {
      case 'config_option_update':
        sessionConfiguration(rawId, upd)
        break
      case 'session_info_update':
        if (typeof upd.title === 'string' && upd.title.trim()) {
          sessionTitles.set(rawId, upd.title.trim())
          rt.sessionsChanged()
        }
        break
      case 'current_mode_update':
        if (typeof upd.currentModeId === 'string') {
          const cur = sessionModes.get(rawId) || {}
          const mapped = agyModeToPersona(upd.currentModeId)
          if (cur.mode === 'bypassPermissions' && ['acceptEdits', 'default'].includes(mapped)) {
            break
          }
          sessionModes.set(rawId, { ...cur, mode: mapped })
          rt.sessionsChanged()
        }
        break
      case 'agent_message_chunk': {
        const chunk = upd.content?.text
        if (typeof chunk === 'string' && chunk) {
          const turn = activeTurns.get(pid)
          if (turn) {
            turn.streamingText += chunk
            rt.stream(pid, turn.streamingText)
          }
        }
        break
      }
      case 'tool_call':
      case 'tool_call_update':
        syncTranscriptAppend(rawId)
        break
      default:
        break
    }
  })

  function readAgySettings() {
    const cfgFile = join(agyDir, 'settings.json')
    if (!existsSync(cfgFile)) return {}
    try {
      return JSON.parse(readFileSync(cfgFile, 'utf8')) || {}
    } catch {
      return {}
    }
  }

  function effectiveModeOf(rawId) {
    const cur = sessionModes.get(rawId)
    if (cur?.mode) return cur.mode
    return configuredMode()
  }

  function configuredMode() {
    const cfg = readAgySettings()
    if (cfg.toolPermission === 'always-proceed' || cfg.agentMode === 'bypassPermissions') {
      return 'bypassPermissions'
    }
    if (cfg.agentMode === 'auto') return 'auto'
    if (cfg.agentMode === 'accept-edits') return 'acceptEdits'
    if (cfg.agentMode === 'plan') return 'plan'
    return 'default'
  }

  function shouldBypassPermission(rawId) {
    const mode = effectiveModeOf(rawId)
    return mode === 'bypassPermissions'
  }

  async function handlePermissionRequest(params) {
    const rawId = params?.sessionId
    const pid = toPersonaId(rawId)
    const tc = params?.toolCall || {}
    const rawName = tc.title || tc.name || 'run_command'
    const toolName = mapToolName(rawName)
    const toolInput = mapToolInput(rawName, tc.rawInput || tc.input || {})
    const opts = Array.isArray(params?.options) ? params.options : []
    const allowAlwaysOpt = opts.find((o) => o.kind === 'allow_always') || opts.find((o) => o.kind === 'allow_once') || opts[0]
    const allowOpt = opts.find((o) => o.kind === 'allow_once') || allowAlwaysOpt
    const rejectOpt = opts.find((o) => o.kind === 'reject_once') || opts.find((o) => o.kind === 'reject_always')

    // A request that crossed the interrupt on the wire: ACP wants every pending one answered 'cancelled'.
    if (cancelling.has(rawId) || archived.has(rawId)) return { outcome: { outcome: 'cancelled' } }
    if (shouldBypassPermission(rawId) && allowAlwaysOpt) {
      return { outcome: { outcome: 'selected', optionId: allowAlwaysOpt.optionId } }
    }

    // Interrupt and the end of the turn withdraw the card (the SDK takes it off the phone) and agy hears 'cancelled'.
    const ac = new AbortController()
    let asks = permissionAsks.get(rawId)
    if (!asks) permissionAsks.set(rawId, (asks = new Set()))
    asks.add(ac)
    let res
    try {
      res = await rt.ask({
        id: interactionId(pid, tc.toolCallId || String(Date.now())),
        kind: 'permission',
        session_id: pid,
        tool_name: toolName,
        tool_input: toolInput,
        summary: summaryOf(toolName, toolInput),
      }, { signal: ac.signal })
    } catch (err) {
      if (ac.signal.aborted) return { outcome: { outcome: 'cancelled' } }
      throw err
    } finally {
      asks.delete(ac)
      if (asks.size === 0 && permissionAsks.get(rawId) === asks) permissionAsks.delete(rawId)
    }

    if (res?.allow && allowOpt) {
      return { outcome: { outcome: 'selected', optionId: allowOpt.optionId } }
    }
    if (rejectOpt) {
      return { outcome: { outcome: 'selected', optionId: rejectOpt.optionId } }
    }
    return { outcome: { outcome: 'cancelled' } }
  }

  /** Take the session's permission cards off the phone: each one answers agy 'cancelled'. */
  function withdrawPermissionAsks(rawId, why) {
    const asks = permissionAsks.get(rawId)
    if (!asks) return
    permissionAsks.delete(rawId)
    for (const ac of asks) ac.abort(new Error(why))
  }

  function readCacheMetadata() {
    const metaFile = join(agyDir, 'cache', 'conversation_metadata.json')
    const lastFile = join(agyDir, 'cache', 'last_conversations.json')
    let convs = {}
    const cwdByConv = {}
    if (existsSync(metaFile)) {
      try {
        convs = JSON.parse(readFileSync(metaFile, 'utf8'))?.conversations || {}
      } catch {}
    }
    if (existsSync(lastFile)) {
      try {
        const last = JSON.parse(readFileSync(lastFile, 'utf8')) || {}
        for (const [dir, cid] of Object.entries(last)) {
          if (typeof cid === 'string' && !cwdByConv[cid]) cwdByConv[cid] = dir
        }
      } catch {}
    }
    return { convs, cwdByConv }
  }

  function brainDirOf(rawId) {
    return join(agyDir, 'brain', rawId)
  }

  function historyOf(rawId) {
    return `agy:${rawId}`
  }

  function readConversationStates() {
    const path = join(agyDir, 'conversation_summaries.db')
    if (!existsSync(path)) return new Map()
    let db
    try {
      db = new DatabaseSync(path, { readOnly: true })
      db.exec('PRAGMA query_only = 1; PRAGMA busy_timeout = 50')
      const rows = db.prepare(`
        SELECT conversation_id, status, not_fully_idle, killed, step_count,
               title, workspace_uris, last_modified_time
        FROM conversation_summaries
      `).all()
      return new Map(rows.map((row) => {
        let workspaceUris = []
        try {
          const parsed = JSON.parse(row.workspace_uris)
          if (Array.isArray(parsed)) workspaceUris = parsed.filter((uri) => typeof uri === 'string' && uri)
        } catch {}
        return [row.conversation_id, { ...row, WorkspaceURIs: workspaceUris }]
      }))
    } catch {
      // A missing or unavailable native summary cannot establish that a text reply ended the turn.
      return new Map()
    } finally {
      db?.close()
    }
  }

  function syncTranscriptAppend(rawId) {
    const pid = toPersonaId(rawId)
    const bDir = brainDirOf(rawId)
    const steps = readTranscriptSteps(bDir, false)
    if (steps.length === 0) return
    const { items: allItems, toSeq } = stepsToItems(steps, { brainDir: bDir, full: false })
    const prevSeq = lastPushedSeq.get(pid) ?? 0
    if (toSeq > prevSeq) {
      const deltaItems = allItems.filter((it) => it.seq > prevSeq)
      lastPushedSeq.set(pid, toSeq)
      rt.append(pid, {
        items: deltaItems,
        fromSeq: prevSeq + 1,
        toSeq,
        history: historyOf(rawId),
      })
    }
  }

  async function listSessions() {
    const { convs, cwdByConv } = readCacheMetadata()
    const states = readConversationStates()
    const brainRoot = join(agyDir, 'brain')
    const seenIds = new Set([...Object.keys(convs), ...states.keys(), ...createdAt.keys()])
    if (existsSync(brainRoot)) {
      try {
        for (const name of readdirSync(brainRoot)) {
          if (/^[0-9a-fA-F-]{36}$/.test(name)) seenIds.add(name)
        }
      } catch {}
    }

    const candidates = []
    for (const rawId of seenIds) {
      if (archived.has(rawId)) continue
      const meta = convs[rawId]
      const summary = meta?.summary
      const native = states.get(rawId)
      const pid = toPersonaId(rawId)
      const running = activeTurns.has(pid) || nativeWorking(native)
      const bDir = brainDirOf(rawId)
      const transcriptFile = join(bDir, '.system_generated', 'logs', 'transcript.jsonl')
      const hasTranscript = existsSync(transcriptFile)
      if (!hasTranscript && !running && !rt.touched(pid)) continue
      if (meta?.is_internal && !summary && !rt.touched(pid) && !running) continue

      let mtimeMs = Math.max(
        Date.parse(summary?.UpdatedAt || meta?.last_modified_time || '') || 0,
        Date.parse(native?.last_modified_time || '') || 0,
      )
      if (hasTranscript) {
        try {
          const st = statSync(transcriptFile)
          if (st.mtimeMs > mtimeMs) mtimeMs = st.mtimeMs
        } catch {}
      }
      if (!mtimeMs) mtimeMs = createdAt.get(rawId) || Date.now()

      const cwd = sessionCwds.get(rawId) || workspacePathOf(native, workspacePathOf(summary, cwdByConv[rawId] || home))
      if (cwd) sessionCwds.set(rawId, cwd)
      if (isAssistantDir(cwd)) assistants.add(pid)

      candidates.push({ rawId, pid, summary, native, cwd, mtimeMs, running, hasTranscript })
    }

    const chosen = rt.recent(candidates, {
      days: listDays,
      max: maxSessions,
      pinned: (c) => c.running || assistants.has(c.pid),
      id: (c) => c.pid,
      updatedAt: (c) => c.mtimeMs,
    })

    return chosen.map(({ rawId, pid, summary, native, cwd, mtimeMs, running, hasTranscript }) => {
      const steps = readTranscriptSteps(brainDirOf(rawId), false)
      const parsed = stepsToItems(steps, { brainDir: brainDirOf(rawId), full: false })
      if (!lastPushedSeq.has(pid)) lastPushedSeq.set(pid, parsed.toSeq)
      let status = running ? 'working' : transcriptStatus(steps, native)
      if (!hasTranscript && !running) {
        if (completedTurns.has(pid)) status = 'done'
        else if (createdAt.has(rawId)) status = 'idle'
      }
      const title =
        sessionTitles.get(rawId) ||
        native?.title ||
        summary?.Title ||
        (cwd ? basename(cwd) : 'Antigravity')
      const modeInfo = sessionModes.get(rawId)
      const row = {
        session_id: pid,
        title,
        cwd: cwd || home,
        status,
        updated_at: new Date(mtimeMs).toISOString(),
        rev: Math.max(1, parsed.toSeq),
        last_text: parsed.lastText || snippet(summary?.Preview || ''),
        model: sessionModels.get(rawId),
        launcher: 'agy',
        agent_label: LABEL,
      }
      if (cwd) {
        row.space_id = cwd
        row.space_name = basename(cwd)
      }
      const effMode = effectiveModeOf(rawId)
      if (effMode && effMode !== 'default') row.permission_mode = effMode
      if (modeInfo?.effort) row.effort = modeInfo.effort
      return row
    })
  }

  async function items(params) {
    const rawId = toAgyId(params.session_id)
    const bDir = brainDirOf(rawId)
    const steps = readTranscriptSteps(bDir, false)
    const { items: allItems, toSeq } = stepsToItems(steps, { brainDir: bDir, full: false })
    return page(allItems, params, { history: historyOf(rawId), toSeq })
  }

  async function cursor(sessionId) {
    const rawId = toAgyId(sessionId)
    const steps = readTranscriptSteps(brainDirOf(rawId), false)
    const { toSeq } = stepsToItems(steps, { brainDir: brainDirOf(rawId), full: false })
    return toSeq
  }

  async function item(params) {
    const rawId = toAgyId(params.session_id)
    const bDir = brainDirOf(rawId)
    const steps = readTranscriptSteps(bDir, true)
    const { items: fullItems } = stepsToItems(steps, { brainDir: bDir, full: true })
    const found = fullItems.find((it) => it.id === params.item_id)
    if (!found) throw new Error(`item ${params.item_id} not found`)
    return found
  }

  function mcpServersFor(pid, isAsst) {
    if (!existsSync(personadBin)) return []
    return mcpServers(personadBin, pid, isAsst)
  }

  async function applySessionOptions(rawId, { model, permission_mode, effort }) {
    try {
      if (model) {
        await acp.setModel(rawId, model)
        sessionModels.set(rawId, model)
        const selected = catalog.find((entry) => entry.id === model)
        if (selected?.default_effort) {
          const cur = sessionModes.get(rawId) || {}
          sessionModes.set(rawId, { ...cur, effort: selected.default_effort })
        }
      }
      if (permission_mode) {
        await acp.setMode(rawId, personaModeToAgy(permission_mode))
        const cur = sessionModes.get(rawId) || {}
        sessionModes.set(rawId, { ...cur, mode: permission_mode })
      }
      if (effort) {
        const res = await acp.setConfigOption(rawId, 'thought_level', effort)
        const cur = sessionModes.get(rawId) || {}
        sessionModes.set(rawId, { ...cur, effort })
        sessionConfiguration(rawId, res)
      }
    } finally {
      // A multi-field change may partially succeed. Publish each acknowledged field even when a later one fails.
      rt.sessionsChanged()
    }
  }

  async function loadSession(rawId, cwd, servers) {
    sessionConfiguration(rawId, await acp.ensureSessionLoaded(rawId, cwd, servers))
  }

  async function start(params) {
    const cwd = params.cwd || home
    const isAsst = Boolean(params.assistant) || isAssistantDir(cwd)
    const res = await acp.newSession({ cwd, mcpServers: [] })
    const rawId = res.sessionId
    createdAt.set(rawId, Date.now())
    sessionConfiguration(rawId, res)
    const pid = toPersonaId(rawId)
    sessionCwds.set(rawId, cwd)
    if (isAsst) assistants.add(pid)

    try {
      await loadSession(rawId, cwd, mcpServersFor(pid, isAsst))
      await applySessionOptions(rawId, {
        model: params.model,
        permission_mode: params.permission_mode || configuredMode(),
        effort: params.effort,
      })
    } catch (err) {
      archived.add(rawId)
      await acp.closeSession(rawId)
      rt.sessionsChanged()
      throw err
    }

    const session = {
      session_id: pid,
      title: basename(cwd) || LABEL,
      cwd,
      status: params.prompt ? 'working' : 'done',
      updated_at: new Date().toISOString(),
      rev: 1,
      model: sessionModels.get(rawId),
      launcher: 'agy',
      effort: sessionModes.get(rawId)?.effort,
      agent_label: LABEL,
      space_id: cwd,
      space_name: basename(cwd),
      permission_mode: effectiveModeOf(rawId) !== 'default' ? effectiveModeOf(rawId) : undefined,
    }

    if (params.prompt) {
      await send({
        session_id: pid,
        text: params.prompt,
        client_msg_id: `start-${Date.now()}`,
        assistant: isAsst,
        mode: params.permission_mode,
        model: params.model,
        effort: params.effort,
      })
    }

    rt.sessionsChanged()
    return { session }
  }

  async function send(params) {
    const pid = toPersonaId(params.session_id)
    completedTurns.delete(pid)
    const rawId = toAgyId(pid)
    if (archived.has(rawId)) throw new Error('Antigravity session is closed')
    const cwd = sessionCwds.get(rawId) || home
    const isAsst = Boolean(params.assistant) || assistants.has(pid) || isAssistantDir(cwd)
    if (isAsst) assistants.add(pid)

    await loadSession(rawId, cwd, mcpServersFor(pid, isAsst))
    await applySessionOptions(rawId, {
      model: params.model,
      permission_mode: params.mode,
      effort: params.effort,
    })

    const blocks = []
    let fullText = params.text || ''
    if (typeof params.context === 'string' && params.context.trim()) {
      const ctx = params.context.trim()
      const wrapped =
        contextWrapped(ctx)
          ? ctx
          : `<persona-context>\n${ctx}\n</persona-context>`
      fullText = `${wrapped}\n\n${fullText}`
    }
    if (fullText) {
      blocks.push({ type: 'text', text: fullText })
    }
    if (Array.isArray(params.images)) {
      for (const img of params.images) {
        if (img?.data) {
          blocks.push({
            type: 'image',
            data: img.data,
            mimeType: img.media_type || img.mime || 'image/png',
          })
        }
      }
    }

    const prevTurn = activeTurns.get(pid)
    if (prevTurn?.pollTimer) clearInterval(prevTurn.pollTimer)
    const pollTimer = setInterval(() => syncTranscriptAppend(rawId), 400)
    const turnToken = Symbol('turn')
    const prevPromise = prevTurn?.promise || Promise.resolve()
    const promise = prevPromise
      .catch(() => {})
      .then(() => {
        if (!archived.has(rawId)) return acp.prompt(rawId, blocks)
      })
      .catch((err) => log(`agy prompt error (${pid}): ${err.message}`))
      .finally(() => {
        // The turn is over: a card still waiting has nothing to answer for any more.
        cancelling.delete(rawId)
        withdrawPermissionAsks(rawId, 'the turn ended')
        if (activeTurns.get(pid)?.token === turnToken) {
          clearInterval(pollTimer)
          activeTurns.delete(pid)
          completedTurns.add(pid)
          rt.streamEnd(pid)
          syncTranscriptAppend(rawId)
          rt.sessionsChanged()
        }
      })
    activeTurns.set(pid, { streamingText: '', startedAt: Date.now(), pollTimer, token: turnToken, promise })
    rt.sessionsChanged()
  }

  async function interrupt(params) {
    const rawId = toAgyId(params.session_id)
    if (activeTurns.has(toPersonaId(rawId))) cancelling.add(rawId)
    withdrawPermissionAsks(rawId, 'interrupted')
    await acp.cancel(rawId)
  }

  async function rename(params) {
    const rawId = toAgyId(params.session_id)
    if (typeof params.title === 'string' && params.title.trim()) {
      sessionTitles.set(rawId, params.title.trim())
      rt.sessionsChanged()
    }
  }

  async function close(params) {
    const rawId = toAgyId(params.session_id)
    archived.add(rawId)
    await interrupt(params)
    await acp.closeSession(rawId)
    rt.sessionsChanged()
  }

  async function modes(params) {
    const rawId = toAgyId(params.session_id)
    if (archived.has(rawId)) throw new Error('Antigravity session is closed')
    if (params.model && (activeTurns.has(toPersonaId(rawId)) || nativeWorking(readConversationStates().get(rawId)))) {
      throw new Error('Antigravity session is busy')
    }
    const cwd = sessionCwds.get(rawId) || home
    await loadSession(rawId, cwd, mcpServersFor(toPersonaId(rawId), assistants.has(toPersonaId(rawId))))
    await applySessionOptions(rawId, {
      model: params.model,
      permission_mode: params.permission_mode,
      effort: params.effort,
    })
    rt.sessionsChanged()
  }

  return {
    start: () => {
      void models().catch((err) => log(`agy model discovery error: ${err.message}`))
      catalogTimer = setInterval(() => { void models({ refresh: true }).catch((err) => log(`agy model discovery error: ${err.message}`)) }, 300_000)
      catalogTimer.unref?.()
      return rt.start()
    },
    /** Resolves once agy is gone. */
    stop: () => {
      stopped = true
      clearInterval(catalogTimer)
      catalogAbort.abort()
      rt.stop()
      // A turn's transcript poll would otherwise keep running; acp.stop() fails the prompts themselves.
      for (const turn of activeTurns.values()) clearInterval(turn.pollTimer)
      activeTurns.clear()
      return acp.stop()
    },
  }
}
