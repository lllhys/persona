import { describe, expect, it } from "vitest";
import { createFsLive, FS_DEBOUNCE_MS, type FsChange, type FsLiveDeps } from "./fs-live";

// A hand-driven page: the socket's sends, the hub's events, the clock and the tab's visibility.
function harness() {
  const sent: Record<string, unknown>[] = [];
  const listeners = new Map<string, Set<(m: Record<string, unknown>) => void>>();
  const vis = new Set<() => void>();
  const timers = new Map<number, { at: number; fn: () => void }>();
  let id = 0;
  let now = 0;
  const st = { open: true, visible: true };
  const deps: FsLiveDeps = {
    send: (m) => void sent.push(m),
    isOpen: () => st.open,
    onEvent: (type, l) => {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type)!.add(l);
      return () => listeners.get(type)!.delete(l);
    },
    visible: () => st.visible,
    onVisibility: (l) => {
      vis.add(l);
      return () => vis.delete(l);
    },
    setTimer: (fn, ms) => {
      timers.set(++id, { at: now + ms, fn });
      return id;
    },
    clearTimer: (t) => void timers.delete(t as number),
  };
  const advance = (ms: number) => {
    now += ms;
    for (const [k, t] of [...timers]) {
      if (t.at <= now) {
        timers.delete(k);
        t.fn();
      }
    }
  };
  return {
    live: createFsLive(deps),
    sent,
    st,
    advance,
    pendingTimers: () => timers.size,
    listenerCount: () => [...listeners.values()].reduce((n, s) => n + s.size, 0) + vis.size,
    emit: (type: string, m: Record<string, unknown>) => listeners.get(type)?.forEach((l) => l({ type, ...m })),
    setVisible(v: boolean) {
      st.visible = v;
      vis.forEach((l) => l());
    },
  };
}

const opts = { mid: "m1", cwd: "/w", paths: ["/w", "/w/src"], git: true };

describe("telling the hub", () => {
  it("sends the open folders when a panel starts and an empty set when the last one stops", () => {
    const h = harness();
    const w = h.live.watch(opts, () => {});
    expect(h.sent).toEqual([{ type: "fs_watch", machine_id: "m1", cwd: "/w", paths: ["/w", "/w/src"], git: true }]);
    w.stop();
    expect(h.sent[1]).toEqual({ type: "fs_watch", machine_id: "m1", cwd: "/w", paths: [], git: false });
    expect(h.live.active).toBe(0);
    expect(h.live.told).toBe(0);
  });

  it("sends again only when the set changes (a folder opened, one closed)", () => {
    const h = harness();
    const w = h.live.watch(opts, () => {});
    w.update({ paths: ["/w", "/w/src"], git: true });
    expect(h.sent).toHaveLength(1);
    w.update({ paths: ["/w", "/w/src", "/w/doc"], git: true });
    expect(h.sent).toHaveLength(2);
    expect(h.sent[1].paths).toEqual(["/w", "/w/doc", "/w/src"]);
    w.update({ paths: ["/w"], git: true });
    expect(h.sent[2].paths).toEqual(["/w"]);
  });

  it("merges the panels of one folder: the file list and the Git panel", () => {
    const h = harness();
    const files = h.live.watch({ ...opts, paths: ["/w"], git: true }, () => {});
    const git = h.live.watch({ ...opts, paths: [], git: true }, () => {});
    expect(h.sent).toHaveLength(1); // the Git panel changed nothing the hub did not already have
    files.stop();
    expect(h.sent[1]).toMatchObject({ paths: [], git: true }); // the Git panel still wants the status
    git.stop();
    expect(h.sent[2]).toMatchObject({ paths: [], git: false });
    expect(h.sent).toHaveLength(3);
  });

  it("keeps two session folders apart", () => {
    const h = harness();
    const a = h.live.watch({ ...opts, cwd: "/a", paths: ["/a"], git: false }, () => {});
    h.live.watch({ ...opts, cwd: "/b", paths: ["/b"], git: false }, () => {});
    a.stop();
    expect(h.sent.map((m) => [m.cwd, m.paths])).toEqual([["/a", ["/a"]], ["/b", ["/b"]], ["/a", []]]);
  });

  it("leaves nothing behind once every panel has stopped", () => {
    const h = harness();
    const a = h.live.watch(opts, () => {});
    const b = h.live.watch({ ...opts, cwd: "/x" }, () => {});
    expect(h.listenerCount()).toBeGreaterThan(0);
    a.stop();
    expect(h.listenerCount()).toBeGreaterThan(0); // b still needs them
    b.stop();
    expect(h.listenerCount()).toBe(0);
    expect(h.pendingTimers()).toBe(0);
    a.stop(); // stopping twice is harmless
    expect(h.sent).toHaveLength(4);
  });

  it("does not send into a closed socket, and sends on hello", () => {
    const h = harness();
    h.st.open = false;
    h.live.watch(opts, () => {});
    expect(h.sent).toHaveLength(0);
    h.st.open = true;
    h.emit("hello", {});
    expect(h.sent).toHaveLength(1);
    h.emit("hello", {}); // a second socket is a second client of the hub
    expect(h.sent).toHaveLength(2);
  });
});

