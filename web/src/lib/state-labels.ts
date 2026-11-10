// The one set of names for an agent's and a task's state on the web (#3415; docs/protocol/i18n.md "Agent states",
// docs/protocol/agent-states.md): every page takes its state words from here, so a state is never Needs you on one page and Waiting for you
// on the next. The words are in lib-state-labels.json; state-labels.test.ts keeps other namespaces from defining them.
import { t } from "../i18n";
import type { AgentState } from "./types";

/** An agent's state (AgentState), Waiting, Waiting in the background (#6045), Awaiting reply (#5096), and the states only a task or a summary has: Done, Interrupted, Follow-up, Waiting (a task whose agent waits to be brought back, #3274), Not started, Didn't start. */
export type StateName = AgentState | "waiting" | "bg_waiting" | "awaits_reply" | "done" | "interrupted" | "follow_up" | "task_waiting" | "not_started" | "failed";

export function stateName(s: StateName): string {
  return t(`lib-state-labels.${s}`);
}

/** The states a list counts in its headers and summaries: "3 need you". */
export type CountedState = "needs_you" | "working" | "bg_waiting" | "unread" | "done";

export function stateCount(s: CountedState, count: number): string {
  return t(`lib-state-labels.count_${s}`, { count });
}

/** The label for marking all finished, unseen agents as seen, optionally with a count. */
export function markAllSeenLabel(count?: number): string {
  return count === undefined ? t("lib-state-labels.mark_all_seen") : t("lib-state-labels.mark_all_seen_count", { count });
}

export const STATE_LABEL: Record<AgentState, string> = {
  needs_you: stateName("needs_you"),
  working: stateName("working"),
  unread: stateName("unread"),
  idle: stateName("idle"),
  offline: stateName("offline"),
  unknown: stateName("unknown"),
};
