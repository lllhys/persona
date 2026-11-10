// Background work in a chat (iOS TaskIndex / TaskNoteText / NoticeRow.wakeSummary / ActivityChips): which call started
// a background task, what its notices say in words, and how a /loop and the session's background tasks read.

import { formatDate, t } from "../i18n";
import type { BackgroundTask, Item, LoopInfo, Subagent, TaskNote } from "./types";
import type { AgentStatus } from "./work-groups";
import { launchedTaskID } from "./task-association";
export { attachedAgentNotices } from "./task-association";

/** Background tasks and the calls that started them. A notice names its call by tool_use_id, or only by the task id
 *  the call's result announced ("running in background with ID: …", "Monitor started (task …"). */
export interface TaskIndex {
  /** A call's notices, oldest first, by the call's tool_use_id. */
  notes: Map<string, TaskNote[]>;
  /** Every associated subagent report item, oldest first; never replace an earlier report. */
  reports: Map<string, Item[]>;
  /** The call a notice came from (notice item id → tool_use_id). */
  calls: Map<string, string>;
  /** Calls whose result says they went to the background (they may not have reported back yet). */
  started: Set<string>;
  /** What each of those calls was: its description, and when it started. */
  descriptions: Map<string, string>;
  startedAt: Map<string, string>;
}

export function emptyTaskIndex(): TaskIndex {
  return { notes: new Map(), reports: new Map(), calls: new Map(), started: new Set(), descriptions: new Map(), startedAt: new Map() };
}

export function buildTaskIndex(items: Item[]): TaskIndex {
  const x = emptyTaskIndex();
  const byTask = new Map<string, string>();
  const uses = new Map<string, Item>();
  const describe = (call: string) => {
    const use = uses.get(call);
    if (!use) return;
    const d = (use.tool_input as Record<string, unknown> | undefined)?.description;
    if (typeof d === "string" && !x.descriptions.has(call)) x.descriptions.set(call, d);
    if (use.ts && !x.startedAt.has(call)) x.startedAt.set(call, use.ts);
  };
  for (const it of items) {
    if (it.kind === "tool_use" && it.tool_use_id) uses.set(it.tool_use_id, it);
    if (it.kind === "tool_result" && it.tool_use_id) {
      const id = launchedTaskID(it.text ?? "");
      if (id) {
        byTask.set(id, it.tool_use_id);
        x.started.add(it.tool_use_id);
      }
    }
  }
  for (const it of items) {
    if (it.kind !== "tool_result" && it.kind !== "notice") continue;
    const n = it.task;
    const call = it.tool_use_id || n?.tool_use_id || (n?.task_id ? byTask.get(n.task_id) : undefined);
    if (!call) continue;
    const name = uses.get(call)?.tool_name;
    const agent = n?.kind === "agent" || name === "Agent" || name === "Task";
    if (n || agent || x.started.has(call)) describe(call);
    if (n) {
      x.notes.set(call, [...(x.notes.get(call) ?? []), n]);
      x.calls.set(it.id, call);
      if (n.status === "running") x.started.add(call);
    }
    if (agent && !isLaunchReceipt(it) && (it.kind === "tool_result" ? it.text?.trim() : n?.detail?.trim())) {
      x.reports.set(call, [...(x.reports.get(call) ?? []), it]);
    }
  }
  return x;
}

export function taskFailed(n: TaskNote): boolean {
  return n.status === "failed" || (n.exit_code ?? 0) !== 0;
}

/** "Background command" / "Monitor event" / "Monitor ended" / "Subagent". */
export function taskLabel(n: TaskNote): string {
  switch (n.kind) {
    case "shell":
      return t("lib-tasks.kind_shell");
    case "monitor":
      return n.status === "event" ? t("lib-tasks.monitor_event") : t("lib-tasks.monitor_ended");
    case "agent":
      return t("lib-tasks.kind_agent");
  }
  return t("lib-tasks.kind_task");
}

/** The call's description when we know the call, else what the notice says (a Monitor event: the event's first line). */
export function taskSubject(n: TaskNote, description?: string): string {
  if (n.kind === "monitor" && n.status === "event" && n.detail) {
    const line = firstLine(n.detail);
    if (line) return line;
  }
  return description || n.title || "";
}

