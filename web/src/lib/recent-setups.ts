// New agent's recent setups (#2134): each setup that started an agent (computer, folder, launcher, model, permission mode,
// effort), newest first, so a tap fills the form with one of them again. The rules are the ones iOS, Mac and Android follow
// (spec/fixtures/recents/README.md, checked against recent_setups.json); the list stays in this browser, for this hub (the
// page is the hub's own, so its localStorage is too), and holds ids only, never a message.
import { BASE } from "./base";
import { removeStoredValue, storedValue, storeValue } from "./compatibility";
import type { RecentSetup } from "./recents-wire";

export type { RecentSetup };

export const MAX_RECENT_SETUPS = 20;
export const MAX_RECENT_CARDS = 3;

const FIELDS = ["machine", "cwd", "launcher", "model", "agent_type", "mode", "effort"] as const;

/** The same setup: every field but `at`. */
export const sameSetup = (a: RecentSetup, b: RecentSetup) => FIELDS.every((f) => a[f] === b[f]);

/** A setup that started an agent goes first; the same one met again moves there with its new time; the first 20 stay. */
export function recordSetup(before: RecentSetup[], setup: RecentSetup): RecentSetup[] {
  return [setup, ...before.filter((e) => !sameSetup(e, setup))].slice(0, MAX_RECENT_SETUPS);
}

export function removeSetup(entries: RecentSetup[], setup: RecentSetup): RecentSetup[] {
  return entries.filter((e) => !sameSetup(e, setup));
}

export interface RecentCard {
  setup: RecentSetup;
  /** Where it is in the entries. */
  index: number;
  /** Its machine is online: an offline one's card is shown, dimmed, and can't be picked. */
  online: boolean;
}

/** The cards New agent shows: the entries whose machine the hub still lists, in order, at most 3. */
export function recentCards(entries: RecentSetup[], machines: { id: string; online?: boolean }[]): RecentCard[] {
  const cards: RecentCard[] = [];
  for (const [index, setup] of entries.entries()) {
    if (cards.length === MAX_RECENT_CARDS) break;
    const m = machines.find((x) => x.id === setup.machine);
    if (m) cards.push({ setup, index, online: Boolean(m.online) });
  }
  return cards;
}

/** The model New agent picks with a machine and the launcher it restores there: that of the machine's latest entry with the
 *  same launcher; none, "" (the default). */
export function machineModel(entries: RecentSetup[], machine: string, launcher: string): string {
  return entries.find((e) => e.machine === machine && e.launcher === launcher)?.model ?? "";
}

/** The form still holds what the card put in (mode and effort only count for Claude Code, the only agent that takes them). */
export function formHolds(setup: RecentSetup, form: { machine: string; cwd: string; launcher: string; model: string; mode: string; effort: string }): boolean {
  if (setup.machine !== form.machine || setup.cwd !== form.cwd.trim() || setup.launcher !== form.launcher || setup.model !== form.model) return false;
  return setup.agent_type !== "claude" || (setup.mode === form.mode && setup.effort === form.effort);
}

/** A card's identity among the entries (the list never holds the same setup twice). */
export const setupKey = (s: RecentSetup) => JSON.stringify(FIELDS.map((f) => s[f]));

// ── this browser's list ─────────────────────────────────

/** One list per hub: a hub served under a sub-path of an origin another one shares keeps its own. */
export const recentSetupsKey = (base = BASE) => (base ? `persona.recentSetups.${base}` : "persona.recentSetups");

const isSetup = (v: unknown): v is RecentSetup =>
  Boolean(v) && typeof v === "object" && [...FIELDS, "at" as const].every((f) => typeof (v as Record<string, unknown>)[f] === "string") && Boolean((v as RecentSetup).machine);

export function loadRecentSetups(store: Storage = localStorage): RecentSetup[] {
  try {
    const raw = storedValue(recentSetupsKey(), store);
    const list: unknown = raw ? JSON.parse(raw) : [];
    if (!Array.isArray(list)) return [];
    // only the fields of a setup: whatever else an entry carries is never kept
    return list.filter(isSetup).slice(0, MAX_RECENT_SETUPS).map((e) => ({ machine: e.machine, cwd: e.cwd, launcher: e.launcher, model: e.model, agent_type: e.agent_type, mode: e.mode, effort: e.effort, at: e.at }));
  } catch {
    return [];
  }
}

function saveRecentSetups(entries: RecentSetup[], store: Storage): RecentSetup[] {
  try {
    if (entries.length) storeValue(recentSetupsKey(), JSON.stringify(entries), store);
    else removeStoredValue(recentSetupsKey(), store);
  } catch {
    // storage full or blocked: the list is a convenience, the start goes on
  }
  return entries;
}

/** Remembers a setup that started an agent; returns the list as it is now. */
export const rememberSetup = (setup: RecentSetup, store: Storage = localStorage) => saveRecentSetups(recordSetup(loadRecentSetups(store), setup), store);

/** "Remove from recent". */
export const forgetSetup = (setup: RecentSetup, store: Storage = localStorage) => saveRecentSetups(removeSetup(loadRecentSetups(store), setup), store);

/** "Clear recent setups". */
export const clearRecentSetups = (store: Storage = localStorage) => saveRecentSetups([], store);

/** `at` as the fixtures write it (RFC 3339 to the second). */
export const setupTime = (now = new Date()) => now.toISOString().replace(/\.\d{3}Z$/, "Z");
