// What each computer can do (#8037, docs/web/web.md "Machines"): one table that feeds people (the Machines page's
// Capabilities view) and placement (the hand-off's target picker, rankTargets). Rows come from the machine list
// (system, engines), the latest metric sample (free disk and memory), the computer's launchers (Agent runtimes, each
// runtime with its named entries) and the node's detected tools. Pure: the page fetches, this only reads.
import { t } from "../i18n";
import { isPending, neverConnected } from "./add-computer";
import { agentId, agentName } from "./agent-kind";
import { bytesBin, connectionText, type DetailMachine } from "./machine-detail";
import { alertTitle, machineAlerts, readings, worst, type MachineAlert, type Severity } from "./machine-alerts";
import { toolName, type DiagnosisTool, type ToolsState } from "./machine-diagnosis";
import { routeText } from "./mesh";
import { providerTitle } from "./provider-names";
import type { Launcher, Machine, MachineMetrics } from "./types";

export type MachinePresence = "online" | "offline" | "pending" | "not_connected";

export interface RuntimeGroup {
  /** agentId of the runtime ("claude", "codex"). */
  id: string;
  name: string;
  /** The launchers' names for people, in the order the computer lists them. */
  entries: string[];
}

export type ListState<T> = { state: "loading" } | { state: "ok"; value: T } | { state: "unavailable"; text: string };

export interface CapabilityRow {
  id: string;
  name: string;
  presence: MachinePresence;
  alerts: MachineAlert[];
  worst: Severity | null;
  /** "macOS 26.1 · arm64", the model on its own. */
  system: string;
  model: string;
  cores?: number;
  memory?: number;
  diskFree?: number;
  engines: string[];
  runtimes: ListState<RuntimeGroup[]>;
  tools: ListState<DiagnosisTool[]>;
  reach: string;
}

export function presenceOf(m: Machine): MachinePresence {
  if (isPending(m)) return "pending";
  if (neverConnected(m)) return "not_connected";
  return m.online ? "online" : "offline";
}

/** "macos", "linux", "windows": the OS family, from the system's facts or the machine's `os`. */
export function osFamily(m: Machine & { system?: { os?: string } }): string {
  const os = ((m.system as { os?: string } | undefined)?.os || m.os || "").toLowerCase();
  if (os === "darwin" || os === "macos") return "macos";
  if (os.startsWith("win")) return "windows";
  return os;
}

/** The computer's launchers by runtime: "Claude Code: claude, Team relay", "Codex: codex". */
export function runtimeGroups(launchers: readonly Launcher[]): RuntimeGroup[] {
  const by = new Map<string, RuntimeGroup>();
  for (const l of launchers) {
    const id = agentId(l.agent || l.agent_type);
    const g = by.get(id) ?? { id, name: agentName(l.agent || l.agent_type), entries: [] };
    const title = providerTitle(l);
    if (!g.entries.includes(title)) g.entries.push(title);
    by.set(id, g);
  }
  return [...by.values()];
}

export interface RowInput {
  machine: Machine;
  machines: Machine[];
  metrics?: MachineMetrics;
  launcherIssues?: number;
  /** GET /machines/{id}/launchers: undefined while loading, null when it could not be read. */
  launchers?: Launcher[] | null;
  tools?: ToolsState;
}

/** One row of the capability index. */
export function capabilityRow({ machine: m, machines, metrics, launcherIssues = 0, launchers, tools }: RowInput): CapabilityRow {
  const d = m as DetailMachine;
  const sys = { ...(d.system ?? {}), ...(metrics?.system ?? {}) } as NonNullable<DetailMachine["system"]>;
  const alerts = machineAlerts({ machine: m, metrics, launchers: launcherIssues });
  const latest = metrics?.latest;
  const diskFree = latest?.disk_total && latest.disk_used !== undefined ? Math.max(0, latest.disk_total - latest.disk_used) : undefined;
  const presence = presenceOf(m);
  return {
    id: m.id,
    name: m.name,
    presence,
    alerts,
    worst: worst(alerts),
    system: [sys.name || m.os, sys.arch || m.arch].filter(Boolean).join(" · "),
    model: sys.model ?? "",
    cores: sys.cores || undefined,
    memory: latest?.mem_total || sys.memory || undefined,
    diskFree,
    engines: (m.engines ?? []).filter((e) => e.available).map((e) => (e.version ? `${e.name} ${e.version}` : e.name)),
    runtimes: launchers === undefined ? (presence === "online" ? { state: "loading" } : { state: "unavailable", text: t("lib-machine-capabilities.when_online") })
      : launchers === null ? { state: "unavailable", text: t("lib-machine-capabilities.not_read") }
      : { state: "ok", value: runtimeGroups(launchers) },
    tools: toolsCell(tools, presence),
    reach: routeText(m, machines) || connectionText(d),
  };
}

