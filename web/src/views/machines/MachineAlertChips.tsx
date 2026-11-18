// A computer's alerts as small buttons on its card (#8037): icon + words, never color alone (the icon's color says how
// bad, the words say what, a screen reader hears the severity first). Each opens the alert sheet on that alert. Red
// only for a critical one. Real <button>s, so the card's own click (open the computer) leaves them alone.
import { alertLabel, alertSpoken, type AlertKind, type MachineAlert } from "../../lib/machine-alerts";
import { cx } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";

export const ALERT_ICON: Record<AlertKind, IconName> = {
  offline: "wifi",
  deploy_failed: "warning",
  memory: "layers",
  disk: "archive",
  cpu: "gauge",
  load: "activity",
  launcher: "terminal",
  mismatch: "retry",
};

export const SEVERITY_ICON_TONE = { critical: "text-red", warning: "text-orange", info: "text-ink-3" } as const;

/** `card`: on a computer's card (lib/machine-alerts.ts alertLabel). */
export function AlertChip({ alert, onOpen, selected, card, className }: { alert: MachineAlert; onOpen: (kind: AlertKind) => void; selected?: boolean; card?: boolean; className?: string }) {
  const spoken = alertSpoken(alert, card);
  return (
    <button
      type="button"
      onClick={() => onOpen(alert.kind)}
      data-alert={alert.kind}
      data-severity={alert.severity}
      aria-pressed={selected}
      aria-label={spoken}
      title={spoken}
      className={cx(
        "inline-flex h-6 shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-2 text-label font-medium transition-[background-color,box-shadow] duration-150",
        selected ? "bg-surface text-ink shadow-btn" : "bg-field text-ink enabled:hover:bg-hover",
        className,
      )}
    >
      <Icon name={ALERT_ICON[alert.kind]} size={12} className={SEVERITY_ICON_TONE[alert.severity]} />
      <span className="tabular-nums">{alertLabel(alert, card)}</span>
    </button>
  );
}

/** The card's row of chips; nothing when the computer has no alerts. */
export function AlertChips({ alerts, onOpen, className }: { alerts: readonly MachineAlert[]; onOpen: (kind: AlertKind) => void; className?: string }) {
  if (!alerts.length) return null;
  return (
    <div className={cx("mx-4 mt-2 flex flex-wrap gap-1.5", className)} data-testid="machine-alerts">
      {alerts.map((a) => (
        <AlertChip key={a.kind} alert={a} onOpen={onOpen} card />
      ))}
    </div>
  );
}