/** How long it ran, short: "40s", "5m", "2h 5m". */
export function took(s: number): string {
  if (s < 60) return t("lib-tasks.took_seconds", { count: s });
  if (s < 3600) return t("lib-tasks.took_minutes", { count: Math.floor(s / 60) });
  const m = Math.floor((s % 3600) / 60);
  return m === 0 ? t("lib-tasks.took_hours", { count: Math.floor(s / 3600) }) : t("lib-tasks.took_hours_minutes", { hours: Math.floor(s / 3600), minutes: m });
}

export type Tone = "green" | "red" | "neutral" | "orange";

/** The outcome as a status pill (done green, failed red, stopped plain, waiting amber), with how long it ran. */
export function taskPill(n: TaskNote, startedAt?: string, at?: string): { text: string; tone: Tone } | null {
  let ran = "";
  const s = Date.parse(startedAt ?? ""), e = Date.parse(at ?? "");
  if (n.status !== "event" && Number.isFinite(s) && Number.isFinite(e) && e > s) ran = took(Math.floor((e - s) / 1000));
  const withRan = (w: string) => (ran ? `${w} · ${ran}` : w);
  switch (n.status) {
    case "completed":
    case "ended":
      return { text: withRan(t("lib-tasks.state_done")), tone: "green" };
    case "failed":
      return { text: withRan(n.exit_code !== undefined ? t("lib-tasks.exit_code", { code: n.exit_code }) : t("lib-tasks.state_failed")), tone: "red" };
    case "killed":
    case "stopped":
      return { text: withRan(t("lib-tasks.state_stopped")), tone: "neutral" };
    case "waiting":
      return { text: t("lib-tasks.state_waiting"), tone: "orange" };
    case "event":
      return { text: t("lib-tasks.state_event"), tone: "neutral" };
  }
  if (n.exit_code !== undefined && n.exit_code !== 0) return { text: withRan(t("lib-tasks.exit_code", { code: n.exit_code })), tone: "red" };
  return null;
}

/** The badge on the call that started background work: "Background · running" / "Background · done" / "Background · 3 events". */
export function taskBadge(notes: TaskNote[]): { text: string; failed: boolean } {
  const events = notes.filter((n) => n.status === "event").length;
  const last = [...notes].reverse().find((n) => n.status !== "event");
  if (!last || last.status === "running" || last.kind === "agent" && !["completed", "failed", "killed", "stopped", "ended", "waiting"].includes(last.status ?? "")) return { text: events > 0 ? t("lib-tasks.badge_events", { count: events }) : t("lib-tasks.badge_running"), failed: false };
  const state = last.status === "failed" ? t("lib-tasks.state_failed") : last.status === "killed" || last.status === "stopped" ? t("lib-tasks.state_stopped") : last.status === "waiting" ? t("lib-tasks.state_waiting") : last.status === "ended" ? t("lib-tasks.state_ended") : t("lib-tasks.state_done");
  return { text: events > 0 ? t("lib-tasks.badge_state_events", { state, count: events }) : t("lib-tasks.badge_state", { state }), failed: taskFailed(last) };
}

/** A wake notice in one line: the hub's text when it read the notification (`task`), else worked out from an older
 *  hub's summary: `Background command "Release" completed (exit code 0)` → "Background command done · Release". */
export function wakeSummary(item: Item): { text: string; failed: boolean } {
  if (item.task) return { text: item.text || item.task.title || "", failed: taskFailed(item.task) };
  const s = item.text ?? "";
  const m = /^(.*?)\s*"(.+)"\s*(.*)$/.exec(s);
  if (!m) return { text: s, failed: false };
  const kind = m[1] === "Background command" ? t("lib-tasks.wake_kind_shell") : m[1] === "Agent" ? t("lib-tasks.wake_kind_agent") : m[1] === "Monitor" ? t("lib-tasks.wake_kind_monitor") : t("lib-tasks.wake_kind_task");
  const rest = m[3].toLowerCase();
  const code = /exit code (\d+)/.exec(rest);
  const failed = rest.includes("fail") || rest.includes("error") || (code ? Number(code[1]) !== 0 : false);
  const state = failed ? t("lib-tasks.wake_failed") : rest.includes("kill") || rest.includes("stop") ? t("lib-tasks.wake_stopped") : rest.includes("complet") ? t("lib-tasks.wake_completed") : t("lib-tasks.wake_updated");
  return { text: t("lib-tasks.wake_summary", { kind, state, subject: m[2] }), failed };
}

