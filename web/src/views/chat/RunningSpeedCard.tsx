// The running bar's speed (cindy's RunningTokenRatePopover; the owner asked on 2026-11-12 that the bar follow cindy,
// #5312): the speed history of the conversation (lib/token-rate.ts), the latest speed in place of the tokens once one
// is measured, and the time and speed as a button whose card shows the latest speed large, a sparkline of the samples,
// then the turn's average, its output and its peak. The card opens above the button, its right edge on the button's,
// on hover as a tooltip or pinned by a click with a ✕. RunningStatusBar.tsx loads this once an Agent's turn runs, off
// the chat's first screen.
import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { formatRate, shownRate, turnAverage, useRateHistory, type RateHistory, type RateSample } from "../../lib/token-rate";
import type { SessionLive } from "../../lib/types";
import { hasFinePointer } from "../../lib/viewport";
import { compactTokens } from "../../lib/tokens";
import { EASE, EXIT_MS, useExit } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { useEscapeClose } from "../../ui/overlay";

type Reveal = "idle" | "hover" | "pinned" | "dismissed";
/** cindy's tooltip delay before a hovered card shows, and the moment the pointer has to cross to the card. */
const HOVER_MS = 500;
const LEAVE_MS = 120;

export interface SpeedCardData {
  rate: number | null;
  average: number | null;
  output: number;
  history: RateHistory;
}

/** cindy's card: 320 wide (less the screen's margins), 8 above the button. */
const CARD_WIDTH = 320;
const GAP = 8;
const SCREEN_MARGIN = 16;

/** Where the card goes: its right edge on the button's, its bottom a gap above it, kept on screen. Drawn on the body, as
 *  what holds the bar may clip it; placed again when the window, the page or the dock under it moves. */
function usePlace(shown: boolean, anchor: RefObject<HTMLElement | null>): { right: number; bottom: number } | null {
  const [place, setPlace] = useState<{ right: number; bottom: number } | null>(null);
  useLayoutEffect(() => {
    const el = anchor.current;
    if (!shown || !el) return;
    const measure = () => {
      const r = el.getBoundingClientRect();
      const screen = document.documentElement.clientWidth || innerWidth;
      const width = Math.min(CARD_WIDTH, screen - 2 * SCREEN_MARGIN);
      // the right edge as far left as the card needs to stay on screen
      const right = Math.min(Math.max(r.right, SCREEN_MARGIN + width), screen - SCREEN_MARGIN);
      setPlace({ right: screen - right, bottom: innerHeight - r.top + GAP });
    };
    measure();
    const vv = window.visualViewport;
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    ro?.observe(el.closest("[data-chat-dock]") ?? el.parentElement ?? el);
    addEventListener("resize", measure);
    addEventListener("scroll", measure, { capture: true, passive: true });
    vv?.addEventListener("resize", measure);
    return () => {
      ro?.disconnect();
      removeEventListener("resize", measure);
      removeEventListener("scroll", measure, { capture: true });
      vv?.removeEventListener("resize", measure);
    };
  }, [shown, anchor]);
  return shown ? place : null;
}

/** cindy's sparkline, 128 × 48: the samples across the measured generation time, scaled to the highest. */
export function Sparkline({ samples }: { samples: RateSample[] }) {
  const first = samples[0]?.durationMs ?? 0;
  const span = (samples.at(-1)?.durationMs ?? 0) - first;
  const ceiling = Math.max(1, ...samples.map((s) => s.rate));
  const points = samples.map((s) => ({ x: span > 0 ? 4 + ((s.durationMs - first) / span) * 108 : 112, y: 44 - (s.rate / ceiling) * 36 }));
  const line = points.map((p, i) => `${i ? "L" : "M"}${p.x},${p.y}`).join(" ");
  const last = points.at(-1);
  return (
    <svg viewBox="0 0 120 48" className="h-12 w-32 shrink-0 self-center text-ink" role="img" aria-label={t("chat-runningspeed.history")} data-speed-chart={samples.length}>
      <path d="M4 44H112" stroke="currentColor" opacity="0.12" />
      {points.length > 1 && (
        <>
          <path d={`${line} L112,44 L${points[0].x},44 Z`} fill="currentColor" opacity="0.08" />
          <path d={line} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
        </>
      )}
      {last && <circle cx={last.x} cy={last.y} r="2.5" fill="currentColor" />}
    </svg>
  );
}

