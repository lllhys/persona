// Authoritative deletion marks last for this hub/account session, across manager switches.
// They fence late HTTP answers, websocket frames and cache writes until logout/reset.
import type { Session } from "./types";

const deleted = new Map<string, Session>();
const deletionListeners = new Set<(mid: string, sid: string) => void>();
let revision = 0;
let scope = 0;
export const conversationDeletionScope = () => scope;
const key = (mid: string, sid: string) => JSON.stringify([mid, sid]);
export const deletedConversation = (mid: string, sid: string) => deleted.get(key(mid, sid));
export const conversationDeleted = (mid: string, sid: string) => deleted.has(key(mid, sid));
export const hasDeletedConversations = () => deleted.size > 0;
export const deletedConversationID = (sid: string) => [...deleted.values()].some((s) => s.session_id === sid);
export const conversationKeyDeleted = (key: string) => [...deleted.values()].some((s) => `${s.machine_id}/${s.session_id}` === key);
export const availableConversation = (s: Session) => !s.deleted && !conversationDeleted(s.machine_id, s.session_id);
export function rememberConversationDeleted(mid: string, sid: string) {
  if (!mid || !sid || conversationDeleted(mid, sid)) return;
  deleted.set(key(mid, sid), { machine_id: mid, session_id: sid, assistant: true, deleted: true, status: "offline", title: "", cwd: "", capabilities: {} });
  revision++;
  deletionListeners.forEach((l) => l(mid, sid));
}
export function clearConversationDeletions() {
  deleted.clear();
  scope++;
  revision++;
}
export function onConversationDeleted(listener: (mid: string, sid: string) => void) {
  deletionListeners.add(listener);
  return () => { deletionListeners.delete(listener); };
}
export const conversationDeletionRevision = () => revision;
