// A subagent the agent launched, in the chat where it launched it (#5602, after Claude Desktop's subagent row): its
// title, then how it is going in one line: while it runs what it is doing now (its latest call), once it ended "Done ·
// N steps". Open, what it was asked, what it reported (a background one's report, which came back as a notice, too),
// its latest steps as they come (it follows the subagent's own transcript while open) and the subagents it launched.
// Its whole conversation opens read-only in the side panel, the chat staying where it is.
import { memo, useRef } from "react";
import { openPanel } from "../../lib/panel";
import { agentSummary, isLaunchReceipt, nestedSubagents, subagentEnded, subagentFor, subagentTitle, taskPill } from "../../lib/tasks";
import { segments } from "../../lib/turns";
import type { Item, Subagent, TaskNote } from "../../lib/types";
import { tsMs, workDuration, type AgentStatus, type ToolStep } from "../../lib/work-groups";
import { Collapse, cx, Shimmer, Spinner, useNow } from "../../ui/atoms";
import { StatusMark } from "../../ui/StatusMark";
import { Icon } from "../../ui/icons";
import { ActionRow } from "./ActionRow";
import { useChatContext } from "./chat-context";
import { useExpanded } from "./expanded";
import { useTranscript } from "./useTranscript";
import { t } from "../../i18n";
import { ScrollFold } from "./fold-scroll";
import { Markdown } from "../../ui/Markdown";
import { FoldedReport } from "./FoldedReport";
import { useFullText } from "./FullText";

const STATUS: Record<AgentStatus, string> = {
  running: t("chat-subagentcard.status_running"),
  completed: t("chat-subagentcard.status_completed"),
  failed: t("chat-subagentcard.status_failed"),
  stopped: t("chat-subagentcard.status_stopped"),
};
/** How many of its latest steps the open card shows. */
const RECENT_STEPS = 3;

const ms = (ts?: string) => tsMs(ts) ?? undefined;

/** Opens a subagent's conversation in the side panel (SidePanel reads an "agent" panel on a `<parent>@<agent>` id as one). */
function openSubagent(mid: string, a: Subagent) {
  openPanel({ kind: "agent", mid, sid: a.session_id });
}

/** What a background subagent reported: its notice's words (the call's own result is only the launch receipt). */
export function reportOf(result: Item | undefined, notes: TaskNote[] | undefined): string {
  if (!isLaunchReceipt(result)) return "";
  const done = [...(notes ?? [])].reverse().find((n) => n.kind === "agent" && n.detail?.trim());
  return done?.detail?.trim() ?? "";
}

/** The line under the title: what it is doing now while it runs, its outcome and how many steps once it ended. */
export function subagentLine(status: AgentStatus, meta: Subagent | undefined): { text: string; live: boolean } {
  const steps = meta?.steps ? t("chat-subagentcard.steps", { count: meta.steps }) : "";
  if (status !== "running") return { text: [STATUS[status], steps].filter(Boolean).join(" · "), live: false };
  const doing = meta?.last_tool?.summary?.trim() || meta?.last_tool?.name || "";
  return { text: doing || t("chat-subagentcard.starting"), live: true };
}

