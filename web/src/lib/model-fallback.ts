// A refusal fallback (#2289, #2528): the upstream refused a request and Claude Code answered it with another model on
// its own, keeping the session there for later turns (system/model_refusal_fallback, scope "session"). What the app
// shows of it, and the one action it offers: "back to the configured model", which closes the session's idle headless
// process so its next start uses the configured model (server/internal/node/model_restore.go). It sends no message and
// replays nothing; whether the configured model then runs is only known from a newer reply, so nothing here says it was
// restored, and the same request may be refused again.

import { t } from "../i18n";
import { publicRefusal } from "./refusal-public";
import { usageLimitClock } from "./usage-limit";
import type { ModelRefusal, Session } from "./types";

/** The turn_error_code of a turn that failed on the missing fallback model: the user's to act on, never "Continue". */
export const FALLBACK_UNAVAILABLE = "model_fallback_unavailable";

export interface FallbackView {
  /** The model the refused request asked for: history, not the session's live configuration. */
  original: string;
  /** The model Claude Code moved to after the refusal. */
  fallback: string;
  /** The turn's error after the fallback, Claude Code's own words; "" none. */
  error: string;
  /** "Back to the configured model" was done: the next start asks for `target`. Not proof that it runs. */
  pending: boolean;
  /** The configured model the node read when it closed the process; "" none pending. */
  target: string;
  /** Why the node's own recovery switched, in plain words (#7824); undefined for a runtime-written record or an
   *  unknown category, whose wording stays exactly as before. */
  cause?: string;
  /** The configured model when it answered a probe: the next request runs on it again; undefined otherwise. */
  returning?: string;
}

/** What the card shows for a session, or null when it has no active refusal fallback. */
export function fallbackView(s: Pick<Session, "model_fallback" | "model_restore" | "turn_error">): FallbackView | null {
  const f = s.model_fallback;
  if (!f || f.subtype !== "model_refusal_fallback" || !f.fallback_model) return null;
  const pending = s.model_restore?.state === "next_start";
  return {
    original: f.original_model ?? "",
    fallback: f.fallback_model,
    error: s.turn_error ?? "",
    pending,
    target: pending ? (s.model_restore?.to ?? "") : "",
    cause: recoveryCause(f.category),
    returning: f.recovery_returning ? (f.original_model ?? "") : undefined,
  };
}

/** The node's bounded-recovery categories (#7824, server proto.ModelRecoveryReason*) mapped to plain words in the
 *  page's language. A runtime-written refusal record carries none of these values, so anything else returns undefined
 *  and the record keeps its existing wording. */
const RECOVERY_CAUSE_KEYS: Record<string, string> = {
  safeguards_refusal: "chat-modelfallback.cause_safeguards_refusal",
  quota_exhausted: "chat-modelfallback.cause_quota_exhausted",
  rate_limited: "chat-modelfallback.cause_rate_limited",
  server_error: "chat-modelfallback.cause_server_error",
  model_unavailable: "chat-modelfallback.cause_model_unavailable",
};

/** Plain-words cause for a recovery category; undefined when the category is not one the node's recovery writes. */
export function recoveryCause(category?: string): string | undefined {
  const key = category ? RECOVERY_CAUSE_KEYS[category] : undefined;
  return key ? t(key) : undefined;
}

/** What the failure card shows for an exhausted recovery chain (#7824); null when the session's fallback is not one. */
export interface RecoveryExhaustedView {
  /** The cause in plain words; "" when the category is not one of the known values. */
  cause: string;
  /** When the configured model is available again, in the user's time zone; "" when the upstream gave no time. */
  reset: string;
  /** Every model the chain tried, in the node's order; "" when the record names none. Only the folded details show it. */
  tried: string;
}

/** Read an exhausted-chain marker from a session. The reset time reuses the usage-limit clock so both surfaces format
 *  a recovery moment the same way, in the user's time zone. */
export function recoveryExhausted(s: Pick<Session, "model_fallback"> | undefined, now = Date.now()): RecoveryExhaustedView | null {
  const f = s?.model_fallback;
  if (!f || f.subtype !== "model_refusal_fallback" || !f.recovery_exhausted) return null;
  return {
    cause: recoveryCause(f.category) ?? "",
    reset: f.recovery_reset ? usageLimitClock(f.recovery_reset, now) : "",
    tried: typeof f.recovery_tried === "string" ? f.recovery_tried : "",
  };
}

/** The configured model once it answered a probe (#7824): the next request runs on it again; undefined otherwise. */
export function recoveryReturning(s: Pick<Session, "model_fallback"> | undefined): string | undefined {
  const f = s?.model_fallback;
  if (!f || f.subtype !== "model_refusal_fallback" || !f.recovery_returning) return undefined;
  return f.original_model || undefined;
}

/** A refusal notice's facts in the order they are shown: the public record only (lib/refusal-public.ts), the refusal's
 *  words last and untouched. Whatever else an older hub sent (its "raw" line) is never read. */
export function refusalFacts(r: ModelRefusal): { original?: string; fallback?: string; category?: string; scope?: string; explanation?: string; text?: string } {
  const p = publicRefusal(r);
  return {
    original: p.original_model,
    fallback: p.fallback_model,
    category: p.category,
    scope: p.scope,
    explanation: p.explanation,
    text: p.text?.trim() ? p.text : undefined,
  };
}
