// A Claude Code model_refusal_* record (#2528), loaded only when such a record is in the transcript. Keeping this
// notice and its words outside the eagerly loaded Lines module keeps rare recovery UI off the first screen.
import { recoveryCause, refusalFacts } from "../../lib/model-fallback";
import { replyModelName } from "../../lib/model-change";
import { clock } from "../../lib/format";
import type { Item } from "../../lib/types";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

/** The refused model, the one Claude Code switched to, the category and scope as recorded, and the refusal's own words
 * verbatim (never reworded, never the record's line). `plain`: a Persona conversation, for everyday people (#5202):
 * one sentence naming the model by its short name, the record under a closed Details. */
export function RefusalNotice({ item, plain }: { item: Item; plain?: boolean }) {
  const r = item.model_refusal!;
  const f = refusalFacts(r);
  const fallback = r.subtype === "model_refusal_fallback" && f.fallback;
  // The node's own recovery names one of the five plain-words causes (#7824); a runtime record's category stays in
  // the folded record with its existing wording.
  const cause = recoveryCause(f.category);
  const returning = r.recovery_returning && r.original_model ? replyModelName(r.original_model) : "";
  const record = (
    <>
      {fallback ? (
        <span className="[overflow-wrap:anywhere]">{t("chat-refusal.refusal_fallback", { from: f.original ?? "?", to: f.fallback! })}</span>
      ) : (
        <span>{t("chat-refusal.refusal_no_switch")}</span>
      )}
      {fallback && f.scope === "session" && <span>{t("chat-refusal.refusal_session")}</span>}
      {f.category && <span className="[overflow-wrap:anywhere]">{t("chat-refusal.refusal_category", { category: f.category })}</span>}
      {f.explanation && <span className="whitespace-pre-wrap">{t("chat-refusal.refusal_explanation", { text: f.explanation })}</span>}
      {f.text && (
        <span className="flex min-w-0 flex-col">
          <span className="text-ink-3">{t("chat-refusal.refusal_text")}</span>
          <span className="whitespace-pre-wrap [overflow-wrap:anywhere]" data-testid="refusal-text">
            {f.text}
          </span>
        </span>
      )}
    </>
  );
  return (
    <div role="note" data-testid="refusal-notice" title={clock(item.ts)} className="flex w-full items-start gap-2 rounded-card bg-orange-tint px-3 py-2 text-left shadow-hairline">
      <Icon name="warning" size={13} className="mt-[3px] shrink-0 text-orange" />
      <span className="flex min-w-0 flex-col gap-0.5 text-label leading-relaxed text-ink-2">
        {plain ? (
          <>
            <span className="font-semibold text-ink">
              {fallback
                ? cause
                  ? t("chat-refusal.plain_refused_switched_cause", { model: replyModelName(f.fallback!), cause })
                  : t("chat-refusal.plain_refused_switched", { model: replyModelName(f.fallback!) })
                : t("chat-refusal.refusal_title")}
            </span>
            {returning && (
              <span data-testid="refusal-returning">{t("chat-refusal.returning", { model: returning })}</span>
            )}
            <details data-testid="refusal-details" className="text-ink-3">
              <summary className="cursor-pointer select-none">{t("chat-refusal.details")}</summary>
              <span className="mt-1 flex min-w-0 flex-col gap-0.5 text-ink-2">{record}</span>
            </details>
          </>
        ) : (
          <>
            <span className="font-semibold text-ink">{t("chat-refusal.refusal_title")}</span>
            {record}
          </>
        )}
      </span>
    </div>
  );
}
