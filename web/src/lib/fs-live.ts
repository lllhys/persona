// Live Files / Git (#2439): the panels tell the hub which folders they show, the hub watches them on the computer and
// pushes `fs_changed`; a panel refreshes what changed after a short debounce (Moshi: 200 ms). While the page is hidden
// nothing is watched (the hub is told so), and when it comes back or the socket reconnects every panel refreshes once,
// since changes were missed. One socket message per session folder replaces the whole set, so any number of panels
// can show the same folder (the file list and the Git panel both want the Git half).
import { isOpen, onHubEvent, send } from "./store";

/** What a panel is told: the folders (as it named them) that changed, and whether the folder's Git status did. */
export interface FsChange {
  dirs: string[];
  git: boolean;
}

export interface FsWatchOptions {
  mid: string;
  /** The session folder: the Git status is its, and the hub keys the set by it. */
  cwd: string;
  paths: string[];
  git: boolean;
}

export interface FsLiveDeps {
  send(msg: Record<string, unknown>): void;
  isOpen(): boolean;
  onEvent(type: string, l: (msg: Record<string, unknown>) => void): () => void;
  visible(): boolean;
  onVisibility(l: () => void): () => void;
  setTimer(fn: () => void, ms: number): unknown;
  clearTimer(t: unknown): void;
}

export const FS_DEBOUNCE_MS = 200;

interface Watcher {
  opts: FsWatchOptions;
  onChange: (c: FsChange) => void;
  pending: Set<string>;
  pendingGit: boolean;
  timer: unknown;
}

const groupKey = (mid: string, cwd: string) => JSON.stringify([mid, cwd]);

export interface FsWatchHandle {
  /** The folders (and Git) shown now; the debounce in progress is kept. */
  update(opts: Pick<FsWatchOptions, "paths" | "git">): void;
  stop(): void;
}

export function createFsLive(deps: FsLiveDeps) {
  const watchers = new Set<Watcher>();
  const sent = new Map<string, string>(); // group → what the hub was last told (absent: nothing)
  let started = false;
  let stops: Array<() => void> = [];

  const union = (key: string) => {
    const [mid, cwd] = JSON.parse(key) as [string, string];
    let git = false;
    const paths = new Set<string>();
    for (const w of watchers) {
      if (groupKey(w.opts.mid, w.opts.cwd) !== key) continue;
      git ||= w.opts.git;
      w.opts.paths.forEach((p) => paths.add(p));
    }
    return { mid, cwd, git, paths: [...paths].sort() };
  };

  const keys = () => new Set([...watchers].map((w) => groupKey(w.opts.mid, w.opts.cwd)));

  /** Brings the hub's set for one session folder in line with the watchers (nothing while the page is hidden). */
  function sync(key: string) {
    const u = union(key);
    const empty = !deps.visible() || (u.paths.length === 0 && !u.git);
    const want = empty ? "" : JSON.stringify([u.paths, u.git]);
    if ((sent.get(key) ?? "") === want) return;
    if (!deps.isOpen()) return; // the hub is told on hello
    if (want === "" && !sent.has(key)) return;
    deps.send({ type: "fs_watch", machine_id: u.mid, cwd: u.cwd, paths: empty ? [] : u.paths, git: empty ? false : u.git });
    if (want) sent.set(key, want);
    else sent.delete(key);
  }

  const flush = (w: Watcher) => {
    w.timer = undefined;
    const dirs = [...w.pending];
    const git = w.pendingGit;
    w.pending.clear();
    w.pendingGit = false;
    if (watchers.has(w) && (dirs.length || git)) w.onChange({ dirs, git });
  };

  const queue = (w: Watcher, dirs: string[], git: boolean) => {
    dirs.forEach((d) => w.pending.add(d));
    w.pendingGit ||= git;
    if (w.timer !== undefined) deps.clearTimer(w.timer);
    w.timer = deps.setTimer(() => flush(w), FS_DEBOUNCE_MS);
  };

  /** Everything is refreshed: the page was away, or the socket was. */
  const refreshAll = () => {
    for (const w of watchers) queue(w, w.opts.paths, w.opts.git);
  };

  /** A hidden page rereads nothing: what is waiting is dropped, the return refreshes everything. */
  const dropPending = () => {
    for (const w of watchers) {
      if (w.timer !== undefined) deps.clearTimer(w.timer);
      w.timer = undefined;
      w.pending.clear();
      w.pendingGit = false;
    }
  };

  function begin() {
    if (started) return;
    started = true;
    stops = [
      deps.onEvent("fs_changed", (m) => {
        const mid = m.machine_id,
          cwd = m.cwd;
        const dirs = Array.isArray(m.dirs) ? (m.dirs as string[]) : [];
        for (const w of watchers) {
          if (w.opts.mid !== mid || w.opts.cwd !== cwd) continue;
          const mine = dirs.filter((d) => w.opts.paths.includes(d));
          const git = m.git === true && w.opts.git;
          if (mine.length || git) queue(w, mine, git);
        }
      }),
      // A new socket is a new client for the hub: it knows nothing of what was open, and changes were missed.
      // While hidden the missed changes wait for the return, which refreshes everything anyway.
      deps.onEvent("hello", () => {
        sent.clear();
        keys().forEach(sync);
        if (deps.visible()) refreshAll();
      }),
      deps.onVisibility(() => {
        keys().forEach(sync);
        if (deps.visible()) refreshAll();
        else dropPending();
      }),
    ];
  }

  function end() {
    if (watchers.size > 0 || !started) return;
    started = false;
    stops.forEach((s) => s());
    stops = [];
  }

  return {
    /** Starts watching; the returned handle changes what is shown and stops (the hub is told when nothing is left). */
    watch(opts: FsWatchOptions, onChange: (c: FsChange) => void): FsWatchHandle {
      const w: Watcher = { opts, onChange, pending: new Set(), pendingGit: false, timer: undefined };
      watchers.add(w);
      begin();
      sync(groupKey(opts.mid, opts.cwd));
      return {
        update(next) {
          if (!watchers.has(w)) return;
          w.opts = { ...w.opts, ...next };
          sync(groupKey(w.opts.mid, w.opts.cwd));
        },
        stop() {
          if (!watchers.delete(w)) return;
          if (w.timer !== undefined) deps.clearTimer(w.timer);
          sync(groupKey(w.opts.mid, w.opts.cwd));
          end();
        },
      };
    },
    /** How many panels are watching (tests, and the leak check). */
    get active() {
      return watchers.size;
    },
    get told() {
      return sent.size;
    },
  };
}

export const fsLive = createFsLive({
  send,
  isOpen,
  onEvent: onHubEvent,
  visible: () => document.visibilityState !== "hidden",
  onVisibility: (l) => {
    document.addEventListener("visibilitychange", l);
    return () => document.removeEventListener("visibilitychange", l);
  },
  setTimer: (fn, ms) => window.setTimeout(fn, ms),
  clearTimer: (t) => window.clearTimeout(t as number),
});
