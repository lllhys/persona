// Source subscriptions (#2041, docs/extensions/subscriptions.md "Pages"): the manager draws the pages as declarative UI
// (POST /api/v1/subscriptions/-/ui); the web opens them at /subscriptions and shows entry rows, once the manager reports
// that subscriptions deliver to the Inbox and the knowledge base (feature source_subscriptions).
import { useEffect, useState } from "react";
import { api } from "./api";
import { onHubEvent, useStore } from "./store";

export const SUBSCRIPTIONS_FEATURE = "source_subscriptions";
/** The manager's pages are Inbox Sources (#5278): built-in sources beside the subscriptions; Settings has its row. */
export const INBOX_SOURCES_FEATURE = "inbox_sources";
/** The manager answers GET /api/v1/subscriptions/-/graph (#5741): Inbox Sources has its List / Relationships switch. */
export const INBOX_SOURCES_GRAPH_FEATURE = "inbox_sources_graph";

export { SUBSCRIPTIONS_UI } from "./subscriptions-ui";

/** A connector's line of GET /api/v1/subscriptions/summary: subscribable is absent until its catalog was read. */
export interface SubscriptionSummaryLine {
  connector: string;
  extension: string;
  subscribable?: boolean;
  on: number;
  failing: number;
}

/** What an entry row says: how many are on and how many need attention. */
export interface SubscriptionCounts {
  on: number;
  failing: number;
}

/** Whether the manager shows subscriptions (they deliver). */
export function useSubscriptionsShown(): boolean {
  return useStore((s) => s.features.includes(SUBSCRIPTIONS_FEATURE));
}

/** Whether the manager draws Inbox Sources (Settings' row, the Inbox menu's item and the page's title say so). */
export function useInboxSourcesShown(): boolean {
  return useStore((s) => s.features.includes(INBOX_SOURCES_FEATURE));
}

/** Whether Inbox Sources offers its relationship view. */
export function useInboxSourcesGraphShown(): boolean {
  return useStore((s) => s.features.includes(INBOX_SOURCES_GRAPH_FEATURE));
}

/** The summary, read when subscriptions are shown and again whenever one changes (at most once a second); null before. */
export function useSubscriptionSummary(): SubscriptionSummaryLine[] | null {
  const shown = useSubscriptionsShown();
  const [lines, setLines] = useState<SubscriptionSummaryLine[] | null>(null);
  useEffect(() => {
    if (!shown) {
      setLines(null);
      return;
    }
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = () =>
      api<{ connectors?: SubscriptionSummaryLine[] }>("/subscriptions/summary").then(
        (r) => alive && setLines(Array.isArray(r.connectors) ? r.connectors : []),
        () => {}, // an entry row is a convenience: on a failure it keeps what it had (or stays hidden)
      );
    load();
    const off = onHubEvent("subscription", () => {
      clearTimeout(timer);
      timer = setTimeout(load, 1000);
    });
    return () => {
      alive = false;
      clearTimeout(timer);
      off();
    };
  }, [shown]);
  return lines;
}

/** The counts for the lines one entry row stands for; null when none of them can be subscribed (the row is hidden). */
export function countsOf(lines: SubscriptionSummaryLine[] | null, pick: (l: SubscriptionSummaryLine) => boolean): SubscriptionCounts | null {
  const mine = (lines ?? []).filter(pick);
  if (!mine.some((l) => l.subscribable === true || l.on > 0)) return null;
  return mine.reduce((c, l) => ({ on: c.on + l.on, failing: c.failing + l.failing }), { on: 0, failing: 0 });
}

/** The pages' first state: all of them, one extension's or one connector's. */
export type SubscriptionsScope = { ext: string } | { c: string } | undefined;

export function subscriptionsHref(scope?: SubscriptionsScope): string {
  return scope ? `/subscriptions?state=${encodeURIComponent(JSON.stringify(scope))}` : "/subscriptions";
}

