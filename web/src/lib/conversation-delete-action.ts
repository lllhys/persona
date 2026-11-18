import { conversationDeletionScope } from "./conversation-deleted";
import { t } from "../i18n";
import { credentialOwner, api, ApiError, enc } from "./api";
import { conversationManager, stampedHeaders } from "./manager-choice";
import { markConversationDeleted } from "./store";
import type { Session } from "./types";

export async function deleteConversation(s: Session) {
  const owner = credentialOwner();
  const scope = conversationDeletionScope();
  let answer: { ok: boolean; deleted: boolean };
  try {
    answer = await api(`/assistant/conversations/${enc(s.session_id)}`, {
      method: "DELETE", json: { machine_id: s.machine_id }, headers: stampedHeaders(s.manager ?? conversationManager(s.session_id)),
    });
  } catch (error) {
    if (!(error instanceof ApiError) || error.code !== "conversation_deleted") throw error;
    // Another device already deleted it. This is still authoritative, never inferred from a generic 404.
    answer = { ok: true, deleted: true };
  }
  if (!answer.ok || !answer.deleted) throw new Error(t("conversation-delete.delete_unconfirmed"));
  if (owner === credentialOwner() && scope === conversationDeletionScope()) markConversationDeleted(s.machine_id, s.session_id);
}
