// Provider import links (#5420): Settings › Model providers › a provider › ⋯ › "Copy import link" puts a Persona
// provider's address and models in a link, as cindy's v1 import JSON (spec/fixtures/provider-import-link: `encode`).
// Whoever opens it gets Add provider filled in on their own hub (lib/import-link.ts takes it out of the address bar) and
// saves it themselves; the hub reads it like a pasted configuration (POST /api/v1/launchers/import/parse). The hub never
// hands out a stored key, so a link has one only when the user pastes it into the copy dialog. Built from what
// GET /api/v1/launchers lists: no new hub route, and the link is checked only by the hub that opens it.
import { managedProvider } from "./compatibility";
import { here } from "./hubs";
import { providerTitle } from "./provider-names";
import type { ProviderModel, ProviderView } from "./providers";

/** The hub's hello feature for reading import links (cindy's and the web's) on the parse route. */
export const IMPORT_LINK_FEATURE = "launcher_import_link";

/** A provider as GET /api/v1/launchers lists it, with where it comes from (`source`). */
export type LinkSource = ProviderView & { source?: string };

/** One model in a link: its ID alone, or with what Persona knows of it. */
export type LinkModel = string | {
  id: string;
  name?: string;
  contextWindow?: number;
  maxOutputTokens?: number;
  supportsImageInput?: boolean;
  supportsToolCalls?: boolean;
  reasoning?: boolean;
  reasoningEfforts?: string[];
  reasoningDefaultEffort?: string;
};

/** cindy's v1 custom provider, in the key order the link is built in (the fixture compares the serialized text). */
export interface LinkPayload {
  kind: "custom";
  name: string;
  auth?: { method: "apiKey"; apiKey: string };
  endpoints: { protocol: string; baseUrl: string; models?: LinkModel[] }[];
}

/** How much of the model list fits in the link: all of it, the IDs alone, or none (whoever opens it fetches them). */
export type ModelsFit = "full" | "ids" | "none";

/** The most a link's fragment carries, as the hub and cindy read one. It is the tighter of their two limits: a fragment
 *  this long decodes to at most 24 570 bytes of JSON, within their 24 KiB for it. */
const MAX_FRAGMENT = 32 * 1024;
const MAX_MODELS = 256;
const MAX_NAME = 60;
const MAX_ID = 256;
const MAX_URL = 2048;
const MAX_KEY = 4 * 1024;
const FRAGMENT_HEAD = "import=";
/** The thinking levels a cindy model may list. */
const EFFORTS = new Set(["minimal", "low", "medium", "high", "xhigh", "max"]);
const PROTOCOL = { messages: "anthropic-messages", responses: "openai-responses", chat: "openai-chat" } as const;

/** The cindy protocol a provider is reached by: a Claude Code launcher's Messages, or a direct Responses or Chat
 *  Completions API's; null for the rest, which no cindy endpoint brings back as they are: Codex, Antigravity, omp, and a
 *  direct Messages API (the hub reads a Messages endpoint back as a Claude Code gateway). */
function protocolOf(p: LinkSource): string | null {
  if (p.kind === "api") return p.api === "responses" || p.api === "chat" ? PROTOCOL[p.api] : null;
  return !p.agent || p.agent === "claude" ? PROTOCOL.messages : null;
}

