// ⌘K's file search (docs/web/web.md "Command palette", views/palette/FilesSection.tsx): the files under a conversation's (or a
// terminal pane's) directory, listed by the node (`/machines/{mid}/fs/find`, one request) and matched here as the
// user types. No DOM: the matcher, the loader and the walk for an older node are tested in file-find.test.ts.
import { api, ApiError, enc } from "./api";
import { joinPath, type FsListing } from "./files";
import type { Route } from "./router";
import { paneCwd } from "./terminals";
import type { TermSpace } from "./terminals";

// ── the matcher ──────────────────────────────────────────

/** The most files one search lists. */
export const MAX_FILE_HITS = 50;

export interface FileHit {
  /** The path as the node listed it: relative to the directory, with `/`. */
  path: string;
  score: number;
  /** Indexes into `path` of the characters that matched (ascending), to draw them. */
  matched: number[];
}

// What a character matched is worth. A match at the start of a word (after / . - _ space) or at a camelCase hump says
// more than one in the middle of a word; characters that follow each other beat scattered ones; the file's own name
// counts for more than the folders above it.
const WORD_START = 10;
const AFTER_SLASH = 12;
const CAMEL = 9;
const FIRST_CHAR = 12;
const CONSECUTIVE = 6;
const GAP = 1.2; // per character skipped between two matches
const LEAD = 0.15; // per character of folders before the first match
const IN_NAME = 3;
const NAME_EXACT = 30;
const NAME_PREFIX = 12;
const SHORT = 0.02; // per character of the whole path: of two equal matches the shorter one first

const NEG = -1e9;
const SEPARATORS = "/\\.-_ ";

/** Splits what was typed into its terms: words separated by spaces, all of which must match. */
export function searchTerms(query: string): string[] {
  return query.trim().split(/\s+/).filter(Boolean);
}

/** The words of a query in one canonical string, so that "narrower than the last query" is a prefix test. */
const canonical = (query: string) => searchTerms(query).join(" ").toLowerCase();

function isUpper(c: string) {
  return c !== c.toLowerCase() && c === c.toUpperCase();
}
function isLower(c: string) {
  return c !== c.toUpperCase();
}

/** What matching the character at `i` of a path is worth by where it stands. */
function boundary(path: string, i: number): number {
  if (i === 0) return FIRST_CHAR;
  const prev = path[i - 1];
  if (prev === "/") return AFTER_SLASH;
  if (SEPARATORS.includes(prev)) return WORD_START;
  const c = path[i];
  if (isUpper(c) && isLower(prev)) return CAMEL;
  return 0;
}

/** Whether `term` (lower case) is a subsequence of `lower`. */
function inOrder(lower: string, term: string): boolean {
  let j = 0;
  for (let i = 0; i < lower.length && j < term.length; i++) if (lower[i] === term[j]) j++;
  return j === term.length;
}

// Scratch space of the alignment, reused between files.
let scratch = { prev: new Float64Array(0), cur: new Float64Array(0), from: new Int32Array(0) };

/**
 * The best way `term` (lower case) can be laid over `path`: its score and, when asked, where its characters land.
 * A dynamic program over (character of the term, position in the path), so "App" in "src/AppView/app.ts" finds the
 * hump of AppView rather than the first scattering of a, p, p. null when it does not fit.
 */
function align(path: string, lower: string, term: string, nameStart: number, positions: boolean): { score: number; at: number[] } | null {
  const n = lower.length;
  const m = term.length;
  if (m === 0 || n < m || !inOrder(lower, term)) return null;
  if (scratch.prev.length < n) scratch = { prev: new Float64Array(n), cur: new Float64Array(n), from: new Int32Array(0) };
  if (positions && scratch.from.length < n * m) scratch.from = new Int32Array(n * m);
  let { prev, cur } = scratch;
  const from = scratch.from;
  for (let i = 0; i < m; i++) {
    const ch = term[i];
    // far: the best the previous character did at j-2 or earlier, less GAP for each character skipped since
    let far = NEG;
    let farAt = -1;
    for (let j = 0; j < n; j++) {
      if (i > 0 && j >= 2) {
        const decayed = far > NEG / 2 ? far - GAP : NEG;
        const fresh = prev[j - 2] > NEG / 2 ? prev[j - 2] - GAP : NEG;
        if (fresh > decayed) {
          far = fresh;
          farAt = j - 2;
        } else far = decayed;
      }
      if (lower[j] !== ch) {
        cur[j] = NEG;
        continue;
      }
      const gain = boundary(path, j) + (j >= nameStart ? IN_NAME : 0);
      if (i === 0) {
        cur[j] = gain - (j < nameStart ? LEAD * j : 0); // a match deep in the folders is worth less; a name is where it is
        if (positions) from[i * n + j] = -1;
        continue;
      }
      // either right after the previous character (consecutive), or after a gap
      const adj = j >= 1 && prev[j - 1] > NEG / 2 ? prev[j - 1] + CONSECUTIVE : NEG;
      if (adj <= NEG / 2 && far <= NEG / 2) {
        cur[j] = NEG;
        continue;
      }
      if (adj >= far) {
        cur[j] = gain + adj;
        if (positions) from[i * n + j] = j - 1;
      } else {
        cur[j] = gain + far;
        if (positions) from[i * n + j] = farAt;
      }
    }
    [prev, cur] = [cur, prev];
  }
  // prev holds the last row now
  let top = NEG;
  let end = -1;
  for (let j = 0; j < n; j++)
    if (prev[j] > top) {
      top = prev[j];
      end = j;
    }
  if (end < 0) return null;
  const at: number[] = [];
  if (positions) {
    // trace back from the end through the choice each position made
    let j = end;
    for (let i = m - 1; i >= 0; i--) {
      at.push(j);
      j = from[i * n + j];
    }
    at.reverse();
  }
  return { score: top, at };
}

