// Identity of the answers a card offers, for the mis-tap guard (lib/tap-guard.ts, #8161). The key covers the question
// and the option set, not the output streamed above the card: a new key restarts the guard window, while redrawing the
// same answers (an unrelated re-render) keeps the window that was already running.
import { offersAlways } from "./always-scope";
import type { Interaction } from "./types";

export function cardTapKey(ix: Interaction): string {
  switch (ix.kind) {
    case "question":
      return JSON.stringify(["question", ix.tool_name ?? "", (ix.questions ?? []).map((q) => [
        q.question,
        q.multi_select ?? false,
        (q.options ?? []).map((o) => [o.label, o.action ?? ""]),
      ])]);
    case "screen": {
      const d = ix.dialog;
      return JSON.stringify([
        "screen",
        d?.question ?? "",
        d?.multi ?? false,
        (d?.options ?? []).map((o) => [o.key, o.kind ?? ""]),
        d?.steps?.length ?? 0,
        d?.review ?? false,
      ]);
    }
    case "elicitation":
      return JSON.stringify(["elicitation", ix.elicitation?.mode ?? "", ix.elicitation?.url ?? "", ix.elicitation?.requested_schema ?? null]);
    case "plan":
      return JSON.stringify(["plan", ix.plan ?? ""]);
    case "permission":
    default:
      return JSON.stringify(["permission", ix.tool_name ?? "", ix.tool_input ?? null, offersAlways(ix), ix.always_label ?? ""]);
  }
}
