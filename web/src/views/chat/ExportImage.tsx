// Export image (#9469; iOS ChatExportSheet / ChatExportRenderer): the picked messages drawn as the chat shows them into a
// picture as wide as a phone (390 px) at 2× or 3×, in the page's theme, at the standard text size: the chat's title, the
// day and who answers on top (Title and Date turns it off; never the computer or the project), your messages as bubbles
// with their pictures, each reply under its author with the chat's own Markdown (code, tables, formulas, diagrams, cards),
// its pictures through the hub only (ReplyPicturesExport), and Persona's mark at the foot. The messages are drawn out of
// sight, measured, and cut into pages of at most MAX_PIXELS (and MAX_BYTES), between messages where they can be
// (lib/pick-export.ts pagePlan); each page is drawn into a PNG by html-to-image, which this chunk loads with itself on the
// first Export image, never before (FIRST_SCREEN_BUDGET). One page: Copy image and Download PNG; several: Download N.
import { Fragment, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { errorText, imageURL } from "../../lib/api";
import type { ForwardChat, ForwardPick } from "../../lib/forward";
import { forwardDraftOf } from "../../lib/forward-draft";
import { managerOf, useRoster } from "../../lib/managers";
import type { MessagePick } from "../../lib/message-pick";
import { exportMessages, exportSubtitle, pageHeight, pagePlan, type ExportMessage, type Slice } from "../../lib/pick-export";
import { getSession } from "../../lib/store";
import { agentLabel } from "../../lib/mentions";
import { Button, Dialog, Spinner, Switch, toast } from "../../ui/atoms";
import { SessionMark } from "../../ui/AgentMark";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
import { ReplyPicturesExport } from "../../ui/ReplyImages";
import { PersonaAvatar, usePersona } from "../assistant/Persona";
import type { ExportRequest } from "./Picking";

/** The picture's width, a phone's, in CSS px. */
export const WIDTH = 390;
/** Around the words: the sides, the top, the foot. */
const PAD = 16;
const TOP = 24;
const FOOT_GAP = 20;
const BOTTOM = 16;
/** Between messages. */
const GAP = 16;

interface Page {
  url: string;
  blob: Blob;
}

/** Sharp on every screen: 2× or 3×. */
function scaleNow(): number {
  return Math.min(3, Math.max(2, Math.round(typeof devicePixelRatio === "number" ? devicePixelRatio : 2)));
}

export default function ExportImage({ request, pick, onClose }: { request: ExportRequest; pick: MessagePick; onClose: () => void }) {
  const include = useSyncExternalStore(pick.subscribe, pick.include, pick.include);
  const offered = request.with.length > request.without.length;
  const [heading, setHeading] = useState(true);
  const picks = offered && include ? request.with : request.without;
  const [messages, setMessages] = useState<ExportMessage[] | null>(null);
  const [pages, setPages] = useState<Page[] | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [plan, setPlan] = useState<{ slices: Slice[][]; heights: number[] } | null>(null);
  const measure = useRef<HTMLDivElement>(null);
  const pageRefs = useRef<(HTMLDivElement | null)[]>([]);
  const roster = useRoster();
  const persona = usePersona();
  const brand = (roster ? managerOf(roster, roster.default)?.name : "") || persona.name;
  const chat = request.chat;
  const agent = chat.assistant ? brand : chat.session ? agentLabel(chat.session) : "";
  const scale = useMemo(scaleNow, []);

  // 1. the messages, read whole
  useEffect(() => {
    let live = true;
    setMessages(null);
    setPages(null);
    setPlan(null);
    setFailed(null);
    exportMessages(picks.map((p) => forwardDraftOf(chat, p))).then(
      (m) => live && setMessages(m),
      (e: unknown) => live && setFailed(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [picks, chat, attempt]);

  // 2. drawn out of sight, settled, measured: the pages planned
  useEffect(() => {
    const box = measure.current;
    if (!messages || !box || plan) return;
    let live = true;
    void settle(box).then(() => {
      if (!live) return;
      const blocks = [...box.querySelectorAll<HTMLElement>(":scope > [data-export-block]")];
      const heights = blocks.map((b) => b.offsetHeight);
      const foot = box.querySelector<HTMLElement>("[data-export-foot]")?.offsetHeight ?? 0;
      const room = pageHeight(WIDTH, scale) - TOP - BOTTOM - FOOT_GAP - foot;
      const gaps = blocks.map((_, i) => (i === 0 ? 0 : GAP));
      setPlan({ slices: pagePlan(heights, gaps, room), heights });
    });
    return () => {
      live = false;
    };
  }, [messages, heading, plan, scale]);

  // a switch changed: measured again
  useEffect(() => {
    setPlan(null);
    setPages(null);
  }, [heading]);

  // 3. each page drawn into a PNG
  useEffect(() => {
    if (!plan) return;
    let live = true;
    const urls: string[] = [];
    void (async () => {
      try {
        const { toBlob, getFontEmbedCSS } = await import("html-to-image");
        const nodes = pageRefs.current.slice(0, plan.slices.length).filter((n): n is HTMLDivElement => Boolean(n));
        await Promise.all(nodes.map(settle));
        const background = getComputedStyle(document.body).backgroundColor;
        const fontEmbedCSS = nodes[0] ? await getFontEmbedCSS(nodes[0]) : "";
        const out: Page[] = [];
        for (const node of nodes) {
          const blob = await toBlob(node, { pixelRatio: scale, backgroundColor: background, fontEmbedCSS, filter: keep });
          if (!blob) throw new Error(t("chat-exportimage.failed"));
          const url = URL.createObjectURL(blob);
          urls.push(url);
          out.push({ url, blob });
          if (!live) return;
        }
        if (live) setPages(out);
      } catch (e) {
        if (live) setFailed(errorText(e));
      }
    })();
    return () => {
      live = false;
      urls.forEach((u) => URL.revokeObjectURL(u));
    };
  }, [plan, scale]);

  const working = !failed && !pages;
  const copy = async () => {
    const page = pages?.[0];
    if (!page) return;
    try {
      await navigator.clipboard.write([new ClipboardItem({ "image/png": page.blob })]);
      toast(t("chat-exportimage.copied"), "ok");
    } catch {
      toast(t("chat-exportimage.copy_failed"), "error");
    }
  };
  const download = () => {
    pages?.forEach((p, i) => {
      const a = document.createElement("a");
      a.href = p.url;
      a.download = `Persona-${i + 1}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
    });
  };

  // What is drawn: the heading, each message, the foot; once measured, the pages built from them.
  const blocks = messages ? exportBlocks(messages, chat, heading, exportSubtitle(messages, agent)) : [];
  const foot = (n: number, of: number) => <ExportFoot name={brand} page={of > 1 ? `${n}/${of}` : undefined} />;
  const sheet = (
    <div aria-hidden className="pointer-events-none fixed top-0" style={{ left: -10000 }} data-export-sheet>
      {!plan && messages && (
        <div ref={measure} style={{ width: WIDTH, padding: `0 ${PAD}px`, display: "flex", flexDirection: "column", gap: GAP, "--text-scale": 1 } as React.CSSProperties}>
          {blocks}
          <div data-export-foot>{foot(1, 1)}</div>
        </div>
      )}
      {plan &&
        plan.slices.map((slices, n) => (
          <div key={n} ref={(el) => void (pageRefs.current[n] = el)}
            style={{ width: WIDTH, padding: `${TOP}px ${PAD}px ${BOTTOM}px`, display: "flex", flexDirection: "column", background: "var(--page)", "--text-scale": 1 } as React.CSSProperties}>
            {slices.map((s, i) => (
              <div key={i} style={{ marginTop: i === 0 ? 0 : GAP, height: s.to - s.from, overflow: "hidden" }}>
                <div style={{ marginTop: -s.from }}>{blocks[s.block]}</div>
              </div>
            ))}
            <div style={{ marginTop: FOOT_GAP }}>{foot(n + 1, plan.slices.length)}</div>
          </div>
        ))}
    </div>
  );

  return (
    <Dialog
      open
      onClose={onClose}
      title={t("chat-forwardto.export_image")}
      width={480}
      footer={
        <>
          {pages?.length === 1 && (
            <Button icon="copy" onClick={() => void copy()} testId="export-copy">
              {t("chat-exportimage.copy_image")}
            </Button>
          )}
          <Button tone="primary" icon="download" disabled={!pages} onClick={download} testId="export-download">
            {pages && pages.length > 1 ? t("chat-exportimage.download_n", { count: pages.length }) : t("chat-exportimage.download")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3" data-export-dialog>
        <div className="flex max-h-[52vh] flex-col items-center gap-2 overflow-y-auto rounded-control bg-field p-3" style={{ minHeight: 160 }} data-export-preview>
          {working && <Spinner />}
          {failed && (
            <div className="flex flex-col items-center gap-2 py-6 text-center">
              <span className="text-meta font-medium">{t("chat-exportimage.failed")}</span>
              <span className="text-submeta text-ink-3">{failed}</span>
              <Button onClick={() => setAttempt((a) => a + 1)}>{t("chat-exportimage.retry")}</Button>
            </div>
          )}
          {pages?.map((p, i) => (
            <Fragment key={p.url}>
              {pages.length > 1 && <span className="text-submeta text-ink-3">{t("chat-exportimage.page_of", { n: i + 1, count: pages.length })}</span>}
              <img src={p.url} alt={t("chat-exportimage.page_alt", { n: i + 1 })} className="w-full rounded-control shadow-card" data-export-page={i + 1} />
            </Fragment>
          ))}
        </div>
        {offered && (
          <label className="flex items-center gap-3">
            <span className="min-w-0 flex-1">
              <span className="block text-meta">{t("chat-forwardto.include_questions")}</span>
              <span className="block text-submeta text-ink-3">{t("chat-exportimage.include_detail")}</span>
            </span>
            <Switch on={include} onChange={(on) => void pick.setInclude(on)} label={t("chat-forwardto.include_questions")} />
          </label>
        )}
        <label className="flex items-center gap-3">
          <span className="min-w-0 flex-1 text-meta">{t("chat-exportimage.title_and_date")}</span>
          <Switch on={heading} onChange={setHeading} label={t("chat-exportimage.title_and_date")} />
        </label>
      </div>
      {createPortal(sheet, document.body)}
    </Dialog>
  );
}

/** What html-to-image draws: not a code block's Copy (ReplyBlocks CodeBar, its language stays) nor a diagram's tools
 *  (Mermaid, always shown on a touch screen); a card's controls are drawn as they look elsewhere. */
function keep(node: HTMLElement): boolean {
  return !(node instanceof HTMLButtonElement && node.closest("pre, [data-code-bar], [data-mermaid]"));
}

/** Until what loads later has: fonts, pictures, diagrams (at most 6 s), then two frames. */
async function settle(box: HTMLElement): Promise<void> {
  const until = Date.now() + 6000;
  await document.fonts?.ready;
  for (;;) {
    const images = [...box.querySelectorAll("img")].filter((i) => !i.complete);
    const diagrams = [...box.querySelectorAll("[data-mermaid]")].filter((d) => !d.querySelector("[data-mermaid-diagram], svg"));
    if ((!images.length && !diagrams.length) || Date.now() > until) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
}

/** The heading (with `heading`), then each message, as blocks the pages are built from. */
function exportBlocks(messages: ExportMessage[], chat: ForwardChat, heading: boolean, subtitle: string): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  if (heading) {
    out.push(
      <div key="heading" data-export-block className="flex flex-col gap-1 border-b border-line pb-3">
        {chat.title && <span className="text-card-title font-semibold">{chat.title}</span>}
        <span className="text-label text-ink-3">{subtitle}</span>
      </div>,
    );
  }
  for (const m of messages) out.push(<ExportMessageView key={m.id} message={m} chat={chat} />);
  return out;
}

function ExportMessageView({ message: m, chat }: { message: ExportMessage; chat: ForwardChat }) {
  if (m.role === "user") {
    return (
      <div data-export-block className="ml-auto flex flex-col items-end gap-1.5" style={{ maxWidth: "80%" }}>
        {m.images.length > 0 && (
          <div className="flex flex-wrap justify-end gap-1">
            {m.images.map((im) => (
              <img key={im.id} src={imageURL(chat.mid, chat.sid, im.id)} alt="" className="size-24 rounded-control object-cover" />
            ))}
          </div>
        )}
        {m.text && <div className="max-w-full rounded-[12px] bg-user-bubble px-4 py-2.5 text-[length:var(--chat-body)] leading-[1.6] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{m.text}</div>}
      </div>
    );
  }
  const session = getSession(chat.mid, chat.sid) ?? chat.session;
  return (
    <div data-export-block className="flex flex-col gap-1">
      <span className="flex items-center gap-1.5 text-label font-semibold text-ink-3">
        {m.role === "persona" ? <PersonaAvatar size={16} /> : m.role === "agent" && session ? <SessionMark s={session} size={16} /> : <Icon name="chat" size={14} />}
        {m.author}
      </span>
      <ReplyPicturesExport.Provider value>
        <div className="[&_.prose-j]:text-[length:var(--chat-body)]! [&_.prose-j]:leading-[1.6]!">
          <Markdown text={m.text} mid={chat.mid} cwd={session?.cwd} lineBreaks />
        </div>
      </ReplyPicturesExport.Provider>
    </div>
  );
}

/** The foot of each page: Persona's mark (the assistant's face and name, "(Persona)" after it), and which page. */
function ExportFoot({ name, page }: { name: string; page?: string }) {
  return (
    <div className="flex items-center justify-center gap-1.5 text-label text-ink-3">
      <PersonaAvatar size={16} />
      <span className="font-semibold">{name}</span>
      {name.trim().toLowerCase() !== "persona" && <span className="opacity-70">(Persona)</span>}
      {page && <span className="tabular-nums">· {page}</span>}
    </div>
  );
}

export type { ForwardPick };
