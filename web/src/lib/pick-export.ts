// What picked messages carry as text and as a picture (#9469; iOS ChatExport.swift): which questions each reply brings
// (Include questions), each message read whole as a forward reads it, the copied text, and where a long picture's
// pages break. The picture itself is drawn by views/chat/ExportImage.tsx, a chunk of its own. No DOM here.
import { fullItem } from "./api";
import { FORWARD_LIMIT, forwardBody } from "./forward-block";
import type { ForwardDraft, ForwardPick, ForwardRole } from "./forward";
import { isLoopTick, segments, type UserMessage } from "./turns";
import type { Item } from "./types";
import { t } from "../i18n";

/** One of your messages as a question a reply brings: what its bubble's checkbox would pick. */
export function questionPick(u: UserMessage): ForwardPick {
  return { itemId: u.id, text: u.text, isUser: true, ts: u.ts, images: u.images, photos: u.photos };
}

/** A message of yours drawn as your bubble: not a line Persona typed in, a relayed or another session's, or a /loop tick. */
function isQuestion(u: UserMessage | undefined): u is UserMessage {
  return Boolean(u && u.text.trim() && !u.injected && !u.source && !u.from && !isLoopTick(u));
}

/** Include questions: each reply (a segment's answer and the replies in its work) → the messages of yours it answers,
 *  its segment's and those of the segments before it that got no reply (two messages sent one after the other). */
export function pickQuestions(items: Item[]): Map<string, ForwardPick[]> {
  const out = new Map<string, ForwardPick[]>();
  let waiting: ForwardPick[] = [];
  for (const seg of segments(items)) {
    const asked = isQuestion(seg.user) ? [...waiting, questionPick(seg.user)] : waiting;
    const replies = [...seg.steps.flatMap((s) => (s.kind === "text" ? [s.item.id] : [])), ...(seg.answer ? [seg.answer.id] : [])];
    if (!replies.length) {
      waiting = asked;
      continue;
    }
    waiting = [];
    if (asked.length) for (const r of replies) out.set(r, asked);
  }
  return out;
}

/** One message as it goes into the copy or the picture: who wrote it, its whole text as a forward carries it, when,
 *  and your message's pictures. */
export interface ExportMessage {
  id: string;
  role: ForwardRole;
  /** "Me" in the reader's language for your own; else the name ("Claude", "Persona"). */
  author: string;
  text: string;
  time?: string;
  images: Item[];
}

/** The drafts' messages, each read whole when the chat holds it cut (`GET …/items/{id}/full`), in the drafts' order. */
export async function exportMessages(drafts: readonly ForwardDraft[]): Promise<ExportMessage[]> {
  const raws = await Promise.all(drafts.map(async (d) => (d.cut ? ((await fullItem(d.source.mid, d.source.sid, d.cut)).text ?? d.raw) : d.raw)));
  return drafts.map((d, i) => ({
    id: d.source.item,
    role: d.source.role,
    author: d.source.role === "user" ? t("chat-forward.me") : d.source.author,
    text: forwardBody(raws[i], undefined, FORWARD_LIMIT),
    time: d.source.time,
    images: d.images ?? [],
  }));
}

/** Copy: who wrote each, then its words, a blank line between them. */
export function copiedText(messages: readonly ExportMessage[]): string {
  return messages.map((m) => `${t("chat-forwardto.copied_author", { author: m.author })}\n${m.text}`).join("\n\n");
}

/** One page's share of a block: from `from` to `to` (CSS px) of block `block`. */
export interface Slice {
  block: number;
  from: number;
  to: number;
}

/** Where a long picture's pages break (iOS ExportPages): blocks (the heading, each message) stacked with `gaps[i]` before
 *  block i, each page holding `room` px of them; a block that does not fit what is left starts the next page, and one
 *  taller than a page is cut, page by page. */
export function pagePlan(heights: readonly number[], gaps: readonly number[], room: number): Slice[][] {
  const pages: Slice[][] = [];
  let page: Slice[] = [];
  let used = 0;
  const next = () => {
    if (page.length) pages.push(page);
    page = [];
    used = 0;
  };
  heights.forEach((h, i) => {
    if (h <= 0) return;
    const gap = page.length ? gaps[i] ?? 0 : 0;
    if (used + gap + h <= room) {
      page.push({ block: i, from: 0, to: h });
      used += gap + h;
      return;
    }
    if (h <= room) {
      next();
      page.push({ block: i, from: 0, to: h });
      used = h;
      return;
    }
    // taller than a page: on pages of its own
    next();
    for (let from = 0; from < h; from += room) {
      page.push({ block: i, from, to: Math.min(h, from + room) });
      if (from + room < h) next();
      else used = h - from;
    }
  });
  next();
  return pages;
}

/** A page is at most this tall in pixels, and at most this much to hold (4 bytes a pixel): a browser's canvas is limited. */
export const MAX_PIXELS = 16_000;
export const MAX_BYTES = 64 * 1024 * 1024;

/** A page's height at most, in CSS px, for a picture `width` px wide drawn at `scale`. */
export function pageHeight(width: number, scale: number): number {
  return Math.floor(Math.min(MAX_PIXELS, MAX_BYTES / (4 * width * scale)) / scale);
}

/** The heading's second line: the day (or days) the messages are from, and who answers. */
export function exportSubtitle(messages: readonly ExportMessage[], agent: string, locale?: string): string {
  const times = messages.map((m) => (m.time ? Date.parse(m.time) : NaN)).filter((x) => !Number.isNaN(x));
  const first = times.length ? Math.min(...times) : Date.now();
  const last = times.length ? Math.max(...times) : first;
  const day = new Intl.DateTimeFormat(locale, { year: "numeric", month: "long", day: "numeric" });
  const a = day.format(first), b = day.format(last);
  return [a === b ? a : `${a} – ${b}`, agent].filter(Boolean).join(" · ");
}
