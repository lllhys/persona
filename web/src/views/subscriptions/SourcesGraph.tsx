// Inbox Sources' relationship view (#5741, docs/extensions/subscriptions.md "Pages"): what brings each source, how it is
// read, the source and where its things go. Wide, four columns joined by curves with the details over them; phone-wide,
// a tree per extension with filters and the details in a sheet. Picking a source lights up its whole path and opens its details: Open Settings (its page in the list) and,
// for a subscription, Set Up with the assistant. The words, badges and duplicates are the list's (the server's).
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { hue, hueTint } from "../../lib/extension-ui-v6";
import { askText } from "../../lib/extension-ui";
import {
  COLUMNS_MIN,
  filtered,
  layoutColumns,
  pathOf,
  type GraphFilter,
  type GraphPath,
  type GraphPick,
  type GraphSource,
  type SourcesGraph,
  useSourcesGraph,
} from "../../lib/sources-graph";
import { cx, Loading } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Badge, glyphOf } from "../extensions/ViewBody6";
import { ROW_ICON } from "../extensions/ViewBody";

/** A link of a source that reads, and of one that can't read now (signed out, failing, needing a new recipe). */
const LINK = "color-mix(in srgb, var(--ink-3) 45%, transparent)";
const LINK_IDLE = "var(--line-strong)";

