// The Machines page's Capabilities view (#8037, docs/web/web.md "Machines"): one row per computer saying what it can
// take on: its state and alerts, system and architecture (and model), cores and memory, free disk, engines, the Agent
// runtimes its launchers start (each runtime with its named entries), the tools its node detected, and how the hub
// reaches it. A table where the page is wide enough, one card per computer where it is not (a container query, so
// the sidebar's width counts). Launchers and tools are read lazily for the online computers, at most three at a time,
// and kept 10 minutes (lib/machine-diagnosis.ts for the tools); the cards never wait for them.
import { useEffect, useMemo, useState } from "react";
import { t } from "../../i18n";
import { api, enc } from "../../lib/api";
import { bytesBin } from "../../lib/machine-detail";
import { capabilityRow, toolsText, type CapabilityRow, type ListState } from "../../lib/machine-capabilities";
import { onToolsChange, requestTools, toolsOf } from "../../lib/machine-diagnosis";
import type { AlertKind } from "../../lib/machine-alerts";
import { href, onLinkClick } from "../../lib/router";
import type { Launcher, Machine, MachineMetrics } from "../../lib/types";
import { cx } from "../../ui/atoms";
import { AlertChip } from "./MachineAlertChips";

// ── launchers, per computer: lazily, three at a time, kept 10 minutes ──

const LAUNCHERS_TTL_MS = 10 * 60_000;
const launchersKept = new Map<string, { at: number; value: Launcher[] | null }>();
const launchersAsked = new Set<string>();
const launchersQueue: string[] = [];
const launcherListeners = new Set<() => void>();
let launchersRunning = 0;

function pumpLaunchers() {
  while (launchersRunning < 3 && launchersQueue.length) {
    const mid = launchersQueue.shift()!;
    launchersRunning++;
    api<{ launchers?: Launcher[] }>(`/machines/${enc(mid)}/launchers`)
      .then((r) => r.launchers ?? [], () => null)
      .then((value) => {
        launchersKept.set(mid, { at: Date.now(), value });
        launchersAsked.delete(mid);
        launchersRunning--;
        launcherListeners.forEach((l) => l());
        pumpLaunchers();
      });
  }
}

function launchersOf(mid: string): Launcher[] | null | undefined {
  const kept = launchersKept.get(mid);
  if (kept && Date.now() - kept.at < LAUNCHERS_TTL_MS) return kept.value;
  if (!launchersAsked.has(mid)) {
    launchersAsked.add(mid);
    launchersQueue.push(mid);
    queueMicrotask(pumpLaunchers);
  }
  return kept?.value;
}

/** For tests: forget every computer's launchers. */
export function resetCapabilityCache() {
  launchersKept.clear();
  launchersAsked.clear();
  launchersQueue.length = 0;
  launchersRunning = 0;
}

function useCacheTick(): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    const bump = () => setN((x) => x + 1);
    launcherListeners.add(bump);
    const off = onToolsChange(bump);
    return () => {
      launcherListeners.delete(bump);
      off();
    };
  }, []);
  return n;
}

// seven columns from 66rem (their least widths, gaps and padding fit in 60rem); below that one card per computer
const COLS = "@min-[66rem]:grid-cols-[minmax(10rem,1.3fr)_minmax(7rem,1fr)_minmax(7rem,0.8fr)_minmax(6rem,0.8fr)_minmax(8rem,1.3fr)_minmax(9rem,1.4fr)_minmax(6rem,0.8fr)]";

export function MachineCapabilities({ machines, metrics, launcherIssues, onAlert }: {
  machines: Machine[];
  metrics: MachineMetrics[] | null;
  launcherIssues: (mid: string) => number;
  onAlert: (mid: string, kind: AlertKind) => void;
}) {
  const tick = useCacheTick();
  useEffect(() => {
    for (const m of machines) if (m.online) requestTools(m.id);
  }, [machines]);
  const rows = useMemo(
    () => machines.map((m) => capabilityRow({ machine: m, machines, metrics: metrics?.find((x) => x.machine_id === m.id), launcherIssues: launcherIssues(m.id), launchers: m.online ? launchersOf(m.id) : undefined, tools: m.online ? toolsOf(m.id) : undefined })),
    // tick: a computer's launchers or tools came in
    [machines, metrics, launcherIssues, tick],
  );
  const heads = [t("machines-capabilities.col_computer"), t("machines-capabilities.col_system"), t("machines-capabilities.col_resources"), t("machines-capabilities.col_engines"), t("machines-capabilities.col_runtimes"), t("machines-capabilities.col_tools"), t("machines-capabilities.col_reach")];
  return (
    <div className="@container" data-testid="capabilities">
      <div role="table" aria-label={t("machines-capabilities.title")} className="@min-[66rem]:overflow-hidden @min-[66rem]:rounded-card @min-[66rem]:bg-surface @min-[66rem]:shadow-card">
        <div role="row" className={cx("hidden gap-3 border-b border-line px-4 py-2 text-submeta font-medium text-ink-3 @min-[66rem]:grid", COLS)}>
          {heads.map((h) => (
            <span key={h} role="columnheader">{h}</span>
          ))}
        </div>
        <div className="grid grid-cols-1 gap-3 @min-[36rem]:grid-cols-2 @min-[66rem]:block @min-[66rem]:divide-y @min-[66rem]:divide-line">
          {rows.map((r) => (
            <Row key={r.id} row={r} heads={heads} onAlert={onAlert} />
          ))}
        </div>
      </div>
    </div>
  );
}