/** How well `path` matches all of `terms` (lower case), or null when one of them does not. */
function scorePath(path: string, terms: string[], positions: boolean): FileHit | null {
  const lower = path.toLowerCase();
  const nameStart = path.lastIndexOf("/") + 1;
  const name = lower.slice(nameStart);
  let score = 0;
  const marks = new Set<number>();
  for (const term of terms) {
    const a = align(path, lower, term, nameStart, positions);
    if (!a) return null;
    score += a.score;
    if (name === term) score += NAME_EXACT;
    else if (name.startsWith(term)) score += NAME_PREFIX;
    if (positions) for (const p of a.at) marks.add(p);
  }
  return { path, score: score - SHORT * path.length, matched: positions ? [...marks].sort((x, y) => x - y) : [] };
}

const byRank = (a: FileHit, b: FileHit) => b.score - a.score || a.path.length - b.path.length || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0);

export interface FileSearch {
  /** The best `limit` files for a query, best first; every term has to match (AND). An empty query matches nothing (the
   *  palette's file section is for something typed). Typing on from the last query narrows the last query's matches. */
  search(query: string, limit?: number): FileHit[];
}

/**
 * A search over one list of files that remembers the last query's matches: a query that continues the last one
 * (more letters, another word) can only match some of those, so it looks at them and not at the whole list again.
 */
export function createFileSearch(files: readonly string[]): FileSearch {
  let lastQuery = "";
  // every match of the last query, unranked and without positions, for the next one to narrow
  let lastMatches: string[] = [...files];
  return {
    search(query, limit = MAX_FILE_HITS) {
      const key = canonical(query);
      if (!key) {
        lastQuery = "";
        lastMatches = [...files];
        return [];
      }
      const terms = key.split(" ");
      const pool = lastQuery && key.startsWith(lastQuery) ? lastMatches : files;
      const hits: FileHit[] = [];
      for (const path of pool) {
        const hit = scorePath(path, terms, false);
        if (hit) hits.push(hit);
      }
      lastQuery = key;
      lastMatches = hits.map((h) => h.path);
      hits.sort(byRank);
      // positions only for the ones listed
      return hits.slice(0, limit).map((h) => scorePath(h.path, terms, true) ?? h);
    },
  };
}

/** A stretch of text to draw, matched or not. */
export interface Run {
  text: string;
  hit: boolean;
}

/** `text` (the part of a hit's path starting at `offset`) cut into runs, the characters the hit matched apart. */
export function highlightRuns(text: string, offset: number, matched: readonly number[]): Run[] {
  const at = new Set(matched);
  const runs: Run[] = [];
  for (let i = 0; i < text.length; i++) {
    const hit = at.has(offset + i);
    const last = runs[runs.length - 1];
    if (last && last.hit === hit) last.text += text[i];
    else runs.push({ text: text[i], hit });
  }
  return runs;
}

/** What Mod+Enter copies: the path as the agent's input takes a file, `@path`. */
export const mentionOf = (hit: Pick<FileHit, "path">) => `@${hit.path}`;

// ── what the search is over ──────────────────────────────

/** Where the user is, as far as files go: a computer and a directory on it. */
export interface FileContext {
  mid: string;
  cwd: string;
}

/**
 * The directory to search: the conversation's (a session, or the pane a tab opens on), or the working directory of
 * the terminal pane the /terminal page has open. null elsewhere: there is nothing to search in.
 */
export function fileContext(
  route: Route,
  pane: { mid: string; sid: string } | undefined,
  sessionCwd: (mid: string, sid: string) => string | undefined,
  spacesOf: (mid: string) => readonly TermSpace[] | undefined,
): FileContext | null {
  if (pane) {
    const cwd = sessionCwd(pane.mid, pane.sid)?.trim();
    return cwd ? { mid: pane.mid, cwd } : null;
  }
  if (route.name === "terminal" && route.mid && route.id) {
    const spaces = spacesOf(route.mid);
    const cwd = spaces ? paneCwd(spaces, route.id).trim() : "";
    return cwd ? { mid: route.mid, cwd } : null;
  }
  return null;
}

