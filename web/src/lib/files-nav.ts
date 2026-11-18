// The Files panel's navigation (#5827; iOS FilesNavigation): the path bar from the home or "/", Back / Forward / Up,
// which folders take changes, permission bits as ls writes them. Only the lazy Files panel imports this: kept out of
// lib/files.ts, which the first screen shares (src/startup-preload.ts FIRST_SCREEN_BUDGET).
import { dirName } from "./files";

/** One folder in the path bar: its name and the path it opens; `home` is drawn as a house. */
export interface PathSegment {
  title: string;
  path: string;
  home?: boolean;
}

/** The path bar for `shown` (absolute): from the home when it is inside it, else from "/". */
export function pathSegments(shown: string, home?: string): PathSegment[] {
  if (home && home !== "/" && (shown === home || shown.startsWith(home + "/"))) {
    const out: PathSegment[] = [{ title: "~", path: home, home: true }];
    let at = home;
    for (const c of shown.slice(home.length).split("/").filter(Boolean)) {
      at += "/" + c;
      out.push({ title: c, path: at });
    }
    return out;
  }
  const out: PathSegment[] = [{ title: "/", path: "/" }];
  let at = "";
  for (const c of shown.split("/").filter(Boolean)) {
    at += "/" + c;
    out.push({ title: c, path: at });
  }
  return out;
}

/** The folder above `shown`; null at "/". */
export function parentPath(shown: string): string | null {
  if (!shown || shown === "/") return null;
  return dirName(shown) || "/";
}

/** An absolute path under the home with "~", else as it is. */
export function tildePath(path: string, home?: string): string {
  if (!home || home === "/") return path;
  if (path === home) return "~";
  return path.startsWith(home + "/") ? "~" + path.slice(home.length) : path;
}

/** Whether the computer takes changes in `shown` (the home, except ~/.persona and ~/Library): only to grey out what
 *  would be refused; the computer decides. Unknown home: yes. */
export function writableHere(shown: string, home?: string): boolean {
  if (!home) return true;
  if (shown !== home && !shown.startsWith(home + "/")) return false;
  const rel = shown.slice(home.length);
  return !["/.persona", "/Library"].some((b) => rel === b || rel.startsWith(b + "/"));
}

/** Browser-like history of the folders shown: Back, Forward, a new folder clears Forward. */
export interface FilesHistory {
  current: string;
  back: string[];
  forward: string[];
}

export const HISTORY_LIMIT = 50;

export function historyGo(h: FilesHistory, path: string): FilesHistory {
  if (!path || path === h.current) return h;
  return { current: path, back: [...h.back, h.current].slice(-HISTORY_LIMIT), forward: [] };
}

export function historyBack(h: FilesHistory): FilesHistory {
  const p = h.back.at(-1);
  return p === undefined ? h : { current: p, back: h.back.slice(0, -1), forward: [...h.forward, h.current] };
}

export function historyForward(h: FilesHistory): FilesHistory {
  const p = h.forward.at(-1);
  return p === undefined ? h : { current: p, back: [...h.back, h.current], forward: h.forward.slice(0, -1) };
}

/** Permission bits as `ls` writes them (rw-r--r--, with s / t for setuid, setgid, sticky). */
export function modeSymbolic(mode: number): string {
  let out = "";
  [6, 3, 0].forEach((shift, i) => {
    const bits = (mode >> shift) & 7;
    out += bits & 4 ? "r" : "-";
    out += bits & 2 ? "w" : "-";
    const special = ([0o4000, 0o2000, 0o1000][i] & mode) !== 0;
    const x = (bits & 1) !== 0;
    out += special ? (i === 2 ? (x ? "t" : "T") : x ? "s" : "S") : x ? "x" : "-";
  });
  return out;
}

/** 644, or 4755 with a special bit. */
export function modeOctal(mode: number): string {
  return (mode & 0o7777).toString(8);
}
