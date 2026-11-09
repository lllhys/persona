import { useState } from "react";
import { clickLink, linkHref } from "../../lib/links";
import { useLinkManager } from "../../lib/manager-choice";
import type { Context } from "../../lib/turns";
import { Collapse, cx } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { t } from "../../i18n";

// What a message carries besides what the user wrote (picked page excerpts, everything a Feishu /agent request hands
// its agent): one folded row under the bubble; open, a list of titles; each title opens to its text.

const ICONS: Record<string, IconName> = { instructions: "info", doc: "book", edit: "pencil" };

export function contextLabel(c: Context): string {
  return c.title || c.url || t("chat-contextgroup.quote");
}

export function contextSummary(cs: Context[]): string {
  if (cs.length === 1) return t(cs[0].kind === "edit" ? "chat-contextgroup.summary_edit" : "chat-contextgroup.summary_single", { label: contextLabel(cs[0]) });
  return t("chat-contextgroup.summary_many", { count: cs.length });
}

export function ContextGroup({ contexts }: { contexts: Context[] }) {
  const [open, setOpen] = useState(false);
  const one = contexts.length === 1;
  return (
    <div className={cx("flex max-w-full flex-col items-end", open && "w-[min(100%,560px)]")}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex max-w-full min-w-0 items-center gap-1.5 rounded-control px-2 py-1 text-[12px] whitespace-nowrap text-ink-3 transition-colors duration-100 hover:bg-hover-2 hover:text-ink-2"
      >
        <Icon name={one ? ICONS[contexts[0].kind] ?? "quote" : "layers"} size={13} className="shrink-0" />
        <span className="min-w-0 truncate">{contextSummary(contexts)}</span>
        <Icon name="chevron" size={13} stroke={2.2} className="shrink-0 transition-transform duration-300" style={{ transform: open ? "rotate(180deg)" : "none" }} />
      </button>
      <Collapse open={open} lazy className="w-full">
        <div className="mt-1 flex flex-col overflow-hidden rounded-[10px] bg-inset shadow-hairline">
          {one ? <ContextBody c={contexts[0]} /> : contexts.map((c, i) => <ContextRow key={i} c={c} first={i === 0} />)}
        </div>
      </Collapse>
    </div>
  );
}

function ContextRow({ c, first }: { c: Context; first: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <div className={cx(!first && "border-t border-line")}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left text-[12.5px] whitespace-nowrap text-ink-2 transition-colors duration-100 hover:bg-hover-2"
      >
        <Icon name={ICONS[c.kind] ?? "quote"} size={13} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate">{contextLabel(c)}</span>
        {!c.text && <span className="shrink-0 text-[11.5px] text-ink-3">{t("chat-contextgroup.empty")}</span>}
        <Icon name="chevronRight" size={13} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(90deg)" : "none" }} />
      </button>
      <Collapse open={open} lazy><ContextBody c={c} /></Collapse>
    </div>
  );
}

function ContextBody({ c }: { c: Context }) {
  const manager = useLinkManager();
  return (
    <div className="flex flex-col gap-1.5 px-3 pb-2.5 pt-0.5">
      {c.text ? (
        <div className="max-h-[320px] overflow-y-auto text-[12.5px] leading-[1.6] whitespace-pre-wrap text-ink-3 [overflow-wrap:anywhere]">{c.text}</div>
      ) : (
        <span className="text-[12px] text-ink-3">{t("chat-contextgroup.no_content")}</span>
      )}
      {c.url && (
        <a
          href={linkHref(c.url)}
          target="_blank"
          rel="noreferrer"
          onClick={(e) => clickLink(e, c.url, c.title, manager)}
          className="flex w-fit max-w-full min-w-0 items-center gap-1 text-[12px] whitespace-nowrap text-accent hover:underline"
        >
          <Icon name="external" size={12} className="shrink-0" />
          <span className="min-w-0 truncate">{t("chat-contextgroup.open_url", { url: c.url })}</span>
        </a>
      )}
    </div>
  );
}
