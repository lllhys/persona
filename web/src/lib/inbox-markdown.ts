// An assistant Inbox row's text (#9041, docs/assistant/inbox.md "Assistant rows"): the hub keeps it as Markdown in `body`
// and says it as plain words in `summary` and `preview`; a row from an older hub has only the Markdown run into one line.
// Only the Inbox views import this, so none of it is on the first screen.
import type { InboxItem } from "./inbox";
import { plainMarkdown } from "./plain-markdown";

/** A row from before the hub kept an assistant post's Markdown as `body` (#9041): its summary and preview hold that
 *  Markdown run into one line. */
export function markdownInSummary(it: InboxItem): boolean {
  return it.source === "assistant" && !it.body;
}

/** What the detail draws as Markdown: an assistant row's `body`, or an older row's summary (`markdownInSummary`);
 *  undefined for any other row, whose summary is the AI's line about it. */
export function markdownBody(it: InboxItem): string | undefined {
  if (it.source !== "assistant") return undefined;
  return (it.body || it.summary || "").trim() || undefined;
}

/** Markdown run into one line, as words: its marks go (`plainMarkdown`), and so do the list markers the lost line
 *  breaks left between words and a mark the cut left alone ("Classes*…"). Only for an older assistant row's text: a
 *  plain line's own dashes are not touched. */
export function withoutMarks(text: string): string {
  return plainMarkdown(text).replace(/\*+…/g, "…").split(/\s+/).map((w) => w.replace(/^\*+|\*+$/g, "")).filter((w) => w && w !== "-" && w !== "+").join(" ");
}

/** The summary as a line or two of the list draws it: an older assistant row's without its marks. */
export function lineSummary(it: InboxItem): string | undefined {
  return markdownInSummary(it) && it.summary ? withoutMarks(it.summary) : it.summary;
}