function Row({ row: r, heads, onAlert }: { row: CapabilityRow; heads: string[]; onAlert: (mid: string, kind: AlertKind) => void }) {
  const detail = { name: "machine" as const, mid: r.id };
  const presence = r.presence === "online" ? t("machines-capabilities.online") : r.presence === "pending" ? t("machines-capabilities.pending") : r.presence === "not_connected" ? t("machines-capabilities.not_connected") : t("machines-capabilities.offline");
  const resources = [r.cores ? t("machines-capabilities.cores", { count: r.cores }) : "", r.memory ? bytesBin(r.memory).replace(/\.00 /, " ") : ""].filter(Boolean).join(" · ");
  return (
    <div role="row" data-row={r.id} className={cx("grid gap-x-3 gap-y-2 rounded-card bg-surface px-4 py-3 shadow-card @min-[66rem]:rounded-none @min-[66rem]:shadow-none", COLS)}>
      <Cell label={heads[0]} wide>
        <a href={href(detail)} onClick={(e) => onLinkClick(e, detail)} className="block truncate text-meta font-semibold text-ink hover:underline">{r.name}</a>
        <span className={cx("mt-0.5 inline-flex items-center gap-1 text-submeta", r.presence === "online" ? "text-green" : "text-ink-3")}>
          <span className={cx("size-1.5 rounded-full", r.presence === "online" ? "bg-green" : "bg-ink-3")} aria-hidden />
          {presence}
        </span>
        {r.alerts.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-1">
            {r.alerts.map((a) => (
              <AlertChip key={a.kind} alert={a} onOpen={(kind) => onAlert(r.id, kind)} />
            ))}
          </div>
        )}
      </Cell>
      <Cell label={heads[1]}>
        <span className="block text-meta text-ink">{r.system || "—"}</span>
        {r.model && <span className="block truncate text-submeta text-ink-3">{r.model}</span>}
      </Cell>
      <Cell label={heads[2]}>
        <span className="block text-meta text-ink tabular-nums">{resources || "—"}</span>
        {r.diskFree !== undefined && <span className="block text-submeta text-ink-3 tabular-nums">{t("machines-capabilities.disk_free", { size: bytesBin(r.diskFree) })}</span>}
      </Cell>
      <Cell label={heads[3]}>
        <span className="block text-meta text-ink">{r.engines.length ? r.engines.join(" · ") : "—"}</span>
      </Cell>
      <Cell label={heads[4]}>
        <ListCell state={r.runtimes} render={(groups) => groups.length ? (
          <ul className="space-y-0.5">
            {groups.map((g) => (
              <li key={g.id} className="text-meta">
                <span className="text-ink">{g.name}</span>
                {g.entries.length > 0 && <span className="text-ink-3"> · {g.entries.join(t("machines-capabilities.list_separator"))}</span>}
              </li>
            ))}
          </ul>
        ) : <span className="text-meta text-ink-3">{t("machines-capabilities.none")}</span>} />
      </Cell>
      <Cell label={heads[5]}>
        <ListCell state={r.tools} render={(tools) => <span className="block text-meta text-ink">{tools.length ? toolsText(tools) : t("machines-capabilities.none")}</span>} />
      </Cell>
      <Cell label={heads[6]}>
        <span className="block text-meta text-ink-2">{r.reach || "—"}</span>
      </Cell>
    </div>
  );
}

function Cell({ label, children, wide }: { label: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <div role="cell" className={cx("min-w-0", wide && "border-b border-line pb-2 @min-[66rem]:border-0 @min-[66rem]:pb-0")}>
      {!wide && <span className="block text-submeta text-ink-3 @min-[66rem]:hidden">{label}</span>}
      {children}
    </div>
  );
}

function ListCell<T>({ state, render }: { state: ListState<T>; render: (value: T) => React.ReactNode }) {
  if (state.state === "loading") return <span className="block text-meta text-ink-3" aria-busy="true">{t("machines-capabilities.loading")}</span>;
  if (state.state === "unavailable") return <span className="block text-submeta text-ink-3">{state.text}</span>;
  return <>{render(state.value)}</>;
}
