// An extension page's last view (docs/web/web.md "Extension pages on reload"): the full page opened again draws it at
// once, inert and marked as updating, until the extension draws the page again. Never a source of authority: nothing in
// it can be pressed. Kept in localStorage under the feed cache's hub and sign-in scope (lib/feed-cache.ts scopedKey), so
// signing out, new credentials or another hub never show it. One entry per extension, page, first state, UI version,
// default manager and language, each at most VIEW_MAX_BYTES of JSON, at most VIEW_MAX_ENTRIES of them: the least
// recently drawn goes first. A secret field's value, a view holding the sign-in token and one over the size are not kept.
import { containsCredential, feedCacheScope, fingerprint, scopedKey } from "./feed-cache";
import { readView, type UIView } from "./extension-ui";

export const VIEW_MAX_BYTES = 64 * 1024;
export const VIEW_MAX_ENTRIES = 24;
/** The UI version a page drawn over the app socket asks for (views/settings/NativeExtensionUI.tsx). */
export const NATIVE_UI_VERSION = 7;

/** Which page: an extension's page (`ui`) opened with its first state, drawn in a language. */
export interface KeptPage {
  extension: string;
  ui: string;
  state: string;
  language: string;
}

/** Whose drawing it is: the UI version it was asked with and the default manager whose extension drew it. */
export interface KeptKey extends KeptPage {
  version: number;
  manager: string;
}

const INDEX = "ext-views";
const parts = (k: KeptKey) => [k.extension, k.ui, k.state, k.language, k.version, k.manager];
const same = (a: KeptKey, b: KeptKey) => JSON.stringify(parts(a)) === JSON.stringify(parts(b));
const entry = (scope: string | null, k: KeptKey) => scopedKey(scope, `ext-view:${fingerprint(JSON.stringify(parts(k)))}`);
const bytes = (raw: string) => new TextEncoder().encode(raw).length;
const valid = (k: Partial<KeptKey> | null): k is KeptKey =>
  !!k && [k.extension, k.ui, k.state, k.language, k.manager].every((x) => typeof x === "string") && Number.isSafeInteger(k.version);

/** The kept keys, least recently drawn first. */
function index(scope: string | null): KeptKey[] {
  try {
    const key = scopedKey(scope, INDEX);
    const list: unknown = key ? JSON.parse(localStorage.getItem(key) ?? "[]") : [];
    return Array.isArray(list) ? list.filter(valid) : [];
  } catch {
    return [];
  }
}

function save(scope: string | null, list: KeptKey[], gone: KeptKey[]) {
  for (const k of gone) {
    const key = entry(scope, k);
    if (key) localStorage.removeItem(key);
  }
  const key = scopedKey(scope, INDEX);
  if (key) localStorage.setItem(key, JSON.stringify(list));
}

/**
 * The page's last view drawn with one of `versions` (the ones the page may ask with now) by `manager`'s extension, if
 * one is kept; `manager` undefined before the roster names the default: the last one drawn.
 */
export function readKeptView(page: KeptPage, versions: readonly number[], manager?: string): { key: KeptKey; view: UIView } | null {
  const scope = feedCacheScope();
  const list = index(scope);
  for (let i = list.length - 1; i >= 0; i--) {
    const k = list[i];
    if (k.extension !== page.extension || k.ui !== page.ui || k.state !== page.state || k.language !== page.language) continue;
    if (!versions.includes(k.version) || (manager !== undefined && k.manager !== manager)) continue;
    try {
      const raw = localStorage.getItem(entry(scope, k) ?? "");
      if (raw && bytes(raw) <= VIEW_MAX_BYTES && !containsCredential(raw)) return { key: k, view: readView(JSON.parse(raw)) };
    } catch {
      // unreadable: dropped below
    }
    try {
      list.splice(i, 1);
      save(scope, list, [k]); // gone, too big or unreadable: never shown
    } catch {
      return null; // storage off: the page waits for the extension, as before
    }
  }
  return null;
}

/** A secret field's value never goes to storage. */
function withoutSecrets(this: unknown, name: string, value: unknown): unknown {
  const item = this as { type?: unknown; kind?: unknown };
  return name === "value" && item?.type === "field" && item.kind === "secret" ? undefined : value;
}

/** Keeps the view the page just drew (the most recently drawn of all); false when it is not kept. */
export function keepView(key: KeptKey, view: UIView): boolean {
  const scope = feedCacheScope();
  const name = entry(scope, key);
  if (!name) return false;
  const all = index(scope);
  const list = all.filter((k) => !same(k, key));
  try {
    const raw = JSON.stringify(view, withoutSecrets);
    if (bytes(raw) > VIEW_MAX_BYTES || containsCredential(raw)) {
      save(scope, list, [key]);
      return false;
    }
    // drawn again as it was (a refresh), and the most recent already: nothing to write
    const last = all.at(-1);
    if (last && same(last, key) && localStorage.getItem(name) === raw) return true;
    list.push(key);
    save(scope, list, list.splice(0, Math.max(0, list.length - VIEW_MAX_ENTRIES)));
    localStorage.setItem(name, raw);
    return true;
  } catch {
    // storage full or off: this one is not kept, nor an older copy of it
    try {
      save(scope, index(scope).filter((k) => !same(k, key)), [key]);
    } catch {
      // as above
    }
    return false;
  }
}