function firstLine(s: string): string {
  return s.split("\n").map((l) => l.trim()).find(Boolean) ?? "";
}

// ── the session's /loop and background tasks ──

/** The task, without the "/loop" command word. */
export function loopTask(loop: LoopInfo): string {
  const p = (loop.prompt ?? "").trim();
  return p.startsWith("/loop") ? p.slice(5).trim() : p;
}

/** "Every 30 minutes" / "On schedule 33 17 * * *". */
export function loopCadence(loop: LoopInfo): string {
  if (loop.kind === "cron" && loop.cron) return t("lib-tasks.cadence_cron", { cron: loop.cron });
  const s = loop.interval_seconds ?? 0;
  if (s <= 0) return "";
  if (s < 120) return t("lib-tasks.every_seconds", { count: s });
  if (s < 7200) return t("lib-tasks.every_minutes", { count: Math.floor(s / 60) });
  return t("lib-tasks.every_hours", { count: Math.floor(s / 3600) });
}

function hhmm(ms: number): string {
  return formatDate(ms, { hour: "2-digit", minute: "2-digit", hour12: false });
}

/** The loop beside its arrows, short: "Next 11:57", or that it is running. */
export function loopShortLabel(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (Number.isFinite(next) && next > now) return t("lib-tasks.loop_next", { time: hhmm(next) });
  return loop.next_at ? t("lib-tasks.loop_running_short") : t("lib-tasks.loop_short");
}

/** "Next 11:57" / "Loop running" / "Every 30 minutes". */
export function loopLabel(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (Number.isFinite(next) && next > now) return t("lib-tasks.loop_next", { time: hhmm(next) });
  if (loop.next_at) return t("lib-tasks.loop_running");
  return loopCadence(loop) || t("lib-tasks.loop_looping");
}

/** "Next wake-up 11:57 (in 4 minutes)" / "Running this round". */
export function loopNext(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (!Number.isFinite(next)) return "";
  if (next <= now) return t("lib-tasks.loop_this_round");
  const m = Math.max(1, Math.ceil((next - now) / 60_000));
  const until =
    m < 60
      ? t("lib-tasks.until_minutes", { count: m })
      : m % 60
        ? t("lib-tasks.until_hours_minutes", { hours: Math.floor(m / 60), minutes: m % 60 })
        : t("lib-tasks.until_hours", { count: m / 60 });
  return t("lib-tasks.next_wake", { time: hhmm(next), until });
}

/** What tells the agent to end its /loop: nothing outside Claude can cancel a scheduled wakeup or its cron job. */
export const STOP_LOOP_MESSAGE =
  // i18n-ignore: an instruction to the agent, not interface text
  "停止当前的 /loop：调用 ScheduleWakeup(stop: true) 结束循环（如果用 CronCreate 建过定时任务，用 CronDelete 删掉），不要再安排下一次唤醒，回复一句「循环已停止」即可。";

/** A background task in one line: the description Claude gave, else the command. */
export function backgroundTitle(task: BackgroundTask): string {
  if (task.description) return task.description;
  return task.command?.split("\n")[0] || (task.kind === "agent" ? t("lib-tasks.background_agent") : t("lib-tasks.kind_shell"));
}

/** "40s" / "5m 12s" / "1h 5m". */
export function elapsed(fromIso: string | undefined, now: number): string {
  const start = Date.parse(fromIso ?? "");
  if (!Number.isFinite(start)) return "";
  const s = Math.max(0, Math.floor((now - start) / 1000));
  if (s < 60) return t("lib-tasks.elapsed_seconds", { count: s });
  if (s < 3600) return t("lib-tasks.elapsed_minutes_seconds", { minutes: Math.floor(s / 60), seconds: s % 60 });
  return t("lib-tasks.elapsed_hours_minutes", { hours: Math.floor(s / 3600), minutes: Math.floor((s % 3600) / 60) });
}

export function subagentTitle(a: Subagent): string {
  return a.description || a.type || t("lib-tasks.kind_agent");
}

