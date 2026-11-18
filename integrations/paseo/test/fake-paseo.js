// A fake Paseo daemon for the bridge's tests: its WebSocket endpoint (/ws) with the requests the bridge makes, in the
// shapes Paseo 0.14 / 0.15 use (packages/protocol messages.ts), and helpers to make things happen in it.

import { createHash, randomUUID } from 'node:crypto'
import { createServer } from 'node:http'
import { WsConnection } from '../../runtime-sdk/lib/listen.js'

const GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11'

/** An agent snapshot with what the bridge reads; fields override. */
export function agentSnapshot(fields = {}) {
  const now = new Date().toISOString()
  return {
    id: randomUUID(), provider: 'claude', cwd: '/tmp', workspaceId: 'wks_1', model: null, thinkingOptionId: null,
    effectiveThinkingOptionId: null, createdAt: now, updatedAt: now, lastUserMessageAt: null, status: 'idle', activeTurn: null,
    capabilities: {}, currentModeId: null, availableModes: [], pendingPermissions: [], persistence: null, runtimeInfo: null,
    title: null, labels: {}, archivedAt: null, ...fields,
  }
}

/** Paseo's Auto Accept switch as an ACP agent lists it (acp-agent.ts buildACPAutoAcceptFeature). */
export function autoAcceptFeature(value = false) {
  return { type: 'toggle', id: 'auto_accept', label: 'Auto Accept', description: 'Automatically approves ACP permission prompts.',
    tooltip: 'Auto accept permission prompts', icon: 'shield-check', value }
}

/**
 * A workspace descriptor (messages.ts WorkspaceDescriptorPayload) with what the bridge reads; fields override. A git
 * one's project placement is its own unless given (project: null for none, as Paseo's answer to workspace.create).
 */
export function workspaceDescriptor(fields = {}) {
  const now = new Date().toISOString()
  const dir = fields.workspaceDirectory ?? '/tmp'
  const kind = fields.workspaceKind ?? 'local_checkout'
  const git = kind !== 'directory'
  return {
    id: `wks_${randomUUID().replaceAll('-', '').slice(0, 16)}`, projectId: 'prj_1', projectDisplayName: 'proj', projectRootPath: dir,
    workspaceDirectory: dir, projectKind: git ? 'git' : 'non_git', workspaceKind: kind, name: fields.name ?? (git ? 'main' : 'notes'),
    title: null, background: false, archivingAt: null, status: 'done', statusEnteredAt: null, activityAt: null, createdAt: now,
    scripts: [], gitRuntime: git ? { currentBranch: 'main', isPaseoOwnedWorktree: false, isDirty: false } : null, githubRuntime: null,
    project: { projectKey: fields.projectId ?? 'prj_1', projectName: fields.projectDisplayName ?? 'proj', workspaceName: fields.name ?? 'main',
      checkout: git ? { cwd: dir, isGit: true, currentBranch: 'main', worktreeRoot: dir, mainRepoRoot: null, isPaseoOwnedWorktree: false } : { cwd: dir, isGit: false } },
    ...fields,
  }
}

/**
 * What Paseo 0.13.2's AgentSessionConfigSchema (messages.ts McpServerConfigSchema) takes in config.mcpServers: a record of
 * servers, each stdio (command, args, env), http or sse, discriminated by `type` — which stdio servers must carry.
 * Returns what is wrong, or null.
 */
export function mcpServersError(servers) {
  if (servers === undefined) return null
  const isStrings = (a) => Array.isArray(a) && a.every((x) => typeof x === 'string')
  const isRecord = (o) => o && typeof o === 'object' && !Array.isArray(o) && Object.values(o).every((x) => typeof x === 'string')
  if (!servers || typeof servers !== 'object' || Array.isArray(servers)) return 'mcpServers: not a record'
  for (const [name, s] of Object.entries(servers)) {
    const at = `mcpServers.${name}`
    if (s?.type === 'stdio') {
      if (typeof s.command !== 'string') return `${at}.command: not a string`
      if (s.args !== undefined && !isStrings(s.args)) return `${at}.args: not a list of strings`
      if (s.env !== undefined && !isRecord(s.env)) return `${at}.env: not a record of strings`
    } else if (s?.type === 'http' || s?.type === 'sse') {
      if (typeof s.url !== 'string') return `${at}.url: not a string`
    } else {
      return `${at}.type: expected stdio, http or sse`
    }
  }
  return null
}

