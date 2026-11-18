// The mis-tap guard (lib/tap-guard.ts) for the chat's cards. One surface per chat view: opening a session exempts what
// it shows first, a card or option set that appears later is held for the guard window. The key is the card's answers
// (lib/card-tap-key.ts); a `suffix` names a visible option set the card swaps in itself (a multi-question card's page).
import { cardTapKey } from "../../lib/card-tap-key";
import { currentViewId, TapGuardSurface, useTapGuard, useTapGuardView, type TapGuardHandle } from "../../lib/tap-guard";
import type { Interaction } from "../../lib/types";

/** A card read in this long after the chat opened still counts as part of opening it. */
const OPEN_GRACE_MS = 1000;

const chatSurface = new TapGuardSurface(OPEN_GRACE_MS);

/** Stamps the opened chat once its dock mounts, also while no card is on screen yet. */
export function useChatTapGuardView(): void {
  useTapGuardView(chatSurface, currentViewId());
}

/** The card's answer buttons: its footer actions, hand-back, and the option buttons that carry the marker. */
const ANSWER_SELECTOR = "[data-card-actions] button:not([data-card-answers]), [data-card-answer], [data-tap-answer]";

/** Tests: the module-level surface outlives any one card, so each test starts with a freshly opened chat. */
export function resetChatTapGuardForTest(): void {
  chatSurface.reset();
}

export function useChatCardTapGuard(ix: Interaction, suffix = ""): TapGuardHandle {
  return useTapGuard(chatSurface, `${ix.id}${cardTapKey(ix)}${suffix}`, currentViewId(), ANSWER_SELECTOR);
}