// ── a subagent call in the chat (its card, cindy's AgentTaskCard) ──

/** What an Agent / Task call returned when it only started the subagent in the background. */
const LAUNCH_RECEIPT = /^\s*(?:Async agent launched|This is an internal ID for your use)/;
/** The "agentId: …" line Claude Code appends to a subagent's report. */
const AGENT_ID_LINE = /^\s*agentId:.*$/gm;

/** The subagent an Agent / Task call launched, by the call's tool_use_id. */
export function subagentFor(all: Subagent[], use: Item): Subagent | undefined {
  return use.tool_use_id ? all.find((a) => a.tool_use_id === use.tool_use_id) : undefined;
}

/** The session a subagent's conversation belongs to: its id is "<parent>@<agent>" (the parent's own id otherwise). */
export function subagentParent(sid: string): string {
  return sid.split("@")[0];
}

export function isLaunchReceipt(result?: Item): boolean {
  if (result?.task?.kind === "agent" && (result.task.detail || agentNoteStatus(result.task) !== "running")) return false;
  return LAUNCH_RECEIPT.test(result?.text ?? "");
}

/** How a subagent call is going, by the call's result first (#5602: the session's list used to win over a result
 *  already in, and its `running` comes and goes, so a finished card flipped back to running): an error failed, a
 *  result done; none yet runs while the exchange is live or the list says it does, and stopped otherwise. A
 *  background subagent (its result is the launch receipt) is the list's to tell; without it, it runs while the
 *  exchange is live. */
/** Structured subagent lifecycle is authoritative; a running report is not a completed tool call. */
function agentNoteStatus(note: TaskNote | undefined): AgentStatus | undefined {
  if (note?.kind !== "agent") return undefined;
  if (taskFailed(note)) return "failed";
  switch (note.status) {
    case "running":
    case "waiting":
    case "event": return "running";
    case "completed":
    case "ended": return "completed";
    case "killed":
    case "stopped": return "stopped";
  }
  return "running";
}

export function agentStatus(result: Item | undefined, meta: Subagent | undefined, live: boolean, notes?: TaskNote[]): AgentStatus {
  if (result?.is_error) return "failed";
  const status = agentNoteStatus(notes?.at(-1)) ?? agentNoteStatus(result?.task);
  if (status) return status;
  if (!result) return live || meta?.running ? "running" : "stopped";
  if (!isLaunchReceipt(result)) return "completed";
  if (meta) return meta.running ? "running" : "completed";
  return live ? "running" : "completed";
}

/** Calls with a terminal result or notification. Launch receipts and interim reports leave work visible. */
export function terminalCalls(items: Item[], index = buildTaskIndex(items)): Set<string> {
  const answered = new Set<string>();
  for (const it of items) {
    const call = it.tool_use_id || index.calls.get(it.id);
    if (!call) continue;
    const status = agentNoteStatus(it.task);
    if (status === "running") answered.delete(call);
    else if (status || it.kind === "tool_result" && !isLaunchReceipt(it)) answered.add(call);
  }
  return answered;
}

/** When a finished subagent ended (ms): at its result, but a background one's result is the launch receipt, written as
 *  it started, so at its last line (#5602: its card said it took no time). */
export function subagentEnded(result: Item | undefined, meta: Subagent | undefined): number | undefined {
  const at = (ts?: string) => {
    const v = Date.parse(ts ?? "");
    return Number.isFinite(v) ? v : undefined;
  };
  return isLaunchReceipt(result) ? (at(meta?.updated_at) ?? at(result?.ts)) : (at(result?.ts) ?? at(meta?.updated_at));
}

const sameTool = (x: Subagent["last_tool"], y: Subagent["last_tool"]) => x === y || (x?.id === y?.id && x?.name === y?.name && x?.summary === y?.summary);

/** The subagents the main agent is waiting on (#5602, Claude Desktop's "2 agents working — …"): running ones it
 *  launched in the foreground whose calls have no result yet (`answered`: the call ids that do), and what they are
 *  doing, newest call first. Null when there are none. */
