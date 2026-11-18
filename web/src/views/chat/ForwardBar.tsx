// Forwarded messages over a box (#3899; iOS QuoteBarRow with the forward arrow and route): the quote bar's shape
// (Quote.tsx QuoteBar), the arrow before "Forwarded from Claude · chat", its first words, and the line saying which
// computer it comes from and where it goes. Several of one chat forwarded at once (#3838) are one bar
// (ForwardGroup.tsx, its own chunk). Over a chat's composer (ForwardCard.tsx ForwardBars), New agent's first message
// and Forward to's preview. No store or router here: New agent takes it alone.
import { t } from "../../i18n";
import { forwardHeading, forwardSummary } from "../../lib/forward";
import type { ContextRef } from "../../lib/links";
import { cx, EASE } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export type Forward = Pick<ContextRef, "title" | "text" | "attrs" | "route">;

/** One forward over a box: the quote bar's shape (Quote.tsx QuoteBar), the arrow, the heading, the words and the route. */
export function ForwardBar({ forward, lines = 2, onRemove }: { forward: Forward; lines?: number; onRemove?: () => void }) {
  const heading = forwardHeading(forward.attrs);
  const summary = forwardSummary(forward.text).replace(/\n/g, " ");
  return (
    <Bar heading={heading} title={`${heading}\n${forward.text}`} route={forward.route} remove={onRemove && { label: t("chat-forward.remove"), run: onRemove }}>
      <span className={cx(lines === 1 ? "line-clamp-1" : "line-clamp-2", "text-[12.5px] leading-[1.5] text-ink-2 [overflow-wrap:anywhere]")}>{summary}</span>
    </Bar>
  );
}

/** The bar's shape, heading, route and ×; ForwardGroup.tsx draws a group in it. */
export function Bar({ heading, title, route, remove, group, children }: { heading: string; title: string; route?: string; remove?: { label: string; run: () => void }; group?: number; children: React.ReactNode }) {
  return (
    <div
      className="flex min-w-0 items-start gap-2 rounded-[10px] bg-inset py-1.5 pr-1 pl-2 shadow-hairline"
      title={title}
      aria-label={[heading, route].filter(Boolean).join(" · ")}
      role="group"
      style={{ animation: `pop-in 180ms ${EASE} both` }}
      data-forward-bar
      data-forward-group={group}
    >
      <span aria-hidden className="w-[3px] shrink-0 self-stretch rounded-full bg-accent" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 py-px">
        <span className="flex min-w-0 items-center gap-1 text-[11.5px] font-medium whitespace-nowrap text-ink-3">
          <Icon name="forward" size={12} className="shrink-0" />
          <span className="min-w-0 truncate">{heading}</span>
        </span>
        {children}
        {route && (
          <span className="min-w-0 truncate text-[11.5px] text-ink-3" data-forward-route>
            {route}
          </span>
        )}
      </div>
      {remove && (
        <button
          type="button"
          aria-label={remove.label}
          title={remove.label}
          onClick={remove.run}
          className="flex size-6 shrink-0 items-center justify-center rounded-full text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2"
        >
          <Icon name="x" size={12} stroke={2.4} />
        </button>
      )}
    </div>
  );
}
