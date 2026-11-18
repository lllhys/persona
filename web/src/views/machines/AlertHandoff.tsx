// "Hand work to another computer", the alert sheet's second step (#8037): which Agents (the working and idle ones that
// belong to a to-do are picked), which computer (the others, ranked and each with why: lib/machine-capabilities.ts
// rankTargets; the best eligible one is picked and marked, none when none is eligible), then a confirmation naming
// the source, the target and the Agents. Confirming sends one request to the assistant's main chat
// (lib/machine-handoff.ts), which starts the work on the target and stops the source Agents after.
import { useEffect, useMemo, useState } from "react";
import { t } from "../../i18n";
import { sessionTitle, stateOf } from "../../lib/format";
import { rankTargets } from "../../lib/machine-capabilities";
import { onToolsChange, requestTools, toolsOf } from "../../lib/machine-diagnosis";
import { defaultHandoff, handoffMessage, sendToPersona } from "../../lib/machine-handoff";
import type { Machine, MachineMetrics, Session } from "../../lib/types";
import { Button, cx, Dialog, StatePill, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

function useToolsTick(): number {
  const [n, setN] = useState(0);
  useEffect(() => onToolsChange(() => setN((x) => x + 1)), []);
  return n;
}

export function HandoffStep({ source, agents, machines, metrics, launcherIssues, onBack, onDone }: {
  source: Machine;
  agents: readonly Session[];
  machines: readonly Machine[];
  metrics: readonly MachineMetrics[] | null;
  launcherIssues: (mid: string) => number;
  onBack: () => void;
  onDone: () => void;
}) {
  const tick = useToolsTick();
  const [picked, setPicked] = useState<Set<string>>(() => new Set(defaultHandoff(agents).map((s) => s.session_id)));
  // the tools of the online computers, for the ranking (asked lazily, at most three at a time, kept 10 minutes)
  useEffect(() => {
    for (const m of [source, ...machines]) if (m.online) requestTools(m.id);
  }, [source, machines]);
  const known = (mid: string) => {
    const s = toolsOf(mid);
    return s?.state === "ok" ? s.tools : undefined;
  };
  const ranked = useMemo(
    () => rankTargets(source, machines.map((m) => ({ machine: m, metrics: metrics?.find((x) => x.machine_id === m.id), launcherIssues: launcherIssues(m.id), tools: known(m.id) })), { sourceTools: known(source.id) }),
    // tick: a computer's tools came in
    [source, machines, metrics, launcherIssues, tick],
  );
  const best = ranked.find((r) => r.eligible)?.machine.id ?? "";
  const [target, setTarget] = useState<string>(best);
  useEffect(() => {
    // the pick goes away when that computer is no longer eligible; a first eligible one is picked when there was none
    if (!ranked.some((r) => r.eligible && r.machine.id === target)) setTarget(best);
  }, [ranked, target, best]);
  const [asking, setAsking] = useState(false);
  const [sending, setSending] = useState(false);
  const chosen = agents.filter((s) => picked.has(s.session_id));
  const to = machines.find((m) => m.id === target);

  const toggle = (sid: string) => {
    const next = new Set(picked);
    if (next.has(sid)) next.delete(sid);
    else next.add(sid);
    setPicked(next);
  };
  const send = async () => {
    if (!to) return;
    setSending(true);
    try {
      const ok = await sendToPersona(handoffMessage(source, to, chosen), (text, tone, action) => toast(text, tone, action));
      setAsking(false);
      if (ok) onDone();
    } catch (e) {
      toast(String(e instanceof Error ? e.message : e), "error");
    } finally {
      setSending(false);
    }
  };

  return (
    <div data-testid="handoff-step">
      <button type="button" onClick={onBack} className="mb-2 inline-flex items-center gap-1 rounded-control text-meta text-ink-2 hover:text-ink">
        <Icon name="chevron" size={12} className="rotate-90" />
        {t("machines-alerts.handoff_back")}
      </button>
      <p className="text-meta leading-relaxed text-ink-2">{t("machines-alerts.handoff_intro", { machine: source.name })}</p>

      <h3 className="mt-4 mb-1.5 text-label font-medium text-ink-3">{t("machines-alerts.handoff_which")}</h3>
      {agents.length ? (
        <ul className="space-y-0.5" data-testid="handoff-agents">
          {agents.map((s) => {
            const needs = stateOf(s) === "needs_you";
            return (
              <li key={s.session_id}>
                <label className="flex cursor-pointer items-center gap-2 rounded-control px-1 py-1.5 hover:bg-hover" data-session={s.session_id}>
                  <input type="checkbox" className="size-4 shrink-0 accent-accent" checked={picked.has(s.session_id)} onChange={() => toggle(s.session_id)} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-meta text-ink">{sessionTitle(s)}</span>
                    <span className="block truncate text-submeta text-ink-3">{s.todo ? t("machines-alerts.handoff_todo", { title: s.todo.title }) : needs ? t("machines-alerts.handoff_needs_you") : t("machines-alerts.handoff_no_todo")}</span>
                  </span>
                  <StatePill state={stateOf(s)} />
                </label>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="text-meta text-ink-3">{t("machines-alerts.agents_none")}</p>
      )}

      <h3 className="mt-4 mb-1.5 text-label font-medium text-ink-3">{t("machines-alerts.handoff_where")}</h3>
      <div role="radiogroup" aria-label={t("machines-alerts.handoff_where")} className="space-y-1" data-testid="handoff-targets">
        {ranked.map((r) => (
          <label
            key={r.machine.id}
            className={cx("flex items-center gap-2.5 rounded-control px-2.5 py-2", r.eligible ? "cursor-pointer hover:bg-hover" : "opacity-60", target === r.machine.id && "bg-field")}
            data-target={r.machine.id}
            data-eligible={r.eligible}
          >
            <input type="radio" name="handoff-target" className="size-4 shrink-0 accent-accent" disabled={!r.eligible} checked={target === r.machine.id} onChange={() => setTarget(r.machine.id)} />
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-1.5">
                <span className="truncate text-meta font-medium text-ink">{r.machine.name}</span>
                {r.machine.id === best && <span className="shrink-0 rounded-chip bg-green-tint px-1.5 text-submeta text-green">{t("machines-alerts.handoff_best")}</span>}
              </span>
              <span className="block truncate text-submeta text-ink-3">{r.reason}</span>
            </span>
          </label>
        ))}
        {!ranked.length && <p className="text-meta text-ink-3">{t("machines-alerts.handoff_no_computers")}</p>}
        {ranked.length > 0 && !best && <p className="text-meta text-ink-2" role="status">{t("machines-alerts.handoff_none_eligible")}</p>}
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Button tone="primary" icon="forward" disabled={!to || !chosen.length} onClick={() => setAsking(true)} testId="handoff-next">
          {t("machines-alerts.handoff_send")}
        </Button>
        <span className="text-submeta text-ink-3">{t("machines-alerts.handoff_note")}</span>
      </div>

      <Dialog
        open={asking}
        onClose={() => !sending && setAsking(false)}
        title={t("machines-alerts.handoff_confirm_title")}
        footer={
          <>
            <Button tone="quiet" disabled={sending} onClick={() => setAsking(false)}>
              {t("common.cancel")}
            </Button>
            <Button tone="primary" busy={sending} onClick={() => void send()} testId="handoff-confirm">
              {t("machines-alerts.handoff_confirm")}
            </Button>
          </>
        }
      >
        <p className="text-meta leading-relaxed text-ink-2">{t("machines-alerts.handoff_confirm_body", { source: source.name, target: to?.name ?? "", count: chosen.length })}</p>
        <ul className="mt-2 space-y-1" data-testid="handoff-confirm-list">
          {chosen.map((s) => (
            <li key={s.session_id} className="flex items-center gap-2 text-meta">
              <span className="min-w-0 flex-1 truncate text-ink">{sessionTitle(s)}</span>
              {s.todo && <span className="max-w-[45%] shrink-0 truncate text-submeta text-ink-3">{s.todo.title}</span>}
            </li>
          ))}
        </ul>
        <p className="mt-2 text-submeta text-ink-3">{t("machines-alerts.handoff_confirm_note", { source: source.name })}</p>
      </Dialog>
    </div>
  );
}
