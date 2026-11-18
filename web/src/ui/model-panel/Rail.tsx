// The model panel's rail (#5032, after Cindy's UnifiedModelRail): a 68 px column of view cells, Recent and Favorites (what
// you keep), a short rule, then All and one cell per provider in the order given. Every cell has its mark and, under it, a
// caption of up to two lines: a provider's caption is the part of its name that tells it from the others (`railCaptions`:
// claude-aster, claude-max and codex read "aster", "max" and "codex"), because their letter marks are often the same and a
// phone has no hover to show the name. The full name stays the cell's accessible name and tooltip. The cell that is on is
// filled in the ink; the others are quiet and light up under the pointer. It scrolls on its own, without a bar: an edge
// with more cells beyond it fades out, so a long list of providers shows it goes on.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { sameView, type PanelProvider, type PanelView } from "../../lib/model-panel";
import { quotaReset, weeklyQuota, type LauncherQuotaAnswer } from "../../lib/quota";
import { cx } from "../atoms";
import { Icon } from "../icons";
import { ProviderMark } from "./ProviderMark";

/** How far an edge with more beyond it fades. */
const FADE = "20px";

/** The mask for a column that has more above (`up`) and / or below (`down`); undefined for none. */
export function railFade(up: boolean, down: boolean): string | undefined {
  if (!up && !down) return undefined;
  return `linear-gradient(180deg,${up ? `transparent,#000 ${FADE}` : "#000"},${down ? `#000 calc(100% - ${FADE}),transparent` : "#000"})`;
}

/** Whether the column scrolled to `scrollTop` has cells hidden above and below. */
export function railEdges(b: { scrollTop: number; clientHeight: number; scrollHeight: number }): { up: boolean; down: boolean } {
  return { up: b.scrollTop > 1, down: b.scrollTop + b.clientHeight < b.scrollHeight - 1 };
}

/** A trailing note in brackets ("hone-claw（Claude）", "Zhipu (GLM)"). */
const NOTE = /\s*[（(][^（()）]*[)）]\s*$/u;
/** Nothing but a note in brackets. */
const ONLY_NOTE = /^\s*[（(][^（()）]*[)）]\s*$/u;
/** The first word of a name and what follows its separator ("claude-aster" → "claude", "aster"). */
const HEAD = /^([^\s\-_./:·]+)[\s\-_./:·]+(.+)$/u;

/** Whether every caption is told apart from the others (case and surrounding spaces aside). */
function distinct(captions: string[]): boolean {
  return new Set(captions.map((c) => c.trim().toLowerCase())).size === captions.length;
}

/** The rail's captions for these provider names, in order: each name without a trailing note in brackets, and without a
 *  first word it shares with another provider ("claude-aster", "claude-max", "codex" → "aster", "max", "codex"), so the
 *  words that tell providers apart are the ones that fit. A step that would make two captions the same, or a caption
 *  empty, is not taken for the names it touches. */
export function railCaptions(names: string[]): string[] {
  const full = names.map((n) => n.trim());
  const bare = full.map((n) => n.replace(NOTE, "").trim() || n);
  const base = distinct(bare) ? bare : full;
  const heads = base.map((n) => HEAD.exec(n));
  const count = new Map<string, number>();
  // a name that is only that word ("claude" beside "claude-max") shares it too
  for (const [i, m] of heads.entries()) {
    const head = (m ? m[1] : base[i]).toLowerCase();
    count.set(head, (count.get(head) ?? 0) + 1);
  }
  const short = base.map((n, i) => {
    const m = heads[i];
    // what is left must be a word of its own, not only a note in brackets ("gw (Claude)" keeps its "gw")
    return m && (count.get(m[1].toLowerCase()) ?? 0) > 1 && !ONLY_NOTE.test(m[2]) ? m[2].trim() : n;
  });
  // a shortened caption that now reads like another one goes back to its whole name, and so does the other one
  const key = (c: string) => c.trim().toLowerCase();
  const seen = new Map<string, number>();
  for (const c of short) seen.set(key(c), (seen.get(key(c)) ?? 0) + 1);
  return short.map((c, i) => ((seen.get(key(c)) ?? 0) > 1 ? base[i] : c));
}

