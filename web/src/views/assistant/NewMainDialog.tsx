import { useState } from "react";
import { t } from "../../i18n";
import { api, ApiError, errorText } from "../../lib/api";
import { archivedMainTitle, NEW_MAIN_PATH, newMainBody } from "../../lib/compaction";
import { navigate } from "../../lib/router";
import { refreshAssistant } from "../../lib/store";
import type { Session } from "../../lib/types";
import { Button, Dialog, toast } from "../../ui/atoms";
import { Switch } from "./parts";

/** Archives the main conversation (renamed "Old main chat · until <date>") and leaves none: the next message starts the new one. */
export function NewMainDialog({ open, onClose, session }: { open: boolean; onClose: () => void; session: Session }) {
  const [carry, setCarry] = useState(true);
  const [busy, setBusy] = useState(false);
  const toEmptyMain = async () => {
    await refreshAssistant().catch(() => {});
    onClose();
    navigate({ name: "persona" });
  };
  const go = async () => {
    setBusy(true);
    try {
      const r = await api<{ ok: boolean; archived?: string; carried?: boolean }>(NEW_MAIN_PATH, { method: "POST", json: newMainBody(carry, session.session_id) });
      await toEmptyMain();
      toast(r.carried ? t("assistant-assistantchat.new_main_done_carried") : t("assistant-assistantchat.new_main_done"), "ok");
    } catch (e) {
      if (e instanceof ApiError && e.code === "main_changed") {
        // another device replaced it: show what the hub has now
        await toEmptyMain();
        toast(t("assistant-assistantchat.new_main_elsewhere"), "info");
      } else {
        toast(t("assistant-assistantchat.new_main_failed", { error: errorText(e) }), "error");
      }
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("assistant-assistantchat.new_main_title")}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>{t("common.cancel")}</Button>
          <Button tone="primary" busy={busy} onClick={() => void go()}>{t("assistant-assistantchat.new_main")}</Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-ink-2">{t("assistant-assistantchat.new_main_body", { title: archivedMainTitle() })}</p>
      <div className="mt-3 flex items-start gap-3 rounded-control bg-inset px-3 py-2.5 shadow-hairline">
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-medium text-ink">{t("assistant-assistantchat.carry_summary")}</div>
          <div className="mt-0.5 text-[12px] leading-relaxed text-ink-3">{t("assistant-assistantchat.carry_summary_hint")}</div>
        </div>
        <Switch checked={carry} onChange={setCarry} label={t("assistant-assistantchat.carry_summary")} disabled={busy} />
      </div>
    </Dialog>
  );
}
