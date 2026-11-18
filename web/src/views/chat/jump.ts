// "Jump to the step that started it" (#2719, iOS ChatView.jumpToCall): from a background task's notice to the call that started it. The
// chat asks; each exchange on screen looks for the call in its own work groups (lib/work-groups.ts groupsHolding) and
// opens the ones it is folded in; the chat then brings the call's row into view.
import { useEffect } from "react";
import type { Item } from "../../lib/types";
import { groupsHolding, type WorkNode } from "../../lib/work-groups";
import { openExpanded, openFoldRange } from "./expanded";

type Seeker = (scope: string, call: string) => boolean;
const seekers = new Set<Seeker>();

/** Opens the groups the call is folded in; true when an exchange on screen holds it. */
export function openToCall(scope: string, call: string): boolean {
  let found = false;
  seekers.forEach((s) => {
    if (!found && s(scope, call)) found = true;
  });
  return found;
}

/** An exchange answers `openToCall` for its own nodes (`scope`: the chat's, as its groups' keys are). */
export function useCallSeeker(scope: string, nodes: WorkNode[]) {
  useEffect(() => {
    const seek: Seeker = (s, call) => {
      if (s !== scope) return false;
      const path = groupsHolding(nodes, call);
      if (!path) return false;
      openExpanded(path.map((id) => scope + id));
      return true;
    };
    seekers.add(seek);
    return () => void seekers.delete(seek);
  }, [scope, nodes]);
}

/** Finds the call and opens what it is in. A long chat's first page folds runs of work (fold_work): a call not among
 *  the steps drawn may be in one of `folds`, which are fetched newest first (`expand`, then `settle` for the exchanges
 *  to redraw) until it shows up. False: it is not in the loaded history. */
export async function seekCall(scope: string, call: string, folds: Item[], expand: (f: Item) => Promise<boolean | void>, settle: () => Promise<void>): Promise<boolean> {
  if (openToCall(scope, call)) return true;
  for (const f of folds.filter((i) => i.kind === "fold" && i.fold).reverse()) {
    openFoldRange(scope, f.seq, f.fold!.through_seq); // its steps land in open groups
    try {
      await expand(f);
    } catch {
      return false;
    }
    await settle();
    if (openToCall(scope, call)) return true;
  }
  return false;
}
