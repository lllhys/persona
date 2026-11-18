// Inbox Sources as relations (#5741, docs/extensions/subscriptions.md "Pages"): GET /api/v1/subscriptions/-/graph, what
// brings each source, how it is read, the source and where its things go, in the pages' own words. The layout and the
// path a selection lights up are worked out here, so the view only draws them.
import { useEffect, useState } from "react";
import { api } from "./api";
import { onHubEvent } from "./store";

export interface GraphBadge {
  text: string;
  tone?: string;
}

export interface GraphExtension {
  /** The extension's id, or "persona" for what Persona reads itself. */
  id: string;
  extension?: string;
  name: string;
  line: string;
}

export interface GraphRead {
  id: string;
  extension: string;
  title: string;
  direct?: boolean;
  push?: boolean;
}

export interface GraphSource {
  /** The list row's key: "s.<subscription>" or "b.<connector>". */
  id: string;
  read: string;
  kind: "subscription" | "builtin" | (string & {});
  name: string;
  icon: string;
  color: string;
  line: string;
  badge?: GraphBadge;
  reading: boolean;
  to: string[];
  duplicates?: string[];
  /** The -subscriptions page that shows it. */
  state: string;
  /** Set Up with the assistant's open_url (a subscription's). */
  ask?: string;
}

export interface GraphDestination {
  id: "inbox" | "kb" | (string & {});
  name: string;
  line: string;
  sources: number;
  reading: number;
}

export interface SourcesGraph {
  extensions: GraphExtension[];
  reads: GraphRead[];
  sources: GraphSource[];
  destinations: GraphDestination[];
}

/** The graph, read now and again whenever a subscription changes (at most once a second). null before the first
 *  answer; error after a failed one (what was shown stays). */
export function useSourcesGraph(): { graph: SourcesGraph | null; error: string } {
  const [graph, setGraph] = useState<SourcesGraph | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = () =>
      api<SourcesGraph>("/subscriptions/-/graph").then(
        (g) => {
          if (!alive) return;
          setGraph({ extensions: g.extensions ?? [], reads: g.reads ?? [], sources: g.sources ?? [], destinations: g.destinations ?? [] });
          setError("");
        },
        (e: unknown) => alive && setError(e instanceof Error ? e.message : String(e)),
      );
    void load();
    const off = onHubEvent("subscription", () => {
      clearTimeout(timer);
      timer = setTimeout(load, 1000);
    });
    return () => {
      alive = false;
      clearTimeout(timer);
      off();
    };
  }, []);
  return { graph, error };
}

/** What is picked: a source, an extension or a destination. */
export type GraphPick = { kind: "source" | "extension" | "destination"; id: string } | null;

/** What a pick lights up: the sources on its paths and every node they pass. An extension or a destination lights up
 *  every source connected to it. */
export interface GraphPath {
  sources: Set<string>;
  reads: Set<string>;
  extensions: Set<string>;
  destinations: Set<string>;
  /** The sources that may duplicate the picked one, ringed apart. */
  duplicates: Set<string>;
}

export function pathOf(g: SourcesGraph, pick: GraphPick): GraphPath | null {
  if (!pick) return null;
  const readOf = new Map(g.reads.map((r) => [r.id, r]));
  const picked = g.sources.filter((s) =>
    pick.kind === "source" ? s.id === pick.id : pick.kind === "extension" ? readOf.get(s.read)?.extension === pick.id : s.to.includes(pick.id),
  );
  const p: GraphPath = { sources: new Set(), reads: new Set(), extensions: new Set(), destinations: new Set(), duplicates: new Set() };
  for (const s of picked) {
    p.sources.add(s.id);
    p.reads.add(s.read);
    const ext = readOf.get(s.read)?.extension;
    if (ext) p.extensions.add(ext);
    for (const d of s.to) p.destinations.add(d);
  }
  if (pick.kind === "source") for (const d of picked[0]?.duplicates ?? []) p.duplicates.add(d);
  return p;
}

/** Where every node of the columns view goes, in pixels from the graph's top left. */
export interface GraphLayout {
  width: number;
  height: number;
  columns: number[];
  widths: number[];
  /** Each node's top, by "<kind>:<id>". */
  top: Record<string, number>;
  heights: { extension: number; read: number; source: number; destination: number };
}

const HEIGHTS = { extension: 52, read: 30, source: 52, destination: 60 };
const ROW = 60;
const GROUP_GAP = 14;
const HEAD = 28;
const MIN_GAP = 24;
/** The narrowest the columns go (their widths and the smallest gaps): below it the page draws the tree. */
export const COLUMNS_MIN = 168 + 140 + 272 + 172 + 3 * MIN_GAP;

/** The columns view: sources in rows, a gap between extensions; each read level with the middle of its sources, each
 *  extension with the middle of its reads, the destinations spread down the side. The server's order already keeps
 *  links from crossing (docs/extensions/subscriptions.md "Pages"). */
export function layoutColumns(g: SourcesGraph, width: number): GraphLayout {
  const widths = [168, 140, 272, 172];
  const gap = Math.max(MIN_GAP, Math.min(72, (width - widths.reduce((a, b) => a + b, 0)) / 3));
  const columns = [0, widths[0] + gap, widths[0] + widths[1] + 2 * gap, widths[0] + widths[1] + widths[2] + 3 * gap];
  const top: Record<string, number> = {};
  const readOf = new Map(g.reads.map((r) => [r.id, r]));
  let y = HEAD;
  let last = "";
  const centers = new Map<string, number[]>();
  for (const s of g.sources) {
    const ext = readOf.get(s.read)?.extension ?? "";
    if (last && ext !== last) y += GROUP_GAP;
    top["source:" + s.id] = y;
    centers.set(s.read, [...(centers.get(s.read) ?? []), y + HEIGHTS.source / 2]);
    y += ROW;
    last = ext;
  }
  const avg = (a: number[]) => a.reduce((x, v) => x + v, 0) / Math.max(1, a.length);
  const extCenters = new Map<string, number[]>();
  for (const r of g.reads) {
    const c = avg(centers.get(r.id) ?? [HEAD]);
    top["read:" + r.id] = c - HEIGHTS.read / 2;
    extCenters.set(r.extension, [...(extCenters.get(r.extension) ?? []), c]);
  }
  for (const e of g.extensions) top["extension:" + e.id] = avg(extCenters.get(e.id) ?? [HEAD]) - HEIGHTS.extension / 2;
  const span = Math.max(y - HEAD, HEIGHTS.destination * 2 + 40);
  g.destinations.forEach((d, i) => {
    top["destination:" + d.id] = HEAD + span * (g.destinations.length === 1 ? 0.4 : 0.22 + (0.4 * i) / Math.max(1, g.destinations.length - 1)) - HEIGHTS.destination / 2;
  });
  const height = Math.max(y, ...Object.values(top).map((t) => t + HEIGHTS.destination)) + 8;
  return { width: columns[3] + widths[3], height, columns, widths, top, heights: HEIGHTS };
}

/** The filters of the tree view (a phone-wide page): all, into the Inbox, into the knowledge base, can't read now. */
export type GraphFilter = "all" | "inbox" | "kb" | "idle";

export function filtered(g: SourcesGraph, f: GraphFilter): GraphSource[] {
  return g.sources.filter((s) => (f === "all" ? true : f === "idle" ? !s.reading : s.to.includes(f)));
}
