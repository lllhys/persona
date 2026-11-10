// Groups inside the inbox's sections (docs/assistant/inbox.md "Grouping and selection", #2059): Needs you, FYI and Collapsed by source
// or by time (the switch at the top, kept on this device), Done always by time. Pure functions: tests give `now`.
import { formatDate, t } from "../i18n";
import type { InboxItem } from "./inbox";

export type Grouping = "source" | "time";

/** A group of a section: its key (stable for the folds kept on this device), title and items, in the section's order. */
export type InboxGroup = { key: string; title: string; items: InboxItem[] };

const DAY = 86_400_000;

/** Local midnight of the day `t` falls on. */
function dayStart(t: number): number {
  const d = new Date(t);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Whole local days from `t`'s day to today's (0: today). Calendar days, not 24-hour windows. */
function daysAgo(t: number, now: number): number {
  return Math.round((dayStart(now) - dayStart(t)) / DAY);
}

/**
 * The time group of a moment: "just" (within the hour; Done only, so a slip is easy to find and undo), "today",
 * "yesterday", "d:YYYY-MM-DD" for each of the 2 to 6 days before (the last week, not the calendar week: a Monday still
 * shows last week's days), "week" (7 to 29 days ago) and "month" (30 days or more). A moment in the future is today.
 */
export function timeKey(at: string | undefined, now: number, recent = false): string {
  const t = at ? Date.parse(at) : NaN;
  if (Number.isNaN(t)) return "month";
  if (recent && now - t <= 3_600_000) return "just";
  const days = Math.max(0, daysAgo(t, now));
  if (days === 0) return "today";
  if (days === 1) return "yesterday";
  if (days <= 6) return "d:" + new Date(t).toLocaleDateString("sv"); // YYYY-MM-DD, local
  return days <= 29 ? "week" : "month";
}

/** A time group's title: "Just now", "Today", "Yesterday", "Fri, Nov 6", "7+ days ago", "1+ month ago". */
export function timeTitle(key: string): string {
  if (key.startsWith("d:")) return formatDate(new Date(keyDay(key)), { weekday: "short", month: "long", day: "numeric" });
  return t(`lib-inbox.group_${key}`);
}

/** A day key's date at local midnight ("d:2026-11-06"; Date.parse of a bare date would be UTC midnight). */
function keyDay(key: string): number {
  const [y, m, d] = key.slice(2).split("-").map(Number);
  return new Date(y, m - 1, d).getTime();
}

/** Where a time group goes: newest first (just now, today, yesterday, the days before, 7+ days, 1+ month). */
function timeRank(key: string, now: number): number {
  const fixed: Record<string, number> = { just: 0, today: 1, yesterday: 2, week: 100, month: 101 };
  return key in fixed ? fixed[key] : 2 + daysAgo(keyDay(key), now);
}

/** The items by time group, newest group first; `at` is the moment that counts (the last message, or when it was done). */
export function groupByTime(items: InboxItem[], now: number, at: (it: InboxItem) => string | undefined, recent = false): InboxGroup[] {
  const groups = new Map<string, InboxItem[]>();
  for (const it of items) {
    const k = timeKey(at(it), now, recent);
    groups.set(k, [...(groups.get(k) ?? []), it]);
  }
  return [...groups].sort((a, b) => timeRank(a[0], now) - timeRank(b[0], now)).map(([key, list]) => ({ key, title: timeTitle(key), items: list }));
}

/** An item's source group: Lark, each mail account, each connector or extension source (by its account), else its source. */
export function sourceKey(it: InboxItem): string {
  if (it.connector) return `c:${it.account}`;
  if (it.source === "mail") return `mail:${it.account}`;
  return it.source;
}

/**
 * The items by source, in the filter chips' order (`order`: the source keys as the chips list them, Lark, the mail
 * accounts, the connectors); a source the chips don't name comes after them, in the order it first shows up.
 */
export function groupBySource(items: InboxItem[], order: string[], mailName: (account: string) => string): InboxGroup[] {
  const groups = new Map<string, InboxItem[]>();
  for (const it of items) {
    const k = sourceKey(it);
    groups.set(k, [...(groups.get(k) ?? []), it]);
  }
  const rank = (k: string) => (order.includes(k) ? order.indexOf(k) : order.length);
  return [...groups].sort((a, b) => rank(a[0]) - rank(b[0])).map(([key, list]) => ({
    key,
    title: key === "lark" ? t("lib-inbox.group_lark")
      : key.startsWith("mail:") ? t("lib-inbox.group_mail", { account: mailName(key.slice(5)) })
      : key.startsWith("c:") ? list[0].account_name || list[0].source
      : list[0].account_name || key,
    items: list,
  }));
}

/** The groups of one section: by source or by time (the last message's), or none when there would be only one. */
export function sectionGroups(items: InboxItem[], grouping: Grouping, now: number, order: string[], mailName: (account: string) => string): InboxGroup[] {
  const groups = grouping === "time" ? groupByTime(items, now, (it) => it.updated_at) : groupBySource(items, order, mailName);
  return groups.length > 1 ? groups : [];
}

/** Done's groups: always by when they were done, "Just now" first. */
export function doneGroups(items: InboxItem[], now: number): InboxGroup[] {
  return groupByTime(items, now, (it) => it.done_at ?? undefined, true);
}

/** Groups open at first: every group of Needs you, FYI and Collapsed; in Done only "Just now" and "Today". */
export function openAtFirst(section: string, key: string): boolean {
  return section !== "done" || key === "just" || key === "today";
}