/**
 * @param {import('node:test').TestContext} t
 * @param {{ version?: string, providers?: object, entries?: object[], agents?: {agent: object, project?: object}[],
 *   handlers?: object, duringPage?: (msg: object) => void }} [o]
 */
export async function startFakePaseo(t, o = {}) {
  const f = {
    version: o.version ?? '0.14.0',
    providers: o.providers ?? {},
    entries: o.entries ?? [],
    agents: new Map((o.agents ?? []).map((e) => [e.agent.id, { agent: e.agent, project: e.project ?? null }])),
    workspaces: new Map((o.workspaces ?? []).map((d) => [d.id, d])), // id → descriptor (archived ones leave)
    worktreesRoot: o.worktreesRoot ?? '/tmp/paseo-worktrees', // where workspace.create puts a worktree
    projects: new Set(o.projects ?? []), // project ids with no workspace (a rename finds them)
    renamed: [], // ['workspace' | 'project', id, the title or custom name set]
    archivedWorkspaces: [], // ids archive_workspace_request archived
    onArchiveWorkspace: undefined, // (descriptor) => the disk part of an archive (a test's; a throw is Paseo's error)
    timelines: new Map(), // agent id → { epoch, entries }
    loads: [], // ids of the agents a timeline request loaded from storage (closed or archived ones), in order
    conns: [],
    requests: [], // every session request, in order
    hellos: [],
    subs: new Map(), // subscriptionId → { conn, kind, events?, agentIds?, held? (while its first page is built: flush) }
    handlers: { ...o.handlers }, // a test's own answers: type → (msg) => payload | undefined
    refuse: undefined, // close code for the next hellos
    refuseDelay: 0, // ms before an MCP refusal is answered (a creation slow to fail)
    noMcp: new Set(o.noMcpProviders ?? []), // providers that take no MCP servers (Paseo's requireExternalMcpSupport)
    creations: new Map(), // idempotency key → { fingerprint, agentId }: the agents made for keyed creations (createAgent)
    // (msg) => what happens while Paseo builds a page of fetch_agents_request / fetch_workspaces_request (a test's):
    // after the page's entries are read, before its reply.
    duringPage: o.duringPage,
  }
  let n = 0
  const http = createServer()
  http.on('upgrade', (req, socket, head) => {
    const accept = createHash('sha1').update(req.headers['sec-websocket-key'] + GUID).digest('base64')
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`)
    const c = new WsConnection(socket, head)
    c.on('error', () => {})
    f.conns.push(c)
    c.on('data', (line) => onMessage(c, line))
    c.on('close', () => {
      for (const [id, s] of f.subs) if (s.conn === c) f.subs.delete(id)
    })
  })
  await new Promise((r) => http.listen(0, '127.0.0.1', r))
  f.port = http.address().port
  t.after(() => {
    for (const c of f.conns) c.socket.destroy()
    http.close()
  })

  const send = (c, inner) => c.write(JSON.stringify({ type: 'session', message: inner }))
  const reply = (c, type, payload) => send(c, { type, payload })

  function onMessage(c, line) {
    if (c.hung) return
    const m = JSON.parse(line)
    if (m.type === 'hello') {
      f.hellos.push(m)
      if (f.refuse) {
        c.fail(f.refuse, 'refused')
        return
      }
      send(c, { type: 'status', payload: { status: 'server_info', protocolVersion: 1, serverId: 'srv_test', hostname: 'test', version: f.version, features: o.features ?? {} } })
      return
    }
    if (m.type === 'ping') {
      c.write('{"type":"pong"}')
      return
    }
    if (m.type !== 'session') return
    const msg = m.message
    f.requests.push(msg)
    const own = f.handlers[msg.type]?.(msg, c)
    if (own === null) return // the test answers later (or never)
    if (own !== undefined) return reply(c, own.type ?? replyType(msg.type), { requestId: msg.requestId, ...own.payload })
    answer(c, msg)
  }

  function answer(c, msg) {
    const id = msg.requestId
    const sub = (kind, extra) => {
      const sid = `sub_${++n}`
      f.subs.set(sid, { conn: c, kind, ...extra })
      return sid
    }
    switch (msg.type) {
      case 'project.list.request':
        return reply(c, 'project.list.response', { requestId: id, projects: [] })
      case 'get_daemon_config_request':
        return reply(c, 'get_daemon_config_response', { requestId: id, config: { providers: f.providers, mcp: { injectIntoAgents: true } } })
      case 'get_providers_snapshot_request':
        return reply(c, 'get_providers_snapshot_response', { requestId: id, entries: f.entries, generatedAt: new Date().toISOString() })
      case 'session.events.set_subscription.request':
        return reply(c, 'session.events.set_subscription.response', { requestId: id, subscriptionId: sub('events', { events: msg.events }) })
      case 'fetch_agents_request': {
        // Archived agents only when asked for, and then among the others; in the order asked for, a page at a time.
        // A new subscription's updates are held while its page is built (duringPage) and sent right after its reply.
        const sid = msg.subscribe ? sub('agents', { held: new Map() }) : null
        const list = [...f.agents.values()].filter((e) => msg.filter?.includeArchived || !e.agent.archivedAt)
        const by = msg.sort?.[0]
        if (by?.key === 'updated_at') {
          const at = (e) => Date.parse(e.agent.updatedAt) || 0
          list.sort((x, y) => (by.direction === 'asc' ? at(x) - at(y) : at(y) - at(x)))
        }
        const from = Number(msg.page?.cursor) || 0
        const limit = msg.page?.limit ?? list.length
        const more = from + limit < list.length
        const entries = list.slice(from, from + limit).map((e) => ({ ...e })) // as they are now (an archive replaces e.agent)
        f.duringPage?.(msg)
        return together(c, () => {
          reply(c, 'fetch_agents_response', {
            requestId: id, subscriptionId: sid, entries, pageInfo: { nextCursor: more ? String(from + limit) : null, prevCursor: null, hasMore: more },
          })
          if (sid) flush(sid, new Map(entries.map((e) => [e.agent.id, Date.parse(e.agent.updatedAt)])))
        })
      }
      case 'fetch_workspaces_request': {
        // Background workspaces only when asked (Paseo's matchesFilter); a page at a time. A new subscription's updates
        // are held while its page is built, as for the agents.
        const sid = msg.subscribe ? sub('workspaces', { held: new Map() }) : null
        const list = [...f.workspaces.values()].filter((d) => msg.includeBackground || msg.filter?.includeBackground || !d.background)
        const from = Number(msg.page?.cursor) || 0
        const limit = msg.page?.limit ?? list.length
        const more = from + limit < list.length
        const entries = list.slice(from, from + limit)
        f.duringPage?.(msg)
        return together(c, () => {
          reply(c, 'fetch_workspaces_response', {
            requestId: id, subscriptionId: sid, entries, emptyProjects: [],
            pageInfo: { nextCursor: more ? String(from + limit) : null, prevCursor: null, hasMore: more },
          })
          if (sid) flush(sid)
        })
      }
      case 'workspace.create.request': {
        // A worktree: cut from the source's repository (Paseo 0.14 createWorktreeCore), its folder the slug or the branch,
        // no agent with it here. Its answer has no project placement (describeCreatedWorktreeWorkspace).
        const s = msg.source
        if (s?.kind === 'directory') {
          const d = workspaceDescriptor({ workspaceDirectory: s.path, projectRootPath: s.path, projectId: s.projectId, workspaceKind: 'directory' })
          f.upsertWorkspace(d)
          return reply(c, 'workspace.create.response', { requestId: id, workspace: d, setupTerminalId: null, error: null })
        }
        if (s?.kind !== 'worktree' || (!s.cwd && !s.projectId)) {
          return reply(c, 'workspace.create.response', { requestId: id, workspace: null, setupTerminalId: null, error: 'cwd or projectId is required', errorCode: 'source_required' })
        }
        const branch = s.action === 'checkout' ? s.refName : (s.branchName ?? s.worktreeSlug ?? 'generated-name')
        const slug = (s.worktreeSlug ?? branch).toLowerCase().replace(/[^a-z0-9]+/g, '-')
        const dir = `${f.worktreesRoot}/${slug}`
        const d = workspaceDescriptor({ workspaceDirectory: dir, projectRootPath: s.cwd, workspaceKind: 'worktree', name: branch, worktreeSlug: slug, project: undefined,
          gitRuntime: { currentBranch: branch, isPaseoOwnedWorktree: true, isDirty: false } })
        delete d.project
        f.upsertWorkspace(d)
        return reply(c, 'workspace.create.response', { requestId: id, workspace: d, setupTerminalId: null, error: null })
      }
      case 'workspace.title.set.request': {
        // Paseo 0.14 handleWorkspaceTitleSetRequest: an empty title clears it; the workspace's descriptor is sent again.
        const d = f.workspaces.get(msg.workspaceId)
        if (!d) return reply(c, 'workspace.title.set.response', { requestId: id, workspaceId: msg.workspaceId, accepted: false, title: null, error: 'Workspace not found' })
        const title = msg.title?.trim() || null
        f.renamed.push(['workspace', msg.workspaceId, title])
        reply(c, 'workspace.title.set.response', { requestId: id, workspaceId: msg.workspaceId, accepted: true, title, error: null })
        f.upsertWorkspace({ ...d, title, name: title ?? 'main', ...(d.project ? { project: { ...d.project, workspaceName: title ?? 'main' } } : {}) })
        return
      }
      case 'project.rename.request': {
        // handleProjectRenameRequest: the project's customName, then every workspace of the project sent again.
        const of = [...f.workspaces.values()].filter((d) => d.projectId === msg.projectId)
        if (!of.length && !f.projects.has(msg.projectId)) {
          return reply(c, 'project.rename.response', { requestId: id, projectId: msg.projectId, accepted: false, customName: null, error: 'Project not found' })
        }
        const customName = msg.customName?.trim() || null
        f.renamed.push(['project', msg.projectId, customName])
        reply(c, 'project.rename.response', { requestId: id, projectId: msg.projectId, accepted: true, customName, error: null })
        for (const d of of) {
          f.upsertWorkspace({ ...d, projectDisplayName: customName ?? d.projectDisplayName, projectCustomName: customName,
            ...(d.project ? { project: { ...d.project, projectName: customName ?? d.project.projectName } } : {}) })
        }
        return
      }
      case 'archive_workspace_request': {
        // handleArchiveWorkspaceRequest: archiving shown first (archivingAt), its agents archived, the record archived,
        // and a worktree Paseo made removed from the disk (f.onArchiveWorkspace does that part here).
        const d = f.workspaces.get(msg.workspaceId)
        if (!d) return reply(c, 'archive_workspace_response', { requestId: id, workspaceId: msg.workspaceId, archivedAt: null, error: `Workspace not found: ${msg.workspaceId}` })
        f.upsertWorkspace({ ...d, archivingAt: new Date().toISOString() })
        for (const [agentId, e] of f.agents) if (e.agent.workspaceId === msg.workspaceId) f.archive(agentId)
        f.archivedWorkspaces.push(msg.workspaceId)
        f.removeWorkspace(msg.workspaceId)
        Promise.resolve().then(() => f.onArchiveWorkspace?.(d)).then(
          () => reply(c, 'archive_workspace_response', { requestId: id, workspaceId: msg.workspaceId, archivedAt: new Date().toISOString(), error: null }),
          (err) => reply(c, 'archive_workspace_response', { requestId: id, workspaceId: msg.workspaceId, archivedAt: null, error: err.message }))
        return
      }
      case 'fetch_agent_request': {
        // An archived agent's record is still there; a deleted one is a null agent, not an error.
        const e = f.agents.get(msg.agentId)
        return reply(c, 'fetch_agent_response', { requestId: id, agent: e?.agent ?? null, project: e?.project ?? null, error: null })
      }
      case 'agent.timeline.set_subscription.request':
        return reply(c, 'agent.timeline.set_subscription.response', { requestId: id, agentIds: msg.agentIds, subscriptionId: sub('timeline', { agentIds: msg.agentIds }) })
      case 'subscription.release.request':
        f.subs.delete(msg.subscriptionId)
        return reply(c, 'subscription.release.response', { requestId: id, subscriptionId: msg.subscriptionId })
      case 'fetch_agent_timeline_request': {
        // Paseo loads an agent it let go before it reads the timeline (0.15.4 ensureAgentLoaded): its provider runs
        // again, and the lists see it loaded, its updatedAt as it was (a restore keeps it). An archived one is loaded
        // as history, still archived and listed nowhere.
        const stored = f.agents.get(msg.agentId)?.agent
        if (stored?.status === 'closed' || stored?.archivedAt) {
          f.loads.push(msg.agentId)
          if (!stored.archivedAt) f.upsert({ ...stored, status: 'idle' })
        }
        const a = f.agents.get(msg.agentId)
        const tl = f.timelines.get(msg.agentId) ?? { epoch: 'ep1', entries: [] }
        let list = tl.entries
        if (msg.direction === 'after' && msg.cursor) list = list.filter((e) => e.seqStart > msg.cursor.seq)
        if (msg.direction === 'tail' && msg.limit > 0) list = list.slice(-msg.limit)
        const maxSeq = Math.max(0, ...tl.entries.map((e) => e.seqEnd))
        const stale = msg.cursor && msg.cursor.epoch !== tl.epoch
        return reply(c, 'fetch_agent_timeline_response', {
          requestId: id, agentId: msg.agentId, agent: a?.agent ?? null, direction: msg.direction ?? 'tail', projection: 'projected',
          epoch: tl.epoch, reset: Boolean(stale), staleCursor: Boolean(stale), gap: false,
          window: { minSeq: 1, maxSeq, nextSeq: maxSeq + 1 }, entries: stale ? tl.entries : list, error: a ? null : 'Agent not found',
        })
      }
      case 'send_agent_message_request': {
        // A message to an archived agent takes it back from the archive first, its labels as they were (Paseo 0.14
        // sendPromptToAgent: unarchive, load, run).
        const e = f.agents.get(msg.agentId)
        if (e?.agent.archivedAt) f.upsert({ ...e.agent, archivedAt: null, status: 'running', updatedAt: new Date().toISOString() })
        return reply(c, 'send_agent_message_response', { requestId: id, agentId: msg.agentId, accepted: true, error: null })
      }
      case 'cancel_agent_request':
        return reply(c, 'cancel_agent_response', { requestId: id, agentId: msg.agentId, agent: f.agents.get(msg.agentId)?.agent ?? null, error: null })
      case 'update_agent_request': {
        // An archived agent's record is renamed where it is stored, and no list is told (it lists the agents Paseo has).
        // Labels are merged into the agent's (Paseo cannot drop one over the wire); a live agent's lists are told.
        const e = f.agents.get(msg.agentId)
        if (e?.agent.archivedAt && msg.name) e.agent = { ...e.agent, title: msg.name, updatedAt: new Date().toISOString() }
        if (e && msg.labels) {
          const labels = { ...e.agent.labels, ...msg.labels }
          if (e.agent.archivedAt) e.agent = { ...e.agent, labels }
          else f.update(msg.agentId, { labels })
        }
        return reply(c, replyType(msg.type), { requestId: id, agentId: msg.agentId, accepted: true, error: null })
      }
      case 'set_agent_model_request':
      case 'set_agent_mode_request':
      case 'set_agent_thinking_request': {
        // Paseo loads the agent first, and refuses an archived one (ensureUnarchivedAgentLoaded).
        if (f.agents.get(msg.agentId)?.agent.archivedAt) {
          return reply(c, replyType(msg.type), { requestId: id, agentId: msg.agentId, accepted: false, error: `Agent is archived: ${msg.agentId}` })
        }
        return reply(c, replyType(msg.type), { requestId: id, agentId: msg.agentId, accepted: true, error: null })
      }
      case 'set_agent_feature_request': {
        // Paseo loads the agent first (ensureLoaded: a closed ACP agent lists its switch again) and refuses a feature its
        // provider lacks; the new value reaches the lists.
        const e = f.agents.get(msg.agentId)
        const features = e?.agent.features ?? (f.providers[e?.agent.provider]?.extends === 'acp' ? [autoAcceptFeature()] : [])
        if (!e || e.agent.archivedAt || !features.some((x) => x.id === msg.featureId)) {
          return reply(c, replyType(msg.type), { requestId: id, agentId: msg.agentId, accepted: false, error: `Unknown ${e?.agent.provider} feature: ${msg.featureId}` })
        }
        f.update(msg.agentId, { features: features.map((x) => (x.id === msg.featureId ? { ...x, value: msg.value } : x)) })
        return reply(c, replyType(msg.type), { requestId: id, agentId: msg.agentId, accepted: true, error: null })
      }
      case 'archive_agent_request': {
        const at = new Date().toISOString()
        reply(c, 'agent_archived', { requestId: id, agentId: msg.agentId, archivedAt: at })
        f.archive(msg.agentId, at)
        return
      }
      case 'agent.create.request': {
        const bad = mcpServersError(msg.config.mcpServers)
        if (bad) return reply(c, 'agent.create.response', { requestId: id, agent: null, error: `Invalid agent config: ${bad}` })
        if (Object.keys(msg.config.mcpServers ?? {}).length && f.noMcp.has(msg.config.provider)) {
          const refuse = () => reply(c, 'agent.create.response', { requestId: id, agent: null, error: `Provider '${msg.config.provider}' does not support MCP servers` })
          if (f.refuseDelay) setTimeout(refuse, f.refuseDelay).unref()
          else refuse()
          return
        }
        return reply(c, 'agent.create.response', { requestId: id, ...f.createAgent(msg) })
      }
      case 'import_agent_request': {
        // Paseo 0.14 (import-sessions.ts): a session an agent has is not imported twice; one an archived agent has is
        // that agent, back from the archive with the request's labels over its own; any other is a new agent.
        const records = [...f.agents.values()].filter((e) => e.agent.persistence?.provider === msg.provider && e.agent.persistence?.sessionId === msg.sessionId)
        if (records.some((e) => !e.agent.archivedAt)) {
          return send(c, { type: 'status', payload: { status: 'agent_create_failed', requestId: id, error: `Provider session is already imported: ${msg.sessionId}` } })
        }
        const old = records.find((e) => e.agent.archivedAt)
        const agent = old
          ? { ...old.agent, archivedAt: null, status: 'idle', updatedAt: new Date().toISOString(), labels: { ...old.agent.labels, ...msg.labels },
            workspaceId: msg.workspaceId ?? old.agent.workspaceId }
          : agentSnapshot({ provider: msg.provider, cwd: msg.cwd, labels: msg.labels ?? {},
            persistence: { provider: msg.provider, sessionId: msg.sessionId, metadata: { provider: 'claude' } },
            runtimeInfo: { provider: msg.provider, sessionId: msg.sessionId } })
        f.upsert(agent)
        return send(c, { type: 'status', payload: { status: 'agent_resumed', requestId: id, agentId: agent.id, agent } })
      }
      case 'agent_permission_response': {
        const e = f.agents.get(msg.agentId)
        if (!e?.agent.pendingPermissions.some((r) => r.id === msg.requestId)) {
          return reply(c, 'rpc_error', { requestId: msg.requestId, requestType: msg.type, error: 'No pending permission request', code: 'handler_error' })
        }
        f.resolvePermission(msg.agentId, msg.requestId, msg.response, c)
        return
      }
      default:
        return reply(c, 'rpc_error', { requestId: id, requestType: msg.type, error: 'Unknown request', code: 'unknown_schema' })
    }
  }

  /** What the daemon answers a request with, for a test's handler that answers later (it returned null). */
  f.answer = (c, msg) => answer(c, msg)

  /**
   * The agent an agent.create.request makes, as its answer's payload ({agent, error}). One with an idempotency key is
   * kept under it, as Paseo 0.15's creation service keeps it (creation/index.js): the same request again (its fields
   * but requestId, type, subscribe) gets the agent made for it, and another request under that key is refused. A
   * refusal keeps nothing, so asked again it runs again.
   */
  f.createAgent = (msg) => {
    const { requestId: _r, type: _t, subscribe: _s, idempotencyKey: key, ...intent } = msg
    const fingerprint = key ? JSON.stringify(sortedKeys(intent)) : undefined
    const prior = key ? f.creations.get(key) : undefined
    if (prior && prior.fingerprint !== fingerprint) return { agent: null, error: 'agent_request_key_conflict' }
    if (prior) {
      const made = f.agents.get(prior.agentId)?.agent
      return made ? { agent: made, error: null } : { agent: null, error: 'Previously created agent no longer exists' }
    }
    // A provider Paseo runs over ACP has the Auto Accept switch, off unless the request turned it on (acp-agent.ts).
    const acp = f.providers[msg.config.provider]?.extends === 'acp'
    const agent = agentSnapshot({ id: msg.agentId ?? randomUUID(), provider: msg.config.provider, cwd: msg.config.cwd, labels: msg.labels ?? {},
      model: msg.config.model ?? null, currentModeId: msg.config.modeId ?? null, thinkingOptionId: msg.config.thinkingOptionId ?? null,
      status: msg.initialPrompt ? 'running' : 'idle', workspaceId: msg.workspaceId ?? 'wks_new',
      ...(acp ? { features: [autoAcceptFeature(msg.config.featureValues?.auto_accept === true)] } : {}) })
    f.upsert(agent, { projectKey: 'prj_1', projectName: 'proj', workspaceName: 'ws', checkout: { cwd: agent.cwd, isGit: false } })
    if (key) f.creations.set(key, { fingerprint, agentId: agent.id })
    return { agent, error: null }
  }

  /**
   * Send a message to every subscription of a kind (only those whose filter takes it). One whose first page is being
   * built holds it instead, the latest per agent or workspace (flush).
   */
  f.emit = (kind, inner, filter = () => true) => {
    for (const [sid, s] of f.subs) {
      if (s.kind !== kind || !filter(s)) continue
      const message = { ...inner, payload: { ...inner.payload, subscriptionId: sid } }
      const p = inner.payload ?? {}
      if (s.held) s.held.set(p.agentId ?? p.agent?.id ?? p.id ?? p.workspace?.id, message)
      else s.conn.write(JSON.stringify({ type: 'session', message }))
    }
  }

  /**
   * What a subscription held while its first page was built, sent right after the page's reply (Paseo 0.15
   * handleFetchAgents, agent-updates-service flushBootstrapped): an agent's update older than the page's copy of it
   * (pageAt: agent id → its updatedAt there) is left out, one as old is sent.
   */
  function flush(sid, pageAt = new Map()) {
    const s = f.subs.get(sid)
    if (!s?.held) return
    const held = [...s.held.values()]
    delete s.held
    for (const message of held) {
      const a = message.payload.kind === 'upsert' ? message.payload.agent : undefined
      if (a && Date.parse(a.updatedAt) < pageAt.get(a.id)) continue
      s.conn.write(JSON.stringify({ type: 'session', message }))
    }
  }

  /** What fn writes to c goes out in one write: the bridge reads it at once, as it often reads Paseo's reply and what follows. */
  function together(c, fn) {
    c.socket.cork()
    try {
      fn()
    } finally {
      process.nextTick(() => c.socket.uncork())
    }
  }

  /** The daemon stops answering on the connections it has (hung), and keeps them open; a new one is answered. */
  f.hang = () => {
    for (const c of f.conns) c.hung = true
  }
  /** A workspace appears or changes (the workspace directory's update; background ones are told nobody here). */
  f.upsertWorkspace = (d) => {
    f.workspaces.set(d.id, d)
    if (!d.background) f.emit('workspaces', { type: 'workspace_update', payload: { kind: 'upsert', workspace: d } })
  }
  /** A workspace is archived: it leaves the list. */
  f.removeWorkspace = (id) => {
    f.workspaces.delete(id)
    f.emit('workspaces', { type: 'workspace_update', payload: { kind: 'remove', id } })
  }
  /** An agent appears or changes (the directory's update). */
  f.upsert = (agent, project) => {
    const prev = f.agents.get(agent.id)
    f.agents.set(agent.id, { agent, project: project ?? prev?.project ?? null })
    f.emit('agents', { type: 'agent_update', payload: { kind: 'upsert', agent, project: project ?? prev?.project ?? null } })
  }
  /**
   * An agent is archived: its record stays, and it leaves the list (told twice, as Paseo does for an agent that ran:
   * when it closes it, and when it stores its record). The agents it created go with it (Paseo's
   * cascadeArchiveChildren): those in its workspace are archived too, one in another workspace is let go of.
   */
  f.archive = (agentId, at = new Date().toISOString()) => {
    const e = f.agents.get(agentId)
    if (!e || e.agent.archivedAt) return
    e.agent = { ...e.agent, archivedAt: at, updatedAt: at }
    for (let i = 0; i < 2; i++) f.emit('agents', { type: 'agent_update', payload: { kind: 'remove', agentId } })
    for (const k of [...f.agents.values()]) {
      if (k.agent.archivedAt || k.agent.labels?.['paseo.parent-agent-id'] !== agentId) continue
      if (k.agent.workspaceId === e.agent.workspaceId) f.archive(k.agent.id, at)
      else {
        const { 'paseo.parent-agent-id': _, ...labels } = k.agent.labels
        f.upsert({ ...k.agent, labels })
      }
    }
  }
  /**
   * An agent is deleted in another client (Paseo's own app, its command line): nothing of it is left. Paseo 0.14 tells
   * the removal only to the lists of the client that deleted it (session.ts handleDeleteAgentRequest): every other
   * one sees the agent closed when it ran (its last state), and nothing when it did not. told: the lists are told
   * of the removal, as Paseo 0.15 tells every client's (agent-manager notifyAgentRemoved), an archived agent's too.
   * after: ms from closing the agent to its record being gone.
   */
  f.delete = (agentId, { told = false, after = 0 } = {}) => {
    const e = f.agents.get(agentId)
    if (!e) return
    if (e.agent.status !== 'closed' && !e.agent.archivedAt) f.upsert({ ...e.agent, status: 'closed' })
    const gone = () => {
      f.agents.delete(agentId)
      if (told) f.emit('agents', { type: 'agent_update', payload: { kind: 'remove', agentId } })
    }
    if (after) setTimeout(gone, after).unref()
    else gone()
  }
  f.update = (agentId, fields) => {
    const e = f.agents.get(agentId)
    f.upsert({ ...e.agent, updatedAt: new Date(Date.now() + 1).toISOString(), ...fields })
  }
  /** A request for the user comes up in an agent. */
  f.permission = (agentId, request) => {
    const e = f.agents.get(agentId)
    f.update(agentId, { pendingPermissions: [...e.agent.pendingPermissions, request] })
    f.emit('events', { type: 'agent_permission_request', payload: { agentId, request } }, (s) => s.events.includes('agent_permission_request'))
  }
  /**
   * It is answered (by the bridge's response on c, or in Paseo's own app when c is undefined), in Paseo 0.15's order
   * (agent-manager respondToPermission, session handleAgentPermissionResponse): the agent's update without it, the
   * subscriptions' agent_permission_resolved, under the permission's id, which is also the response's requestId, then
   * the response's own reply.
   */
  f.resolvePermission = (agentId, requestId, resolution, c) => {
    const e = f.agents.get(agentId)
    f.update(agentId, { pendingPermissions: e.agent.pendingPermissions.filter((r) => r.id !== requestId) })
    f.emit('events', { type: 'agent_permission_resolved', payload: { agentId, requestId, resolution } }, (s) => s.events.includes('agent_permission_resolved'))
    if (c) c.write(JSON.stringify({ type: 'session', message: { type: 'agent_permission_resolved', payload: { agentId, requestId, resolution } } }))
  }
  /** An agent's timeline (projected entries) and its epoch. */
  f.setTimeline = (agentId, entries, epoch = 'ep1') => f.timelines.set(agentId, { epoch, entries })
  /** A live event of an agent, to the timeline subscriptions following it. */
  f.stream = (agentId, event, seq) => {
    f.emit('timeline', { type: 'agent_stream', payload: { agentId, event, timestamp: new Date().toISOString(), ...(seq ? { seq, epoch: f.timelines.get(agentId)?.epoch } : {}) } },
      (s) => s.agentIds.includes(agentId))
  }
  f.requestsOf = (type) => f.requests.filter((r) => r.type === type)
  /** Close every connection (the daemon restarting). */
  f.drop = () => {
    for (const c of f.conns) c.socket.destroy()
    f.conns = []
  }
  return f
}

/** A value with its objects' keys in order, at every depth: as Paseo's creation service digests a request. */
function sortedKeys(v) {
  if (Array.isArray(v)) return v.map(sortedKeys)
  if (!v || typeof v !== 'object') return v
  return Object.fromEntries(Object.keys(v).sort().map((k) => [k, sortedKeys(v[k])]))
}

function replyType(type) {
  if (type.endsWith('.request')) return `${type.slice(0, -'.request'.length)}.response`
  if (type.endsWith('_request')) return `${type.slice(0, -'_request'.length)}_response`
  return `${type}_response`
}

/** A projected timeline entry. */
export function entry(seqStart, item, seqEnd = seqStart, turnId = 'turn-1') {
  return { provider: 'claude', item, timestamp: new Date(1_800_000_000_000 + seqStart * 1000).toISOString(), seqStart, seqEnd,
    sourceSeqRanges: [{ startSeq: seqStart, endSeq: seqEnd }], turnId, collapsed: [] }
}