const rateText = (rate: number) => t("chat-runningspeedcard.token_rate", { rate: formatRate(rate) });

export function RunningSpeedCard({ id, anchor, closeRef, open, pinned, data, onClose, onPointerEnter, onPointerLeave }: {
  id: string;
  anchor: RefObject<HTMLButtonElement | null>;
  closeRef: RefObject<HTMLButtonElement | null>;
  open: boolean;
  pinned: boolean;
  data: SpeedCardData;
  onClose: () => void;
  onPointerEnter: () => void;
  onPointerLeave: (e: React.PointerEvent) => void;
}) {
  const [card, setCard] = useState<HTMLDivElement | null>(null);
  const exit = useExit(open, EXIT_MS.card, () => [card?.animate([{ opacity: 1 }, { opacity: 0 }], { duration: EXIT_MS.card, easing: "ease-in", fill: "forwards" })]);
  const place = usePlace(exit.mounted, anchor);
  if (!exit.mounted || !place) return null;
  const { history, rate, average, output } = data;
  const peak = history.peak > 0 ? rateText(history.peak) : "—";
  return createPortal(
    <div
      ref={setCard}
      id={id}
      role={pinned ? "dialog" : "tooltip"}
      aria-label={pinned ? t("chat-runningspeed.history") : undefined}
      aria-description={t("chat-runningspeed.description")}
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
      className="fixed z-30 rounded-xl border border-line bg-surface px-4 py-3 text-ink shadow-overlay"
      style={{ right: place.right, bottom: place.bottom, width: CARD_WIDTH, maxWidth: `calc(100vw - ${2 * SCREEN_MARGIN}px)`, animation: `pop-in 160ms ${EASE} both`, transformOrigin: "bottom right" } as CSSProperties}
      data-speed-card={exit.leaving ? "leaving" : pinned ? "pinned" : "hover"}
    >
      {pinned && (
        <button
          ref={closeRef}
          type="button"
          onClick={onClose}
          onKeyDown={(e) => {
            // back to the button the card belongs to; going forward the browser moves on from it
            if (e.key !== "Tab") return;
            anchor.current?.focus();
            if (e.shiftKey) e.preventDefault();
          }}
          aria-label={t("chat-runningspeed.close")}
          title={t("chat-runningspeed.close")}
          className="absolute top-2 right-2 inline-flex size-6 cursor-pointer items-center justify-center rounded-full text-ink-2 transition-colors hover:bg-hover hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent pointer-coarse:before:absolute pointer-coarse:before:-inset-2.5 pointer-coarse:before:content-['']"
          data-speed-close
        >
          <Icon name="x" size={12} stroke={2} />
        </button>
      )}
      <div className={`grid grid-cols-[minmax(0,1fr)_128px] gap-3 ${pinned ? "pr-6" : "pr-1"}`}>
        <div className="min-w-0 self-center">
          <div className="mb-1 flex items-center gap-1.5 text-[12px] text-ink-2">
            <Icon name="activity" size={14} className="shrink-0" />
            <span className="truncate">{t("chat-runningspeed.latest")}</span>
          </div>
          <div className="flex items-baseline gap-1.5 tabular-nums">
            <span className="text-[28px] leading-none font-medium" data-speed-latest>{rate !== null ? formatRate(rate) : "—"}</span>
            <span className="text-[12px] text-ink-2">{t("chat-runningspeed.unit")}</span>
          </div>
        </div>
        <Sparkline samples={history.samples} />
        <dl className="col-span-2 grid grid-cols-3 gap-2 text-[12px] tabular-nums">
          <div className="flex min-w-0 flex-col items-start gap-1">
            <dt className="text-ink-2">{t("chat-runningspeed.average")}</dt>
            <dd className="font-medium" data-speed-average>{average !== null ? rateText(average) : "—"}</dd>
          </div>
          <div className="flex min-w-0 flex-col items-start gap-1">
            <dt className="text-ink-2">{t("chat-runningspeed.output")}</dt>
            <dd className="font-medium" data-speed-output>{t("chat-runningstatusbar.token_count", { tokens: compactTokens(output) })}</dd>
          </div>
          <div className="flex min-w-0 flex-col items-start gap-1">
            <dt className="text-ink-2">{t("chat-runningspeed.peak")}</dt>
            <dd className="font-medium" data-speed-peak>{peak}</dd>
          </div>
        </dl>
      </div>
    </div>,
    document.body,
  );
}

