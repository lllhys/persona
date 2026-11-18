// The pieces every section of a computer's alert sheet shares (#8037, MachineAlertSheet.tsx): the three parts in their
// fixed order (what is wrong, why, what you can do), the diagnosis read with its loading, error and "update" states,
// and a share bar. Tokens only.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { diagnosisFailure, fetchDiagnosis, retryable, type Diagnosis, type DiagnosisFailure, type DiagnosisSection } from "../../lib/machine-diagnosis";
import { isAbort } from "../../lib/api";
import { Button, cx, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export type Part = "what" | "why" | "do";

const PART_TITLE: Record<Part, () => string> = {
  what: () => t("machines-alerts.part_what"),
  why: () => t("machines-alerts.part_why"),
  do: () => t("machines-alerts.part_do"),
};

/** One of the sheet's three parts, under its small heading. */
export function SheetPart({ part, children, className }: { part: Part; children: ReactNode; className?: string }) {
  return (
    <section className={cx("mt-4 first:mt-1", className)} data-part={part}>
      <h3 className="mb-1.5 text-label font-medium text-ink-3">{PART_TITLE[part]()}</h3>
      {children}
    </section>
  );
}

/** A label and its value, the sheet's numbers. */
export function Figure({ label, value, detail, tone }: { label: string; value: string; detail?: string; tone?: "critical" | "warning" }) {
  return (
    <div className="min-w-0">
      <div className="text-submeta text-ink-3">{label}</div>
      <div className={cx("truncate text-card-title font-medium tabular-nums", tone === "critical" ? "text-red" : tone === "warning" ? "text-orange" : "text-ink")}>{value}</div>
      {detail && <div className="truncate text-submeta text-ink-3 tabular-nums" title={detail}>{detail}</div>}
    </div>
  );
}

/** A thin bar for a share of a whole (0…1), in the ink's shade; `tone` only where the share itself is the alert. */
export function ShareBar({ share, tone, label }: { share: number; tone?: "critical" | "warning"; label?: string }) {
  const pct = Math.max(0, Math.min(1, share)) * 100;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-field" role={label ? "img" : undefined} aria-label={label} aria-hidden={label ? undefined : true}>
      <div className={cx("h-full rounded-full", tone === "critical" ? "bg-red" : tone === "warning" ? "bg-orange" : "bg-ink-3")} style={{ width: `${pct}%` }} />
    </div>
  );
}

export type DiagnosisRead =
  | { state: "loading" }
  | { state: "ok"; data: Diagnosis }
  | { state: "failed"; failure: DiagnosisFailure };

/** The diagnosis of `mid` for `sections`, read when the sheet shows it; `reload` asks again. Only inside the sheet: the
 *  cards never wait for it. */
export function useDiagnosis(mid: string, sections: readonly DiagnosisSection[], on = true): { read: DiagnosisRead; reload: () => void } {
  const [read, setRead] = useState<DiagnosisRead>({ state: "loading" });
  const [tick, setTick] = useState(0);
  const key = sections.join(",");
  const keyRef = useRef(key);
  keyRef.current = key;
  useEffect(() => {
    if (!on || !mid) return;
    const ctl = new AbortController();
    setRead({ state: "loading" });
    fetchDiagnosis(mid, keyRef.current.split(",") as DiagnosisSection[], ctl.signal).then(
      (data) => setRead({ state: "ok", data }),
      (e) => {
        if (!isAbort(e)) setRead({ state: "failed", failure: diagnosisFailure(e) });
      },
    );
    return () => ctl.abort();
  }, [mid, key, on, tick]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { read, reload };
}

/** While the diagnosis loads: a line the height of a short answer, so nothing below it jumps much. */
export function DiagnosisLoading({ label }: { label: string }) {
  return (
    <p className="flex items-center gap-2 py-2 text-meta text-ink-3" role="status">
      <Spinner />
      {label}
    </p>
  );
}

/** Why there is no diagnosis, and what to do: try again, update the computer (with Redeploy when the hub can), update the
 *  hub, or use a fully trusted device. Everything else in the sheet still works. */
export function DiagnosisNote({ failure, onRetry, redeploy }: { failure: DiagnosisFailure; onRetry: () => void; redeploy?: ReactNode }) {
  const upgrade = failure.kind === "node_outdated" || failure.kind === "hub_outdated" || failure.kind === "forbidden";
  return (
    <div className={cx("flex flex-wrap items-center gap-2 rounded-control px-3 py-2 text-meta", upgrade ? "bg-field text-ink-2" : "bg-red-tint text-red")} data-diagnosis={failure.kind} role={upgrade ? "note" : "alert"}>
      <Icon name={upgrade ? "info" : "warning"} size={14} className="shrink-0" />
      <span className="min-w-0 flex-1 basis-48 break-words">{failure.text}</span>
      {retryable(failure) && (
        <Button size="sm" icon="retry" onClick={onRetry}>
          {t("machines-alerts.retry")}
        </Button>
      )}
      {failure.kind === "node_outdated" && redeploy}
    </div>
  );
}
