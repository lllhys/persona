// The alert sheet's section for a computer that is not connected (#8037): why, in our words (the hub's error_code, else
// what its error says: lib/node-health.ts), when it was last seen, a numbered checklist for that reason, the hub's raw
// error under Details, and Reconnect (the card's own call); Redeploy when the reason is the version and the hub can
// update it.
import { useState } from "react";
import { t } from "../../i18n";
import { machineErrorText } from "../../lib/add-computer";
import type { OfflineReason } from "../../lib/machine-alerts";
import { machineDeployment } from "../../lib/machine-deploy";
import { classifyConnection } from "../../lib/node-health";
import { relativeTime } from "../../lib/relative-time";
import type { Machine } from "../../lib/types";
import { Button, Collapse } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { SheetPart } from "./AlertParts";

/** How many checklist lines each reason has (machines-alerts.check_<reason>_<n>). */
const CHECKS: Record<Exclude<OfflineReason, "node_outdated">, number> = {
  auth: 3, unreachable: 4, timeout: 3, version: 3, node_service_down: 3, link_offline: 3, never_connected: 2, unknown: 4,
};

export function checklist(reason: OfflineReason): string[] {
  const r = reason === "node_outdated" ? "version" : reason;
  return Array.from({ length: CHECKS[r] }, (_, i) => t(`machines-alerts.check_${r}_${i + 1}`));
}

export function OfflineSection({ machine, reason, onReconnect, onRedeploy }: { machine: Machine; reason: OfflineReason; onReconnect: (id: string) => Promise<void> | void; onRedeploy: () => void }) {
  const [details, setDetails] = useState(false);
  const [busy, setBusy] = useState(false);
  const failure = classifyConnection({ ...machine, online: false });
  const title = machineErrorText(machine.error_code) || failure?.title || t("machines-alerts.offline_unknown");
  const seen = relativeTime(machine.last_seen);
  const deployment = machineDeployment(machine);
  const version = reason === "version" || reason === "node_outdated";
  const reconnect = async () => {
    setBusy(true);
    try {
      await onReconnect(machine.id);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <SheetPart part="what">
        <p className="text-card-title font-medium text-ink" data-reason={reason}>{title}</p>
        {failure?.next && <p className="mt-0.5 text-meta text-ink-2">{failure.next}</p>}
        <p className="mt-1 text-submeta text-ink-3">{seen ? t("machines-alerts.last_seen", { time: seen }) : t("machines-alerts.last_seen_unknown")}</p>
      </SheetPart>
      <SheetPart part="why">
        <ol className="space-y-1.5" data-testid="offline-checklist">
          {checklist(reason).map((line, i) => (
            <li key={i} className="flex gap-2 text-meta leading-relaxed text-ink">
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-field text-submeta text-ink-2 tabular-nums">{i + 1}</span>
              <span className="min-w-0 flex-1">{line}</span>
            </li>
          ))}
        </ol>
        {machine.error && (
          <div className="mt-2">
            <button type="button" className="inline-flex items-center gap-1 rounded-control text-submeta text-ink-3 hover:text-ink" aria-expanded={details} onClick={() => setDetails(!details)}>
              <Icon name="chevron" size={11} className={details ? "transition-transform" : "-rotate-90 transition-transform"} />
              {t("machines-alerts.details")}
            </button>
            <Collapse open={details}>
              <pre className="mt-1 whitespace-pre-wrap break-words rounded-control bg-field px-2.5 py-2 font-mono text-submeta text-ink-2" data-testid="raw-error">{machine.error}</pre>
            </Collapse>
          </div>
        )}
      </SheetPart>
      <SheetPart part="do">
        <div className="flex flex-wrap gap-2">
          <Button tone="primary" icon="retry" busy={busy} onClick={() => void reconnect()} testId="reconnect">
            {t("machines.reconnect")}
          </Button>
          {version && deployment.supported && (
            <Button tone="secondary" disabled={!deployment.canDeploy} title={deployment.reason || undefined} onClick={onRedeploy} testId="offline-redeploy">
              {t("lib-machine-deploy.redeploy")}
            </Button>
          )}
        </div>
        {version && deployment.reason && <p className="mt-1.5 text-submeta text-ink-3">{deployment.reason}</p>}
      </SheetPart>
    </>
  );
}
