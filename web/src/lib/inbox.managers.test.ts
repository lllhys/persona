// @vitest-environment jsdom
// Several managers (docs/assistant/managers.md "Clients"): an inbox item is its id and its manager. Two managers may list the
// same id (one account in both): each copy keeps its own state, and every request about one names its manager — its
// mutes, rules and Trail too, which each manager keeps on its own.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./store", () => ({ onHubEvent: () => () => {} }));
vi.mock("../ui/atoms", () => ({ toast: vi.fn() }));

import {
  fetchInboxTree, getInbox, itemKey, itemRoute, listInboxMutes, listInboxPolicies, muteItem, reloadInbox, removeInboxPolicy, replaceItem,
  resetInboxForTests, saveInboxPolicy, setAllDone, setDone, unmuteInbox, type InboxItem,
} from "./inbox";
import { resetManagerChoice, setRosterDefault } from "./manager-choice";

const at = (minutes: number) => new Date(Date.UTC(2026, 9, 30, 10, 0) - minutes * 60_000).toISOString();
const item = (id: string, patch: Partial<InboxItem> = {}): InboxItem => ({
  id, source: "lark", kind: "dm", account: "feishu", account_name: "Lark", title: id, preview: "text", count: 1, updated_at: at(10), tier: "action", ...patch,
});
const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body) });
const chat = { tag: "chat", value: "oc_1", name: "Example group" };

let fetcher: ReturnType<typeof vi.fn>;
const header = (i: number) => new Headers(fetcher.mock.calls[i][1]?.headers).get("X-Persona-Manager");

beforeEach(async () => {
  resetInboxForTests();
  resetManagerChoice();
  setRosterDefault("assistant");
  // the same chat item in two managers' inboxes (one Lark account in both), and another of the default's
  const items = [
    item("lark:f1:1", { manager: "assistant", mutes: [chat], lark: { feed_id: "f1", is_group: true } }),
    item("lark:f1:1", { manager: "m-1", mutes: [chat], lark: { feed_id: "f1", is_group: true } }),
    item("lark:f1:2", { manager: "assistant", mutes: [chat], lark: { feed_id: "f1", is_group: true } }),
  ];
  fetcher = vi.fn().mockResolvedValueOnce(ok({ items }));
  vi.stubGlobal("fetch", fetcher);
  await reloadInbox();
});

afterEach(() => {
  vi.unstubAllGlobals();
  resetManagerChoice();
});

describe("an item is its id and its manager", () => {
  it("keeps two managers' copies of one id apart", () => {
    const [a, b] = getInbox().items;
    expect(getInbox().items).toHaveLength(3);
    expect(itemKey(a)).not.toBe(itemKey(b));
  });

  it("marks only the copy asked about, and names its manager", async () => {
    const b = getInbox().items[1];
    fetcher.mockResolvedValueOnce(ok({ ...b, manager: undefined, done_at: at(0), done_by: "user" }));
    await setDone(b, true);
    expect(header(1)).toBe("m-1");
    expect(getInbox().items.map((x) => Boolean(x.done_at))).toEqual([false, true, false]);
    // an unstamped answer goes to the copy of the manager it came from
    replaceItem({ ...b, manager: undefined, title: "fresh" }, "m-1");
    expect(getInbox().items.map((x) => x.title)).toEqual(["lark:f1:1", "fresh", "lark:f1:2"]);
  });

  it("files both copies in a bulk, one request per manager, each answer to its own", async () => {
    const [a, b] = getInbox().items;
    const filed = (x: InboxItem) => ({ ...x, manager: undefined, done_at: at(0), done_by: "user" });
    fetcher.mockResolvedValueOnce(ok({ changed: [filed(a)], unchanged: [], skipped: [] })).mockResolvedValueOnce(ok({ changed: [], unchanged: [], skipped: [{ id: b.id, reason: "changed" }] }));
    fetcher.mockResolvedValue(ok({ items: getInbox().items })); // the reload a skip asks for
    await setAllDone([a, b], true, false);
    expect([header(1), header(2)]).toEqual([null, "m-1"]);
    // the default's copy is filed; the same id of m-1 was left by its manager and stays open
    expect(getInbox().items.slice(0, 2).map((x) => Boolean(x.done_at))).toEqual([true, false]);
  });

  it("routes another manager's copy with its manager, the default's without", () => {
    const [a, b] = getInbox().items;
    expect(itemRoute(a)).toEqual({ name: "inbox", id: "lark:f1:1" });
    expect(itemRoute(b)).toEqual({ name: "inbox", id: "lark:f1:1", manager: "m-1" });
  });
});

