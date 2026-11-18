// Skill-aware / palette and its loading/error view. This module loads on / intent, never with the idle command palette.
// Command-only markup mirrors SlashPalette.tsx; SlashPalette.test.tsx compares both views.
import { Suspense, useEffect, useId, useLayoutEffect, useReducer, useRef, useState, type ComponentProps, type ReactNode } from "react";
import type { ComposerCommand as Command } from "../../lib/extension-prompts";
import { cx, EASE } from "../../ui/atoms";
import { t } from "../../i18n";
import { lazyPage } from "../../lib/lazy-page";
import { OverlayBoundary } from "../LazyOverlay";
import type { MentionRow, MentionSource } from "./ComposerPanel";

const PANEL_W = 320;
const CARD_W = 280;
/** Between the panel and the card. */
const GAP = 8;
/** Kept from the window's edges. */
const EDGE = 8;
const CARD_MAX = 400;

/** Beside the panel: its top and height in the palette; or in the panel, under the rows. */
type Place = { side: "right" | "left" | "inside"; top?: number; maxHeight?: number };

const samePlace = (a: Place | null, b: Place) => a?.side === b.side && a.top === b.top && a.maxHeight === b.maxHeight;

/** A command as its row names it: without the "/" already typed. */
const commandName = (c: Command) => c.word.replace(/^\//, "");
/** What picking a command types, when that is more than the command itself (Persona's text commands, quick prompts). */
const typed = (c: Command) => (c.text.trim() === c.word ? "" : c.text.trim());
const keepFocus = (e: React.SyntheticEvent) => e.preventDefault();
const SlashSkillPalette = lazyPage(() => import("./SlashSkillPalette").then((m) => m.SlashSkillPalette));

function Description({ c, id, className }: { c: Command; id?: string; className?: string }) {
  const text = typed(c);
  return (
    <div id={id} className={className}>
      {c.desc && <p className="text-ink-2">{c.desc}</p>}
      {text && <p className={`line-clamp-4 whitespace-pre-line text-ink-3${c.desc ? " mt-1" : ""}`}>{t("chat-slashpalette.types", { text })}</p>}
    </div>
  );
}

export type SlashPaletteInput = {
  /** Commands already ranked by the composer, with the existing whole-list cap applied first. */
  rows: Command[];
  active: number;
  onHover: (i: number) => void;
  onPick: (c: Command | MentionRow) => void;
  /** A press outside `within` (the composer) closes it. */
  onClose: () => void;
  /** What follows the slash; the catalog is read once for this opening, not for each query. */
  query?: string | null;
  source?: MentionSource;
  machineId?: string;
  within: React.RefObject<HTMLElement | null>;
  /** The skill stage's chunk or catalog state; loading also suppresses a premature empty message. */
  status?: "loading" | "error" | "partial";
};

export type SlashSkillViewProps = SlashPaletteInput & {
  /** Display rows; `rows` remains the command-filter generation used to restore scroll. */
  entries?: Command[];
  renderRow?: (row: Command, common: ComponentProps<"button">) => ReactNode;
};

/** The reliable intent-loaded entry: commands keep their view while the catalog stage loads or fails. */
export function SlashSkillEntry(props: SlashPaletteInput) {
  return (
    <OverlayBoundary open onClose={props.onClose} fallback={<SlashSkillView {...props} status="error" />}>
      <Suspense fallback={<SlashSkillView {...props} status="loading" />}>
        <SlashSkillPalette {...props} />
      </Suspense>
    </OverlayBoundary>
  );
}

export function SlashSkillView({ rows, entries = rows, active, onHover, onPick, onClose, within, source, status, renderRow }: SlashSkillViewProps) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const glide = useRef<HTMLSpanElement>(null);
  const [place, setPlace] = useState<Place | null>(null);
  // the list scrolled or the window changed size: measure again
  const [, remeasure] = useReducer((n: number) => n + 1, 0);
  const current = Math.min(active, entries.length - 1);
  const focused = entries[current];

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
    const row = refs.current[current];
    if (!l || !row) return;
    if (row.offsetTop < l.scrollTop) l.scrollTop = row.offsetTop;
    else if (row.offsetTop + row.offsetHeight > l.scrollTop + l.clientHeight) l.scrollTop = row.offsetTop + row.offsetHeight - l.clientHeight;
  }, [current, rows]);

  // After every render, since the rows, the focus, the list's scroll and the card's own height all move the card; a
  // place that did not change renders nothing more.
  useLayoutEffect(() => {
    const r = root.current;
    const p = panel.current;
    const l = list.current;
    if (!r || !p) return;
    const row = refs.current[current];
    const g = glide.current;
    if (g) Object.assign(g.style, { top: `${row?.offsetTop ?? 0}px`, height: `${row?.offsetHeight ?? 0}px`, opacity: row ? 1 : 0 });
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
        {entries.length ? (
          <div ref={list} role="listbox" aria-label={t(source?.skills ? "chat-slashpalette.label_with_skills" : "chat-slashpalette.label")} onScroll={remeasure} className="relative min-h-0 overflow-y-auto overscroll-contain">
            <span ref={glide} aria-hidden className="pointer-events-none absolute inset-x-0 rounded-control bg-hover" style={{ top: 0, height: 0, opacity: 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}` }} />
            {entries.map((c, i) => {
              const hover = () => onHover(i);
              const common: ComponentProps<"button"> = {
                ref: (el: HTMLButtonElement | null) => { refs.current[i] = el; },
                type: "button",
                role: "option",
                "aria-selected": i === current,
                "aria-describedby": described(c) ? `${id}-${i}` : undefined,
                // the box keeps the focus; a click picks
                onMouseDown: keepFocus,
                // Async rows can move any row under a resting pointer; only real movement changes the active row.
                [source?.skills ? "onMouseMove" : "onMouseEnter"]: hover,
                onClick: () => onPick(c),
              };
              return renderRow?.(c, common) ?? (
                <button
                  key={c.key ?? c.word}
                  {...common}
                  className={cx(
                    "relative flex h-9 w-full items-center justify-between gap-3 rounded-control px-2.5 text-left text-sm leading-[1.43] text-ink [@media(pointer:coarse)]:h-11",
                    i === current ? "font-medium" : "font-normal",
                  )}
                >
                  <span className="min-w-0 truncate">{commandName(c)}</span>
                  {c.source && <span className="max-w-[50%] shrink-0 truncate text-xs font-normal text-ink-3">{c.source}</span>}
                </button>
              );
            })}
          </div>
        ) : status !== "loading" ? (
          <div role="status" className="flex h-10 items-center justify-center text-meta text-ink-3">
            {t(source?.skills ? "chat-slashpalette.no_match_skills" : "chat-slashpalette.no_match")}
          </div>
        ) : null}
        {place?.side === "inside" && focused && described(focused) && (
          <div data-slash-card aria-hidden className="mt-1.5 shrink-0 border-t border-line px-2.5 pt-2 pb-1 text-meta leading-[1.5]">
            <Description c={focused} />
          </div>
        )}
        {status && (
          <div role="status" className="mt-1.5 shrink-0 border-t border-line px-[10px] pt-2 pb-1 text-xs text-ink-3">
            {t(`chat-composerpanel.skills_${status}`)}
          </div>
        )}
        {/* each row's description for assistive technology; the card and the line above only show the focused one */}
        <div hidden>
          {entries.map((c, i) => described(c) && <Description key={c.key ?? c.word} c={c} id={`${id}-${i}`} />)}
        </div>
      </div>
      {focused && place && place.side !== "inside" && (
        <div
          ref={card}
          data-slash-card
          aria-hidden
          onMouseDown={keepFocus}
          className="absolute overflow-y-auto rounded-xl bg-surface p-3.5 shadow-overlay"
          style={{ width: CARD_W, left: place.side === "right" ? PANEL_W + GAP : -(GAP + CARD_W), top: place.top, maxHeight: place.maxHeight }}
        >
          <div className="truncate text-sm font-medium text-ink">{commandName(focused)}</div>
          <Description c={focused} className="mt-2 text-meta leading-[1.5]" />
        </div>
      )}
    </div>
  );
}