export function SourcesGraphView({ assistant, onOpen, onAsk }: { assistant: string; onOpen: (state: string) => void; onAsk: (text: string) => void }) {
  const { graph, error } = useSourcesGraph();
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [pick, setPick] = useState<GraphPick>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    setWidth(el.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setPick(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  // A picked source that is gone (turned off, deleted) unpicks.
  useEffect(() => {
    if (pick && graph && pick.kind === "source" && !graph.sources.some((s) => s.id === pick.id)) setPick(null);
  }, [graph, pick]);
  const picked = pick?.kind === "source" ? graph?.sources.find((s) => s.id === pick.id) : undefined;
  const toggle = (p: GraphPick) => setPick((cur) => (cur && p && cur.kind === p.kind && cur.id === p.id ? null : p));
  const details = (across = false) =>
    picked && graph ? <Details graph={graph} source={picked} assistant={assistant} across={across} onOpen={onOpen} onAsk={onAsk} onClose={() => setPick(null)} /> : null;
  return (
    <div ref={box} className="relative">
      {!graph && error ? (
        <p role="alert" className="px-1 py-6 text-[13px] text-ink-2">{t("subscriptions.graph_failed", { error })}</p>
      ) : !graph || !width ? (
        <Loading />
      ) : graph.sources.length === 0 ? (
        <p className="px-1 py-6 text-[13px] text-ink-2">{t("subscriptions.graph_empty")}</p>
      ) : width < COLUMNS_MIN ? (
        <>
          <Tree graph={graph} pick={pick} onPick={toggle} />
          {picked && <Sheet onClose={() => setPick(null)}>{details()}</Sheet>}
        </>
      ) : (
        <>
          <Legend picked={!!pick} />
          {/* the details over the columns, so nothing of the path they light up is covered */}
          {picked && <div className="mb-4 rounded-card bg-surface p-4 shadow-[0_0_0_1px_var(--line-strong)]">{details(true)}</div>}
          {/* an inset, so the rings of the nodes at the edges aren't cut by the scroller */}
          <div className="overflow-x-auto p-1">
            <Columns graph={graph} width={width - 8} pick={pick} onPick={toggle} />
          </div>
          <p className="mt-4 px-1 text-[12px] text-ink-3">{pick ? t("subscriptions.graph_hint_picked") : t("subscriptions.graph_hint")}</p>
        </>
      )}
    </div>
  );
}

function Legend({ picked }: { picked: boolean }) {
  const line = (color: string) => <span aria-hidden className="inline-block w-5 border-t-2 align-middle" style={{ borderColor: color }} />;
  return (
    <div className="mb-3 flex flex-wrap gap-x-4 gap-y-1 px-1 text-[12px] text-ink-2">
      <span className="inline-flex items-center gap-1.5">{line(LINK)}{t("subscriptions.graph_reading")}</span>
      <span className="inline-flex items-center gap-1.5">{line(LINK_IDLE)}{t("subscriptions.graph_idle")}</span>
      {picked && <span className="inline-flex items-center gap-1.5">{line("var(--accent)")}{t("subscriptions.graph_picked")}</span>}
    </div>
  );
}

const sourceGlyph = (s: GraphSource): IconName => glyphOf(s.icon, ROW_ICON) ?? "extension";

function Columns({ graph, width, pick, onPick }: { graph: SourcesGraph; width: number; pick: GraphPick; onPick: (p: GraphPick) => void }) {
  const L = useMemo(() => layoutColumns(graph, width), [graph, width]);
  const path = useMemo(() => pathOf(graph, pick), [graph, pick]);
  const on = (set: keyof Omit<GraphPath, "duplicates">, id: string) => !path || path[set].has(id);
  const pos = (kind: string, id: string, col: number) => ({ left: L.columns[col], top: L.top[`${kind}:${id}`], width: L.widths[col] });
  // Links: right edge of one node to the left edge of the next column's, fanned over a destination's height.
  const links = useMemo(() => {
    const out: { d: string; idle: boolean; lit: boolean }[] = [];
    const mid = (kind: keyof typeof L.heights, key: string) => L.top[key] + L.heights[kind] / 2;
    const curve = (x1: number, y1: number, x2: number, y2: number) => {
      const m = (x1 + x2) / 2;
      return `M${x1},${y1} C${m},${y1} ${m},${y2} ${x2},${y2}`;
    };
    for (const r of graph.reads) {
      const mine = graph.sources.filter((s) => s.read === r.id);
      out.push({
        d: curve(L.columns[0] + L.widths[0], mid("extension", "extension:" + r.extension), L.columns[1], mid("read", "read:" + r.id)),
        idle: mine.every((s) => !s.reading),
        lit: !!path && path.reads.has(r.id) && path.extensions.has(r.extension),
      });
    }
    const fan = new Map<string, number>();
    for (const d of graph.destinations) {
      const into = graph.sources.filter((s) => s.to.includes(d.id));
      const top = L.top["destination:" + d.id];
      into.forEach((s, i) => fan.set(`${s.id}>${d.id}`, top + 12 + (L.heights.destination - 24) * (into.length === 1 ? 0.5 : i / (into.length - 1))));
    }
    for (const s of graph.sources) {
      const lit = !!path && path.sources.has(s.id);
      const y = mid("source", "source:" + s.id);
      out.push({ d: curve(L.columns[1] + L.widths[1], mid("read", "read:" + s.read), L.columns[2], y), idle: !s.reading, lit });
      for (const d of s.to) out.push({ d: curve(L.columns[2] + L.widths[2], y, L.columns[3], fan.get(`${s.id}>${d}`) ?? y), idle: !s.reading, lit });
    }
    return out.sort((a, b) => Number(a.lit) - Number(b.lit)); // the lit ones over the rest
  }, [graph, L, path]);
  const node = "absolute flex items-center gap-2 rounded-[10px] bg-surface text-left shadow-[0_0_0_1px_var(--line-strong)] transition-opacity duration-150 focus-visible:outline-2 focus-visible:outline-accent";
  const state = (lit: boolean, dup = false) => (path ? (dup ? "shadow-[0_0_0_2px_var(--orange)]" : lit ? "shadow-[0_0_0_2px_var(--accent)]" : "opacity-30") : "");
  const heads = [t("subscriptions.graph_col_extension"), t("subscriptions.graph_col_read"), t("subscriptions.graph_col_source"), t("subscriptions.graph_col_destination")];
  return (
    <div className="relative" style={{ width: L.width, height: L.height }}>
      {heads.map((h, i) => (
        <div key={h} className="absolute top-0 text-[12px] text-ink-3" style={{ left: L.columns[i] }}>{h}</div>
      ))}
      <svg aria-hidden className="pointer-events-none absolute inset-0 overflow-visible" width={L.width} height={L.height}>
        {links.map((l, i) => (
          <path key={i} d={l.d} fill="none" strokeWidth={l.lit ? 2.25 : 1.5} stroke={l.lit ? "var(--accent)" : l.idle ? LINK_IDLE : LINK} opacity={path && !l.lit ? 0.35 : 1} />
        ))}
      </svg>
      {graph.extensions.map((e) => (
        <button key={e.id} type="button" onClick={() => onPick({ kind: "extension", id: e.id })} aria-pressed={pick?.kind === "extension" && pick.id === e.id}
          className={cx(node, "px-2.5 py-2", state(on("extensions", e.id)))} style={{ ...pos("extension", e.id, 0), height: L.heights.extension }}>
          <span className="flex size-[26px] shrink-0 items-center justify-center rounded-[7px] bg-field text-ink-2"><Icon name={e.id === "persona" ? "sparkles" : "extension"} size={14} /></span>
          <span className="min-w-0"><span className="block truncate text-[13px] font-medium text-ink">{e.name}</span><span className="block truncate text-[11.5px] text-ink-2">{e.line}</span></span>
        </button>
      ))}
      {graph.reads.map((r) => (
        <div key={r.id} className={cx(node, "rounded-full bg-inset px-2.5 text-[12px] text-ink-2", path && !on("reads", r.id) && "opacity-30", path && on("reads", r.id) && "shadow-[0_0_0_2px_var(--accent)]")}
          style={{ ...pos("read", r.id, 1), height: L.heights.read }}>
          {(r.direct || r.push) && <Icon name={r.push ? "phone" : "sparkles"} size={12} className="shrink-0" />}
          <span className="truncate">{r.title}</span>
        </div>
      ))}
      {graph.sources.map((s) => (
        <button key={s.id} type="button" onClick={() => onPick({ kind: "source", id: s.id })} aria-pressed={pick?.kind === "source" && pick.id === s.id}
          className={cx(node, "px-2.5", state(on("sources", s.id), !!path?.duplicates.has(s.id)))} style={{ ...pos("source", s.id, 2), height: L.heights.source }}>
          <Icon name={sourceGlyph(s)} size={15} className="shrink-0" style={{ color: hue(s.color) }} />
          <span className="min-w-0 flex-1"><span className="block truncate text-[13px] font-medium text-ink">{s.name}</span><span className="block truncate text-[11.5px] text-ink-2">{s.line}</span></span>
          {s.badge?.text && <Badge badge={s.badge} />}
        </button>
      ))}
      {graph.destinations.map((d) => (
        <button key={d.id} type="button" onClick={() => onPick({ kind: "destination", id: d.id })} aria-pressed={pick?.kind === "destination" && pick.id === d.id}
          className={cx(node, "px-3", path && (on("destinations", d.id) ? "shadow-[0_0_0_2px_var(--accent)]" : "opacity-30"))} style={{ ...pos("destination", d.id, 3), height: L.heights.destination }}>
          <Icon name={d.id === "kb" ? "book" : "inbox"} size={17} className="shrink-0 text-accent" />
          <span className="min-w-0"><span className="block truncate text-[14px] font-medium text-ink">{d.name}</span><span className="block truncate text-[11.5px] text-ink-2">{d.line}</span></span>
        </button>
      ))}
    </div>
  );
}

function Tree({ graph, pick, onPick }: { graph: SourcesGraph; pick: GraphPick; onPick: (p: GraphPick) => void }) {
  const [filter, setFilter] = useState<GraphFilter>("all");
  const shown = useMemo(() => new Set(filtered(graph, filter).map((s) => s.id)), [graph, filter]);
  const readOf = useMemo(() => new Map(graph.reads.map((r) => [r.id, r])), [graph]);
  const chips: { f: GraphFilter; label: string }[] = [
    { f: "all", label: t("subscriptions.graph_filter_all", { count: graph.sources.length }) },
    { f: "inbox", label: t("subscriptions.graph_filter_inbox", { count: filtered(graph, "inbox").length }) },
    { f: "kb", label: t("subscriptions.graph_filter_kb", { count: filtered(graph, "kb").length }) },
    { f: "idle", label: t("subscriptions.graph_filter_idle", { count: filtered(graph, "idle").length }) },
  ];
  return (
    <div>
      <div role="radiogroup" aria-label={t("subscriptions.graph_filter")} className="-mx-1 mb-3 flex gap-2 overflow-x-auto px-1 pb-1">
        {chips.map((c) => (
          <button key={c.f} type="button" role="radio" aria-checked={filter === c.f} onClick={() => setFilter(c.f)}
            className={cx("h-8 shrink-0 whitespace-nowrap rounded-full px-3 text-[13px]", filter === c.f ? "bg-ink text-page" : "bg-surface text-ink-2 shadow-[0_0_0_1px_var(--line-strong)]")}>
            {c.label}
          </button>
        ))}
      </div>
      {graph.extensions.map((e) => {
        const mine = graph.sources.filter((s) => readOf.get(s.read)?.extension === e.id && shown.has(s.id));
        if (mine.length === 0) return null;
        return (
          <section key={e.id} className="mb-3 rounded-card bg-surface px-3 pt-2.5 pb-1.5 shadow-[0_0_0_1px_var(--line)]">
            <div className="flex items-center gap-2.5 pb-1.5">
              <span className="flex size-[30px] shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-2"><Icon name={e.id === "persona" ? "sparkles" : "extension"} size={15} /></span>
              <span className="min-w-0 truncate text-[14px] font-semibold text-ink">{e.name}</span>
              <span className="ml-auto shrink-0 text-[12.5px] text-ink-3">{e.line}</span>
            </div>
            <ul>
              {mine.map((s, i) => {
                const r = readOf.get(s.read);
                const below = mine.slice(i + 1);
                const lit = pick?.kind === "source" && pick.id === s.id;
                return (
                  <li key={s.id} className="relative">
                    {/* the trunk from the extension down to this row, then its elbow; under it, the trunk to the rows below */}
                    <span aria-hidden className="absolute top-[-8px] left-[14px] h-[calc(50%+8px)] w-3 rounded-bl-[8px] border-b-[1.5px] border-l-[1.5px]" style={{ borderColor: lit ? "var(--accent)" : s.reading ? LINK : LINK_IDLE }} />
                    {below.length > 0 && <span aria-hidden className="absolute top-[calc(50%-8px)] bottom-[-8px] left-[14px] border-l-[1.5px]" style={{ borderColor: below.some((b) => b.reading) ? LINK : LINK_IDLE }} />}
                    <button type="button" onClick={() => onPick({ kind: "source", id: s.id })} aria-pressed={lit}
                      className="flex min-h-11 w-full items-center gap-2 rounded-[8px] py-1.5 pr-1 pl-[34px] text-left hover:bg-hover focus-visible:outline-2 focus-visible:outline-accent">
                      <span className="shrink-0 rounded-full bg-inset px-2 py-0.5 text-[12px] text-ink-2 shadow-[0_0_0_1px_var(--line-strong)]">{r?.direct ? t("subscriptions.graph_direct_short") : r?.title}</span>
                      <span className={cx("min-w-0 truncate text-[14px] font-medium", lit ? "text-accent" : "text-ink")}>{s.name}</span>
                      {s.badge?.text && <Badge badge={s.badge} />}
                      <span className="ml-auto flex shrink-0 gap-1.5">
                        {(["inbox", "kb"] as const).map((d) => (
                          <Icon key={d} name={d === "kb" ? "book" : "inbox"} size={16} aria-label={s.to.includes(d) ? graph.destinations.find((x) => x.id === d)?.name : undefined}
                            className={cx(!s.to.includes(d) && "invisible", s.reading ? "text-accent" : "text-ink-3")} />
                        ))}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
        );
      })}
    </div>
  );
}

/** A picked source: its path, its line, a possible duplicate, and what to do with it. */
function Details({ graph, source, assistant, across, onOpen, onAsk, onClose }: { graph: SourcesGraph; source: GraphSource; assistant: string; across?: boolean; onOpen: (state: string) => void; onAsk: (text: string) => void; onClose: () => void }) {
  const read = graph.reads.find((r) => r.id === source.read);
  const ext = graph.extensions.find((e) => e.id === read?.extension);
  const to = source.to.map((d) => graph.destinations.find((x) => x.id === d)?.name ?? d).join(t("subscriptions.graph_and"));
  const dup = (source.duplicates ?? []).map((id) => graph.sources.find((s) => s.id === id)?.name).filter(Boolean)[0];
  const steps: [string, string][] = [
    [t("subscriptions.graph_col_extension"), ext?.name ?? ""],
    [t("subscriptions.graph_col_read"), read?.direct ? t("subscriptions.graph_direct") : read?.push ? read.title : t("subscriptions.graph_with_tool", { tool: read?.title ?? "" })],
    [t("subscriptions.graph_col_destination"), to],
  ];
  const ask = source.ask ? askText(source.ask) : undefined;
  const actions = (
    <div className={cx("flex gap-2", across && "shrink-0")}>
      <button type="button" onClick={() => onOpen(source.state)} className={cx("h-8 whitespace-nowrap rounded-control bg-field px-3 text-[13px] font-medium text-ink hover:bg-hover-2", !across && "flex-1")}>
        {t("subscriptions.graph_open")}
      </button>
      {ask && (
        <button type="button" onClick={() => onAsk(ask)} className={cx("h-8 whitespace-nowrap rounded-control bg-accent px-3 text-[13px] font-medium text-white hover:opacity-90", !across && "flex-1")}>
          {assistant ? t("subscriptions.graph_ask", { name: assistant }) : t("subscriptions.graph_ask_assistant")}
        </button>
      )}
    </div>
  );
  if (across)
    return (
      <div className="flex flex-col gap-2.5">
        <div className="flex items-center gap-2">
          <Icon name={sourceGlyph(source)} size={16} className="shrink-0" style={{ color: hue(source.color) }} />
          <h2 className="min-w-0 truncate text-[15px] font-semibold text-ink">{source.name}</h2>
          {source.badge?.text && <Badge badge={source.badge} />}
          <span className="flex-1" />
          {actions}
          <button type="button" onClick={onClose} aria-label={t("subscriptions.graph_close")} className="flex size-7 shrink-0 items-center justify-center rounded-full text-ink-3 hover:bg-hover">
            <Icon name="x" size={13} />
          </button>
        </div>
        <p className="flex flex-wrap items-center gap-x-1.5 text-[13px] text-ink">
          {steps.map(([k, v], i) => (
            <span key={k} className="inline-flex items-center gap-1.5">
              {i > 0 && <Icon name="chevronRight" size={11} className="text-ink-3" />}
              <span className="text-ink-3">{k}</span>
              <span className="font-medium">{v}</span>
            </span>
          ))}
        </p>
        <p className="text-[12.5px] leading-relaxed text-ink-2">{source.line}</p>
        {dup && <p className="rounded-[8px] px-2.5 py-2 text-[12px] leading-relaxed" style={{ background: hueTint("orange"), color: "var(--orange)" }}>{t("subscriptions.graph_duplicate", { name: dup })}</p>}
      </div>
    );
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start gap-2">
        <Icon name={sourceGlyph(source)} size={17} className="mt-0.5 shrink-0" style={{ color: hue(source.color) }} />
        <h2 className="min-w-0 flex-1 text-[15px] leading-snug font-semibold text-ink">{source.name}</h2>
        {source.badge?.text && <Badge badge={source.badge} />}
        <button type="button" onClick={onClose} aria-label={t("subscriptions.graph_close")} className="-mt-1 -mr-1 flex size-7 shrink-0 items-center justify-center rounded-full text-ink-3 hover:bg-hover">
          <Icon name="x" size={13} />
        </button>
      </div>
      <ol className="flex flex-col">
        {steps.map(([k, v], i) => (
          <li key={k} className="flex gap-2.5">
            <span aria-hidden className="flex w-3 flex-col items-center">
              <span className="mt-1.5 size-2 rounded-full bg-accent" />
              {i < steps.length - 1 && <span className="w-0.5 flex-1 bg-accent opacity-50" />}
            </span>
            <span className="pb-2"><span className="block text-[11.5px] text-ink-3">{k}</span><span className="block text-[13px] font-medium text-ink">{v}</span></span>
          </li>
        ))}
      </ol>
      <p className="border-t border-line pt-2.5 text-[12.5px] leading-relaxed text-ink-2">{source.line}</p>
      {dup && <p className="rounded-[8px] px-2.5 py-2 text-[12px] leading-relaxed" style={{ background: hueTint("orange"), color: "var(--orange)" }}>{t("subscriptions.graph_duplicate", { name: dup })}</p>}
      {actions}
    </div>
  );
}

/** A phone-wide page's details: a sheet from the bottom over the page; the backdrop or Esc closes it. Drawn on the
 *  body: the pages around it are animated in with a transform, which would hold a fixed sheet to them. */
function Sheet({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return createPortal(
    <div className="fixed inset-0 z-40 flex items-end" role="dialog" aria-modal="true">
      <button type="button" aria-label={t("subscriptions.graph_close")} onClick={onClose} className="absolute inset-0 bg-black/25" />
      <div className="relative max-h-[85dvh] w-full overflow-y-auto rounded-t-[20px] bg-surface px-5 pt-2.5 pb-[max(20px,env(safe-area-inset-bottom))] shadow-[0_-8px_30px_rgba(0,0,0,.18)]">
        <div aria-hidden className="mx-auto mb-3 h-1 w-9 rounded-full bg-line-strong" />
        {children}
      </div>
    </div>,
    document.body,
  );
}