/** The time and speed as a button (cindy's RunningTokenRatePopover trigger): hovering shows the speed history card, a
 *  click pins it open with a ✕, and ✕, Esc or a second click close it; while pinned, clicks elsewhere leave it (cindy).
 *  Closed by hand, it does not open again on the same hover. `onPinned` tells the bar, which keeps a pinned card's button
 *  after the turn; `onEngaged`, whether the pointer is on it or its card, it has the focus or it is pinned, so it stays
 *  while a call pauses the speed. */
function SpeedButton({ elapsed, card, onPinned, onEngaged, children }: { elapsed: string; card: SpeedCardData; onPinned: (pinned: boolean) => void; onEngaged: (engaged: boolean) => void; children: ReactNode }) {
  const button = useRef<HTMLButtonElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [mode, setMode] = useState<Reveal>("idle");
  const [over, setOver] = useState(false);
  const [focused, setFocused] = useState(false);
  const hoverTimer = useRef<number | undefined>(undefined);
  const leaveTimer = useRef<number | undefined>(undefined);
  const pinned = mode === "pinned";
  const revealed = pinned || mode === "hover";
  const engaged = pinned || over || focused;
  useEffect(() => {
    if (!pinned) return;
    onPinned(true);
    return () => onPinned(false);
  }, [pinned, onPinned]);
  useEffect(() => {
    if (!engaged) return;
    onEngaged(true);
    return () => onEngaged(false);
  }, [engaged, onEngaged]);
  useEffect(() => () => {
    clearTimeout(hoverTimer.current);
    clearTimeout(leaveTimer.current);
  }, []);
  // pinned it is a dialog: Esc closes it first, and the chat's Esc, which stops the turn, leaves it be (ChatView)
  useEscapeClose(pinned, () => setMode("dismissed"));
  const enter = (e: React.PointerEvent) => {
    if (e.pointerType !== "mouse") return;
    clearTimeout(leaveTimer.current);
    setOver(true);
    setMode((m) => (m === "dismissed" ? "idle" : m));
    clearTimeout(hoverTimer.current);
    hoverTimer.current = window.setTimeout(() => setMode((m) => (m === "idle" ? "hover" : m)), HOVER_MS);
  };
  // the pointer has a moment to cross from the button to the card
  const leave = (e: React.PointerEvent) => {
    if (e.pointerType !== "mouse") return;
    clearTimeout(hoverTimer.current);
    clearTimeout(leaveTimer.current);
    leaveTimer.current = window.setTimeout(() => {
      setOver(false);
      setMode((m) => (m === "hover" ? "idle" : m));
    }, LEAVE_MS);
  };
  const id = useId();
  const label = card.rate !== null
    ? `${elapsed} · ${t("chat-runningspeed.latest")}: ${rateText(card.rate)}`
    : card.output ? `${elapsed} · ${t("chat-runningstatusbar.token_count", { tokens: compactTokens(card.output) })}` : elapsed;
  return (
    <>
      <button
        ref={button}
        type="button"
        onPointerEnter={enter}
        onPointerLeave={leave}
        onFocus={(e) => {
          setFocused(true);
          if (e.currentTarget.matches(":focus-visible")) setMode((m) => (m === "idle" ? "hover" : m));
        }}
        onBlur={() => {
          setFocused(false);
          setMode((m) => (m === "hover" || m === "dismissed" ? "idle" : m));
        }}
        onClick={() => {
          clearTimeout(hoverTimer.current);
          setMode((m) => (m === "pinned" ? "dismissed" : "pinned"));
        }}
        onKeyDown={(e) => {
          // the card is drawn on the body: Tab goes from here to its ✕, as if it came right after the button
          if (e.key === "Tab" && !e.shiftKey && pinned && closeButton.current) {
            e.preventDefault();
            closeButton.current.focus();
          }
        }}
        aria-label={label}
        aria-expanded={revealed}
        aria-controls={revealed ? id : undefined}
        className="touch-hit inline-flex min-h-6 min-w-6 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-full px-1 tabular-nums transition-colors hover:bg-hover hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        data-running-speed={mode}
      >
        {children}
      </button>
      <RunningSpeedCard
        id={id}
        anchor={button}
        closeRef={closeButton}
        open={revealed}
        pinned={pinned}
        data={card}
        onClose={() => {
          setMode("dismissed");
          const own = button.current;
          const composer = own?.closest("[data-chat-dock]")?.querySelector("textarea");
          own?.focus({ preventScroll: true });
          // after the turn the button goes with the card: the keyboard goes back to the composer, not on a touch screen
          // alone (that would raise the keyboard)
          window.setTimeout(() => {
            if (own && !own.isConnected && (!document.activeElement || document.activeElement === document.body) && hasFinePointer()) composer?.focus({ preventScroll: true });
          });
        }}
        onPointerEnter={() => clearTimeout(leaveTimer.current)}
        onPointerLeave={leave}
      />
    </>
  );
}

