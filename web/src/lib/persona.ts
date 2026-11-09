import { legacyBuiltins } from "./compatibility";
// Persona's look (avatar, docs/apple/persona.md, docs/extensions/extensions.md "Avatar"): its name, its face and the animated dot, from a
// package on the hub or an extension (GET /looks, files under /personas/{id}/{file}), or the default dumpling (MochiFace).
// The choice is the user's, kept on the hub (PUT /looks/current) so
// every device shows the same one; localStorage keeps the last one for the first paint and for older hubs.
// The pure parts (checks, which file, which state) are here; the views and the list are views/assistant/Persona.tsx.
import { BASE } from "./base";
import { t } from "../i18n";
import { stateName } from "./state-labels";

/** The default, Mochi (nothing saved means it). */
export const BUILTIN_PERSONA = "persona";
/** Retired bundled IDs stay reserved so a package cannot hijack an existing user's selection. */
export const RETIRED_PERSONAS = ["persona-visor", "persona-circuit", "persona-pixel", "persona-facet", "persona-blob"] as const;
const RESERVED: readonly string[] = [BUILTIN_PERSONA, ...RETIRED_PERSONAS, ...legacyBuiltins];

export function isRetiredPersona(id: string): boolean {
  return (RETIRED_PERSONAS as readonly string[]).includes(id) || legacyBuiltins.includes(id);
}

/** A retired selection always becomes the default, including before the hub is reachable. */
export function normalizePersonaChoice(id: string): string {
  return !id || isRetiredPersona(id) ? BUILTIN_PERSONA : id;
}

/** Whether a choice is drawn by the page rather than a package. */
export function isBuiltinPersona(id: string): boolean {
  return id === BUILTIN_PERSONA;
}

export type PersonaState = "idle" | "thinking" | "working" | "needs_you" | "offline";

/** The five states in the order the phone shows them (PersonaState.allCases), with their names. */
export const PERSONA_STATES: { id: PersonaState; title: string }[] = [
  { id: "idle", title: t("lib-persona.state_idle") },
  { id: "thinking", title: t("lib-persona.state_thinking") },
  { id: "working", title: stateName("working") },
  { id: "needs_you", title: stateName("needs_you") },
  { id: "offline", title: t("lib-persona.state_offline") },
];

/** persona.json, format 1. */
export interface PersonaManifest {
  format: number;
  id: string;
  name: string;
  subtitle?: string;
  /** A few words for the look's card in the picker, in place of its colors (the milk frog: where its level comes from). */
  about?: string;
  /** Names in the other languages (an extension's look, docs/extensions/extensions.md "Avatar"). */
  names?: Record<string, string>;
  subtitles?: Record<string, string>;
  /** sizes: PNGs by side; svg (an extension's look, cleaned by the hub); dark: the same for dark mode. */
  avatar: { sizes: Record<string, string>; safe?: number; svg?: string; dark?: { sizes?: Record<string, string>; svg?: string } };
  /** The built-in look whose icon stands for this one where only the app's own icons can be shown. */
  app_icon_fallback?: string;
  source?: { kind: string; extension?: string; extension_name?: string };
  dot: { size?: number; states: Record<string, { file: string; fps?: number; frames?: number }> };
  colors?: { light?: Record<string, string>; dark?: Record<string, string> };
}

/** One entry of GET /personas. */
export interface PersonaInfo {
  id: string;
  rev: string;
  bytes?: number;
  manifest: PersonaManifest;
}

/** One look of GET /looks (#3200): a built-in one the page draws (no manifest), a hub package or an extension's. */
export interface LookView {
  id: string;
  source: "builtin" | "hub" | "extension";
  name: string;
  names?: Record<string, string>;
  subtitle?: string;
  subtitles?: Record<string, string>;
  app_icon: string;
  extension?: string;
  extension_name?: string;
  rev?: string;
  bytes?: number;
  manifest?: PersonaManifest;
  /** The hub made it from an image the user uploaded (#2404): the only looks that can be deleted from the apps. */
  uploaded?: boolean;
}

/** GET /looks's current: the chosen look (null: never chosen on this hub) and the one to show. */
export interface LookCurrent {
  id: string | null;
  effective: string;
  updated_at?: string;
}

/** The packages among the looks (the hub's and the extensions'), as persona entries. */
export function lookPackages(looks: LookView[] | null | undefined): PersonaInfo[] {
  return (looks ?? []).filter((l) => l.source !== "builtin" && l.manifest && l.rev).map((l) => ({ id: l.id, rev: l.rev!, bytes: l.bytes, manifest: l.manifest! }));
}

/** A text in the interface language: its own language's, else English, else the source (docs/protocol/i18n.md). */
export function localized(text: string, others: Record<string, string> | undefined, lang: string): string {
  return others?.[lang]?.trim() || (lang !== "zh-Hans" ? others?.en?.trim() : "") || text;
}

