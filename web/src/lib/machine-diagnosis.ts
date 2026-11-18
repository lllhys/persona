// What is using a computer (#8037, docs/web/web.md "Machines"): the manager's bounded diagnosis of one node
// (`GET /api/v1/machines/{mid}/diagnosis?sections=`) and its cache cleaner (`POST …/cleanup {ids}`). The node answers a
// fixed question (process groups by name, the largest folders under home, a fixed list of rebuildable caches, a fixed
// list of tools); nothing here sends a path or a command. A node or hub from before it says so in its own way, which
// the sheet turns into an "update" note while every other action keeps working (diagnosisFailure).
import { t } from "../i18n";
import { api, ApiError, enc, errorText, pastHub } from "./api";

export type DiagnosisSection = "processes" | "disk" | "tools";
export type ProcessKind = "agent" | "engine" | "persona" | "other";

export interface ProcessGroup {
  name: string;
  kind: ProcessKind | string;
  count: number;
  rss: number;
  /** Percent of one core summed over the group; absent when unknown. */
  cpu?: number;
}

export interface DiagnosisVolume {
  mount: string;
  used: number;
  total: number;
}

export interface DiagnosisFolder {
  /** "~"-relative. */
  path: string;
  bytes: number;
}

export interface DiagnosisCache {
  id: string;
  path: string;
  bytes: number;
  in_use: boolean;
}

export interface DiagnosisTool {
  id: string;
  /** "" when unknown. */
  version: string;
}

export interface Diagnosis {
  machine_id: string;
  at: number;
  processes?: { mem_total: number; count: number; groups: ProcessGroup[] };
  disk?: { volumes: DiagnosisVolume[]; folders: DiagnosisFolder[]; partial: boolean; caches: DiagnosisCache[] };
  tools?: DiagnosisTool[];
}

export type CleanupStatus = "cleaned" | "in_use" | "missing" | "failed";

export interface CleanupResult {
  freed_bytes: number;
  available_before?: number;
  available_after?: number;
  items: { id: string; bytes: number; status: CleanupStatus | string; error?: string }[];
}

/** Why the diagnosis is not there. node_outdated: the computer's Persona predates it; hub_outdated: the hub does;
 *  forbidden: this device is not fully trusted; offline: the computer is not connected; deadline: it did not answer
 *  in time; error: anything else (`text` says what). */
export type DiagnosisFailureKind = "node_outdated" | "hub_outdated" | "forbidden" | "offline" | "deadline" | "error";

export interface DiagnosisFailure {
  kind: DiagnosisFailureKind;
  text: string;
}

/** The failure of a diagnosis or cleanup request, as the sheet says it. An older hub has no such route and answers
 *  404 without a code (or not even JSON); the manager's own 404 for an unknown computer carries one. */
export function diagnosisFailure(e: unknown): DiagnosisFailure {
  if (e instanceof ApiError) {
    const code = typeof e.body?.code === "string" ? e.body.code : "";
    if (e.status === 409 && code === "node_outdated") return { kind: "node_outdated", text: t("lib-machine-diagnosis.node_outdated") };
    if (e.status === 404 && (!code || !e.isJson)) return { kind: "hub_outdated", text: t("lib-machine-diagnosis.hub_outdated") };
    if (e.status === 403) return { kind: "forbidden", text: t("lib-machine-diagnosis.forbidden") };
    if (e.status === 503 || code === "machine_offline") return { kind: "offline", text: t("lib-machine-diagnosis.offline") };
    if (e.status === 504 || code === "deadline") return { kind: "deadline", text: t("lib-machine-diagnosis.deadline") };
  }
  return { kind: "error", text: errorText(e) };
}

/** Whether the request can just be tried again (the others need an update or another device). */
export const retryable = (f: DiagnosisFailure): boolean => f.kind === "error" || f.kind === "deadline" || f.kind === "offline";

/** The node scans home for up to 20 s; the hub's own answer comes after that. */
const DIAGNOSIS_MS = pastHub(30_000);

