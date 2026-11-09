// The whole document of /kb/selection. Edits retain history and untouched optional fields.
export const MESSAGE_RULES = ["mentions", "mine", "dm", "group", "bot", "official", "muted", "collapsed", "done"] as const;
export const DOC_RULES = ["mentions", "comment_mentions", "authored", "viewed"] as const;
export interface SelectionRule {
  on: boolean;
  days: number | null;
  attachment_days?: number | null;
  include_all?: boolean | null;
  context?: number | null;
}
export interface SelectionRules { order: string[][]; rules: Record<string, SelectionRule> }
export interface SelectionItem {
  source: string;
  kind: "chat" | "doc";
  id: string;
  name?: string;
  read: boolean;
  store: boolean;
  days: number | null;
}
export interface KBSelection {
  version: number;
  messages: SelectionRules;
  docs: SelectionRules;
  items: SelectionItem[] | null;
  history: Record<string, { back_days: number | null }> | null;
}
export interface SelectionCandidates {
  source: string;
  cached?: boolean;
  chats: { id: string; name: string; source: string }[];
}

export const selectionItemKey = (item: Pick<SelectionItem, "source" | "kind" | "id">): string => JSON.stringify([item.source, item.kind, item.id]);
const daysValid = (n: unknown): boolean => n === null || (typeof n === "number" && Number.isInteger(n) && n >= 1 && n <= 36500);

/** Refuse unsupported/malformed reads, rather than enable a save of invented defaults. */
export function validSelection(value: unknown): value is KBSelection {
  if (!value || typeof value !== "object") return false;
  const s = value as KBSelection;
  if (s.version !== 1 || (s.items !== null && !Array.isArray(s.items)) || (s.history !== null && (typeof s.history !== "object" || Array.isArray(s.history)))) return false;
  for (const [part, names] of [[s.messages, MESSAGE_RULES], [s.docs, DOC_RULES]] as const) {
    if (!part || !part.rules || !Array.isArray(part.order) || !part.order.length || part.order.some((l) => !Array.isArray(l) || !l.length)) return false;
    const flat = part.order.flat();
    if (flat.length !== names.length || new Set(flat).size !== names.length || names.some((n) => !flat.includes(n))) return false;
    if (Object.keys(part.rules).length !== names.length) return false;
    for (const name of names) {
      const r = part.rules[name];
      if (!r || typeof r.on !== "boolean" || !daysValid(r.days)) return false;
      if (part === s.messages && !daysValid(r.attachment_days)) return false;
      if (r.include_all != null && (name !== "mentions" || typeof r.include_all !== "boolean")) return false;
      if (r.context != null && (name !== "mine" || !Number.isInteger(r.context) || r.context < 0 || r.context > 200)) return false;
    }
  }
  const keys = new Set<string>();
  for (const item of s.items ?? []) {
    if (!item || typeof item.source !== "string" || !item.source || typeof item.id !== "string" || !item.id || !["chat", "doc"].includes(item.kind) || typeof item.read !== "boolean" || typeof item.store !== "boolean" || (item.store && !item.read) || !daysValid(item.days)) return false;
    const key = selectionItemKey(item);
    if (keys.has(key)) return false;
    keys.add(key);
  }
  return Object.values(s.history ?? {}).every((h) => h && daysValid(h.back_days));
}

/** Group operations never put the implicit per-item tier in the reorderable rule levels. */
export function moveSelectionLevel(order: string[][], level: number, delta: -1 | 1): string[][] {
  const next = order.map((l) => [...l]);
  const to = level + delta;
  if (level >= 0 && to >= 0 && level < next.length && to < next.length) [next[level], next[to]] = [next[to], next[level]];
  return next;
}

export function groupSelectionRule(order: string[][], name: string, joinPrevious: boolean): string[][] {
  const next = order.map((l) => [...l]);
  const i = next.findIndex((l) => l.includes(name));
  if (i < 0 || (joinPrevious ? i === 0 : next[i].length === 1)) return next;
  next[i] = next[i].filter((n) => n !== name);
  if (joinPrevious) next[i - 1].push(name);
  else next.splice(i + 1, 0, [name]);
  return next.filter((l) => l.length);
}