/** The bar's right side in an Agent session once this is loaded: the time, then the latest speed while the turn runs, no
 *  call pauses it and there is a fresh one, else the tokens (`count`), never both (cindy). With a speed the two are a
 *  button, and it stays one while it is pointed at, focused or pinned (its card says "—" then); otherwise the tokens
 *  stand without one. Fed with each status line while the turn runs (`working`) and the hub is in reach. */
export function RunningSpeed({ speedKey, turn, live, working, paused, offline, now, elapsed, time, count, onPinned }: {
  speedKey: string;
  turn: string | undefined;
  live: SessionLive | null;
  working: boolean;
  /** A call is running: the count stands still for it. */
  paused?: boolean;
  offline?: boolean;
  now: number;
  elapsed: string;
  time: ReactNode;
  count: ReactNode;
  onPinned: (pinned: boolean) => void;
}) {
  const history = useRateHistory(speedKey, turn, live?.tokens_down, live?.at, working && !offline, paused);
  // a turn that is over generates nothing: its tokens stand where its speed was
  const rate = working && !offline ? shownRate(history, now, paused) : null;
  const [engaged, setEngaged] = useState(false);
  const speed = rate !== null ? <span data-running-rate>{rateText(rate)}</span> : count;
  const shown = (
    <>
      {time}
      {speed && <span aria-hidden>·</span>}
      {speed}
    </>
  );
  // the hub out of reach: no speed, and a pinned card closes (cindy)
  if (offline || (rate === null && !engaged)) return shown;
  return (
    <SpeedButton elapsed={elapsed} onPinned={onPinned} onEngaged={setEngaged} card={{ rate, average: turnAverage(history), output: live?.tokens_down ?? 0, history }}>
      {shown}
    </SpeedButton>
  );
}
