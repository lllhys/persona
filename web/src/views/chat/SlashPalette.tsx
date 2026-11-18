// The composer's "/" palette (#5325), drawn as cindy's slash command palette at the owner's request (2026-11-12): a
// 320 px panel at the composer's left edge, one 36 px row per command with where it comes from on the right, and the
// focused command's description in a 280 px card beside the panel (to its right, to its left when the window has no
// room there, inside the panel under the rows when it has room on neither side, as on a phone). Rows come ranked
// (lib/composer-commands.ts rankCommands); the composer keeps the keys and the focus. Its own chunk: drawn only once
// "/" is typed (Composer.tsx). Command-only markup mirrors SlashSkillView.tsx; SlashPalette.test.tsx compares both views.
import { useEffect, useId, useLayoutEffect, useReducer, useRef, useState } from "react";
import type { ComposerCommand as Command } from "../../lib/extension-prompts";
import { cx, EASE } from "../../ui/atoms";
import { t } from "../../i18n";

const PANEL_W = 320;
const CARD_W = 280;
/** Between the panel and the card. */
const GAP = 8;
/** Kept from the window's edges. */
const EDGE = 8;
const CARD_MAX = 400;

/** Beside the panel: its top and height in the palette; or in the panel, under the rows. */
type Place = { side: "right" | "left"; top: number; maxHeight: number } | { side: "inside" };

const samePlace = (a: Place | null, b: Place) =>
  a?.side === b.side && (b.side === "inside" || (a.side !== "inside" && a.top === b.top && a.maxHeight === b.maxHeight));

