// Context bodies (including selected-skill metadata) load only when the existing disclosure opens.
import { useState } from "react";
import { clickLink, linkHref } from "../../lib/links";
import { useLinkManager } from "../../lib/manager-choice";
import type { Context } from "../../lib/turns";
import { Collapse, cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";
import { contextLabel, CONTEXT_ICONS } from "./ContextGroup";

export function ContextDetails({ contexts }: { contexts: Context[] }) {
  return (
    <div className="mt-1 flex flex-col overflow-hidden rounded-[10px] bg-inset shadow-hairline">
      {contexts.length === 1 ? <ContextBody c={contexts[0]} /> : contexts.map((c, i) => <ContextRow key={i} c={c} first={i === 0} />)}
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
        <Icon name={CONTEXT_ICONS[c.kind] ?? "quote"} size={13} className="shrink-0 text-ink-3" />
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
