// One urgent call in its conversation (docs/assistant/urgent-call.md): a pill like an answered card's ("Urgent call ·
// answered · 2 min 5 s"), opening to why Persona called, what came of it and what was said. Loaded with the first call.
import { useState } from "react";
import { t } from "../../i18n";
import type { FeedItem } from "../../lib/feed";
import { duration } from "../../lib/format";
import { Collapse, cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import type { UrgentCall } from "./urgent-calls";

/** The row's words for how the call went. */
export function callSummary(c: UrgentCall): string {
  switch (c.state) {
    case "answered":
    case "ended":
      return c.seconds ? t("assistant-urgentcall.answered_for", { duration: duration(c.seconds * 1000) }) : t("assistant-urgentcall.answered");
    case "missed":
      return (c.attempt ?? 1) > 1 ? t("assistant-urgentcall.missed_twice") : t("assistant-urgentcall.missed");
    case "declined":
      return t("assistant-urgentcall.declined");
    case "cancelled":
      return t("assistant-urgentcall.cancelled");
    default:
      return t("assistant-urgentcall.ringing");
  }
}

export function UrgentCallLine({ item }: { item: FeedItem }) {
  const [open, setOpen] = useState(false);
  const c = item.body as unknown as UrgentCall;
  const why = c.rule?.text || (c.why === "owner_asked" ? t("assistant-urgentcall.you_asked") : "");
  const what = c.happening || c.body || "";
  return (
    <div className={cx("flex max-w-full flex-col self-start bg-field text-label text-ink-2", open ? "w-full rounded-card" : "rounded-full")}>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="flex min-w-0 items-center gap-1.5 px-3 py-1.5 text-left enabled:cursor-pointer">
        <Icon name="hangUp" size={12} className="shrink-0" />
        <span className="max-w-[45%] shrink-0 truncate">{t("assistant-urgentcall.call", { title: c.title })}</span>
        <span className="min-w-0 flex-1 truncate">· {callSummary(c)}</span>
        <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(90deg)" : "none" }} />
      </button>
      <Collapse open={open} lazy>
        <div className="flex flex-col gap-2.5 px-3 pt-0.5 pb-2.5 select-text" data-testid="urgent-call-detail">
          {(why || what) && (
            <div className="flex flex-col gap-0.5 whitespace-pre-wrap [overflow-wrap:anywhere]">
              <div className="font-medium text-ink-3">{t("assistant-urgentcall.why")}</div>
              {why && <div className="text-meta leading-[1.55] text-ink">{why}</div>}
              {what && <div className="text-meta leading-[1.55] text-ink">{what}</div>}
            </div>
          )}
          {c.outcome && (
            <div className="flex flex-col gap-0.5 whitespace-pre-wrap [overflow-wrap:anywhere]">
              <div className="font-medium text-ink-3">{t("assistant-urgentcall.outcome")}</div>
              <div className="text-meta leading-[1.55] text-ink">{c.outcome}</div>
            </div>
          )}
          {c.transcript && (
            <div className="flex flex-col gap-0.5 whitespace-pre-wrap [overflow-wrap:anywhere]">
              <div className="font-medium text-ink-3">{t("assistant-urgentcall.transcript")}</div>
              <div className="text-meta leading-[1.55] text-ink">{c.transcript}</div>
            </div>
          )}
        </div>
      </Collapse>
    </div>
  );
}
