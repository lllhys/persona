// Day dividers for the Agent/machine transcript (#8160): a DayLine over the first line of each new calendar day,
// the same rule as the assistant feed's withDays (lib/feed.ts), reusing its startOfDay so there is one date rule.
// The lines keep their order, which is not always the order of their times (a clock that was behind): a line from a
// day already passed gets no divider of its own, so the days only go forward and each divider (`day-<date>`) is once.
import { startOfDay } from "../../lib/feed";
import type { ChatLine } from "../../lib/turns";

export type ChatDayRow =
  | { kind: "line"; id: string; line: ChatLine }
  | { kind: "day"; id: string; date: Date };

/** When a line happened, for the day dividers. Mirrors lib/feed's lineTime for the machine transcript's lines. */
export function chatLineTime(line: ChatLine): number | undefined {
  if (line.kind === "supervision") return line.lines.map(chatLineTime).find((at) => at !== undefined);
  if (line.kind === "segment") return Date.parse(line.seg.user?.ts ?? line.seg.start ?? "") || undefined;
  if (line.kind === "notice" || line.kind === "apiError" || line.kind === "compact") return Date.parse(line.item.ts ?? "") || undefined;
  return undefined;
}

/** The lines with a divider over the first line of each later day. A divider only goes forward (a line from a clock
 *  that was behind is not a day back, nor the same day twice), and none comes from a line dated after now (a clock
 *  ahead would take every day after it with it). Keys derive from the calendar day only, so a page inserted above or
 *  a line streamed in around midnight neither shifts nor duplicates a divider already drawn. The first line's own day
 *  is not a boundary and gets no divider (`includeLeadingDay`: an earlier stretch's successor opens on a real boundary). */
export function withDayDividers(lines: readonly ChatLine[], now: number = Date.now(), opts: { includeLeadingDay?: boolean } = {}): ChatDayRow[] {
  const out: ChatDayRow[] = [];
  let last = "";
  let seen = false;
  for (const line of lines) {
    const t = chatLineTime(line);
    if (t !== undefined && !Number.isNaN(t) && t <= now + 60_000) {
      // YYYY-MM-DD: compares as a string
      const day = new Date(t).toLocaleDateString("sv");
      if (day > last && (opts.includeLeadingDay || seen)) {
        out.push({ kind: "day", id: `day-${day}`, date: startOfDay(t) });
        last = day;
      }
      seen = true;
      if (day > last) last = day;
    }
    out.push({ kind: "line", id: line.id, line });
  }
  return out;
}

/** The local day key (YYYY-MM-DD) a divider row carries, for guarding against a second divider for the same day. */
export function dayRowKey(row: ChatDayRow): string | undefined {
  return row.kind === "day" ? row.id.slice("day-".length) : undefined;
}
