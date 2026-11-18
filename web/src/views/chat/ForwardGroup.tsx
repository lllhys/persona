// Several forwards at once (#3838; iOS QuoteBar's group row and ForwardGroupBlock): over the box and at the top of a
// bubble, those of one chat next to each other (forwardRuns) are one bar, in ForwardBar's shape, and one card, in
// ForwardBlock's; the others are drawn alone. A chunk of their own, from one import: over the box it comes after Forward
// to, which loads it; New agent loads it when several come; in a bubble, until it is in, a line says how many
// (ForwardCard.tsx ForwardCards).
import { useState } from "react";
import { t } from "../../i18n";
import { forwardMeta } from "../../lib/forward";
import { forwardGroupHeading, forwardLine, forwardRuns } from "../../lib/forward-group";
import { removeContext, type ContextRef } from "../../lib/links";
import type { Context } from "../../lib/turns";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
import { Bar, ForwardBar } from "./ForwardBar";
import { ForwardBlock, openForwarded } from "./ForwardCard";

/** The messages of a group over a box with a line each; the rest are counted ("2 more"). */
const GROUP_LINES = 3;

export function ForwardGroupBar({ forwards, onRemove }: { forwards: readonly Pick<ContextRef, "text" | "attrs" | "route">[]; onRemove?: () => void }) {
  const heading = forwardGroupHeading(forwards);
  const lines = forwards.map(forwardLine);
  const more = lines.length - GROUP_LINES;
  return (
    <Bar heading={heading} title={[heading, ...lines].join("\n")} route={forwards.find((f) => f.route)?.route} remove={onRemove && { label: t("chat-forwardto.remove_group"), run: onRemove }} group={forwards.length}>
      {lines.slice(0, GROUP_LINES).map((l, i) => (
        <span key={i} className="line-clamp-1 text-xs leading-[1.5] text-ink-2 [overflow-wrap:anywhere]">
          {l}
        </span>
      ))}
      {more > 0 && <span className="text-[11.5px] text-ink-3">{t("chat-forwardto.more", { count: more })}</span>}
    </Bar>
  );
}

/** Several forwards over `draftKey`'s box: a bar for each chat's run, × dropping that run. */
export function ForwardGroupBars({ forwards, draftKey, onRemove }: { forwards: ContextRef[]; draftKey: string; onRemove?: () => void }) {
  return forwardRuns(forwards).map((run) => {
    const rs = run.map((i) => forwards[i]);
    const drop = () => {
      rs.forEach((r) => removeContext(draftKey, r.id));
      onRemove?.();
    };
    return rs.length > 1 ? <ForwardGroupBar key={rs[0].id} forwards={rs} onRemove={drop} /> : <ForwardBar key={rs[0].id} forward={rs[0]} lines={1} onRemove={drop} />;
  });
}

/** Several forwards at the top of a bubble, of its `blocks` (another block between two ends a run): a card for each
 *  chat's run. */
export function ForwardGroupCards({ blocks }: { blocks: Context[] }) {
  return forwardRuns(blocks).map((run) => (run.length > 1 ? <ForwardGroupBlock key={run[0]} group={run.map((i) => blocks[i])} /> : <ForwardBlock key={run[0]} c={blocks[run[0]]} />));
}

/** The messages of a group a bubble shows before "2 more" (iOS ForwardGroupBlock). */
const GROUP_SHOWN = 3;

/** Several forwards of one chat at the top of a bubble as one card (#3838; iOS ForwardGroupBlock): the arrow and "3
 *  forwarded messages · chat", then each message on up to two lines ("Claude: Three times…"), a click opening it in its
 *  chat (or its whole text in place, when this page does not have that chat); past GROUP_SHOWN, "2 more" shows the rest. */
export function ForwardGroupBlock({ group }: { group: Context[] }) {
  const [all, setAll] = useState(false);
  const heading = forwardGroupHeading(group);
  const shown = all ? group : group.slice(0, GROUP_SHOWN);
  const more = group.length - shown.length;
  return (
    <div className="flex min-w-0 gap-2 rounded-[8px] bg-ink/[0.05] py-1.5 pr-2.5 pl-2 whitespace-normal" data-forward data-forward-group={group.length}>
      <span aria-hidden className="w-[3px] shrink-0 self-stretch rounded-full bg-ink/20" />
      <div className="flex min-w-0 flex-1 flex-col items-start gap-1">
        <span className="flex max-w-full min-w-0 items-center gap-1 text-xs font-medium whitespace-nowrap text-ink-3">
          <Icon name="forward" size={12} className="shrink-0" />
          <span className="min-w-0 truncate">{heading}</span>
        </span>
        {shown.map((c, i) => (
          <GroupLine key={i} c={c} />
        ))}
        {more > 0 && (
          <button
            type="button"
            onClick={() => setAll(true)}
            className="-ml-1 rounded-[6px] px-1 text-xs font-medium whitespace-nowrap text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2"
          >
            {t("chat-forwardto.more", { count: more })}
          </button>
        )}
      </div>
    </div>
  );
}

/** One message of a group card: its line, a click opening the original (openForwarded) or its whole text here. */
function GroupLine({ c }: { c: Context }) {
  const [open, setOpen] = useState(false);
  const line = forwardLine(c);
  const meta = forwardMeta(c.attrs);
  return (
    <div className="flex w-full min-w-0 flex-col items-start gap-0.5">
      <button
        type="button"
        onClick={() => {
          if (!openForwarded(c)) setOpen((o) => !o);
        }}
        title={[t("chat-forward.open_original"), meta].filter(Boolean).join(" · ")}
        aria-label={[line, meta].filter(Boolean).join(" · ")}
        aria-expanded={open || undefined}
        className="grid w-full min-w-0 grid-cols-[minmax(0,auto)] rounded-[6px] text-left"
      >
        <span className="line-clamp-2 text-xs leading-[1.55] text-ink-2 [overflow-wrap:anywhere]">{line}</span>
      </button>
      {open && (
        <div className="max-h-[320px] w-full overflow-y-auto text-ink-2 [overflow-wrap:anywhere]" data-scroll-box>
          <Markdown text={c.text} small compact />
        </div>
      )}
    </div>
  );
}