/** The path the file viewer opens for a hit. */
export function hitPath(ctx: FileContext, hit: Pick<FileHit, "path">): string {
  return joinPath(ctx.cwd, hit.path);
}

// ── the index ────────────────────────────────────────────

export interface FileIndex {
  files: string[];
  /** The node stopped at its cap: the list is the shallowest part of a bigger tree. */
  truncated: boolean;
}

/** How long a list is reused when the palette opens again. */
export const INDEX_TTL_MS = 20_000;
/** The node lists a big tree in one go; give it as long as the hub gives a call. */
export const INDEX_TIMEOUT_MS = 30_000;

interface FindResponse {
  files?: string[];
  truncated?: boolean;
}

/** What an older node (or hub) answers to a call it does not know: the walk below takes over. */
export function lacksFind(e: unknown): boolean {
  return e instanceof ApiError && (e.status === 404 || e.status === 405 || /unknown method/i.test(e.message));
}

/** Limits of the walk for an older node: it asks the node for one folder at a time. */
export interface WalkLimits {
  folders: number;
  files: number;
  parallel: number;
}
export const WALK_LIMITS: WalkLimits = { folders: 300, files: 20_000, parallel: 4 };

// Folders that are not worth a request each; the node's own listing leaves these out too (git ignores them).
const WALK_SKIP = new Set([".git", ".hg", ".svn", "node_modules", "__pycache__", ".cache", ".gradle", ".next", ".nuxt", ".venv", ".turbo", "DerivedData"]);

/**
 * Lists the files under `root` breadth first through `list` (one folder per call, the `fs` endpoint), so a big tree
 * ends with its shallow files rather than one deep corner. A folder that cannot be read is skipped.
 */
export async function walkTree(list: (dir: string) => Promise<FsListing>, root: string, limits: WalkLimits = WALK_LIMITS): Promise<FileIndex> {
  const files: string[] = [];
  let truncated = false;
  let level: string[] = [""];
  let asked = 0;
  while (level.length && !truncated) {
    const next: string[] = [];
    for (let i = 0; i < level.length && !truncated; i += limits.parallel) {
      const batch = level.slice(i, i + limits.parallel);
      if (asked + batch.length > limits.folders) {
        truncated = true;
        break;
      }
      asked += batch.length;
      const listings = await Promise.all(batch.map((rel) => list(rel ? joinPath(root, rel) : root).catch(() => null)));
      listings.forEach((listing, k) => {
        if (!listing) return;
        for (const e of listing.entries) {
          const rel = batch[k] ? `${batch[k]}/${e.name}` : e.name;
          if (e.dir) {
            if (!WALK_SKIP.has(e.name)) next.push(rel);
          } else if (files.length < limits.files) files.push(rel);
          else truncated = true;
        }
        if (listing.truncated) truncated = true;
      });
    }
    level = next;
  }
  return { files, truncated };
}

const cache = new Map<string, { at: number; index: FileIndex }>();
const inflight = new Map<string, Promise<FileIndex>>();
const keyOf = (ctx: FileContext) => `${ctx.mid}\n${ctx.cwd}`;

/** The index already held for a directory, however old; undefined when there is none. */
export function cachedIndex(ctx: FileContext): FileIndex | undefined {
  return cache.get(keyOf(ctx))?.index;
}

/** Whether the held index is recent enough to use as it is. */
export function indexFresh(ctx: FileContext, now = Date.now()): boolean {
  const c = cache.get(keyOf(ctx));
  return !!c && now - c.at < INDEX_TTL_MS;
}

/** Forgets what is held (tests; a changed hub). */
export function clearIndexes() {
  cache.clear();
  inflight.clear();
}

async function fetchIndex(ctx: FileContext): Promise<FileIndex> {
  const signal = AbortSignal.timeout(INDEX_TIMEOUT_MS);
  try {
    const res = await api<FindResponse>(`/machines/${enc(ctx.mid)}/fs/find?cwd=${enc(ctx.cwd)}`, { signal });
    return { files: res.files ?? [], truncated: !!res.truncated };
  } catch (e) {
    if (!lacksFind(e)) throw e;
    return walkTree((dir) => api<FsListing>(`/machines/${enc(ctx.mid)}/fs?path=${enc(dir)}&cwd=${enc(ctx.cwd)}`, { signal }), ctx.cwd);
  }
}

/** The files under a directory: the held list while it is fresh, else one request (shared by whoever asks meanwhile). */
export function loadFileIndex(ctx: FileContext, fetcher: (ctx: FileContext) => Promise<FileIndex> = fetchIndex): Promise<FileIndex> {
  const key = keyOf(ctx);
  if (indexFresh(ctx)) return Promise.resolve(cache.get(key)!.index);
  const running = inflight.get(key);
  if (running) return running;
  const p = fetcher(ctx)
    .then((index) => {
      cache.set(key, { at: Date.now(), index });
      if (cache.size > 12) cache.delete(cache.keys().next().value!);
      return index;
    })
    .finally(() => inflight.delete(key));
  inflight.set(key, p);
  return p;
}
