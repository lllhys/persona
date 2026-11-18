// An Agent's context meter (#2430, moved from the header to the usage row under the composer in #5056): shows how much is used,
// with the tooltip / tap popover explaining what fills and what to do near the limit. "Show details in Agent rows" does not
// gate it. The model is named by the composer's model pill, not repeated here. Nothing, including no empty usage row,
// while the window is unknown (no reply yet, a model the node doesn't know, an old node).
import { useEffect } from "react";
import { contextHint, contextLabel, contextNotes, contextTier, contextTooltip, contextUsage, type ContextTier } from "../../lib/context-usage";
import { cx, EASE, Menu, useMenu } from "../../ui/atoms";
import type { Session } from "../../lib/types";

const TIER_BAR: Record<ContextTier, string> = { ok: "bg-accent", warn: "bg-orange", danger: "bg-orange" };
const TIER_TEXT: Record<ContextTier, string> = { ok: "", warn: "font-medium text-orange", danger: "font-medium text-orange" };

export function ContextMeter({ session, compact = false }: { session: Session; compact?: boolean }) {
  const info = useMenu();
  const usage = contextUsage(session);
  const known = usage !== null;
  // the window went unknown (a /clear) while it was open: close it, or it would come back by itself on the next reply
  useEffect(() => {
    if (!known) info.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only a change of `known` closes it
  }, [known]);
  if (!usage) return null;
  const tier = contextTier(usage);
  const tip = contextTooltip(usage);
  const title = [tip, contextHint(usage)].join("\n");
  return (
    <>
      <button
        type="button"
        onClick={info.open}
        aria-haspopup="dialog"
        aria-expanded={Boolean(info.anchor)}
        aria-label={tip}
        aria-description={contextHint(usage)}
        title={info.anchor ? undefined : title}
        className={cx("touch-hit -mx-1.5 flex shrink-0 items-center gap-1.5 rounded-chip px-1.5 text-submeta whitespace-nowrap text-ink-3 transition-colors", info.anchor && "bg-hover")}
      >
        {!compact && (
          <span
            className="block h-1 w-10 overflow-hidden rounded-full bg-hover-2"
            role="progressbar"
            aria-label={tip}
            aria-valuenow={usage.used}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <span className={cx("block h-full rounded-full motion-safe:transition-[width] motion-safe:duration-300", TIER_BAR[tier])} style={{ width: `${usage.used}%`, transitionTimingFunction: EASE }} />
          </span>
        )}
        <span className={cx("tabular-nums", TIER_TEXT[tier])}>{contextLabel(usage)}</span>
      </button>
      {info.anchor && <Menu anchor={info.anchor} onClose={info.close} align="right" width={300} items={contextNotes(usage)} />}
    </>
  );
}