describe("mutes, rules and the Trail are each manager's", () => {
  it("mutes for the item's manager, and files only that manager's items it covers", async () => {
    const b = getInbox().items[1];
    fetcher.mockResolvedValueOnce(ok({}));
    await muteItem(b, chat);
    expect(fetcher.mock.calls[1][0]).toBe("/api/v1/inbox/mutes");
    expect(header(1)).toBe("m-1");
    expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ item_id: "lark:f1:1", tag: "chat" });
    // the default's items of the same chat are not muted by it
    expect(getInbox().items.map((x) => Boolean(x.done_at))).toEqual([false, true, false]);
  });

  it("lists and unmutes a manager's mutes with its name", async () => {
    fetcher.mockResolvedValueOnce(ok({ mutes: [{ id: "u1", source: "lark", tag: "chat", value: "oc_1", name: "Example group" }] }));
    const mutes = await listInboxMutes("m-1");
    expect(header(1)).toBe("m-1");
    expect(mutes[0].manager).toBe("m-1");
    fetcher.mockResolvedValueOnce(ok({}));
    await unmuteInbox(mutes[0]);
    expect(fetcher.mock.calls[2][0]).toBe("/api/v1/inbox/mutes/u1");
    expect(header(2)).toBe("m-1");
    // the default's is named too: the device's default may be stale, and the list stamps what it read with the one it asked
    fetcher.mockResolvedValueOnce(ok({ mutes: [] }));
    await listInboxMutes("assistant");
    expect(header(3)).toBe("assistant");
    fetcher.mockResolvedValueOnce(ok({ policies: [] }));
    await listInboxPolicies("assistant");
    expect(header(4)).toBe("assistant");
  });

  it("unmutes and removes the rule of the manager a list was read from after it became the default", async () => {
    fetcher.mockResolvedValueOnce(ok({ mutes: [{ id: "u1", source: "lark", tag: "chat", value: "oc_1", name: "Example group" }] }));
    const mutes = await listInboxMutes("m-1");
    fetcher.mockResolvedValueOnce(ok({ policies: [{ id: "p1", source: "lark", tag: "chat", value: "oc_1", name: "Example group" }] }));
    const rules = await listInboxPolicies("m-1");
    setRosterDefault("m-1");
    fetcher.mockResolvedValueOnce(ok({}));
    await unmuteInbox(mutes[0]);
    expect(header(3)).toBe("m-1");
    fetcher.mockResolvedValueOnce(ok({}));
    await removeInboxPolicy(rules[0]);
    expect(header(4)).toBe("m-1");
    // one manager: the lists are read unstamped, and nothing is named
    fetcher.mockResolvedValueOnce(ok({ mutes: [{ id: "u2", source: "lark", tag: "chat", value: "oc_2", name: "Another group" }] }));
    const [mine] = await listInboxMutes();
    fetcher.mockResolvedValueOnce(ok({ policies: [{ id: "p2", source: "lark", tag: "chat", value: "oc_2", name: "Another group" }] }));
    const [rule] = await listInboxPolicies();
    fetcher.mockResolvedValueOnce(ok({}));
    await unmuteInbox(mine);
    fetcher.mockResolvedValueOnce(ok({}));
    await removeInboxPolicy(rule);
    expect([5, 6, 7, 8].map(header)).toEqual([null, null, null, null]);
  });

  it("sets, lists and removes a rule of the item's manager, which reaches only its items", async () => {
    const b = getInbox().items[1];
    fetcher.mockResolvedValueOnce(ok({ policy: { id: "p1", source: "lark", tag: "chat", value: "oc_1", name: "Example group", tier: "noise" } }));
    const saved = await saveInboxPolicy({ item: b, option: chat, source: "lark" }, "noise", "");
    expect(header(1)).toBe("m-1");
    expect(saved?.manager).toBe("m-1");
    expect(getInbox().items.map((x) => x.tier)).toEqual(["action", "noise", "action"]);
    fetcher.mockResolvedValueOnce(ok({ policies: [{ id: "p1", source: "lark", tag: "chat", value: "oc_1", name: "Example group" }] }));
    const rules = await listInboxPolicies("m-1");
    expect(header(2)).toBe("m-1");
    expect(rules[0].manager).toBe("m-1");
    fetcher.mockResolvedValueOnce(ok({ policy: { ...rules[0], tier: "fyi" } }));
    await saveInboxPolicy({ option: rules[0], source: "lark", id: "p1", manager: rules[0].manager }, "fyi", "");
    expect(fetcher.mock.calls[3][1].method).toBe("PATCH");
    expect(header(3)).toBe("m-1");
    fetcher.mockResolvedValueOnce(ok({}));
    await removeInboxPolicy(rules[0]);
    expect(header(4)).toBe("m-1");
  });

  it("reads the Trail from the item's manager", async () => {
    fetcher.mockResolvedValueOnce(ok({ node: { kind: "inbox", id: "lark:f1:1" }, ancestors: [], children: [] }));
    await fetchInboxTree("inbox", "lark:f1:1", "m-1");
    expect(fetcher.mock.calls[1][0]).toBe("/api/v1/inbox/tree/inbox/lark:f1:1");
    expect(header(1)).toBe("m-1");
    fetcher.mockResolvedValueOnce(ok({ node: { kind: "inbox", id: "lark:f1:2" }, ancestors: [], children: [] }));
    await fetchInboxTree("inbox", "lark:f1:2", "assistant");
    expect(header(2)).toBeNull();
  });
});
