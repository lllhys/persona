// What a client may keep and show of a Claude Code refusal record (#2528): the record's known fields and its own
// words. Older hubs sent the whole JSONL line too (model_refusal.raw, and as the notice's text); that line may carry
// fields no one reviewed, so it is never shown and never kept.
import type { Item, ModelRefusal } from "./types";

/** The record's public fields, whatever else an older hub put in it. The node's bounded-recovery fields (#7824) are
 *  part of the public protocol shape, so they pass through; anything else an older hub added stays out. */
export function publicRefusal(r: ModelRefusal): ModelRefusal {
  const out: ModelRefusal = { subtype: typeof r.subtype === "string" ? r.subtype : "" };
  for (const k of ["original_model", "fallback_model", "category", "scope", "explanation", "text", "recovery_reset", "recovery_tried"] as const) {
    const v = r[k];
    if (typeof v === "string" && v) out[k] = v;
  }
  for (const k of ["recovery_exhausted", "recovery_returning"] as const) {
    if (r[k]) out[k] = true;
  }
  return out;
}

/** An item as it may be kept: a refusal notice with its public record, and as text the refusal's words (else its
 *  explanation, else its subtype), never the line. Other items as they are. */
export function publicRefusalItem(it: Item): Item {
  if (!it.model_refusal) return it;
  const r = publicRefusal(it.model_refusal);
  return { ...it, model_refusal: r, text: r.text || r.explanation || r.subtype };
}

/** The item still holds what an older hub sent of the record beyond its public fields. */
export function hasPrivateRefusal(it: Item): boolean {
  if (!it.model_refusal) return false;
  const r = it.model_refusal as unknown as Record<string, unknown>;
  const pub = publicRefusalItem(it);
  return Object.keys(r).some((k) => !(k in pub.model_refusal!)) || it.text !== pub.text;
}
