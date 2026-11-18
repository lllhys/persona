// The line under a card that carries a suggested rule (#4929, #5802, docs/assistant/unified-entry.md "A rule after
// the same answer three times"): what the hub noticed and for which computer and directory (its sentence), exactly what
// Always stores (lib/rule-suggestion ruleCreates), and the two answers. After Always it says, in the same place, what
// was made and where it is listed and removed; that is kept by card (lib/rule-suggestion ruleMade), so it stays when
// the hub sends the card again without the suggestion. A suggestion that is gone on the hub (404) only clears the
// line. Loaded with the first card that carries one, not with the first screen (views/assistant/Tasks ruleLine, which
// also keeps it mounted under a plan card or a question once one was answered there); under a decision's row it is
// always mounted (TaskDecision).
import { useState, useSyncExternalStore } from "react";
import { t } from "../../i18n";
import { ApiError, credentialOwner, errorText } from "../../lib/api";
import { conversationHeaders } from "../../lib/feed";
import { itemHeaders } from "../../lib/manager-choice";
import { navigate } from "../../lib/router";
import { answerRuleSuggestion, markRuleAnswered, markRuleMade, ruleAnswered, ruleCreates, ruleJoin, ruleMade, ruleResult, ruleScope, ruleSuggestion, subscribeRuleAnswered, type RuleCard } from "../../lib/rule-suggestion";
import { Button, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { underCard } from "../chat/Interactions";

/** `of`: the card the line is under. */
export default function RuleSuggestionLine({ of }: { of: RuleCard }) {
  const suggestion = ruleSuggestion(of.suggestion);
  const [busy, setBusy] = useState<"always" | "not-now" | null>(null);
  // the hub refused this device (403: only one with full access makes a rule): the line stays, its buttons do not
  const [refused, setRefused] = useState(false);
  // the hub and manager the card is of: what it answers with, and what this page keeps its answers under
  const headers = of.headers ?? (of.conversation ? conversationHeaders(of.conversation) : itemHeaders(of.manager));
  const scope = ruleScope(headers);
  const gone = useSyncExternalStore(subscribeRuleAnswered, () => !!suggestion && ruleAnswered(scope, suggestion));
  const result = useSyncExternalStore(subscribeRuleAnswered, () => ruleMade(of.id));
  // what Always made, until another suggestion comes to this card
  if (result && (!suggestion || gone)) {
    const page = result.page;
    return (
      <div data-rule-result role="status" className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-1 text-xs text-ink-2">
        <p className="flex min-w-0 flex-1 basis-64 items-start gap-1.5 [overflow-wrap:anywhere]">
          <Icon name="check" size={13} stroke={2.2} className="mt-0.5 shrink-0 text-green" />
          <span>
            {ruleJoin([result.text, result.where])}
          </span>
        </p>
        {page && (
          <Button size="sm" tone="quiet" onClick={() => navigate(page)}>
            {t("assistant-rulesuggestion.open_settings")}
          </Button>
        )}
      </div>
    );
  }
  if (!suggestion || gone) return null;
  const answer = (which: "always" | "not-now") => {
    if (busy) return;
    setBusy(which);
    // an answer that comes back after the page signed in as someone else says nothing about what it shows now
    const owner = credentialOwner();
    const current = () => owner === credentialOwner();
    // a plan card's or a question's line is drawn only while the card carries a suggestion: once answered there, it
    // stays mounted to say what was made (views/assistant/Tasks ruleLine)
    if (which === "always" && of.id) (underCard.made ??= new Set()).add(of.id);
    answerRuleSuggestion(suggestion.id, which, headers, current).then(
      (res) => {
        setBusy(null);
        if (!current()) return;
        if (res.state === "made") markRuleMade(owner, of.id, ruleResult(suggestion, res.made));
        else if (res.state === "hidden") toast(t("assistant-rulesuggestion.hidden"), "ok");
        markRuleAnswered(scope, suggestion);
      },
      (e) => {
        setBusy(null);
        if (!current()) return;
        if (e instanceof ApiError && e.status === 403) setRefused(true);
        toast(t("assistant-rulesuggestion.failed", { error: errorText(e) }), "error");
      },
    );
  };
  const off = !!busy || refused || !!of.disabled;
  return (
    <div data-rule-suggestion={suggestion.kind} role="group" aria-label={t("assistant-rulesuggestion.label")} className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-1 text-xs text-ink-2">
      <p className="min-w-0 flex-1 basis-64 [overflow-wrap:anywhere]">
        <span data-rule-text>{suggestion.text}</span>
        {ruleJoin(["", ""]) /* what this language puts between two sentences */}
        <span data-rule-creates className="text-ink-3">
          {ruleJoin(ruleCreates(suggestion))}
        </span>
      </p>
      <span className="flex shrink-0 items-center gap-2">
        <Button size="sm" busy={busy === "always"} disabled={off} title={t("assistant-rulesuggestion.always_title")} onClick={() => answer("always")}>
          {t("assistant-rulesuggestion.always")}
        </Button>
        <Button size="sm" tone="quiet" busy={busy === "not-now"} disabled={off} title={t("assistant-rulesuggestion.not_now_title")} onClick={() => answer("not-now")}>
          {t("assistant-rulesuggestion.not_now")}
        </Button>
      </span>
    </div>
  );
}
