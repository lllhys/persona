import { deleteConversation } from "../lib/conversation-delete-action";
import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { errorText } from "../lib/api";
import { isBackgroundSupervisor } from "../lib/background-supervisor";
import { conversationDeleteAllowed, CONVERSATION_DELETE_FEATURE } from "../lib/conversation-delete";
import { getSession, getState, useStore } from "../lib/store";
import { sessionTitle } from "../lib/format";
import type { Session } from "../lib/types";
import { Button, Dialog } from "../ui/atoms";

export function DeleteConversationDialog({ s, onClose }: { s: Session; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);
  const deleted = useStore(() => getSession(s.machine_id, s.session_id)?.deleted === true);
  useEffect(() => { if (deleted) onClose(); }, [deleted, onClose]);
  const remove = async () => {
    if (pending.current) return;
    const state = getState();
    const current = getSession(s.machine_id, s.session_id) ?? s;
    if (!conversationDeleteAllowed(current, {
      supported: !!state.assistant && state.features.includes(CONVERSATION_DELETE_FEATURE), main: state.assistant?.main === s.session_id,
      builtIn: isBackgroundSupervisor(state.assistant, s.session_id),
      pending: state.interactions.some((ix) => ix.machine_id === s.machine_id && ix.session_id === s.session_id),
    })) { setError(t("conversation-delete.delete_unavailable")); return; }
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await deleteConversation(current);
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally { pending.current = false; setBusy(false); }
  };
  return <Dialog open title={t("conversation-delete.delete_title", { title: sessionTitle(s) })} onClose={() => !busy && onClose()} footer={<>
    <Button tone="quiet" disabled={busy} onClick={onClose}>{t("common.cancel")}</Button>
    <Button tone="danger" icon="trash" busy={busy} onClick={() => void remove()}>{t("conversation-delete.delete_confirm")}</Button>
  </>}>
    <p className="text-meta leading-relaxed text-ink-2">{t("conversation-delete.delete_body")}</p>
    {error && <p role="alert" className="mt-3 text-meta text-red">{error}</p>}
  </Dialog>;
}
