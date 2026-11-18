// Compare runs (`/compare/<id>`, #8916, docs/web/web.md "Compare runs"): the runs of one New agent start side by side, each
// in a worktree of its own. A card per run says how far it got (its worktree, its start), then, as a session, the sidebar's
// state mark and title, its branch and what it has changed since its branch left the main one; Open and Review changes
// go to it. Nothing here merges or deletes: keeping one result is the Git and worktree tools' job.
import { useEffect, useState } from "react";
import { errorText } from "../lib/api";
import { awaitsReply, justFinished } from "../lib/agent-groups";
import { runStatus, useCompareGroup, type CompareGroup, type CompareRun, type RunStatus } from "../lib/compare-runs";
import { retryRun, useRunner } from "../lib/compare-runner";
import { ago, backgroundWaiting, sessionTitle, shortPath, stateOf } from "../lib/format";
import { machineWithHub } from "../lib/mesh";
import { openPanel } from "../lib/panel";
import { navigate } from "../lib/router";
import { loadReview, REVIEW_FEATURE, reviewTotals } from "../lib/session-review";
import { STATE_LABEL, stateName } from "../lib/state-labels";
import { hubHas, useStore } from "../lib/store";
import type { Session } from "../lib/types";
import { Button, cx, IconButton, Spinner, useNow } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { StatusMark } from "../ui/StatusMark";
import { Empty } from "./assistant/parts";
import { t } from "../i18n";

export function CompareRuns({ id, onToggleSidebar }: { id: string; onToggleSidebar: () => void }) {
  const group = useCompareGroup(id);
  // the header's refresh reads every run's changes again
  const [reading, setReading] = useState(0);
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("assistant-history.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{t("compare-runs.title")}</h1>
        {group && <IconButton icon="retry" label={t("compare-runs.refresh")} onClick={() => setReading((n) => n + 1)} />}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[760px] px-4 py-5 sm:px-8 sm:py-6">
          {group ? <Runs group={group} reading={reading} /> : (
            <Empty icon="branch" title={t("compare-runs.not_found_title")} action={<Button tone="secondary" onClick={() => navigate({ name: "agent-list" })}>{t("compare-runs.go_agents")}</Button>}>
              {t("compare-runs.not_found_body")}
            </Empty>
          )}
        </div>
      </div>
    </div>
  );
}

function Runs({ group, reading }: { group: CompareGroup; reading: number }) {
  const machines = useStore((s) => s.machines);
  const sessions = useStore((s) => s.sessions);
  const launches = useStore((s) => s.launches);
  const mutes = useStore((s) => s.mutes);
  const reviews = useStore((s) => hubHas(s.features, REVIEW_FEATURE));
  const runner = useRunner(group.id);
  const now = useNow(60_000);
  const ctx = { sessions, launches, active: runner.active, holds: runner.holds };
  return (
    <>
      <p className="px-1 text-meta text-ink-2">
        <span className="font-medium text-ink">{t("compare-runs.summary", { count: group.runs.length, computer: machineWithHub(group.mid, machines) })}</span>
        {" · "}
        <span className="font-mono [overflow-wrap:anywhere]">{shortPath(group.cwd)}</span>
        {" · "}
        {ago(new Date(group.at).toISOString(), now)}
      </p>
      <p className="mt-1 mb-5 px-1 text-submeta leading-relaxed text-ink-3">{t("compare-runs.description")}</p>
      <div data-compare-runs className="grid gap-3 sm:grid-cols-2">
        {group.runs.map((run) => (
          <RunCard key={run.n} group={group} run={run} status={runStatus(run, group, ctx)} reviews={reviews} reading={reading}
            awaiting={(s) => awaitsReply(s, mutes)} finished={(s) => justFinished(s, mutes)} />
        ))}
      </div>
    </>
  );
}

function RunCard({ group, run, status, reviews, reading, awaiting, finished }: {
  group: CompareGroup; run: CompareRun; status: RunStatus; reviews: boolean; reading: number; awaiting: (s: Session) => boolean;
  finished: (s: Session) => boolean;
}) {
  const session = status.kind === "started" ? status.session : undefined;
  return (
    <section data-run={run.n} data-run-status={status.kind} aria-label={t("compare-runs.run", { n: run.n })}
      className="flex min-w-0 flex-col gap-2.5 rounded-card bg-surface p-4 shadow-card">
      <div className="flex items-center gap-2">
        <span className="shrink-0 text-label font-semibold tracking-wide text-ink-3 uppercase">{t("compare-runs.run", { n: run.n })}</span>
        <span className="min-w-0 flex-1" />
        <StatusLine status={status} awaiting={awaiting} finished={finished} />
      </div>
      <p className="truncate text-card-title font-semibold text-ink" title={session ? sessionTitle(session) : undefined}>
        {session ? sessionTitle(session) : t("compare-runs.not_started_title")}
      </p>
      <p className="flex min-w-0 items-center gap-1.5 text-label text-ink-3">
        <Icon name="branch" size={12} stroke={2.2} className="shrink-0" />
        <span className="truncate font-mono" title={run.branch}>{run.branch}</span>
      </p>
      {session && reviews && <Changes mid={group.mid} cwd={session.cwd} state={stateOf(session)} reading={reading} />}
      <Detail status={status} />
      <Actions group={group} run={run} status={status} reviews={reviews} />
    </section>
  );
}

