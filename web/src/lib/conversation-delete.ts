import { getSession, useStore } from "./store";
import type { Session } from "./types";

export const CONVERSATION_DELETE_FEATURE = "assistant_conversation_delete";
export function conversationDeleteAllowed(s: Session, options: { supported: boolean; main?: boolean; builtIn?: boolean; pending?: boolean }): boolean {
  return options.supported && !!s.assistant && !options.main && !options.builtIn && !options.pending && !s.deleted
    && !s.starting && !s.waiting && !s.pending_interactions && !s.queued?.length && s.compaction?.state !== "running" && !s.background_working && !s.background_waiting
    && s.status !== "working" && s.status !== "blocked" && s.state !== "working" && s.state !== "needs_you";
}

export function useCanDeleteConversation(s: Session, main?: boolean, builtIn?: boolean) {
  return useStore((state) => conversationDeleteAllowed(getSession(s.machine_id, s.session_id) ?? s, {
    supported: !!state.assistant && state.features.includes(CONVERSATION_DELETE_FEATURE), main, builtIn,
    pending: state.interactions.some((ix) => ix.machine_id === s.machine_id && ix.session_id === s.session_id),
  }));
}
