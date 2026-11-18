import { useState } from "react";
import { api, ApiError, enc, errorText } from "../../lib/api";
import { continuationLaunch, takeoverBody, type CodexTakeoverPlan } from "../../lib/codex-takeover";
import { launchAgent } from "../../lib/new-agent";
import { t } from "../../i18n";
import type { Session } from "../../lib/types";
import { Button, Dialog, toast } from "../../ui/atoms";
import { upsertSession } from "../../lib/store";

export default function TakeoverAction({ session }: { session: Session }) {
  const [plan, setPlan] = useState<CodexTakeoverPlan | null>(null);
  const [loading, setLoading] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const [busy, setBusy] = useState(false);
  const path = `/machines/${enc(session.machine_id)}/codex/${enc(session.session_id)}/takeover`;
  const takeoverAvailable = Boolean(session.capabilities?.takeover);

  const commit = async (p: CodexTakeoverPlan, confirm: boolean) => {
    const body = takeoverBody(p, confirm);
    if (!body) return;
    setBusy(true);
    try {
      const result = await api<{ session: Session }>(path, { method: "POST", json: body });
      upsertSession({ ...result.session, machine_id: session.machine_id });
      setPlan(null); setConfirmStop(false);
    } catch (e) {
      if (e instanceof ApiError && e.code === "codex_takeover_continue_only") {
        setPlan({ ...p, can_resume: false, reason: "encrypted_context" });
        setConfirmStop(false);
      } else {
        // A failed response may follow token consumption. A retry must start with a fresh GET.
        setPlan(null);
        setConfirmStop(false);
        toast(errorText(e), "error");
      }
    } finally { setBusy(false); }
  };

  const prepare = async () => {
    setLoading(true);
    try {
      const result = await api<CodexTakeoverPlan>(path);
      setPlan(result);
      if (!result.can_resume) return;
      if (result.running) { setConfirmStop(true); return; }
      await commit(result, false);
    } catch (e) { toast(errorText(e), "error"); }
    finally { setLoading(false); }
  };

  const continueNew = async () => {
    setLoading(true);
    try {
      const result = await api<CodexTakeoverPlan>(path);
      if (!result.can_resume) { setPlan(result); return; }
      launchAgent(continuationLaunch(result, session.machine_id, session.todo?.id));
    } catch (e) { toast(errorText(e), "error"); }
    finally { setLoading(false); }
  };

  const continueElsewhere = () => {
    if (!plan) return;
    launchAgent(continuationLaunch(plan, session.machine_id, session.todo?.id));
    setPlan(null); setConfirmStop(false);
  };

  if (!takeoverAvailable && !plan) return null;

  return <>
    {session.capabilities?.takeover?.can_resume && <Button size="sm" busy={loading} disabled={loading} onClick={() => void prepare()}>{t("chat-takeover.takeover")}</Button>}
    {takeoverAvailable && <Button size="sm" disabled={loading} onClick={() => void continueNew()}>{t("chat-takeover.takeover_new")}</Button>}
    <Dialog open={Boolean(plan && (confirmStop || !plan.can_resume))} onClose={() => { setPlan(null); setConfirmStop(false); }} title={t("chat-takeover.takeover_title")}
      footer={plan && !plan.can_resume
        ? <Button tone="secondary" onClick={continueElsewhere}>{t("chat-takeover.takeover_new")}</Button>
        : <><Button tone="quiet" onClick={() => { setPlan(null); setConfirmStop(false); }}>{t("chat-chatview.not_now")}</Button>
          {plan?.can_resume && <Button tone="accent" busy={busy} onClick={() => plan && void commit(plan, confirmStop)}>{t("chat-takeover.takeover_confirm")}</Button>}
          {plan && <Button tone="secondary" onClick={continueElsewhere}>{t("chat-takeover.takeover_new")}</Button>}</>}>
      {confirmStop ? <p>{t("chat-takeover.takeover_running_confirm")}</p> : <p>{takeoverReason(plan?.reason)}</p>}
    </Dialog>
  </>;
}

function takeoverReason(reason: string | undefined): string {
  switch (reason) {
    case "encrypted_context": return t("chat-takeover.reason_encrypted_context");
    case "unavailable": return t("chat-takeover.reason_unavailable");
    default: return t("chat-takeover.reason_unavailable");
  }
}