/** An absolute http(s) address with no user name, password, query or fragment: those can hold a key. */
function plainUrl(s: string | undefined): s is string {
  if (!s || s.length > MAX_URL || /[\s?#]/.test(s)) return false;
  try {
    const u = new URL(s);
    return (u.protocol === "http:" || u.protocol === "https:") && !u.username && !u.password;
  } catch {
    return false;
  }
}

/** Whether a provider can be shared as a link: one of Persona's own (an rc-file alias or a built-in is a computer's,
 *  not Persona's to share), on a plain address, reached the way a cindy endpoint describes. */
export function linkable(p: LinkSource): boolean {
  return managedProvider(p.source) && plainUrl(p.base_url) && protocolOf(p) !== null;
}

/** `s` cut to `max` UTF-16 units without splitting a surrogate pair. */
function cut(s: string, max: number): string {
  if (s.length <= max) return s;
  const end = /[\uD800-\uDBFF]/.test(s[max - 1]!) ? max - 1 : max;
  return s.slice(0, end).trimEnd();
}

/** A model as the link carries it: the ID alone when that is all Persona knows of it. Thinking levels go only when
 *  they are cindy's, so one unknown level never makes the hub refuse the whole link; a model the gateway said takes
 *  none goes as reasoning false, and its output limit, image input and tool calls as cindy names them (#8778). */
function linkModel(m: ProviderModel): LinkModel {
  const out: Exclude<LinkModel, string> = { id: m.id };
  const name = m.name?.trim();
  if (name && name !== m.id && name.length <= MAX_ID) out.name = name;
  if (Number.isInteger(m.context_window) && m.context_window! > 0) out.contextWindow = m.context_window;
  if (Number.isInteger(m.max_output_tokens) && m.max_output_tokens! > 0) out.maxOutputTokens = m.max_output_tokens;
  if (typeof m.image_input === "boolean") out.supportsImageInput = m.image_input;
  if (typeof m.tool_calls === "boolean") out.supportsToolCalls = m.tool_calls;
  const efforts = [...new Set((m.efforts ?? []).filter((e) => EFFORTS.has(e)))];
  if (efforts.length) {
    out.reasoning = true;
    out.reasoningEfforts = efforts;
    if (m.default_effort && efforts.includes(m.default_effort)) out.reasoningDefaultEffort = m.default_effort;
  } else if (m.no_effort) {
    out.reasoning = false;
  }
  return Object.keys(out).length === 1 ? m.id : out;
}

/** The provider's models, its default first, each ID trimmed and once, as the hub compares them (an empty or overlong
 *  one is left out, and a repeat: the hub would refuse the link for it). */
function modelsOf(p: LinkSource): ProviderModel[] {
  const list = p.models ?? [];
  const def = p.model?.trim();
  const all = def ? [list.find((m) => m.id.trim() === def) ?? { id: def }, ...list] : list;
  const seen = new Set<string>();
  const out: ProviderModel[] = [];
  for (const m of all) {
    const id = m.id.trim();
    if (!id || id.length > MAX_ID || seen.has(id)) continue;
    seen.add(id);
    out.push(id === m.id ? m : { ...m, id });
  }
  return out;
}

const utf8 = (s: string) => new TextEncoder().encode(s);

/** Base64url without padding, as the hub decodes it (strict: no padding, no other alphabet). */
function base64url(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** Whether a payload fits a link: its fragment, the JSON's UTF-8 in base64url (4 characters for every 3 bytes), within
 *  MAX_FRAGMENT. */
function fits(payload: LinkPayload): boolean {
  return FRAGMENT_HEAD.length + Math.ceil((utf8(JSON.stringify(payload)).length * 4) / 3) <= MAX_FRAGMENT;
}

/** The key as a link carries it (trimmed), or null when it can't be one: blank, with spaces, line breaks or control
 *  characters inside, or longer than a key may be. */
export function linkKey(key: string): string | null {
  const k = key.trim();
  return k && !/[\s\p{Cc}]/u.test(k) && utf8(k).length <= MAX_KEY ? k : null;
}

/** The link's payload for a provider, with how much of its model list fits (all of it, the IDs alone, or none), or
 *  null when it can't be shared (linkable). With `key` (linkKey's), whoever opens the link has the key too. */
export function linkPayload(p: LinkSource, key?: string): { payload: LinkPayload; fit: ModelsFit } | null {
  if (!linkable(p)) return null;
  const endpoint = { protocol: protocolOf(p)!, baseUrl: p.base_url! };
  const payload = (models?: LinkModel[]): LinkPayload => ({
    kind: "custom",
    name: cut(providerTitle(p).trim(), MAX_NAME),
    ...(key ? { auth: { method: "apiKey" as const, apiKey: key } } : {}),
    endpoints: [models?.length ? { ...endpoint, models } : endpoint],
  });
  const models = modelsOf(p);
  if (!models.length) return { payload: payload(), fit: "full" };
  if (models.length <= MAX_MODELS) {
    const full = payload(models.map(linkModel));
    if (fits(full)) return { payload: full, fit: "full" };
    const ids = payload(models.map((m) => m.id));
    if (fits(ids)) return { payload: ids, fit: "ids" };
  }
  return { payload: payload(), fit: "none" };
}

/** The payload as a link's `import`: its JSON (JSON.stringify's key order, no spaces) in UTF-8, base64url. */
export function encodeImportData(payload: LinkPayload): string {
  return base64url(utf8(JSON.stringify(payload)));
}

/** The link that opens Add provider with `data` on this hub's web page. */
export function importLinkHref(data: string): string {
  return `${here()}/settings?tab=models#${FRAGMENT_HEAD}${data}`;
}
