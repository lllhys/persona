// A rule after the same answer three times (#4929, #5802, docs/assistant/unified-entry.md "A rule after the same answer
// three times"; docs/protocol/protocol.md `RuleSuggestion`): once the owner gave the same answer to a kind of card for
// the same scope three times, the hub attaches a suggestion to the next plan card, decision or passed-up question.
// Always makes the rule, Not now hides the line for that kind and scope for 30 days. The hub counts and words the line;
// this page says what Always stores and, afterwards, where the rule is listed and removed. The pure parts and the two
// requests; the view is views/assistant/RuleSuggestionLine.tsx.
import { t } from "../i18n";
import { api, ApiError, credentialOwner, enc } from "./api";

/** What a suggested rule would cover; empty for `delegate`, whose setting is not per directory. */
export interface RuleScope {
  machine_id?: string;
  /** The computer's name, for display. */
  machine?: string;
  directory?: string;
  /** A standing authorization's action and the conditions it would be granted under. */
  action?: string;
  conditions?: string[];
  /** What the standing authorization would keep as the owner's words: their latest answer. */
  quote?: string;
}

export type RuleKind = "plan_start" | "standing_authorization" | "delegate";

/** The line a card carries. `text` is the hub's sentence, in the request's language. */
export interface RuleSuggestion {
  id: string;
  kind: RuleKind | (string & {});
  text: string;
  scope?: RuleScope;
  /** What Always stores: `tasks.trust`, `standing_authorization`, or "" when it stores nothing (`setting` names what to turn on). */
  creates?: string;
  setting?: string;
  count?: number;
}

/** A card that may carry one: an open plan (which names its conversation), a passed-up question (its manager), or a
 *  to-do's decision (which gives the `headers` its own answers go with). `suggestion` is as the hub sent it, checked
 *  by ruleSuggestion where it is drawn. */
export interface RuleCard {
  /** The card's own id (a plan's, a question's, a decision's round): what Always made is kept by it. */
  id?: string;
  /** The card cannot be answered from here (a question whose own controls are off): the line's buttons are off too. */
  disabled?: boolean;
  suggestion?: RuleSuggestion;
  conversation?: string;
  manager?: string;
  headers?: Record<string, string>;
}

const KINDS: readonly string[] = ["plan_start", "standing_authorization", "delegate"];

/** The suggestion when it is one this page can say everything about: an id to answer, the hub's sentence and a kind
 *  whose rule it knows. Anything else (no field, a kind from a newer hub) draws nothing, and the card is as before. */
export function ruleSuggestion(value: unknown): (RuleSuggestion & { kind: RuleKind }) | undefined {
  if (!value || typeof value !== "object") return undefined;
  const s = value as Partial<RuleSuggestion>;
  if (typeof s.id !== "string" || !s.id || typeof s.text !== "string" || !s.text.trim() || typeof s.kind !== "string" || !KINDS.includes(s.kind)) return undefined;
  return s as RuleSuggestion & { kind: RuleKind };
}

function actionLabel(action: string | undefined): string {
  return action === "merge" || action === "push" ? t(`settings-standing.action_${action}`) : action === "deploy-test" ? t("settings-standing.action_deploy_test") : action || "";
}

function conditionLabel(condition: string): string {
  return condition === "ci_green" || condition === "no_conflicts" ? t(`settings-standing.condition_${condition}`) : condition;
}

/** The computer as the line names it: its name, else its id. */
function scopeMachine(scope: RuleScope | undefined): string {
  return scope?.machine || scope?.machine_id || "";
}

/** Exactly what Always stores, in sentences: the remembered kind of task, the standing authorization with its
 *  conditions and the words kept as the owner's, or, for `delegate`, that it stores no rule and turns nothing on. */
export function ruleCreates(s: RuleSuggestion & { kind: RuleKind }): string[] {
  const scope = s.scope ?? {};
  const where = { directory: scope.directory ?? "", machine: scopeMachine(scope) };
  switch (s.kind) {
    case "plan_start":
      return [t("assistant-rulesuggestion.creates_plan_start", where)];
    case "standing_authorization": {
      const conditions = (scope.conditions ?? []).filter((c) => typeof c === "string" && c).map(conditionLabel);
      return [
        t("assistant-rulesuggestion.creates_standing", { ...where, action: actionLabel(scope.action) }),
        conditions.length ? t("assistant-rulesuggestion.creates_conditions", { conditions: conditions.join(t("assistant-rulesuggestion.list_separator")) }) : "",
        scope.quote ? t("assistant-rulesuggestion.creates_quote", { quote: scope.quote }) : "",
      ].filter(Boolean);
    }
    case "delegate":
      // no key and no place here: the hub's own sentence says what the setting does
      return [t("assistant-rulesuggestion.creates_delegate")];
  }
}

/** Sentences one after another, as this language sets them (a space between in English, nothing in Chinese and Japanese). */
export function ruleJoin(sentences: string[]): string {
  return sentences.reduce((first, second) => t("assistant-rulesuggestion.joined", { first, second }));
}

