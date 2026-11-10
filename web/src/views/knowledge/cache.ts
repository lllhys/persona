// What the knowledge page keeps between visits: entries it has seen (a detail shows its row at once while it
// loads), the lists by kind and tag, and the results of the most recent searches by query (coming back to a search
// shows it again).
//
// All of it is kept per assistant: the knowledge base is the current assistant's (its routes name the pick, lib/
// manager-choice.ts), so another assistant made current never paints the last one's entries, and switching back
// shows its own at once again.
import type { KBEntry, KBList, KBResult } from "../../lib/kb";
import { chosenManager } from "../../lib/manager-choice";

/** Whose knowledge base a key is of: the picked assistant, "" for the default (whose requests name none). */
const scope = () => chosenManager() ?? "";

const entries = new Map<string, KBEntry>();

export function remember(list: (KBEntry | undefined | null)[] | null | undefined) {
  for (const e of list ?? []) if (e) entries.set(`${scope()}|${e.kind}/${e.id}`, e);
}

export function known(kind: string, id: string): KBEntry | undefined {
  return entries.get(`${scope()}|${kind}/${id}`);
}

export const lists = new Map<string, KBList>();
export const listKey = (kind: string, tag?: string) => `${scope()}|${kind}|${tag ?? ""}`;
/** The key of a search's results in `searches`. */
export const searchKey = (q: string) => `${scope()}|${q}`;

/** Queries whose results are kept. Every query typed and paused on adds one while the page stays open, so keep only
 * the latest: an older query typed again shows the searching state until the hub answers, as on a first visit. */
export const SEARCHES_KEPT = 30;

/** A map that keeps only its `limit` most recently read or written keys. */
class Recent<V> {
  private readonly map = new Map<string, V>();

  constructor(private readonly limit: number) {}

  get size() {
    return this.map.size;
  }

  get(key: string): V | undefined {
    const v = this.map.get(key);
    if (v !== undefined) this.set(key, v);
    return v;
  }

  set(key: string, value: V) {
    this.map.delete(key);
    this.map.set(key, value);
    if (this.map.size > this.limit) this.map.delete(this.map.keys().next().value!);
    return this;
  }

  clear() {
    this.map.clear();
  }
}

export const searches = new Recent<KBResult[]>(SEARCHES_KEPT);
