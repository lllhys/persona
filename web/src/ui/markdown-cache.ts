// Finished Markdown drawn once per content (#2465, docs/web/web.md "Markdown cache and on-demand highlighting"). The React
// tree that react-markdown's synchronous Markdown() makes of a block is kept, so a block drawn again from the same input
// skips the whole parse (micromark, mdast, hast, the rehype plugins, the JSX): a conversation opened again (ChatView
// remounts per conversation) or a reply that re-renders for another reason. The key is everything that tree is made
// from: the Markdown text, the remark plugin list (by identity), which rehype plugins run (KaTeX in or not, the
// path-link set) and the machine and directory the link components belong to; anything new a tree is made from must
// join the key, or a kept tree is drawn stale. A tree is plain data (elements and the hast nodes they carry), so one tree can be drawn
// in several places at once. Streaming and fading blocks are not kept: they change at every update.
import type { ReactElement } from "react";
import ReactMarkdown, { type Options } from "react-markdown";

/** Rollback switch: off draws every block through <ReactMarkdown> again, as before #2465. */
export const markdownCache = { on: true };

/** At most this many characters of keys (the source, mostly) are kept; the least recently drawn blocks go first. A tree
 *  takes about 13 bytes of heap per character (9 MB for a 200-turn chat's 722 KB), so this is roughly 25 MB at most. */
const MAX_CHARS = 2_000_000;
/** At most this many blocks: well above a long conversation's count, so drawing one again in order does not push out
 *  the blocks it is about to draw (a 200-turn chat has about 600). */
const MAX_BLOCKS = 4_000;
/** A block longer than this is drawn but not kept: one long document would push out a whole chat. */
const MAX_BLOCK_CHARS = MAX_CHARS / 8;

const trees = new Map<string, ReactElement>();
let chars = 0;

/**
 * The key of a block: `plugins` names the rehype plugins that run (their options included), `mid` / `cwd` the link
 * components (none without a machine). Separated by NUL, which neither a machine id, a path nor the plugin names have.
 */
export function markdownKey(md: string, plugins: string, mid: string | undefined, cwd: string | undefined): string {
  return `${plugins}\0${mid ?? ""}\0${mid ? (cwd ?? "\u0001") : ""}\0${md}`;
}

/** The remark plugin lists met, by identity: a block parsed with another list (a module constant each) is kept apart
 *  without the caller naming it in the key. A list made afresh for each draw would only miss. */
const remarkLists = new WeakMap<object, number>();
let remarkCount = 0;
function remarkId(list: Options["remarkPlugins"]): number {
  if (!list) return 0;
  let id = remarkLists.get(list);
  if (id === undefined) remarkLists.set(list, (id = ++remarkCount));
  return id;
}

/** The tree of `options` (react-markdown's Markdown), kept under `key` and its remark plugin list: drawn again from the
 *  cache while it is there. */
export function cachedMarkdown(blockKey: string, options: Readonly<Options>): ReactElement {
  const key = `${remarkId(options.remarkPlugins)}\0${blockKey}`;
  const hit = trees.get(key);
  if (hit) {
    trees.delete(key);
    trees.set(key, hit);
    return hit;
  }
  const tree = ReactMarkdown(options);
  if (key.length > MAX_BLOCK_CHARS) return tree;
  trees.set(key, tree);
  chars += key.length;
  for (const k of trees.keys()) {
    if (chars <= MAX_CHARS && trees.size <= MAX_BLOCKS) break;
    trees.delete(k);
    chars -= k.length;
  }
  return tree;
}

/** Tests: forget every kept tree. */
export function forgetMarkdown(): void {
  trees.clear();
  chars = 0;
}