describe("refreshing", () => {
  const events = () => {
    const got: FsChange[] = [];
    return { got, fn: (c: FsChange) => void got.push(c) };
  };

  it("waits 200 ms after the last event and merges what came in between", () => {
    const h = harness();
    const ev = events();
    h.live.watch(opts, ev.fn);
    h.emit("fs_changed", { machine_id: "m1", cwd: "/w", dirs: ["/w"], git: false });
    h.advance(FS_DEBOUNCE_MS - 1);
    h.emit("fs_changed", { machine_id: "m1", cwd: "/w", dirs: ["/w/src"], git: true });
    h.advance(FS_DEBOUNCE_MS - 1);
    expect(ev.got).toHaveLength(0); // the second event moved the end
    h.advance(1);
    expect(ev.got).toEqual([{ dirs: ["/w", "/w/src"], git: true }]);
    h.advance(1000);
    expect(ev.got).toHaveLength(1);
  });

  it("ignores another folder, another machine, and Git when the panel does not show it", () => {
    const h = harness();
    const ev = events();
    h.live.watch({ ...opts, paths: ["/w"], git: false }, ev.fn);
    h.emit("fs_changed", { machine_id: "m2", cwd: "/w", dirs: ["/w"], git: true });
    h.emit("fs_changed", { machine_id: "m1", cwd: "/other", dirs: ["/w"], git: true });
    h.emit("fs_changed", { machine_id: "m1", cwd: "/w", dirs: ["/w/src"], git: false });
    h.emit("fs_changed", { machine_id: "m1", cwd: "/w", dirs: [], git: true });
    h.advance(1000);
    expect(ev.got).toEqual([]);
  });

  it("drops what is pending when the panel stops", () => {
    const h = harness();
    const ev = events();
    const w = h.live.watch(opts, ev.fn);
    h.emit("fs_changed", { machine_id: "m1", cwd: "/w", dirs: ["/w"], git: false });
    w.stop();
    h.advance(1000);
    expect(ev.got).toEqual([]);
  });

  it("stops the hub watching while the page is hidden and refreshes everything when it comes back", () => {
    const h = harness();
    const ev = events();
    h.live.watch(opts, ev.fn);
    h.setVisible(false);
    expect(h.sent[1]).toMatchObject({ paths: [], git: false });
    h.advance(1000);
    expect(ev.got).toEqual([]);
    h.setVisible(true);
    expect(h.sent[2]).toMatchObject({ paths: ["/w", "/w/src"], git: true });
    h.advance(FS_DEBOUNCE_MS);
    expect(ev.got).toEqual([{ dirs: ["/w", "/w/src"], git: true }]);
  });

  it("refreshes everything after a reconnect: changes were missed", () => {
    const h = harness();
    const ev = events();
    h.live.watch(opts, ev.fn);
    h.emit("hello", {});
    h.advance(FS_DEBOUNCE_MS);
    expect(ev.got).toEqual([{ dirs: ["/w", "/w/src"], git: true }]);
  });

  it("a reconnect while the page is hidden rereads nothing until it shows, then once", () => {
    const h = harness();
    const ev = events();
    h.live.watch(opts, ev.fn);
    h.setVisible(false);
    h.emit("hello", {});
    h.advance(FS_DEBOUNCE_MS);
    expect(ev.got).toEqual([]);
    expect(h.pendingTimers()).toBe(0);
    h.setVisible(true);
    h.advance(FS_DEBOUNCE_MS);
    expect(ev.got).toEqual([{ dirs: ["/w", "/w/src"], git: true }]);
  });

  it("a reread still waiting when the page turns hidden is dropped: the return refreshes once", () => {
    const h = harness();
    const ev = events();
    h.live.watch(opts, ev.fn);
    h.emit("hello", {});
    h.setVisible(false);
    expect(h.pendingTimers()).toBe(0);
    h.advance(1000);
    expect(ev.got).toEqual([]);
    h.setVisible(true);
    h.advance(FS_DEBOUNCE_MS);
    expect(ev.got).toEqual([{ dirs: ["/w", "/w/src"], git: true }]);
  });
});