export const SubagentCard = memo(function SubagentCard({ step, status, mid, sid, scope = "" }: { step: ToolStep; status: AgentStatus; mid: string; sid?: string; scope?: string }) {
  const { subagents, tasks } = useChatContext();
  const { use, result } = step;
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const meta = subagentFor(subagents, use);
  const [open, setOpen] = useExpanded(`${scope}task:${use.id}`);
  const running = status === "running";
  const now = useNow(1000, running);
  const title = (typeof inp.description === "string" && inp.description.trim()) || (meta ? subagentTitle(meta) : "") || t("chat-subagentcard.untitled_task");
  const prompt = typeof inp.prompt === "string" ? inp.prompt.trim() : "";
  const reports = use.tool_use_id ? tasks.reports.get(use.tool_use_id) ?? [] : [];
  const summary = reports.length || result?.task?.kind === "agent" ? "" : reportOf(result, use.tool_use_id ? tasks.notes.get(use.tool_use_id) : undefined) || agentSummary(result, meta);
  const type = (typeof inp.subagent_type === "string" && inp.subagent_type) || meta?.type || "";
  const start = ms(meta?.started_at) ?? ms(use.ts);
  const end = running ? now : subagentEnded(result, meta);
  const took = start !== undefined && end !== undefined && end >= start ? workDuration(end - start) : "";
  const nested = meta ? nestedSubagents(subagents, meta.id) : [];
  const line = subagentLine(status, meta);
  // the dot pulses for a call that came while the card was on screen, not for the one it found when it was drawn
  const toolId = meta?.last_tool?.id ?? "";
  const toolAtMount = useRef(toolId);
  return (
    <div className="flex w-full min-w-0" data-subagent-card={status} data-tool-use={use.tool_use_id}>
      <div className="w-full min-w-0 rounded-xl border border-line bg-surface px-3 py-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          aria-label={open ? t("chat-subagentcard.collapse_details") : t("chat-subagentcard.expand_details")}
          className="flex w-full min-w-0 items-start gap-2 rounded-lg text-left [@media(pointer:coarse)]:min-h-10"
        >
          <span className="relative mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-field text-ink-2">
            <Icon name="agent" size={14} />
            {running && (
              <span
                key={toolId}
                aria-hidden
                className={cx("absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-accent ring-2 ring-surface", toolId !== toolAtMount.current && "animate-[pop-in_0.3s_ease-out]")}
              />
            )}
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex min-w-0 items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-[length:var(--chat-secondary)] leading-[1.428571] font-medium text-ink">{title}</span>
              {took && <span className="shrink-0 text-[12px] leading-[1.333333] whitespace-nowrap text-ink-3 tabular-nums">{took}</span>}
            </span>
            <span
              className={cx("mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] leading-[1.333333]", status === "failed" ? "text-red" : "text-ink-2")}
              data-subagent-line
            >
              {running ? <Spinner size={11} /> : <Icon name={status === "failed" ? "info" : status === "stopped" ? "stop" : "check"} size={12} />}
              {line.live ? <Shimmer className="min-w-0 truncate">{line.text}</Shimmer> : <span className="min-w-0 truncate">{line.text}</span>}
            </span>
            <span className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] leading-[1.333333] text-ink-3">
              <span>{t("chat-subagentcard.subagent")}</span>
              {type && <span className="inline-flex items-center rounded-full bg-field px-1.5 py-0.5">{meta?.fork ? t("chat-subagentcard.fork") : type}</span>}
              {meta?.background && <span>{t("chat-subagentcard.background")}</span>}
            </span>
          </span>
          <Icon name="chevronRight" size={14} stroke={2.2} className="mt-1 shrink-0 text-ink-3 transition-transform duration-150 motion-reduce:transition-none" style={{ transform: open ? "rotate(90deg)" : "none" }} />
        </button>
        <Collapse open={open} lazy>
          <div className="mt-2 flex flex-col gap-1.5 border-l-2 border-tool-rail pl-3 text-[13px] leading-5 text-ink-2">
            {prompt && <ScrollFold className="max-h-[6lh] whitespace-pre-wrap" data-subagent-prompt>{prompt}</ScrollFold>}
            {summary && (
              <div className="flex flex-col gap-0.5" data-subagent-summary>
                {!running && <span className="text-[12px] text-ink-3">{t("chat-subagentcard.result")}</span>}
                <FoldedReport id={`task:${use.id}`} text={summary}>
                  <Markdown text={summary} small mid={mid} />
                </FoldedReport>
              </div>
            )}
            {reports.map((item) => <SubagentReport key={item.id} item={item} mid={mid} sid={sid} scope={scope} />)}
            {meta && open && <RecentSteps mid={mid} agent={meta} running={running} />}
            {nested.length > 0 && (
              <div className="flex flex-col" data-subagent-nested>
                {nested.map(({ agent: a, depth }) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => openSubagent(mid, a)}
                    className="flex min-w-0 items-center gap-2 rounded-[8px] py-1 pr-2 text-left transition-colors hover:bg-tool-hover"
                    style={{ paddingLeft: 8 + depth * 16 }}
                  >
                    {depth > 0 && <span aria-hidden className="h-2 w-2.5 shrink-0 rounded-bl-[4px] border-b border-l border-line-strong" />}
                    <StatusMark state={a.running ? "working" : "unread"} size="sm" />
                    <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{subagentTitle(a)}</span>
                    <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3">{a.running ? t("chat-subagentcard.status_running") : t("chat-subagentcard.ended")}</span>
                  </button>
                ))}
              </div>
            )}
            {meta && (
              <button
                type="button"
                onClick={() => openSubagent(mid, meta)}
                className="mt-0.5 flex w-fit items-center gap-1 rounded-[6px] py-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline"
                data-subagent-open
              >
                {t("chat-subagentcard.view_full")}
                <Icon name="chevronRight" size={12} stroke={2.2} />
              </button>
            )}
          </div>
        </Collapse>
      </div>
    </div>
  );
});

/** One report keeps its own disclosure and full-output identity, including after another report arrives. */
function SubagentReport({ item, mid, sid, scope }: { item: Item; mid: string; sid?: string; scope: string }) {
  const full = useFullText(item.kind === "tool_result" ? item : undefined, mid, sid);
  const text = item.kind === "tool_result" ? full.text : item.task?.detail ?? "";
  const pill = item.task ? taskPill(item.task) : null;
  return (
    <div className="flex min-w-0 flex-col gap-0.5" data-subagent-summary data-items={item.id}>
      <span className="text-xs text-ink-3">{pill?.text ?? (item.task?.status === "running" ? t("chat-subagentcard.status_running") : t("chat-subagentcard.result"))}</span>
      <FoldedReport id={`${scope}task-report:${item.id}`} text={text}>
        <Markdown text={text} small mid={mid} />
        {full.more}
      </FoldedReport>
    </div>
  );
}

/** The subagent's latest few tool calls while the card is open: it follows the subagent's own transcript (the hub
 *  pushes what it writes), instead of one fetch that went stale (#5602). */
function RecentSteps({ mid, agent, running }: { mid: string; agent: Subagent; running: boolean }) {
  const tr = useTranscript(mid, agent.session_id, { updatedAt: agent.updated_at });
  const steps = lastTools(tr.items, RECENT_STEPS);
  if (tr.loading && !steps.length) return <Spinner size={11} />;
  if (!steps.length) return null;
  return (
    <div className="-ml-2 flex flex-col" data-subagent-recent>
      {steps.map((s) => (
        <ActionRow key={s.id} step={s} running={!s.result && running} mid={mid} sid={agent.session_id} />
      ))}
    </div>
  );
}

function lastTools(items: Item[], n: number): ToolStep[] {
  return segments(items)
    .flatMap((s) => s.steps)
    .filter((s): s is ToolStep => s.kind === "tool")
    .slice(-n);
}
