// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setToken } from "./api";
import { feedCacheScope, readFeedCache, readMemoryPage, writeMemoryPage, writeFeedCache } from "./feed-cache";
import { getState, markConversationDeleted, refresh, refreshAssistant, reset } from "./store";
import { resetManagerChoice } from "./manager-choice";

const cached = { machineID: "hub", sessionID: "old", items: [{ id: "one", seq: 1, role: "user", kind: "text", text: "Last visit" }], hasMore: false } as const;
const seed = () => writeFeedCache(feedCacheScope()!, { ...cached, items: [...cached.items] });
const answer = (main: string) => ({ machine_id: "hub", main, conversations: [] });

beforeEach(() => {
  reset();
  resetManagerChoice();
  localStorage.clear();
  setToken("test-credential-a");
  seed();
});
afterEach(() => { reset(); resetManagerChoice(); vi.unstubAllGlobals(); });

it("applies the main conversation before a slow sessions list and discards the old reference", async () => {
  let finishSessions!: (value: Response) => void;
  vi.stubGlobal("fetch", vi.fn((url: string) => url.endsWith("/sessions")
    ? new Promise<Response>((done) => { finishSessions = done; })
    : Promise.resolve(Response.json(url.endsWith("/assistant") ? answer("new") : {}))));
  const pending = refresh();
  await vi.waitFor(() => expect(getState().assistant?.main).toBe("new"));
  expect(getState().loaded).toBe(false);
  expect(readFeedCache()).toBeNull();
  finishSessions(Response.json({ sessions: [] }));
  await pending;
});

it("keeps the cache when assistant refresh fails, and removes it when the hub removes the main chat", async () => {
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(Response.json({ error: "Unavailable" }, { status: 503 }))));
  await expect(refreshAssistant()).rejects.toThrow();
  expect(readFeedCache()?.sessionID).toBe("old");
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(Response.json(answer("")))));
  await refreshAssistant();
  expect(readFeedCache()).toBeNull();
});

it("keeps the cold-start loading gate while the assistant reference is still pending", async () => {
  let finishAssistant!: (value: Response) => void;
  vi.stubGlobal("fetch", vi.fn((url: string) => url.endsWith("/assistant")
    ? new Promise<Response>((done) => { finishAssistant = done; })
    : Promise.resolve(Response.json({}))));
  const pending = refresh();
  await new Promise((done) => setTimeout(done, 0));
  expect(getState().loaded).toBe(false);
  finishAssistant(Response.json(answer("old")));
  await pending;
  expect(getState().loaded).toBe(true);
});

it("clears persisted content on account changes and logout, including a return to the same credential", () => {
  const oldScope = feedCacheScope()!;
  setToken("test-credential-b");
  expect(readFeedCache()).toBeNull();
  setToken("test-credential-a");
  expect(readFeedCache()).toBeNull();
  seed();
  setToken("");
  expect(readFeedCache()).toBeNull();
  setToken("test-credential-a");
  expect(writeFeedCache(oldScope, { ...cached, items: [...cached.items] })).toBe(false);
  expect(readFeedCache()).toBeNull();
});

it("purges deleted persistent and memory transcripts and refuses late cache writes", () => {
  const scope = feedCacheScope();
  const page = { ...cached, items: [...cached.items] };
  writeMemoryPage(scope, page);
  writeMemoryPage(scope, { ...page, sessionID: "kept" });
  expect(readMemoryPage(scope, "hub", "old")).not.toBeNull();
  markConversationDeleted("hub", "old");
  expect(readFeedCache()).toBeNull();
  expect(readMemoryPage(scope, "hub", "old")).toBeNull();
  expect(readMemoryPage(scope, "hub", "kept")).not.toBeNull();
  writeMemoryPage(scope, page);
  expect(writeFeedCache(scope, page)).toBe(false);
  expect(readMemoryPage(scope, "hub", "old")).toBeNull();
});