export function agentsWorking(all: Subagent[] | undefined, answered: ReadonlySet<string>): { count: number; doing: string[] } | null {
  const waiting = (all ?? []).filter((a) => a.running && !a.background && a.tool_use_id && !answered.has(a.tool_use_id));
  if (!waiting.length) return null;
  const doing = waiting
    .filter((a) => a.last_tool)
    .sort((a, b) => (Date.parse(b.updated_at ?? "") || 0) - (Date.parse(a.updated_at ?? "") || 0))
    .map((a) => a.last_tool!.summary?.trim() || a.last_tool!.name);
  return { count: waiting.length, doing: [...new Set(doing)] };
}

/** What of a subagent its card and list show: everything but `updated_at`, which moves with every line it writes. */
function subagentShown(a: Subagent): Omit<Subagent, "updated_at"> {
  const { updated_at: _, ...shown } = a;
  return shown;
}

/** `next`, or `prev` when nothing a card shows changed (#5602: the session list's subagents are a new array on every
 *  push, about once a second while one runs, and every exchange of the chat drew again). A subagent that stopped
 *  changed `running`, so the array that says so carries its last `updated_at`. */
export function stableSubagents(prev: Subagent[] | undefined, next: Subagent[] | undefined): Subagent[] | undefined {
  if (!prev || !next || prev.length !== next.length) return next;
  for (let i = 0; i < next.length; i++) {
    const a = subagentShown(prev[i]);
    const b = subagentShown(next[i]);
    const keys = Object.keys(b) as (keyof typeof b)[];
    const same = (k: keyof typeof b) => (k === "last_tool" ? sameTool(a.last_tool, b.last_tool) : a[k] === b[k]);
    if (keys.length !== Object.keys(a).length || !keys.every(same)) return next;
  }
  return prev;
}

/** What the subagent reported, without its agentId line; a launch receipt says nothing yet (its latest words do). */
export function agentSummary(result: Item | undefined, meta: Subagent | undefined): string {
  const text = isLaunchReceipt(result) ? "" : (result?.text ?? "").replace(AGENT_ID_LINE, "").trim();
  return text || meta?.last_text?.trim() || "";
}

/** The subagents a subagent launched, and theirs, one level deeper each (`depth` 0: launched by `id`). */
export function nestedSubagents(all: Subagent[] | undefined, id: string): SubagentRow[] {
  const rows = subagentTree(all);
  const at = rows.findIndex((r) => r.agent.id === id);
  if (at < 0) return [];
  const base = rows[at].depth;
  const out: SubagentRow[] = [];
  for (let i = at + 1; i < rows.length && rows[i].depth > base; i++) out.push({ agent: rows[i].agent, depth: rows[i].depth - base - 1 });
  return out;
}

export interface SubagentRow {
  agent: Subagent;
  /** 0: the session launched it (or its launcher is not listed); 1: a subagent did; … */
  depth: number;
}

/** Every subagent by who launched whom (`parent_id`), flattened for a list: each right under its launcher, one level
 *  deeper; one whose launcher is not listed (it ended before the node's one-day cutoff) at the top level. Within a
 *  level running ones first, then the newest first (as the node lists them). */
export function subagentTree(all: Subagent[] | undefined): SubagentRow[] {
  const list = all ?? [];
  const listed = new Set(list.map((a) => a.id));
  const newest = (a: Subagent) => Date.parse(a.started_at ?? "") || 0;
  const order = (a: Subagent, b: Subagent) => Number(Boolean(b.running)) - Number(Boolean(a.running)) || newest(b) - newest(a);
  const kids = new Map<string, Subagent[]>();
  for (const a of [...list].sort(order)) {
    const p = a.parent_id && a.parent_id !== a.id && listed.has(a.parent_id) ? a.parent_id : "";
    kids.set(p, [...(kids.get(p) ?? []), a]);
  }
  const out: SubagentRow[] = [];
  const placed = new Set<string>();
  const place = (a: Subagent, depth: number) => {
    if (placed.has(a.id)) return;
    placed.add(a.id);
    out.push({ agent: a, depth });
    for (const k of kids.get(a.id) ?? []) place(k, depth + 1);
  };
  for (const a of kids.get("") ?? []) place(a, 0);
  // launchers naming each other (only a runtime could write that) are reached from nowhere: list them at the top
  for (const a of [...list].sort(order)) place(a, 0);
  return out;
}
