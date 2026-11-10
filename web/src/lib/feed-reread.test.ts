import { afterEach, expect, it, vi } from "vitest";
import { getFeedState, loadFeed, type FeedItem } from "./feed";

afterEach(() => vi.unstubAllGlobals());

it("a read asked for while one is out (a reconnect's hello) runs after it, so items pushed in between are not lost", async () => {
  const item = (id: string, s: number): FeedItem => ({ id, conversation: "c-race", at: new Date(Date.parse("2026-01-05T00:00:00Z") + s * 1000).toISOString(), kind: "routed", ref: "r" });
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  const urls: string[] = [];
  const fetcher = vi.fn(async (url: string) => {
    urls.push(url);
    if (urls.length === 1) { await gate; return { ok: true, status: 200, text: async () => JSON.stringify({ items: [item("a", 1)] }) }; }
    return { ok: true, status: 200, text: async () => JSON.stringify({ items: [item("b", 2)] }) };
  });
  vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
  vi.stubGlobal("fetch", fetcher);
  const first = loadFeed("c-race");
  await Promise.resolve();
  // the socket dropped and came back without resuming while the first read was out: its answer predates item b
  const second = loadFeed("c-race");
  release();
  await Promise.all([first, second]);
  await vi.waitFor(() => expect(getFeedState().feeds["c-race"]?.map((i) => i.id)).toEqual(["a", "b"]));
  expect(urls).toHaveLength(2);
  expect(new URL(urls[1], "http://hub").searchParams.get("since")).toBe(item("a", 1).at);
});

it("a read that fails drops the one asked for meanwhile (an older hub has no feed), and a later read asks once", async () => {
  let release!: () => void;
  const gate = new Promise<void>((r) => (release = r));
  const urls: string[] = [];
  const fetcher = vi.fn(async (url: string) => {
    urls.push(url);
    if (urls.length === 1) await gate;
    return { ok: false, status: 404, text: async () => "not found" };
  });
  vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
  vi.stubGlobal("fetch", fetcher);
  const first = loadFeed("c-old-hub");
  await Promise.resolve();
  const second = loadFeed("c-old-hub");
  release();
  await Promise.all([first, second]);
  expect(urls).toHaveLength(1);
  await loadFeed("c-old-hub");
  expect(urls).toHaveLength(2);
});