/** Why a package can't be used, or null (the hub checks the same: server/internal/hub/persona.go, PersonaManifest.problem). */
export function personaProblem(p: PersonaInfo): string | null {
  const m = p.manifest;
  if (!m || typeof m !== "object") return "no manifest";
  if (m.format !== 1) return `format ${m.format}`;
  if (m.id !== p.id || RESERVED.includes(m.id) || !/^[a-z0-9][a-z0-9-]{0,31}$/.test(m.id)) return `id ${m.id}`;
  if (!m.name?.trim()) return "no name";
  if (!m.avatar?.sizes || !Object.keys(m.avatar.sizes).length) return "no avatar";
  if (!m.dot?.states?.idle) return "no idle dot";
  for (const f of personaFiles(m)) {
    if (!f || f.startsWith("/") || f.split("/").some((x) => x === ".." || x.startsWith("."))) return `file ${f}`;
  }
  return null;
}

/** Every file the manifest names. */
export function personaFiles(m: PersonaManifest): string[] {
  const a = m.avatar ?? { sizes: {} };
  return [...new Set([
    ...Object.values(a.sizes ?? {}), ...Object.values(a.dark?.sizes ?? {}), ...[a.svg, a.dark?.svg].filter((f): f is string => !!f),
    ...Object.values(m.dot?.states ?? {}).map((s) => s.file),
  ])].sort();
}

/**
 * The avatar file for a face `px` pixels wide: the SVG when there is one, else the smallest PNG at least that big, else
 * the largest; `dark` takes the dark variant's where it has them.
 */
export function avatarFile(m: PersonaManifest, px: number, dark = false): string | undefined {
  const d = dark ? m.avatar?.dark : undefined;
  const darkSizes = d?.sizes && Object.keys(d.sizes).length ? d.sizes : undefined;
  if (d?.svg) return d.svg;
  // Dark PNGs win over the light SVG: the variant is the point.
  if (!darkSizes && m.avatar?.svg) return m.avatar.svg;
  const sizes = Object.entries(darkSizes ?? m.avatar?.sizes ?? {})
    .map(([k, v]) => [Number(k), v] as const)
    .filter(([n]) => Number.isFinite(n))
    .sort((a, b) => a[0] - b[0]);
  return (sizes.find(([n]) => n >= px) ?? sizes[sizes.length - 1])?.[1];
}

/** The state's frames (an APNG, or a still PNG), idle's for a state the package lacks. */
export function dotFile(m: PersonaManifest, s: PersonaState): string | undefined {
  return (m.dot?.states?.[s] ?? m.dot?.states?.idle)?.file;
}

/** Where an <img> gets a package file: it can't send a header, so the token goes in the query (the hub never logs it). */
export function personaFileURL(p: Pick<PersonaInfo, "id" | "rev">, file: string, token: string): string {
  const path = file.split("/").map(encodeURIComponent).join("/");
  return `${BASE}/api/v1/personas/${encodeURIComponent(p.id)}/${path}?rev=${encodeURIComponent(p.rev)}&token=${encodeURIComponent(token)}`;
}

/**
 * The list to show after a refresh: the chosen package at its new rev once its files are in the browser (`ready`),
 * else still at the rev on screen, as the phone keeps its copy until the new one is downloaded whole (a tiered look
 * moving to another tier never shows half a face).
 */
export function withChosenReady(next: PersonaInfo[], prev: PersonaInfo[], chosen: string, ready: boolean): PersonaInfo[] {
  if (ready) return next;
  const old = prev.find((p) => p.id === chosen);
  return old ? next.map((p) => (p.id === chosen ? old : p)) : next;
}

/** The packages that check out, in the hub's order. */
export function usablePersonas(list: PersonaInfo[] | null | undefined): PersonaInfo[] {
  return (list ?? []).filter((p) => p && personaProblem(p) === null);
}

/**
 * What the face shows for a Persona conversation (AppModel.personaState, docs/apple/persona.md "State"): "Offline" while the hub or
 * Persona's computer is out of reach, "Needs you" when it waits on you, "Thinking" / "Working" while a turn runs (thinking until it
 * writes or calls a tool), else "Idle".
 */
export function personaState(o: {
  online: boolean;
  machineOnline?: boolean;
  state?: string;
  interactions?: number;
  last?: { role: string; kind: string } | null;
}): PersonaState {
  if (!o.online || o.machineOnline === false) return "offline";
  if (o.state === "needs_you" || (o.interactions ?? 0) > 0) return "needs_you";
  if (o.state !== "working") return "idle";
  return !o.last || o.last.role === "user" || o.last.kind === "thinking" ? "thinking" : "working";
}
