// The todo detail's stage bar (#2776): Plan → Develop → Review → Merge → Deploy → Verified with the current step marked, drawn
// in Beautiful UI's language rather than the Feishu report's emoji: passed steps are green with a check, the current
// one is the accent color, the ones ahead are hollow. Labels take the page's ink tokens, so both themes read the same.
import { t } from "../i18n";
import { TODO_STAGES, todoStageIndex, todoStageState, type TodoStage, type TodoStageState } from "../lib/todo-stage";
import { cx } from "../ui/atoms";
import { Icon } from "../ui/icons";

const STAGE_LABEL: Record<TodoStage, string> = {
  plan: t("todos.stage_plan"),
  develop: t("todos.stage_develop"),
  review: t("todos.stage_review"),
  merge: t("todos.stage_merge"),
  deploy: t("todos.stage_deploy"),
  verify: t("todos.stage_verify"),
};

const DOT: Record<TodoStageState, string> = {
  done: "bg-green-tint text-green",
  current: "bg-accent text-white ring-2 ring-accent-tint",
  ahead: "border border-line-strong bg-surface",
};

const LABEL: Record<TodoStageState, string> = {
  done: "text-ink-2",
  current: "font-semibold text-ink",
  ahead: "text-ink-2",
};

/** The connector from a step to its neighbour: colored once the neighbour is reached. */
function connector(state: TodoStageState, side: "before" | "after") {
  const reached = side === "before" ? state !== "ahead" : state === "done";
  return cx("h-px flex-1", reached ? "bg-green/50" : "bg-line-strong");
}

/** PR/MR links allow only http(s); merge_requests paths use MR !N, while other labels keep their existing form. */
export function prLink(raw?: string): { url: string; text: string; label: string } | null {
  const url = raw?.trim() ?? "";
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return null;
  }
  if (u.protocol !== "https:" && u.protocol !== "http:") return null;
  const mr = /\/merge_requests\/(\d+)\/?$/.exec(u.pathname)?.[1];
  const n = /\/(\d+)\/?$/.exec(u.pathname)?.[1];
  const text = mr ? t("todos.stage_mr_n", { n: mr }) : n ? t("todos.stage_pr_n", { n }) : t("todos.stage_pr");
  return { url, text, label: t("todos.stage_pr_label", { pr: text, host: u.host }) };
}

export function TodoStageBar({ stage, stageKind, prURL, className = "" }: { stage?: string; stageKind?: string; prURL?: string; className?: string }) {
  const current = stageKind === "general" ? -1 : todoStageIndex(stage);
  const pr = prLink(prURL);
  if (current < 0 && !pr) return null;
  return (
    <div className={cx("flex flex-col gap-2", className)}>
      {current >= 0 && (
        <ol aria-label={t("todos.stage_label")} className="flex w-full items-start">
          {TODO_STAGES.map((s, i) => {
            const state = todoStageState(i, current);
            return (
              <li
                key={s}
                data-stage={s}
                data-state={state}
                aria-current={state === "current" ? "step" : undefined}
                className="flex min-w-0 flex-1 flex-col items-center gap-1.5"
              >
                <span className="flex w-full items-center">
                  <span aria-hidden className={cx(i === 0 && "opacity-0", connector(state, "before"))} />
                  <span
                    className={cx(
                      "relative flex size-4 shrink-0 items-center justify-center rounded-full",
                      DOT[state],
                    )}
                  >
                    {state === "done" ? (
                      <Icon name="check" size={9} stroke={3.2} />
                    ) : state === "current" ? (
                      <span aria-hidden className="size-1.5 rounded-full bg-white" />
                    ) : null}
                  </span>
                  <span aria-hidden className={cx(i === TODO_STAGES.length - 1 && "opacity-0", connector(state, "after"))} />
                </span>
                <span className={cx("max-w-full truncate text-[11px] leading-4 whitespace-nowrap", LABEL[state])}>
                  {STAGE_LABEL[s]}
                </span>
              </li>
            );
          })}
        </ol>
      )}
      {pr && (
        <a
          href={pr.url}
          target="_blank"
          rel="noreferrer noopener"
          aria-label={pr.label}
          className="inline-flex items-center gap-1 self-start whitespace-nowrap text-[12px] font-medium text-accent-ink hover:underline"
        >
          <Icon name="external" size={11} />
          {pr.text}
        </a>
      )}
    </div>
  );
}