export function Rail({ providers, view, onView, disabled, quotas }: { providers: PanelProvider[]; view: PanelView; onView: (v: PanelView) => void; disabled?: boolean; quotas?: Readonly<Record<string, LauncherQuotaAnswer>> }) {
  // `tip`: the tooltip when it says more than the name (a provider's startup ID, #5029)
  const captions = useMemo(() => railCaptions(providers.map((p) => p.name)), [providers]);
  const cell = (v: PanelView, label: string, icon: React.ReactNode, tip = label, caption = label) => {
    const on = sameView(v, view);
    const quota = v.kind === "provider" ? weeklyQuota(quotas?.[v.provider]) : undefined;
    const detail = quota ? [t("ui-modelpanel.weekly_left", { label: quota.window.label, percent: quota.remaining }), quotaReset(quota.window.resets_at ?? "")].filter(Boolean).join("\n") : "";
    return (
      <button
        key={v.kind === "provider" ? `provider:${v.provider}` : v.kind}
        type="button"
        disabled={disabled}
        aria-label={label}
        aria-pressed={on}
        title={[tip, detail].filter(Boolean).join("\n")}
        aria-description={detail || undefined}
        data-rail-cell={v.kind === "provider" ? v.provider : v.kind}
        onClick={() => onView(v)}
        className={cx(
          "relative flex min-h-[46px] w-[60px] shrink-0 flex-col items-center justify-start gap-0.5 rounded-[9px] px-0.5 pt-1.5 pb-2 transition-colors duration-150",
          on ? "bg-ink text-surface" : "text-ink-3 enabled:hover:bg-hover-2 enabled:hover:text-ink-2",
          disabled && "cursor-not-allowed opacity-50",
        )}
      >
        <span className="flex h-4 items-center justify-center">{icon}</span>
        <span data-rail-caption className="line-clamp-2 w-full text-center text-submeta leading-3.5 break-words [overflow-wrap:anywhere]">{caption}</span>
        {quota && (
          <span aria-hidden className="absolute bottom-[3px] h-[3px] w-[22px] overflow-hidden rounded-full" style={{ backgroundColor: "color-mix(in srgb, currentColor 18%, transparent)" }}>
            <span
              data-weekly-remaining={quota.remaining}
              className={cx("block h-full rounded-full", quota.tone === "red" ? "bg-red" : quota.tone === "orange" ? "bg-orange" : "bg-accent")}
              style={{ width: `${quota.remaining}%` }}
            />
          </span>
        )}
      </button>
    );
  };
  const box = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ up: false, down: false });
  const measure = useCallback(() => {
    const el = box.current;
    if (!el) return;
    const e = railEdges(el);
    setEdges((v) => (v.up === e.up && v.down === e.down ? v : e));
  }, []);
  useLayoutEffect(measure, [measure, providers.length]);
  useEffect(() => {
    const el = box.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);
  const mask = railFade(edges.up, edges.down);
  return (
    <div
      ref={box}
      onScroll={measure}
      data-rail-fade={[edges.up && "up", edges.down && "down"].filter(Boolean).join(" ") || undefined}
      style={mask ? { maskImage: mask, WebkitMaskImage: mask } : undefined}
      role="group" aria-label={t("ui-modelpanel.views")} className="flex min-h-0 w-[68px] shrink-0 scrollbar-none flex-col items-center gap-0.5 overflow-x-hidden overflow-y-auto border-r border-line px-1.5 py-2">
      {cell({ kind: "recent" }, t("ui-modelpanel.recent"), <Icon name="history" size={16} />)}
      {cell({ kind: "favorites" }, t("ui-modelpanel.favorites"), <Icon name="star" size={16} className={view.kind === "favorites" ? "fill-current" : undefined} />)}
      <div aria-hidden className="my-[3px] w-[22px] shrink-0 border-t border-line" />
      {cell({ kind: "all" }, t("ui-modelpanel.all"), <Icon name="grid" size={16} />)}
      {providers.map((p, i) => cell({ kind: "provider", provider: p.id }, p.name, <ProviderMark name={p.name} />, p.idNote ? `${p.name} (${p.idNote})` : p.name, captions[i]))}
    </div>
  );
}