function toolsCell(tools: ToolsState | undefined, presence: MachinePresence): ListState<DiagnosisTool[]> {
  if (presence !== "online") return { state: "unavailable", text: t("lib-machine-capabilities.when_online") };
  if (!tools || tools.state === "loading") return { state: "loading" };
  if (tools.state === "ok") return { state: "ok", value: tools.tools };
  switch (tools.failure.kind) {
    case "node_outdated":
      return { state: "unavailable", text: t("lib-machine-capabilities.tools_node_outdated") };
    case "hub_outdated":
      return { state: "unavailable", text: t("lib-machine-capabilities.tools_hub_outdated") };
    case "forbidden":
      return { state: "unavailable", text: t("lib-machine-capabilities.tools_forbidden") };
    default:
      return { state: "unavailable", text: t("lib-machine-capabilities.not_read") };
  }
}

/** "Docker 27.3.1 · Go · Git 2.47". */
export function toolsText(tools: readonly DiagnosisTool[]): string {
  return tools.map((x) => (x.version ? `${toolName(x.id)} ${x.version}` : toolName(x.id))).join(" · ");
}

// ── hand-off targets ──

export interface TargetCandidate {
  machine: Machine;
  metrics?: MachineMetrics;
  launcherIssues?: number;
  /** Its detected tools, when known. */
  tools?: readonly DiagnosisTool[];
}

export interface RankedTarget {
  machine: Machine;
  eligible: boolean;
  /** Why it is or is not a good place: "Offline", "Memory pressure · 91%", "Same system · 12 GB free". */
  reason: string;
  alerts: MachineAlert[];
  freeMemory?: number;
}

/** The other computers as places to hand the source's work to, the best first. Not eligible: offline, waiting to be
 *  confirmed or never connected, or with a critical alert of its own. Among the rest: fewer alerts, then more free
 *  memory, then the source's OS family, then more of the source's tools (when both are known). Every row says why;
 *  the ineligible ones keep the order given after the eligible ones. */
export function rankTargets(source: Machine, candidates: readonly TargetCandidate[], opts: { sourceTools?: readonly DiagnosisTool[] } = {}): RankedTarget[] {
  const family = osFamily(source);
  const wanted = new Set((opts.sourceTools ?? []).map((x) => x.id));
  const rows = candidates
    .filter((c) => c.machine.id !== source.id)
    .map((c, index) => {
      const m = c.machine;
      const alerts = machineAlerts({ machine: m, metrics: c.metrics, launchers: c.launcherIssues ?? 0 });
      const r = readings(m, c.metrics);
      const total = c.metrics?.latest?.mem_total || c.metrics?.system?.memory || m.system?.memory;
      const used = c.metrics?.latest?.mem_used;
      const freeMemory = total && used !== undefined ? Math.max(0, total - used) : undefined;
      const same = !!family && osFamily(m) === family;
      const overlap = c.tools && wanted.size ? c.tools.filter((x) => wanted.has(x.id)).length : 0;
      const missing = c.tools && wanted.size ? [...wanted].filter((id) => !c.tools!.some((x) => x.id === id)) : [];
      const presence = presenceOf(m);
      const critical = alerts.find((a) => a.severity === "critical");
      let eligible = true;
      let reason: string;
      if (presence === "offline") {
        eligible = false;
        reason = t("lib-machine-capabilities.target_offline");
      } else if (presence === "pending" || presence === "not_connected") {
        eligible = false;
        reason = t("lib-machine-capabilities.target_pending");
      } else if (critical) {
        eligible = false;
        reason = alertReason(critical, r);
      } else {
        const parts = [same ? t("lib-machine-capabilities.same_system") : systemName(m)];
        if (freeMemory !== undefined) parts.push(t("lib-machine-capabilities.free_memory", { size: bytesBin(freeMemory).replace(/\.0+ /, " ") }));
        const warning = alerts.find((a) => a.severity !== "critical"); // whatever ranked it lower is said, a version mismatch included
        if (warning) parts.push(alertReason(warning, r));
        if (missing.length) parts.push(t("lib-machine-capabilities.missing_tool", { tool: toolName(missing[0]) }));
        reason = parts.filter(Boolean).join(" · ");
      }
      return { machine: m, eligible, reason, alerts, freeMemory, same, overlap, index };
    });
  rows.sort((a, b) => {
    if (a.eligible !== b.eligible) return a.eligible ? -1 : 1;
    if (!a.eligible) return a.index - b.index;
    return a.alerts.length - b.alerts.length
      || (b.freeMemory ?? -1) - (a.freeMemory ?? -1)
      || Number(b.same) - Number(a.same)
      || b.overlap - a.overlap
      || a.index - b.index;
  });
  return rows.map(({ machine, eligible, reason, alerts, freeMemory }) => ({ machine, eligible, reason, alerts, ...(freeMemory !== undefined ? { freeMemory } : {}) }));
}

function alertReason(a: MachineAlert, r: ReturnType<typeof readings>): string {
  const pct = (v: number | undefined) => (v === undefined ? "" : `${Math.round(v)}%`);
  switch (a.kind) {
    case "memory":
      return t("lib-machine-capabilities.reason_memory", { value: pct(r.memory) });
    case "disk":
      return t("lib-machine-capabilities.reason_disk", { value: pct(r.disk) });
    case "cpu":
      return t("lib-machine-capabilities.reason_cpu", { value: pct(r.cpu) });
    default:
      return alertTitle(a);
  }
}

function systemName(m: Machine): string {
  const family = osFamily(m);
  return family === "macos" ? "macOS" : family === "linux" ? "Linux" : family === "windows" ? "Windows" : m.system?.name || "";
}