/** A command as its row names it: without the "/" already typed. */
const name = (c: Command) => c.word.replace(/^\//, "");
/** What picking it types, when that is more than the command itself (Persona's text commands, quick prompts). */
const typed = (c: Command) => (c.text.trim() === c.word ? "" : c.text.trim());

function Description({ c, id, className }: { c: Command; id?: string; className?: string }) {
  const text = typed(c);
  return (
    <div id={id} className={className}>
      {c.desc && <p className="text-ink-2">{c.desc}</p>}
      {text && <p className={cx("line-clamp-4 whitespace-pre-line text-ink-3", c.desc && "mt-1")}>{t("chat-slashpalette.types", { text })}</p>}
    </div>
  );
}

export function SlashPalette({ rows, active, onHover, onPick, onClose, within }: {
  rows: Command[];
  active: number;
  onHover: (i: number) => void;
  onPick: (c: Command) => void;
  /** A press outside `within` (the composer) closes it. */
  onClose: () => void;
  within: React.RefObject<HTMLElement | null>;
}) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const [glide, setGlide] = useState<{ top: number; height: number } | null>(null);
  const [place, setPlace] = useState<Place | null>(null);
  // the list scrolled or the window changed size: measure again
  const [, remeasure] = useReducer((n: number) => n + 1, 0);
  const focused = rows[active];

  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const down = (e: Event) => {
      if (!within.current?.contains(e.target as Node)) close.current();
    };
    document.addEventListener("pointerdown", down, true);
    addEventListener("resize", remeasure);
    return () => {
      document.removeEventListener("pointerdown", down, true);
      removeEventListener("resize", remeasure);
    };
  }, [within]);

  // the focused row into the list's view, on a key or a new filter only: the wheel may leave it
  useLayoutEffect(() => {
    const l = list.current;
    const row = refs.current[active];
    if (!l || !row) return;
    if (row.offsetTop < l.scrollTop) l.scrollTop = row.offsetTop;
    else if (row.offsetTop + row.offsetHeight > l.scrollTop + l.clientHeight) l.scrollTop = row.offsetTop + row.offsetHeight - l.clientHeight;
  }, [active, rows]);

  // After every render, since the rows, the focus, the list's scroll and the card's own height all move the card; a
  // place that did not change renders nothing more.
  useLayoutEffect(() => {
    const r = root.current;
    const p = panel.current;
    const l = list.current;
    if (!r || !p) return;
    const row = refs.current[active];
    const at = row ? { top: row.offsetTop, height: row.offsetHeight } : null;
    setGlide((cur) => (cur?.top === at?.top && cur?.height === at?.height ? cur : at));
    // the panel's left edge does not move while it grows in (it grows from its bottom left corner)
    const box = r.getBoundingClientRect();
    const right = box.left + PANEL_W + GAP + CARD_W + EDGE <= innerWidth;
    const left = box.left - GAP - CARD_W >= EDGE;
    let next: Place = { side: "inside" };
    if (right || left) {
      // in the palette's own coordinates: the card starts a little above the focused row and never ends below the
      // panel, nor above the window
      const bottom = p.offsetTop + p.offsetHeight;
      const ceiling = EDGE - box.top;
      const rowTop = row && l ? p.offsetTop + l.offsetTop + row.offsetTop - l.scrollTop : p.offsetTop;
      const maxHeight = Math.max(1, Math.min(CARD_MAX, bottom - ceiling));
      const height = Math.min(card.current?.scrollHeight ?? 0, maxHeight);
      const top = Math.round(Math.max(ceiling, Math.min(rowTop - 6, bottom - height)));
      next = { side: right ? "right" : "left", top, maxHeight: Math.round(maxHeight) };
    }
    setPlace((cur) => (samePlace(cur, next) ? cur : next));
  });

  const described = (c: Command) => Boolean(c.desc || typed(c));
  return (
    <div ref={root} data-slash-palette className="absolute bottom-full left-0 z-10 mb-2" style={{ animation: `pop-in 150ms ${EASE} both`, transformOrigin: "bottom left" }}>
      <div
        ref={panel}
        className="relative flex flex-col rounded-xl bg-surface p-1.5 shadow-overlay"
        style={{ width: PANEL_W, maxWidth: `calc(100vw - ${2 * EDGE}px)`, maxHeight: `min(${CARD_MAX}px, calc(var(--kb-h) * 0.5))` }}
      >
        {rows.length > 0 ? (
          <div ref={list} role="listbox" aria-label={t("chat-slashpalette.label")} onScroll={remeasure} className="relative min-h-0 overflow-y-auto overscroll-contain">
            <span aria-hidden className="pointer-events-none absolute inset-x-0 rounded-control bg-hover" style={{ top: glide?.top ?? 0, height: glide?.height ?? 0, opacity: glide ? 1 : 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}` }} />
            {rows.map((c, i) => (
              <button
                key={c.key ?? c.word}
                ref={(el) => {
                  refs.current[i] = el;
                }}
                type="button"
                role="option"
                aria-selected={i === active}
                aria-describedby={described(c) ? `${id}-${i}` : undefined}
                // the box keeps the focus; a click picks
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => onHover(i)}
                onClick={() => onPick(c)}
                className={cx(
                  "relative flex h-9 w-full items-center justify-between gap-3 rounded-control px-2.5 text-left text-[14px] leading-[1.43] text-ink [@media(pointer:coarse)]:h-11",
                  i === active ? "font-medium" : "font-normal",
                )}
              >
                <span className="min-w-0 truncate">{name(c)}</span>
                {c.source && <span className="max-w-[50%] shrink-0 truncate text-[12px] font-normal text-ink-3">{c.source}</span>}
              </button>
            ))}
          </div>
        ) : (
          <div role="status" className="flex h-10 items-center justify-center text-[13px] text-ink-3">
            {t("chat-slashpalette.no_match")}
          </div>
        )}
        {place?.side === "inside" && focused && described(focused) && (
          <div data-slash-card aria-hidden className="mt-1.5 shrink-0 border-t border-line px-2.5 pt-2 pb-1 text-[13px] leading-[1.5]">
            <Description c={focused} />
          </div>
        )}
        {/* each row's description for assistive technology; the card and the line above only show the focused one */}
        <div hidden>
          {rows.map((c, i) => described(c) && <Description key={c.key ?? c.word} c={c} id={`${id}-${i}`} />)}
        </div>
      </div>
      {focused && place && place.side !== "inside" && (
        <div
          ref={card}
          data-slash-card
          aria-hidden
          onMouseDown={(e) => e.preventDefault()}
          className="absolute overflow-y-auto rounded-xl bg-surface p-3.5 shadow-overlay"
          style={{ width: CARD_W, left: place.side === "right" ? PANEL_W + GAP : -(GAP + CARD_W), top: place.top, maxHeight: place.maxHeight }}
        >
          <div className="truncate text-[14px] font-medium text-ink">{name(focused)}</div>
          <Description c={focused} className="mt-2 text-[13px] leading-[1.5]" />
        </div>
      )}
    </div>
  );
}
