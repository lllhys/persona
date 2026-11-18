// The words of an ⓘ (InfoTip), loaded the first time one is asked for: a dark tip under it (above it when there is no
// room), drawn over the page so a list that clips or scrolls never cuts it off. A press elsewhere, Escape or a scroll
// puts it away.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { placeMenu } from "./overlay";
import { TOOLTIP_SURFACE } from "./tooltip-surface";

/** The tip's width at most, in px. */
const TIP_W = 260;

export default function InfoTipPanel({ id, anchor, text, onClose }: { id: string; anchor: React.RefObject<HTMLElement | null>; text: string; onClose: () => void }) {
  const tip = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  const close = useRef(onClose);
  useLayoutEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const down = (e: PointerEvent) => {
      const at = e.target as Node;
      if (!anchor.current?.contains(at) && !tip.current?.contains(at)) close.current();
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    // the list scrolling under it would leave the tip behind
    const away = () => close.current();
    addEventListener("pointerdown", down, true);
    addEventListener("keydown", key);
    addEventListener("scroll", away, true);
    return () => {
      removeEventListener("pointerdown", down, true);
      removeEventListener("keydown", key);
      removeEventListener("scroll", away, true);
    };
  }, [anchor]);
  useLayoutEffect(() => {
    if (!anchor.current || !tip.current) return;
    const { left, top } = placeMenu(anchor.current.getBoundingClientRect(), tip.current.offsetHeight, { width: TIP_W, align: "left", side: "bottom", screenW: innerWidth, screenH: innerHeight, gap: 4 });
    setPlace({ left: Math.max(8, Math.min(left - 8, innerWidth - tip.current.offsetWidth - 8)), top });
  }, [anchor, text]);
  return createPortal(
    <div
      ref={tip}
      id={id}
      role="tooltip"
      style={{ left: place?.left ?? -9999, top: place?.top ?? -9999, maxWidth: TIP_W }}
      className={TOOLTIP_SURFACE}
    >
      {text}
    </div>,
    document.body,
  );
}