/** The run's state in the sidebar's mark and words once it is a session; before that, how far its start got. */
function StatusLine({ status, awaiting, finished }: { status: RunStatus; awaiting: (s: Session) => boolean; finished: (s: Session) => boolean }) {
  if (status.kind === "started") {
    const s = status.session;
    const state = stateOf(s);
    const reply = awaiting(s);
    const done = !reply && finished(s);
    const bg = backgroundWaiting(s);
    return (
      <span className="flex shrink-0 items-center gap-1.5 text-label text-ink-2">
        <StatusMark state={state} awaiting={reply} finished={done} waiting={bg} label={false} />
        {reply ? stateName("awaits_reply") : done ? stateName("just_finished") : bg ? stateName("bg_waiting") : STATE_LABEL[state]}
      </span>
    );
  }
  const busy = status.kind === "worktree" || status.kind === "starting";
  return (
    <span className={cx("flex shrink-0 items-center gap-1.5 text-label", status.kind === "failed" ? "text-ink" : "text-ink-2")}>
      {busy && <Spinner size={11} />}
      {status.kind === "failed" && <Icon name="alertCircle" size={13} className="text-red" />}
      {phaseWords(status)}
    </span>
  );
}

function phaseWords(status: Exclude<RunStatus, { kind: "started" }>): string {
  switch (status.kind) {
    case "waiting":
      return t("compare-runs.waiting");
    case "worktree":
      return t("compare-runs.making_worktree");
    case "starting":
      return t("compare-runs.starting");
    case "elsewhere":
      return t("compare-runs.elsewhere");
    case "stopped":
      return t("compare-runs.stopped");
    case "failed":
      return stateName("failed");
  }
}

/** Why a run did not start, or what a stopped one needs. */
function Detail({ status }: { status: RunStatus }) {
  let text = "";
  if (status.kind === "failed") text = status.uncertain ? t("compare-runs.uncertain") : status.error;
  else if (status.kind === "stopped") text = status.retry ? t("compare-runs.stopped_retry") : t("compare-runs.stopped_gone");
  else if (status.kind === "elsewhere") text = t("compare-runs.elsewhere_hint");
  if (!text) return null;
  return <p role={status.kind === "failed" ? "alert" : undefined} className="text-label leading-snug text-ink-2 [overflow-wrap:anywhere]">{text}</p>;
}

function Actions({ group, run, status, reviews }: { group: CompareGroup; run: CompareRun; status: RunStatus; reviews: boolean }) {
  if (status.kind === "started") {
    const s = status.session;
    const open = () => navigate({ name: "session", mid: s.machine_id, sid: s.session_id });
    return (
      <div className="mt-auto flex flex-wrap gap-1.5 pt-1">
        <Button tone="secondary" icon="chat" onClick={open}>{t("compare-runs.open")}</Button>
        {reviews && (
          <Button tone="quiet" icon="fileDiff" onClick={() => {
            // the panel is the session's: it stays as the page arrives there (lib/panel-memory.ts)
            openPanel({ kind: "review", mid: s.machine_id, sid: s.session_id, cwd: s.cwd, scope: "branch" });
            open();
          }}>
            {t("compare-runs.review")}
          </Button>
        )}
      </div>
    );
  }
  const retry = (status.kind === "failed" && status.retry) || (status.kind === "stopped" && status.retry);
  // a start whose outcome is unknown is decided on its own page, where New agent's start shows it
  const launchPage = status.kind === "failed" && status.uncertain && run.launch;
  if (!retry && !launchPage) return null;
  return (
    <div className="mt-auto flex flex-wrap gap-1.5 pt-1">
      {retry && <Button tone="secondary" icon="retry" onClick={() => retryRun(group.id, run.n)}>{t("compare-runs.retry")}</Button>}
      {launchPage && <Button tone="secondary" icon="chat" onClick={() => navigate({ name: "session", mid: group.mid, sid: run.launch! })}>{t("compare-runs.open")}</Button>}
    </div>
  );
}

type ChangesRead = { files: number; added: number; deleted: number } | { error: string } | null;

/** What the run changed since its branch left the main one: its commits and what is not committed yet (the review's
 *  Branch scope). Read when the card shows, when the session's state moves (a turn ended) and on the header's refresh. */
function Changes({ mid, cwd, state, reading }: { mid: string; cwd: string; state: string; reading: number }) {
  const [read, setRead] = useState<ChangesRead>(null);
  useEffect(() => {
    const c = new AbortController();
    loadReview(mid, cwd, { scope: "branch" }, c.signal).then(
      (r) => !c.signal.aborted && setRead({ files: r.files.length, ...reviewTotals(r.files) }),
      (e) => !c.signal.aborted && setRead({ error: errorText(e) }),
    );
    return () => c.abort();
  }, [mid, cwd, state, reading]);
  let text: string;
  if (!read) text = t("compare-runs.changes_reading");
  else if ("error" in read) text = t("compare-runs.changes_failed");
  else if (!read.files) text = t("compare-runs.no_changes");
  else text = t("compare-runs.changes", { count: read.files, added: read.added, deleted: read.deleted });
  return (
    <p data-run-changes className="flex min-w-0 items-center gap-1.5 text-label text-ink-2 tabular-nums" title={read && "error" in read ? read.error : undefined}>
      <Icon name="fileDiff" size={12} stroke={2.2} className="shrink-0 text-ink-3" />
      <span className="truncate">{text}</span>
    </p>
  );
}
