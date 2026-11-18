// Picking messages at once (#3838, #9469; iOS MessagePick.swift): the message menu's Select messages, or Forward to's
// Select messages…, makes it with that message picked and hands it to the chat (ChatView), which holds it while it picks.
// Each message with a Forward then has a checkbox over it (views/chat/Picking.tsx PickRow), and a bar in the composer's
// place (PickBar) says how many are picked, offers Include questions when it means something, and forwards, copies or
// exports them as a picture; Cancel drops it, leaving the chat as it was. Read with useSyncExternalStore where it shows:
// a pick draws that message's checkbox and the bar again, not the chat. Include questions (#9469): each picked reply
// brings its questions (lib/pick-export.ts pickQuestions), on by default and remembered in this browser. Not on the first
// screen: it comes with Forward to or the menu's Select messages. No DOM here.
import type { ForwardPick } from "./forward";

/** The most messages forwarded at once (spec/fixtures/forward several.json `budget.most`; iOS ForwardMessage.most). With
 *  Include questions on, the questions the picked replies bring count too. */
export const FORWARD_MOST = 30;

/** Where Include questions is remembered (this browser's; iOS `pickIncludeQuestions`). */
export const INCLUDE_KEY = "persona.pickIncludeQuestions";

export interface MessagePick {
  subscribe: (listener: () => void) => () => void;
  /** Picking now. */
  on: () => boolean;
  /** The message `id` (ForwardPick.itemId) is picked. */
  has: (id: string) => boolean;
  count: () => number;
  /** The picked messages, in the order they were picked (inChatOrder puts them in the chat's). */
  picks: () => ForwardPick[];
  /** Starts picking, with `first` picked; reads Include questions as it was last left. */
  start: (first: ForwardPick) => void;
  /** Picks `p`, or unpicks it; false when that would send more than FORWARD_MOST: nothing changes but `refusals`. */
  toggle: (p: ForwardPick) => boolean;
  /** How many picks were refused so far (the bar says so as it changes). */
  refusals: () => number;
  /** Stops picking; nothing stays picked. */
  end: () => void;
  /** Each reply's questions, by the reply's id: read from the chat as picking starts and at each pick. */
  setQuestions: (read: () => ReadonlyMap<string, readonly ForwardPick[]>) => void;
  /** Include questions: on by default, remembered. */
  include: () => boolean;
  /** Turns it on or off; false when on would send more than FORWARD_MOST (counted as a refusal). */
  setInclude: (on: boolean) => boolean;
  /** The questions picked replies bring that are not picked themselves (none with Include questions off). */
  brought: () => ForwardPick[];
  /** The message `id` goes as a question a picked reply brings. */
  isBrought: (id: string) => boolean;
  /** Include questions means something now: a picked reply has a question that is not picked. */
  offered: () => boolean;
}

/** `picked`'s replies' questions that are not picked themselves, each once, in the order they come. */
export function questionsOf(picked: readonly ForwardPick[], questions: ReadonlyMap<string, readonly ForwardPick[]>): ForwardPick[] {
  const ids = new Set(picked.map((p) => p.itemId));
  const out = new Map<string, ForwardPick>();
  for (const p of picked) for (const q of questions.get(p.itemId) ?? []) if (!ids.has(q.itemId) && !out.has(q.itemId)) out.set(q.itemId, q);
  return [...out.values()];
}

function remembered(): boolean {
  try {
    return localStorage.getItem(INCLUDE_KEY) !== "0";
  } catch {
    return true;
  }
}

function remember(on: boolean) {
  try {
    localStorage.setItem(INCLUDE_KEY, on ? "1" : "0");
  } catch {
    // private browsing without storage: it is still on for this pick
  }
}

export function messagePick(): MessagePick {
  let on = false;
  let refused = 0;
  let include = true;
  let picked = new Map<string, ForwardPick>();
  let read: () => ReadonlyMap<string, readonly ForwardPick[]> = () => new Map();
  let questions: ReadonlyMap<string, readonly ForwardPick[]> = new Map();
  // what the views read, kept until a change so useSyncExternalStore gets the same value back
  let broughtNow: ForwardPick[] = [];
  const listeners = new Set<() => void>();
  const refresh = () => {
    broughtNow = include ? questionsOf([...picked.values()], questions) : [];
  };
  const changed = () => {
    refresh();
    listeners.forEach((l) => l());
  };
  const going = (next: Map<string, ForwardPick>, inc: boolean) => next.size + (inc ? questionsOf([...next.values()], questions).length : 0);
  return {
    subscribe: (l) => {
      listeners.add(l);
      return () => void listeners.delete(l);
    },
    on: () => on,
    has: (id) => picked.has(id),
    count: () => picked.size,
    picks: () => [...picked.values()],
    start: (first) => {
      on = true;
      include = remembered();
      questions = read();
      picked = new Map([[first.itemId, first]]);
      changed();
    },
    toggle: (p) => {
      if (!on) return false;
      questions = read();
      const next = new Map(picked);
      if (next.has(p.itemId)) next.delete(p.itemId);
      else {
        next.set(p.itemId, p);
        if (going(next, include) > FORWARD_MOST) {
          refused++;
          changed();
          return false;
        }
      }
      picked = next;
      changed();
      return true;
    },
    refusals: () => refused,
    end: () => {
      if (!on) return;
      on = false;
      picked = new Map();
      changed();
    },
    setQuestions: (r) => {
      read = r;
      if (on) {
        questions = read();
        changed();
      }
    },
    include: () => include,
    setInclude: (want) => {
      if (want && going(picked, true) > FORWARD_MOST) {
        refused++;
        changed();
        return false;
      }
      include = want;
      remember(want);
      changed();
      return true;
    },
    brought: () => broughtNow,
    isBrought: (id) => broughtNow.some((q) => q.itemId === id),
    offered: () => questionsOf([...picked.values()], questions).length > 0,
  };
}
