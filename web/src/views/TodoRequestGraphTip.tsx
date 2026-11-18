// The requests graph's hover card (views/TodoRequestGraph.tsx): what a piece of a request has to say (who is on it, what
// it waits for) is more than the ⓘ tip (ui/InfoTip.tsx) holds, which is one sentence under its own button. The card is
// drawn in the same tooltip colors and place (placeMenu). It comes up when the pointer has rested on a piece for
// TIP_DELAY, or when keyboard focus lands on one; it goes when the pointer leaves, the focus does, Escape is pressed, the
// page scrolls or a press lands. It takes no pointer and no focus (pointer-events: none), so it never stands between the
// pointer and what it is going for, and it opens above the anchor when there is no room below.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { placeMenu } from "../ui/overlay";
import { TOOLTIP_SURFACE } from "../ui/tooltip-surface";

/** How long the pointer rests before the card shows, so a pointer passing over the lanes does not flash it. */
export const TIP_DELAY = 150;
/** The card's width at most, in px. */
const TIP_W = 360;

export interface TipApi {
  /** Show `content` by `anchor` after `delay` ms (0: at once). A card already up is replaced by the new one. */
  show: (anchor: Element, content: () => ReactNode, delay?: number) => void;
  /** Take the card away now, or only when it is the one `anchor` asked for. */
  hide: (anchor?: Element) => void;
}

/** The card and the api that raises it: render `layer` once, hand `api` to whatever has something to say. */
export function useTips(): { api: TipApi; layer: ReactNode } {
  const [shown, setShown] = useState<{ anchor: Element; content: () => ReactNode } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const current = useRef<Element | null>(null);
  const pending = useRef<Element | null>(null);
  useEffect(() => () => clearTimeout(timer.current), []);
  const api = useMemo<TipApi>(() => ({
    show(anchor, content, delay = TIP_DELAY) {
      clearTimeout(timer.current);
      pending.current = anchor;
      const raise = () => {
        pending.current = null;
        current.current = anchor;
        setShown({ anchor, content });
      };
      if (delay <= 0) raise();
      else timer.current = setTimeout(raise, delay);
    },
    hide(anchor) {
      // a card asked for by another anchor stays: the pointer left one lane as it entered the next
      if (anchor && current.current !== anchor && pending.current !== anchor) return;
      clearTimeout(timer.current);
      pending.current = null;
      current.current = null;
      setShown(null);
    },
  }), []);
  const hide = useCallback(() => api.hide(), [api]);
  const layer = shown ? <TipCard key="tip" anchor={shown.anchor} content={shown.content} onClose={hide} /> : null;
  return { api, layer };
}

/** The handlers that make an element the anchor of a card: the mouse resting on it, keyboard focus (not a click's), and
 *  both going away. Spread them on an HTML or SVG element. */
export function tipHandlers(api: TipApi, content: () => ReactNode) {
  return {
    onPointerEnter: (e: React.PointerEvent<Element>) => {
      if (e.pointerType === "mouse" || e.pointerType === "pen" || !e.pointerType) api.show(e.currentTarget, content);
    },
    onPointerLeave: (e: React.PointerEvent<Element>) => api.hide(e.currentTarget),
    onPointerDown: () => api.hide(),
    onFocus: (e: React.FocusEvent<Element>) => {
      let visible = true;
      try {
        visible = e.currentTarget.matches(":focus-visible");
      } catch {
        // no such selector here: take the focus as the keyboard's
      }
      if (visible) api.show(e.currentTarget, content, 0);
    },
    onBlur: (e: React.FocusEvent<Element>) => api.hide(e.currentTarget),
  };
}

function TipCard({ anchor, content, onClose }: { anchor: Element; content: () => ReactNode; onClose: () => void }) {
  const card = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  const close = useRef(onClose);
  useLayoutEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    // the list scrolling under it would leave the card behind
    const away = () => close.current();
    addEventListener("keydown", key);
    addEventListener("scroll", away, true);
    addEventListener("resize", away);
    return () => {
      removeEventListener("keydown", key);
      removeEventListener("scroll", away, true);
      removeEventListener("resize", away);
    };
  }, []);
  useLayoutEffect(() => {
    const el = card.current;
    if (!el) return;
    if (!anchor.isConnected) return close.current();
    const r = anchor.getBoundingClientRect();
    const { left, top } = placeMenu(r, el.offsetHeight, { width: TIP_W, align: "left", side: "bottom", screenW: innerWidth, screenH: innerHeight, gap: 4 });
    setPlace({ left: Math.max(8, Math.min(left, innerWidth - el.offsetWidth - 8)), top });
  }, [anchor, content]);
  return createPortal(
    <div
      ref={card}
      role="tooltip"
      data-graph-tip=""
      style={{ left: place?.left ?? -9999, top: place?.top ?? -9999, maxWidth: TIP_W }}
      className={`pointer-events-none space-y-1.5 ${TOOLTIP_SURFACE}`}
    >
      {content()}
    </div>,
    document.body,
  );
}