export function fetchDiagnosis(mid: string, sections: readonly DiagnosisSection[], signal?: AbortSignal): Promise<Diagnosis> {
  return api<Diagnosis>(`/machines/${enc(mid)}/diagnosis?sections=${sections.join(",")}`, { signal, timeout: DIAGNOSIS_MS });
}

export function cleanup(mid: string, ids: readonly string[]): Promise<CleanupResult> {
  return api<CleanupResult>(`/machines/${enc(mid)}/cleanup`, { method: "POST", json: { ids }, timeout: pastHub(120_000) });
}

// ── tools, for the capability index: lazily, at most three computers at a time, kept 10 minutes ──

export type ToolsState = { state: "loading" } | { state: "ok"; tools: DiagnosisTool[] } | { state: "failed"; failure: DiagnosisFailure };

const TOOLS_TTL_MS = 10 * 60_000;
/** A failure that can be retried (offline, too slow) is asked again after this, not on every render. */
const TOOLS_RETRY_MS = 30_000;
const TOOLS_PARALLEL = 3;
const toolsKept = new Map<string, { at: number; value: ToolsState }>();
const toolsWaiting: { mid: string; run: () => Promise<void> }[] = [];
const toolsListeners = new Set<() => void>();
let toolsRunning = 0;

function toolsChanged() {
  toolsListeners.forEach((l) => l());
}

export function onToolsChange(l: () => void): () => void {
  toolsListeners.add(l);
  return () => void toolsListeners.delete(l);
}

/** What is known of the computer's tools: kept less than 10 minutes, or still loading; undefined when never asked. */
export function toolsOf(mid: string, now = Date.now()): ToolsState | undefined {
  const kept = toolsKept.get(mid);
  if (!kept) return undefined;
  const ttl = kept.value.state === "failed" && retryable(kept.value.failure) ? TOOLS_RETRY_MS : TOOLS_TTL_MS;
  if (kept.value.state !== "loading" && now - kept.at > ttl) return undefined;
  return kept.value;
}

function pump() {
  while (toolsRunning < TOOLS_PARALLEL && toolsWaiting.length) {
    const next = toolsWaiting.shift()!;
    toolsRunning++;
    void next.run().finally(() => {
      toolsRunning--;
      pump();
    });
  }
}

/** Asks for the computer's tools unless they are known or asked for already; `fetcher` is for tests. */
export function requestTools(mid: string, fetcher: (mid: string) => Promise<Diagnosis> = (m) => fetchDiagnosis(m, ["tools"]), now = Date.now()) {
  if (toolsOf(mid, now)) return;
  toolsKept.set(mid, { at: now, value: { state: "loading" } });
  toolsChanged();
  toolsWaiting.push({
    mid,
    run: async () => {
      let value: ToolsState;
      try {
        const d = await fetcher(mid);
        value = { state: "ok", tools: d.tools ?? [] };
      } catch (e) {
        value = { state: "failed", failure: diagnosisFailure(e) };
      }
      toolsKept.set(mid, { at: Date.now(), value });
      toolsChanged();
    },
  });
  pump();
}

/** For tests: forget every computer's tools. */
export function resetTools() {
  toolsKept.clear();
  toolsWaiting.length = 0;
  toolsRunning = 0;
}

/** A tool's name for people. */
export function toolName(id: string): string {
  return TOOL_NAMES[id] ?? id;
}

const TOOL_NAMES: Record<string, string> = {
  xcode: "Xcode", docker: "Docker", podman: "Podman", go: "Go", node: "Node.js", python: "Python", rust: "Rust", java: "Java",
  swift: "Swift", git: "Git", gh: "GitHub CLI", brew: "Homebrew", bun: "Bun", pnpm: "pnpm", uv: "uv", tmux: "tmux",
  claude: "Claude Code", codex: "Codex", opencode: "opencode", gemini: "Gemini CLI",
};
