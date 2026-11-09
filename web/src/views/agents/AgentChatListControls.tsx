import { memo, useMemo, useState } from "react";
import { t } from "../../i18n";
import { flipPin } from "../../lib/agent-actions";
import {
  buildEvidenceSnippet,
  hasActiveDimensionFilters,
  SEARCH_SCOPES,
  sessionHarnessLabel,
  sessionHasError,
  sessionModelId,
  sessionProjectName,
  toggleFacetValue,
  type AgentDimensionFilters,
  type AgentFacetCounts,
  type EvalContext,
  type FacetOption,
  type SearchPlan,
  type SearchScope,
} from "../../lib/agent-chatlist-filter";
import { awaitsReply, sessionSource, sourceName, type AgentMutes } from "../../lib/agent-groups";
import { ago, backgroundWaiting, sessionPreview, sessionTitle, stateOf } from "../../lib/format";
import { machineWithHub } from "../../lib/mesh";
import { plainMarkdown } from "../../lib/plain-markdown";
import { href, type Route } from "../../lib/router";
import { draftLine, rowLine } from "../../lib/row-line";
import { useLeftDraft } from "../../lib/drafts";
import { rememberSessionRow, useStore } from "../../lib/store";
import type { Session } from "../../lib/types";
import { ultracodeMarkShown } from "../../lib/ultracode";
import { SessionMark } from "../../ui/AgentMark";
import { cx, Menu, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { contextMenuProps } from "../../ui/context-menu";
import { Icon, type IconName } from "../../ui/icons";
import { SearchField } from "../../ui/SearchField";
import { StatusMark } from "../../ui/StatusMark";
import { UltraMark } from "../chat/Ultracode";
import { CURRENT_ROW, type GoFn, type RegFn, type RowMenuFn } from "../SidebarParts";

export function AgentDimensionFilterBar({
  filters,
  onChange,
  facets,
  plan,
  chatList,
  onToggleChatList,
}: {
  filters: AgentDimensionFilters;
  onChange: (next: AgentDimensionFilters) => void;
  facets: AgentFacetCounts;
  plan: SearchPlan;
  chatList: boolean;
  onToggleChatList: () => void;
}) {
  const scopeMenu = useMenu();
  const harnessMenu = useMenu();
  const projectMenu = useMenu();
  const modelMenu = useMenu();
  const machineMenu = useMenu();
  const [editingPlan, setEditingPlan] = useState(false);

  const activeScope = SEARCH_SCOPES.find((s) => s.key === filters.scope) ?? SEARCH_SCOPES[0];
  const hasActive = hasActiveDimensionFilters(filters);

  const resetAll = () => {
    onChange({
      query: "",
      scope: "all",
      harnesses: [],
      projects: [],
      models: [],
      machines: [],
      pinnedOnly: false,
      since: undefined,
      searchAsCode: false,
      customPlanCode: undefined,
    });
    setEditingPlan(false);
  };

  const buildFacetMenuItems = (
    options: FacetOption[],
    selected: string[],
    onUpdate: (next: string[]) => void,
  ): MenuItem[] => {
    const allIds = options.map((o) => o.id);
    return [
      {
        key: "__all__",
        label: t("lib-agent-filter.all"),
        checked: selected.length === 0,
        keepOpen: true,
        onSelect: () => onUpdate([]),
      },
      ...options.map((o, idx) => ({
        key: o.id,
        label: o.label,
        hint: String(o.count),
        divider: idx === 0,
        checked: selected.includes(o.id),
        keepOpen: true,
        onSelect: () => onUpdate(toggleFacetValue(selected, o.id, allIds)),
      })),
    ];
  };

  const activeChips: { key: string; label: string; onRemove: () => void }[] = [];
  if (filters.query.trim()) {
    activeChips.push({
      key: "query",
      label: `“${filters.query.trim()}”`,
      onRemove: () => onChange({ ...filters, query: "" }),
    });
  }
  if (filters.searchAsCode) {
    activeChips.push({
      key: "search-as-code",
      label: t("lib-agent-filter.search_as_code"),
      onRemove: () => onChange({ ...filters, searchAsCode: false, customPlanCode: undefined }),
    });
  }
  if (filters.scope !== "all") {
    activeChips.push({
      key: `scope:${filters.scope}`,
      label: `${t("lib-agent-filter.search_in")}: ${t(activeScope.labelKey)}`,
      onRemove: () => onChange({ ...filters, scope: "all" }),
    });
  }
  if (filters.pinnedOnly) {
    activeChips.push({
      key: "pinned",
      label: t("lib-agent-filter.filter_pinned"),
      onRemove: () => onChange({ ...filters, pinnedOnly: false }),
    });
  }
  for (const h of filters.harnesses) {
    const label = facets.harnesses.find((x) => x.id === h)?.label ?? h;
    activeChips.push({
      key: `harness:${h}`,
      label: `${t("lib-agent-filter.filter_harness")}: ${label}`,
      onRemove: () => onChange({ ...filters, harnesses: filters.harnesses.filter((x) => x !== h) }),
    });
  }
  for (const p of filters.projects) {
    activeChips.push({
      key: `project:${p}`,
      label: `${t("lib-agent-filter.filter_project")}: ${p}`,
      onRemove: () => onChange({ ...filters, projects: filters.projects.filter((x) => x !== p) }),
    });
  }
  for (const m of filters.models) {
    activeChips.push({
      key: `model:${m}`,
      label: `${t("lib-agent-filter.filter_model")}: ${m}`,
      onRemove: () => onChange({ ...filters, models: filters.models.filter((x) => x !== m) }),
    });
  }
  for (const mc of filters.machines) {
    const label = facets.machines.find((x) => x.id === mc)?.label ?? mc;
    activeChips.push({
      key: `machine:${mc}`,
      label: `${t("lib-agent-filter.filter_machine")}: ${label}`,
      onRemove: () => onChange({ ...filters, machines: filters.machines.filter((x) => x !== mc) }),
    });
  }
  if (filters.since) {
    activeChips.push({
      key: `since:${filters.since}`,
      label: `since:${filters.since}`,
      onRemove: () => onChange({ ...filters, since: undefined }),
    });
  }

  return (
    <div className="mt-1.5 flex flex-col gap-1.5 px-1" data-testid="agent-dimension-filter-bar">
      {/* Row 1: Search input + Search in scope selector + Search as Code button + Chat-list view toggle */}
      <div className="flex items-center gap-1">
        <SearchField
          tone="inset"
          size="sm"
          className="min-w-0 flex-1"
          value={filters.query}
          onChange={(q) => onChange({ ...filters, query: q })}
          placeholder={t("lib-agent-filter.search_placeholder")}
        />
        <button
          type="button"
          onClick={scopeMenu.open}
          aria-haspopup="menu"
          aria-expanded={Boolean(scopeMenu.anchor)}
          aria-label={t("lib-agent-filter.search_in")}
          title={`${t("lib-agent-filter.search_in")}: ${t(activeScope.labelKey)}`}
          data-testid="agent-search-scope-button"
          className={cx(
            "touch-hit relative z-10 flex h-8 shrink-0 items-center gap-1 rounded-control px-2 text-[11.5px] shadow-hairline transition-colors",
            filters.scope !== "all" ? "bg-accent-tint font-medium text-accent-ink" : "bg-inset text-ink-2 hover:bg-hover hover:text-ink",
          )}
        >
          <Icon name="filter" size={11} stroke={2} />
          <span className="max-w-[72px] truncate">{t(activeScope.labelKey)}</span>
          <Icon name="chevron" size={10} stroke={2.2} className="text-ink-3" />
        </button>
        <button
          type="button"
          aria-pressed={filters.searchAsCode}
          aria-label={t("lib-agent-filter.search_as_code")}
          title={t("lib-agent-filter.search_as_code")}
          data-testid="agent-search-as-code-toggle"
          onClick={() => onChange({ ...filters, searchAsCode: !filters.searchAsCode })}
          className={cx(
            "touch-hit relative z-10 flex h-8 shrink-0 items-center gap-1 rounded-control px-2 text-[11.5px] shadow-hairline transition-colors",
            filters.searchAsCode ? "bg-accent-tint font-medium text-accent-ink ring-1 ring-accent/30" : "bg-inset text-ink-2 hover:bg-hover hover:text-ink",
          )}
        >
          <Icon name="sparkles" size={12} stroke={2} />
          <span className="hidden sm:inline">{t("lib-agent-filter.search_as_code")}</span>
        </button>
        <button
          type="button"
          aria-pressed={chatList}
          aria-label={t("lib-agent-filter.chat_list_view")}
          title={t("lib-agent-filter.chat_list_view")}
          data-testid="agent-chatlist-view-toggle"
          onClick={onToggleChatList}
          className={cx(
            "touch-hit relative z-10 flex size-8 shrink-0 items-center justify-center rounded-control shadow-hairline transition-colors",
            chatList ? "bg-surface font-medium text-ink shadow-btn" : "bg-inset text-ink-3 hover:bg-hover hover:text-ink",
          )}
        >
          <Icon name="chat" size={13} stroke={2} />
        </button>
      </div>

      {/* Scope menu */}
      {scopeMenu.anchor && (
        <Menu
          anchor={scopeMenu.anchor}
          onClose={scopeMenu.close}
          align="right"
          width={190}
          header={t("lib-agent-filter.search_in")}
          items={SEARCH_SCOPES.map((sc) => ({
            key: sc.key,
            label: t(sc.labelKey),
            checked: filters.scope === sc.key,
            onSelect: () => onChange({ ...filters, scope: sc.key as SearchScope }),
          }))}
        />
      )}

      {/* Row 2: Orthogonal dimension filter pills (Pinned, Harness, Project, Model, Machine) */}
      <div className="flex flex-wrap items-center gap-1" data-testid="agent-dimension-pills">
        <button
          type="button"
          aria-pressed={filters.pinnedOnly}
          data-testid="filter-pill-pinned"
          onClick={() => onChange({ ...filters, pinnedOnly: !filters.pinnedOnly })}
          className={cx(
            "touch-hit relative z-10 flex h-6 items-center gap-1 whitespace-nowrap rounded-full px-2 text-[11.5px] transition-colors",
            filters.pinnedOnly ? "bg-accent-tint font-medium text-accent-ink shadow-hairline" : "bg-inset text-ink-2 hover:bg-hover hover:text-ink",
          )}
        >
          <Icon name="pin" size={10} stroke={2.2} />
          <span>{t("lib-agent-filter.filter_pinned")}</span>
          {facets.pinned > 0 && <span className="text-[10.5px] text-ink-3 tabular-nums">{facets.pinned}</span>}
        </button>

        <DimensionPill
          testId="filter-pill-harness"
          icon="agent"
          label={t("lib-agent-filter.filter_harness")}
          selected={filters.harnesses}
          options={facets.harnesses}
          onClick={harnessMenu.open}
          expanded={Boolean(harnessMenu.anchor)}
        />
        <DimensionPill
          testId="filter-pill-project"
          icon="folder"
          label={t("lib-agent-filter.filter_project")}
          selected={filters.projects}
          options={facets.projects}
          onClick={projectMenu.open}
          expanded={Boolean(projectMenu.anchor)}
        />
        <DimensionPill
          testId="filter-pill-model"
          icon="spark"
          label={t("lib-agent-filter.filter_model")}
          selected={filters.models}
          options={facets.models}
          onClick={modelMenu.open}
          expanded={Boolean(modelMenu.anchor)}
        />
        <DimensionPill
          testId="filter-pill-machine"
          icon="machine"
          label={t("lib-agent-filter.filter_machine")}
          selected={filters.machines}
          options={facets.machines}
          onClick={machineMenu.open}
          expanded={Boolean(machineMenu.anchor)}
        />
      </div>

      {harnessMenu.anchor && (
        <Menu
          anchor={harnessMenu.anchor}
          onClose={harnessMenu.close}
          width={210}
          header={t("lib-agent-filter.filter_harness")}
          items={buildFacetMenuItems(facets.harnesses, filters.harnesses, (harnesses) => onChange({ ...filters, harnesses }))}
        />
      )}
      {projectMenu.anchor && (
        <Menu
          anchor={projectMenu.anchor}
          onClose={projectMenu.close}
          width={220}
          header={t("lib-agent-filter.filter_project")}
          items={buildFacetMenuItems(facets.projects, filters.projects, (projects) => onChange({ ...filters, projects }))}
        />
      )}
      {modelMenu.anchor && (
        <Menu
          anchor={modelMenu.anchor}
          onClose={modelMenu.close}
          width={220}
          header={t("lib-agent-filter.filter_model")}
          items={buildFacetMenuItems(facets.models, filters.models, (models) => onChange({ ...filters, models }))}
        />
      )}
      {machineMenu.anchor && (
        <Menu
          anchor={machineMenu.anchor}
          onClose={machineMenu.close}
          width={220}
          header={t("lib-agent-filter.filter_machine")}
          items={buildFacetMenuItems(facets.machines, filters.machines, (machines) => onChange({ ...filters, machines }))}
        />
      )}

      {/* Row 3: Scoped active filter strip (Orca #15334 style) */}
      {(activeChips.length > 0 || hasActive) && (
        <div className="flex flex-wrap items-center gap-1 rounded-control bg-inset/70 px-2 py-1 text-[11px]" data-testid="scoped-filter-strip">
          {activeChips.map((chip) => (
            <span
              key={chip.key}
              className="inline-flex items-center gap-1 rounded-full bg-surface px-2 py-0.5 text-[11px] font-medium text-ink shadow-hairline"
            >
              <span className="max-w-[140px] truncate">{chip.label}</span>
              <button
                type="button"
                aria-label={`${t("lib-agent-filter.reset_filters")} ${chip.label}`}
                onClick={chip.onRemove}
                className="flex size-3.5 items-center justify-center rounded-full text-ink-3 hover:bg-hover hover:text-ink"
              >
                <Icon name="x" size={9} stroke={2.4} />
              </button>
            </span>
          ))}
          <button
            type="button"
            data-testid="scoped-filter-reset"
            onClick={resetAll}
            className="ml-auto inline-flex items-center gap-1 rounded-control px-1.5 py-0.5 text-[11px] font-medium text-accent-ink hover:bg-hover"
          >
            <Icon name="retry" size={10} stroke={2.2} />
            {t("lib-agent-filter.reset_filters")}
          </button>
        </div>
      )}

      {/* Row 4: Search as Code inspectable / editable code plan bar (Perplexity "Search as Code Generation") */}
      {(filters.searchAsCode || Boolean(filters.customPlanCode)) && (
        <div
          data-testid="search-as-code-plan"
          className="flex items-center gap-1.5 rounded-control bg-inset px-2 py-1 text-[11px] shadow-hairline ring-1 ring-accent/25"
        >
          <Icon name="code" size={12} stroke={2} className="shrink-0 text-accent-ink" />
          <span className="shrink-0 font-medium text-accent-ink">{t("lib-agent-filter.search_plan")}:</span>
          {editingPlan ? (
            <input
              autoFocus
              data-testid="search-as-code-input"
              value={filters.customPlanCode ?? plan.code}
              onChange={(e) => onChange({ ...filters, customPlanCode: e.target.value })}
              onBlur={() => setEditingPlan(false)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === "Escape") setEditingPlan(false);
              }}
              className="min-w-0 flex-1 bg-transparent font-mono text-[11px] text-ink outline-none"
            />
          ) : (
            <button
              type="button"
              onClick={() => setEditingPlan(true)}
              title={plan.code}
              className="min-w-0 flex-1 truncate text-left font-mono text-[11px] text-ink hover:text-accent-ink"
            >
              {plan.code}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function DimensionPill({
  testId,
  icon,
  label,
  selected,
  options,
  onClick,
  expanded,
}: {
  testId: string;
  icon: IconName;
  label: string;
  selected: string[];
  options: FacetOption[];
  onClick: (e: React.MouseEvent<HTMLElement>) => void;
  expanded: boolean;
}) {
  const active = selected.length > 0;
  const summary =
    selected.length === 0
      ? label
      : selected.length === 1
        ? options.find((o) => o.id === selected[0])?.label ?? selected[0]
        : `${label} (${selected.length})`;
  return (
    <button
      type="button"
      data-testid={testId}
      aria-haspopup="menu"
      aria-expanded={expanded}
      onClick={onClick}
      className={cx(
        "touch-hit relative z-10 flex h-6 items-center gap-1 whitespace-nowrap rounded-full px-2 text-[11.5px] transition-colors",
        active ? "bg-accent-tint font-medium text-accent-ink shadow-hairline" : "bg-inset text-ink-2 hover:bg-hover hover:text-ink",
      )}
    >
      <Icon name={icon} size={10} stroke={2} />
      <span className="max-w-[96px] truncate">{summary}</span>
      <Icon name="chevron" size={9} stroke={2.2} className=" opacity-70" />
    </button>
  );
}

/** IM Chat-List style Agent Session Row:
 *  - Left: Harness avatar tile + live status dot badge on the bottom-right corner
 *  - Top line: Bold session title (+ Ultracode / Pinned indicator) + relative timestamp on the right
 *  - Second line: Role-attributed search evidence snippet (with highlighted match) OR live last message / tool summary
 *  - Bottom compact chips: `Machine · Project · Harness · Model`
 *  - Right action: One-click Pin / Unpin button + `⋯` actions menu
 */
export const AgentChatListRow = memo(function AgentChatListRow({
  s,
  k,
  reg,
  hover,
  active,
  to,
  go,
  pinned,
  mutes,
  filters,
  ctx,
  onMenu,
}: {
  s: Session;
  k: string;
  reg: RegFn;
  hover: (k: string | null) => void;
  active: boolean;
  to: Route;
  go: GoFn;
  pinned: boolean;
  mutes: AgentMutes;
  filters: AgentDimensionFilters;
  ctx: EvalContext;
  onMenu?: RowMenuFn;
}) {
  const st = stateOf(s);
  const awaiting = awaitsReply(s, mutes);
  const ix = useStore((state) => state.interactions.find((i) => i.machine_id === s.machine_id && i.session_id === s.session_id));
  const draft = draftLine(useLeftDraft(`${s.machine_id}/${s.session_id}`));
  const line: { lead?: string; text: string; tone?: "needs" | "reply" } | undefined = draft ?? rowLine(s, ix, awaiting);
  const previewText = line?.text || plainMarkdown(sessionPreview(s)) || s.activity || "";
  const now = useNow(60_000, Boolean(s.updated_at));
  const timeLabel = s.updated_at ? ago(s.updated_at, now) : "";
  const ultra = ultracodeMarkShown(s);
  const hasErr = sessionHasError(s, ctx.interactions);

  const evidence = useMemo(
    () => (filters.query.trim() || filters.scope === "errors" ? buildEvidenceSnippet(s, filters.query, filters.scope, ctx) : null),
    [s, filters.query, filters.scope, ctx],
  );

  const machineLabel = machineWithHub(s.machine_id, ctx.machines) || s.machine_id;
  const projectLabel = sessionProjectName(s);
  const harnessLabel = sessionHarnessLabel(s);
  const modelLabel = sessionModelId(s);
  const source = sessionSource(s);
  const regRow = useMemo(() => reg(k), [reg, k]);

  return (
    <div
      data-testid="agent-chatlist-row"
      data-session-id={s.session_id}
      data-pinned={pinned ? "true" : "false"}
      className={cx(
        "group/row relative rounded-[10px] transition-colors",
        pinned && !active && "bg-inset/45 ring-1 ring-line/60",
      )}
    >
      <a
        ref={regRow}
        href={href(to)}
        onClick={(e) => {
          rememberSessionRow(s);
          go(to)(e);
        }}
        {...(onMenu ? contextMenuProps((at, el) => onMenu(s, el, at)) : {})}
        onMouseEnter={() => hover(k)}
        onFocus={() => hover(k)}
        onBlur={() => hover(null)}
        aria-current={active ? "page" : undefined}
        className={cx(
          "relative z-10 flex w-full items-start gap-2.5 rounded-[10px] px-2.5 py-2 transition-colors",
          active ? CURRENT_ROW : "hover:bg-hover",
        )}
      >
        {/* Left: Harness Avatar + corner live StatusMark badge */}
        <span className="relative mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-inset shadow-hairline">
          <SessionMark s={s} size={17} />
          <span className="absolute -right-0.5 -bottom-0.5 flex size-3.5 items-center justify-center rounded-full bg-canvas ring-1 ring-canvas">
            <StatusMark state={st} size="sm" awaiting={awaiting} waiting={backgroundWaiting(s)} />
          </span>
        </span>

        {/* Center column: Title + Time, Second line (Evidence snippet or Last message), Metadata chips */}
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          {/* Line 1: Session Title + Ultracode + Pinned badge + Relative Activity Time */}
          <span className="flex min-w-0 items-center gap-1.5">
            {pinned && (
              <span className="shrink-0 text-accent" title={t("sidebar.pinned")}>
                <Icon name="pin" size={11} stroke={2.3} />
              </span>
            )}
            <span
              className={cx(
                "min-w-0 flex-1 truncate text-[13px] leading-tight",
                active || st === "unread" || st === "needs_you" ? "font-semibold text-ink" : st === "offline" ? "text-ink-3" : "font-medium text-ink-2",
              )}
            >
              {sessionTitle(s)}
            </span>
            {ultra && <UltraMark size={10} />}
            {timeLabel && (
              <span className="shrink-0 text-[11px] text-ink-3 tabular-nums group-hover/row:invisible">
                {timeLabel}
              </span>
            )}
          </span>

          {/* Line 2: Role-attributed Evidence Snippet when searching, else Last message / status line */}
          {evidence ? (
            <span data-testid="agent-evidence-snippet" className="truncate text-[11.5px] leading-4 text-ink-2">
              <span
                className={cx(
                  "mr-1 font-semibold",
                  evidence.role === "error" ? "text-red" : evidence.role === "user" ? "text-accent-ink" : "text-ink-2",
                )}
              >
                {evidence.roleLabel}
              </span>
              <span>{evidence.before}</span>
              {evidence.match && (
                <mark className="rounded-[3px] bg-accent-tint px-0.5 font-medium text-accent-ink">
                  {evidence.match}
                </mark>
              )}
              <span>{evidence.after}</span>
            </span>
          ) : (
            (line?.lead || previewText) && (
              <span className="truncate text-[11.5px] leading-4 text-ink-3">
                {line?.lead && (
                  <span className={cx("mr-1 font-medium", line.tone === "reply" ? "text-green" : "text-attention")}>
                    {line.lead}
                  </span>
                )}
                {previewText}
              </span>
            )
          )}

          {/* Line 3: Compact metadata capsules (Machine · Project · Harness · Model) */}
          <span className="mt-0.5 flex flex-wrap items-center gap-1 text-[10.5px] leading-4 text-ink-3" data-testid="agent-chatlist-chips">
            {machineLabel && (
              <span className="inline-flex max-w-[96px] items-center gap-0.5 truncate rounded bg-inset px-1.5 py-px">
                <Icon name="machine" size={9} stroke={2} />
                <span className="truncate">{machineLabel}</span>
              </span>
            )}
            {projectLabel && (
              <span className="inline-flex max-w-[110px] items-center gap-0.5 truncate rounded bg-inset px-1.5 py-px">
                <Icon name="folder" size={9} stroke={2} />
                <span className="truncate">{projectLabel}</span>
              </span>
            )}
            {harnessLabel && (
              <span className="inline-flex max-w-[96px] items-center gap-0.5 truncate rounded bg-inset px-1.5 py-px">
                <span className="truncate">{harnessLabel}</span>
              </span>
            )}
            {modelLabel && (
              <span className="inline-flex max-w-[110px] items-center gap-0.5 truncate rounded bg-inset px-1.5 py-px font-mono text-[10px]">
                <span className="truncate">{modelLabel}</span>
              </span>
            )}
            {source && (
              <span className="inline-flex items-center rounded bg-inset px-1 py-px" title={sourceName(source)}>
                {sourceName(source)}
              </span>
            )}
            {hasErr && st !== "needs_you" && (
              <span className="inline-flex items-center gap-0.5 rounded bg-red/10 px-1.5 py-px font-medium text-red">
                <Icon name="warning" size={9} stroke={2.2} />
              </span>
            )}
          </span>
        </span>
      </a>

      {/* Hover / right actions: One-click Pin/Unpin button + More menu button */}
      <div className="absolute top-1.5 right-1.5 z-20 hidden items-center gap-0.5 group-hover/row:flex [@media(hover:none)]:flex">
        <button
          type="button"
          data-testid="agent-row-pin-button"
          aria-label={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          title={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            void flipPin(s);
          }}
          className={cx(
            "flex size-6 items-center justify-center rounded-[6px] bg-hover transition-colors hover:bg-hover-2",
            pinned ? "text-accent" : "text-ink-3 hover:text-ink",
          )}
        >
          <Icon name="pin" size={12} stroke={2.1} />
        </button>
        {onMenu && (
          <button
            type="button"
            aria-label={t("sidebar.agent_actions")}
            aria-haspopup="menu"
            title={t("sidebar.more")}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onMenu(s, e.currentTarget.parentElement ?? e.currentTarget);
            }}
            className="flex size-6 items-center justify-center rounded-[6px] bg-hover text-ink-3 hover:bg-hover-2 hover:text-ink"
          >
            <Icon name="more" size={13} />
          </button>
        )}
      </div>
    </div>
  );
});
