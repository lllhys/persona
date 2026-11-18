// The personal Agents list reads a refresh's page and counts in one request when the hub says it can
// (`agents_page_counts`, #6496), as the runtime list does with `sessions_page_counts`.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { AGENTS_PAGE_COUNTS_FEATURE, personalSessions, resetAgents, setPersonalPageCounts } from "./agents";
import type { ListCounts } from "./session-list";

vi.mock("./api", async (original) => ({ ...await original<typeof import("./api")>(), api: vi.fn() }));

const counts = (total: number): ListCounts => ({ total, machine: {}, project: {}, group: {}, state: { unread: total }, revision: "r" });
const row = (id: string) => ({ id, source: "lark-bridge", sessionKey: id, provider: "p", title: id, canSend: true, unreadCount: 1, rankTimeMs: 1 });
const query = (path: string) => Object.fromEntries(new URL(path, "https://example.com").searchParams);
const calls = () => vi.mocked(api).mock.calls.map(([path]) => [String(path).split("?")[0], query(String(path))]);

beforeEach(() => {
  vi.mocked(api).mockReset().mockImplementation((path) => {
    const q = query(String(path));
    if (q.counts) return Promise.resolve(counts(2)) as ReturnType<typeof api>;
    const page = { sessions: [row("a"), row("b")], remove: [], has_more: false, revision: q.since ? "r2" : "r1", total: 2 };
    return Promise.resolve(q.with_counts ? { ...page, counts: counts(2) } : page) as ReturnType<typeof api>;
  });
});

afterEach(() => {
  setPersonalPageCounts(false);
  resetAgents();
  personalSessions.reset();
});

describe("personal Agents page counts", () => {
  it("names the hello feature", () => {
    expect(AGENTS_PAGE_COUNTS_FEATURE).toBe("agents_page_counts");
  });

  it("reads the first page and each delta with their counts, one request per refresh", async () => {
    setPersonalPageCounts(true);
    expect(personalSessions.usesPageCounts()).toBe(true);
    await personalSessions.load();
    await personalSessions.load();
    expect(personalSessions.getSnapshot()).toMatchObject({ loaded: true, total: 2, counts: counts(2) });
    expect(calls()).toEqual([
      ["/agents/sessions", { summary: "1", limit: "50", with_counts: "1" }],
      ["/agents/sessions", { summary: "1", limit: "50", since: "r1", with_counts: "1" }],
    ]);
  });

  it("takes two requests per refresh on a hub without the feature, as before", async () => {
    expect(personalSessions.usesPageCounts()).toBe(false);
    await personalSessions.load();
    await personalSessions.load();
    expect(personalSessions.getSnapshot().counts).toEqual(counts(2));
    expect(api).toHaveBeenCalledTimes(4);
    expect(calls().every(([, q]) => !("with_counts" in (q as object)))).toBe(true);
  });
});
