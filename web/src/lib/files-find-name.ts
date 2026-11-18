// Finding by name under the folder the Files panel shows (#5827): the folder's files from the node once (`fs/find`), the
// folders they are in as hits of their own, and every typed word in the name (any case), best first: the whole name,
// then a name that starts with it, then a word in it that does, then anywhere; then folders before files, then the
// shorter path. The iPhone's file page ranks by the same rule (apple/Persona/Views/Session/FilesNavigation.swift FileFind).

export interface NameHit {
  /** Relative to the folder searched, with "/". */
  path: string;
  dir: boolean;
}

export const MAX_NAME_HITS = 200;

export function hitName(h: NameHit): string {
  return h.path.slice(h.path.lastIndexOf("/") + 1);
}

/** The folder a hit is in, relative ("" at the top). */
export function hitFolder(h: NameHit): string {
  const i = h.path.lastIndexOf("/");
  return i < 0 ? "" : h.path.slice(0, i);
}

/** The files and, before them, every folder they are in. */
export function nameEntries(files: string[]): NameHit[] {
  const dirs = new Set<string>();
  for (const f of files) {
    let d = f.slice(0, Math.max(0, f.lastIndexOf("/")));
    while (d && !dirs.has(d)) {
      dirs.add(d);
      d = d.slice(0, Math.max(0, d.lastIndexOf("/")));
    }
  }
  return [...[...dirs].sort().map((path) => ({ path, dir: true })), ...files.map((path) => ({ path, dir: false }))];
}

function wordStart(s: string, i: number): boolean {
  return i === 0 || !/[\p{L}\p{N}]/u.test(s[i - 1]);
}

export function searchNames(entries: NameHit[], query: string, limit = MAX_NAME_HITS): NameHit[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [];
  const scored: [number, NameHit][] = [];
  for (const e of entries) {
    const name = hitName(e).toLowerCase();
    let score = 0;
    let fits = true;
    for (const w of words) {
      const i = name.indexOf(w);
      if (i < 0) {
        fits = false;
        break;
      }
      score += name === w ? 4 : i === 0 ? 3 : wordStart(name, i) ? 2 : 1;
    }
    if (fits) scored.push([score, e]);
  }
  scored.sort(([sa, a], [sb, b]) => sb - sa || Number(b.dir) - Number(a.dir) || a.path.length - b.path.length || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  return scored.slice(0, limit).map(([, e]) => e);
}