/** What the hub answers Always with: the kind, what it stored and, by kind, the entry it made. */
export interface RuleMade {
  ok?: boolean;
  kind?: string;
  creates?: string;
  /** `plan_start`: the `tasks.trust` entry's key. */
  key?: string;
  /** `standing_authorization`: the grant as stored. */
  authorization?: { machine_id?: string; directory?: string; actions?: string[]; conditions?: string[]; quote?: string };
  /** `delegate`: the setting to turn on; nothing was stored. */
  setting?: string;
}

/** What is said once Always went through: what was made, and where it is listed and removed. `page` is where this
 *  app lists it; `delegate` stores nothing and its setting has no switch in any app yet (the assistant's configuration
 *  file on the hub's computer), so it has none. */
export interface RuleResult {
  text: string;
  where: string;
  page?: string;
}

export function ruleResult(s: RuleSuggestion & { kind: RuleKind }, made: RuleMade): RuleResult {
  const kind = KINDS.includes(made.kind ?? "") ? (made.kind as RuleKind) : s.kind;
  // both lists are on the assistant's own settings page, each under its own heading
  const where = (list: string) => t("assistant-rulesuggestion.where", { tab: t("settings-nav.tab_profile"), list });
  switch (kind) {
    case "plan_start":
      return { text: t("assistant-rulesuggestion.made_plan_start"), where: where(t("settings-assistantsettings.trust_title")), page: "/settings?tab=persona" };
    case "standing_authorization":
      return { text: t("assistant-rulesuggestion.made_standing"), where: where(t("settings-standing.title")), page: "/settings?tab=persona" };
    case "delegate":
      return { text: t("assistant-rulesuggestion.made_delegate"), where: t("assistant-rulesuggestion.where_delegate", { setting: made.setting || s.setting || "agents.delegate" }) };
  }
}

/** How answering went: the hub took it (`made` for Always), or the suggestion is gone (404: answered on another device,
 *  or no longer earned), which only clears the line. Any other failure throws, and the line stays to try again. */
export type RuleAnswer = { state: "made"; made: RuleMade } | { state: "hidden" } | { state: "gone" };

/** `current`: whether the page is still signed in as it was when the answer was given (the request's fence). */
export async function answerRuleSuggestion(id: string, answer: "always" | "not-now", headers?: Record<string, string>, current?: () => boolean): Promise<RuleAnswer> {
  try {
    const made = await api<RuleMade>(`/assistant/rule-suggestions/${enc(id)}/${answer}`, { method: "POST", headers, isAuthContextCurrent: current });
    return answer === "always" ? { state: "made", made: made ?? {} } : { state: "hidden" };
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return { state: "gone" };
    throw e;
  }
}

// What this page knows about the suggestions it answered, kept per hub (the page's credential). A suggestion's id is a
// hash of its kind and what it covers, so the `delegate` one has the same id on every hub and under every manager:
// `answered` is per manager too (`ruleScope`), and one answered for one of them says nothing about another's.
//
// `answered`: the hub takes an answered suggestion off every card and sends the cards again, but a plan and a to-do's
// decision are only worded when read, so the line goes at once here whatever this page still holds. It hides the
// copies the page held when the answer was given, and what arrives right after it (a read that was on its way); a copy
// the hub sends later is a suggestion earned again, which is drawn.
// `made`: what Always made, by the card it was answered on, so it stays said there once the card comes without the
// suggestion.
const ANSWER_SETTLES_MS = 15_000;
const answered = new Map<string, { at: number; held: WeakSet<object> }>();
const made = new Map<string, RuleResult>();
const listeners = new Set<() => void>();

/** The hub and manager a card's suggestion belongs to: this page's credential and what the card's requests carry. */
export function ruleScope(headers: Record<string, string> | undefined): string {
  return `${credentialOwner()}\n${JSON.stringify(Object.entries(headers ?? {}).sort())}`;
}

/** Whether `suggestion` (the object a card carries, by `scope`) was answered here and is not one the hub sent since. */
export function ruleAnswered(scope: string, suggestion: { id: string }, now = Date.now()): boolean {
  const key = `${scope}\n${suggestion.id}`;
  const was = answered.get(key);
  if (!was) return false;
  if (was.held.has(suggestion)) return true;
  if (now - was.at < ANSWER_SETTLES_MS) {
    was.held.add(suggestion);
    return true;
  }
  answered.delete(key); // earned again: the hub sent it after it took the answer
  return false;
}

export function markRuleAnswered(scope: string, suggestion: { id: string }, now = Date.now()) {
  answered.set(`${scope}\n${suggestion.id}`, { at: now, held: new WeakSet([suggestion]) });
  listeners.forEach((l) => l());
}

/** What Always made on the card `card` (its id), if this page made it on the hub it is signed in to. By the card
 *  alone, not its manager: the hub sends a card again with or without its manager's name, and a card's id is its own. */
export function ruleMade(card: string | undefined): RuleResult | undefined {
  return card ? made.get(`${credentialOwner()}\n${card}`) : undefined;
}

/** `owner`: the credential the answer was given under (credentialOwner when it was tapped). */
export function markRuleMade(owner: string, card: string | undefined, result: RuleResult) {
  if (card) made.set(`${owner}\n${card}`, result);
}

export function subscribeRuleAnswered(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** For tests. */
export function resetRuleAnswered() {
  answered.clear();
  made.clear();
}
