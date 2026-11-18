// An ⓘ beside a title that says, in a sentence or two, what the thing it names holds (#6458; docs/design/DESIGN_SYSTEM.md
// 3.8 "Info tips"). The words stay out of the list until asked for: the pointer resting on the ⓘ, keyboard focus or a
// tap shows them (InfoTipPanel, loaded then: the sidebar's first screen carries only the button); a tap or a click
// keeps them until a press elsewhere, Escape or another tap. The ⓘ names them for a screen reader (aria-describedby
// while shown).
import { lazy, Suspense, useCallback, useEffect, useId, useRef, useState } from "react";
import { t } from "../i18n";
import { cx } from "./atoms";
import { Icon } from "./icons";

const loadPanel = () => import("./InfoTipPanel");
const InfoTipPanel = lazy(loadPanel);

/** How long the pointer rests on the ⓘ before the tip shows, so a pointer passing over the list doesn't flash it. */
const HOVER_MS = 250;

export function InfoTip({ name, text, className }: { name: string; text: string; className?: string }) {
  const [hover, setHover] = useState(false);
  const [pinned, setPinned] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const id = useId();
  const open = hover || pinned;
  useEffect(() => () => clearTimeout(timer.current), []);
  const close = useCallback(() => {
    setPinned(false);
    setHover(false);
  }, []);
  const rest = (on: boolean) => {
    clearTimeout(timer.current);
    if (!on) return setHover(false);
    void loadPanel();
    timer.current = setTimeout(() => setHover(true), HOVER_MS);
  };
  return (
    <>
      <button
        ref={button}
        type="button"
        aria-label={t("ui-atoms.about", { name })}
        // no aria-expanded: the sidebar reads that on a group heading's buttons as the group's fold (GroupFoldButton)
        data-pinned={pinned || undefined}
        aria-describedby={open ? id : undefined}
        onPointerEnter={(e) => e.pointerType === "mouse" && rest(true)}
        onPointerLeave={(e) => e.pointerType === "mouse" && rest(false)}
        onFocus={(e) => e.currentTarget.matches(":focus-visible") && setHover(true)}
        onBlur={() => rest(false)}
        onClick={() => {
          clearTimeout(timer.current);
          setPinned((p) => !p);
          setHover(false);
        }}
        className={cx("touch-hit relative z-10 flex size-4 shrink-0 items-center justify-center rounded-full text-ink-3 transition-colors hover:text-ink-2 data-pinned:text-ink-2", className)}
      >
        <Icon name="info" size={11} stroke={2} />
      </button>
      {open && (
        <Suspense fallback={null}>
          <InfoTipPanel id={id} anchor={button} text={text} onClose={close} />
        </Suspense>
      )}
    </>
  );
}
