import { useState } from "react";
import { t } from "../../i18n";
import { ago } from "../../lib/format";
import { checkCount, reportText, reviewBadge, type ReviewTone } from "../../lib/supervision";
import type { Todo } from "../../lib/todos";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";

// How a supervised task's review stands (#3124, docs/assistant/unified-entry.md "Supervision"): a chip and the count of criteria met
// on its card (a review is no line in the conversation since #8237: its task's row says how it went). The existing
// checklist stays with the lazy to-do detail.

const TONE: Record<ReviewTone, string> = {
  accent: "bg-field text-accent-ink",
  red: "bg-field text-red",
  orange: "bg-field text-orange",
  green: "bg-green-tint text-green",
  muted: "bg-field text-ink-2",
};

/** The review's chip, with the criteria met; nothing for a task that is not supervised. */
export function ReviewBadge({ todo, className }: { todo: Todo; className?: string }) {
  const badge = reviewBadge(todo);
  if (!badge) return null;
  const count = checkCount(todo.supervision);
  return (
    <span data-testid="review-badge" className={cx("inline-flex h-5 shrink-0 items-center gap-1 whitespace-nowrap rounded-md px-1.5 text-[11.5px] font-semibold", TONE[badge.tone], className)}>
      <Icon name="target" size={11} stroke={2.2} />
      {badge.label}
      {count && <span className="font-medium tabular-nums opacity-80">· {count}</span>}
    </span>
  );
}

/** What the task's agent last reported to Persona, as it wrote it (#3181): the Progress row shows the hub's paraphrase,
 *  this is the original, Markdown rendered. A section on the to-do's page; `banner`: over its agent's chat in the task's
 *  details, folding to its title line. Nothing before its first report (or on an older hub). */
export function LatestReport({ todo, banner }: { todo: Todo; banner?: boolean }) {
  const [open, setOpen] = useState(true);
  const text = reportText(todo.supervision);
  if (!text) return null;
  const when = ago(todo.supervision?.progress?.at);
  const body = (
    <div className={cx("min-w-0 select-text text-ink [overflow-wrap:anywhere]", banner ? "mt-1.5 max-h-[40vh] overflow-y-auto overscroll-contain" : "mt-2")}>
      <Markdown text={text} small />
    </div>
  );
  if (banner) {
    return (
      <section data-testid="latest-report" aria-label={t("assistant-tasks.report_latest")} className="border-b border-line bg-inset px-4 py-2.5">
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="touch-hit flex w-full min-w-0 items-center gap-2 text-left text-[12.5px] text-ink-2 hover:text-ink">
          <Icon name="chat" size={12} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate font-semibold">{t("assistant-tasks.report_latest")}</span>
          {when && <span className="shrink-0 text-[12px] text-ink-3">{when}</span>}
          <Icon name="chevronRight" size={12} className="shrink-0 text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(90deg)" : undefined }} />
        </button>
        {open && body}
      </section>
    );
  }
  return (
    <section data-testid="latest-report" className="mt-4 rounded-control bg-inset p-3.5 shadow-hairline">
      <div className="flex items-center gap-2 text-[12.5px] text-ink-2">
        <Icon name="chat" size={12} className="shrink-0" />
        <h3 className="min-w-0 flex-1 truncate font-semibold">{t("assistant-tasks.report_latest")}</h3>
        {when && <span className="shrink-0 text-[12px] text-ink-3">{when}</span>}
      </div>
      {body}
    </section>
  );
}
