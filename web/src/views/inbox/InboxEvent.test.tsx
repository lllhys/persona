// @vitest-environment jsdom
// Inbox events on the page (docs/web/web.md "Inbox"): the "In progress" section and the rows' marks, the detail's summary → recap →
// suggestion card → handling chain, "Edit" / "Say something", "Original"; and an older hub's items (none of the fields) exactly as before.
// Driven by the fake hub's fixture (scripts/mock/inbox_events.json).
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
  // a Feishu item's chat draws under its event (LarkThread): what jsdom lacks
  Object.assign(globalThis, {
    ResizeObserver: globalThis.ResizeObserver ?? class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: globalThis.IntersectionObserver ?? class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } },
  });
  globalThis.CSS ??= { escape: (s: string) => s } as unknown as typeof CSS;
});

const calls = vi.hoisted(() => ({ list: [] as { path: string; json?: unknown }[], items: [] as unknown[] }));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { json?: unknown }) => {
    calls.list.push({ path, json: init?.json });
    if (path.startsWith("/inbox?")) return Promise.resolve({ items: calls.items });
    if (path === "/mail/accounts") return Promise.resolve({ accounts: [] });
    if (path.endsWith("/suggestion")) return Promise.reject(new Error("offline"));
    return Promise.resolve({});
  },
}));
// the store's first snapshot is in (the list waits for it), with no agents waiting
vi.mock("../../lib/store", async (orig) => {
  const m = await orig<typeof import("../../lib/store")>();
  return { ...m, useStore: <T,>(sel: (s: ReturnType<typeof m.getState>) => T) => m.useStore((s) => sel({ ...s, loaded: true })) };
});
vi.mock("../../lib/extension-slots", () => ({ useInboxItemPlace: () => undefined }));
vi.mock("../../lib/translate", () => ({ useTranslatePrefs: () => ({ enabled: false }), hubTranslation: () => undefined, translatesChat: () => false }));

import type { InboxItem } from "../../lib/inbox";
import { resetInboxForTests } from "../../lib/inbox";
import { resetMailForTests } from "../../lib/mail";
import { InboxDetail } from "./InboxDetail";
import { EventBody } from "./InboxEvent";
import { InboxList } from "./InboxList";
import { settleRosterDefault } from "../../lib/manager-choice";

// the roster's default is known, as once the app has read it: an item's page draws at once
settleRosterDefault();

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const fixture = JSON.parse(readFileSync(join(__dirname, "../../../../scripts/mock/inbox_events.json"), "utf8")) as { items: InboxItem[] };
const byId = (id: string) => fixture.items.find((x) => x.id === id)!;
const reply = byId("lark:oc_demo1:100");
const urgent = byId("lark:oc_demo2:200");
const waiting = byId("lark:oc_demo3:300");
const agentSuggestion = byId("mail:a1:contract:88");
const failed = byId("lark:oc_demo6:600");
const conclusion = byId("lark:oc_demo7:700");
const managerDone = byId("mail:a1:notice:110");
const legacy = (it: InboxItem): InboxItem => {
  const { status: _s, urgent: _u, recap: _r, handling: _h, suggestion: _g, chain: _c, related: _rel, ...rest } = it;
  // an older hub never files as the manager
  return rest.done_by === "manager" ? { ...rest, done_by: "user" } : rest;
};
/** An item with no source page of its own (no chat, no mail): its body is the preview, so "Original" needs no reads. */
const plain = (it: InboxItem): InboxItem => ({ ...it, lark: undefined, mail: undefined });

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const buttons = () => [...host.querySelectorAll("button")].map((b) => b.textContent?.trim() ?? "");
const press = async (text: string) => {
  const b = [...host.querySelectorAll("button")].find((x) => x.textContent?.trim() === text);
  if (!b) throw new Error(`no button ${text}: ${buttons().join(" | ")}`);
  await act(async () => b.click());
};

beforeEach(() => {
  resetInboxForTests();
  resetMailForTests();
  calls.list = [];
  calls.items = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("收件箱事件：详情", () => {
  it("draws 摘要, 总结, the reply suggestion with 驳回 · 修改 · 发送, then the chain folded to its newest entry", async () => {
    act(() => root.render(<EventBody item={reply} onSource={() => {}} />));
    const text = host.textContent ?? "";
    expect(text.indexOf(reply.summary!)).toBeLessThan(text.indexOf(reply.recap!));
    expect(text.indexOf(reply.recap!)).toBeLessThan(text.indexOf("建议回复"));
    expect(text.indexOf("建议回复")).toBeLessThan(text.indexOf("处理链条"));
    expect(host.querySelector("[data-suggestion-text]")?.textContent).toBe(reply.suggestion!.text);
    expect(buttons()).toEqual(expect.arrayContaining(["驳回", "修改", "发送"]));
    expect(host.querySelectorAll("[data-chain-entry]")).toHaveLength(1);
    expect(host.querySelector("[data-chain-entry]")?.getAttribute("data-chain-entry")).toBe("handling");
    await press("展开全部（3）");
    expect([...host.querySelectorAll("[data-chain-entry]")].map((e) => e.getAttribute("data-chain-entry"))).toEqual(["received", "triaged", "handling"]);
    expect(buttons()).toContain("收起");
  });
  it("labels each kind's buttons", () => {
    act(() => root.render(<EventBody item={agentSuggestion} onSource={() => {}} />));
    expect(buttons()).toEqual(expect.arrayContaining(["驳回", "修改", "开始"]));
    act(() => root.render(<EventBody item={conclusion} onSource={() => {}} />));
    expect(buttons()).toEqual(expect.arrayContaining(["驳回", "知道了"]));
    expect(buttons()).not.toContain("修改");
    act(() => root.render(<EventBody item={{ ...reply, suggestion: { ...reply.suggestion!, handback: true } }} onSource={() => {}} />));
    expect(buttons()).toEqual(expect.arrayContaining(["驳回", "修改", "确认"]));
    expect(host.textContent).toContain("需要你判断");
  });
  it("says 「AI 正在准备建议」 for 急事, why it failed, and what a working one waits for", () => {
    act(() => root.render(<EventBody item={urgent} onSource={() => {}} />));
    expect(host.querySelector("[data-event-preparing]")?.textContent).toContain("AI 正在准备建议");
    expect(host.querySelector("[data-suggestion]")).toBeNull();
    act(() => root.render(<EventBody item={failed} onSource={() => {}} />));
    expect(host.querySelector("[data-event-failed]")?.textContent).toContain("处理超时了");
    act(() => root.render(<EventBody item={waiting} onSource={() => {}} />));
    expect(host.querySelector('[data-event-handling="waiting"]')?.textContent).toContain("等对方回复");
    expect(host.querySelector("[data-event-related]")?.textContent).toContain("评审材料");
  });
  it("修改: edits the text and confirms it, or 说一句话 goes back as an instruction", async () => {
    act(() => root.render(<EventBody item={reply} onSource={() => {}} />));
    await press("修改");
    const area = host.querySelector<HTMLTextAreaElement>("[data-suggestion-editor]")!;
    expect(area.value).toBe(reply.suggestion!.text);
    const say = host.querySelector<HTMLInputElement>("[data-suggestion-say]")!;
    const sayButton = () => [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "说一句话")!;
    expect(sayButton().disabled).toBe(true);
    // a React-controlled input: set the value through the native setter, then fire input
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    await act(async () => {
      setValue.call(say, "别这么回，先问时间");
      say.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(sayButton().disabled).toBe(false);
    await act(async () => sayButton().click());
    await flush();
    const sent = calls.list.find((c) => c.path.endsWith("/suggestion"));
    expect(sent?.path).toBe("/inbox/lark:oc_demo1:100/suggestion");
    expect(sent?.json).toMatchObject({ version: 2, action: "instruct", text: "别这么回，先问时间" });
    // it did not go through (offline here): the editor stays with the words
    expect(host.querySelector("[data-suggestion-editor]")).not.toBeNull();
  });
  it("a chain entry goes to its source at its position, or opens its messages in place", async () => {
    const onSource = vi.fn();
    act(() => root.render(<EventBody item={reply} onSource={onSource} />));
    await press("展开全部（3）");
    const received = host.querySelector('[data-chain-entry="received"]')!;
    await act(async () => received.querySelector("button")!.click());
    expect(onSource).toHaveBeenCalledWith({ position: "100", uid: undefined });
    await act(async () => received.querySelector<HTMLButtonElement>("button[aria-label]")!.click());
    expect(received.querySelector("[data-chain-detail]")?.textContent).toContain("客户催报价了");
    const triaged = host.querySelector('[data-chain-entry="triaged"]')!;
    await act(async () => triaged.querySelector("button")!.click());
    expect(triaged.querySelector("[data-chain-detail]")?.textContent).toBe("需要你 · 私聊直接提问");
  });
  it("opens on the event, 看原文 shows the item's own page, 看处理 goes back", async () => {
    act(() => root.render(<InboxDetail item={plain(reply)} loaded />));
    expect(host.querySelector("[data-event-body]")).not.toBeNull();
    expect(host.textContent).not.toContain(reply.preview);
    await press("看原文");
    expect(host.querySelector("[data-event-body]")).toBeNull();
    expect(host.textContent).toContain(reply.preview);
    await press("看处理");
    expect(host.querySelector("[data-event-body]")).not.toBeNull();
  });
  it("a Feishu item: the event above its chat, no 看原文; a chain entry scrolls the chat to its message (#3262)", async () => {
    const item: InboxItem = { ...reply, lark: { ...reply.lark!, start_position: "102" } };
    const around = () => calls.list.map((c) => c.path).filter((p) => p.includes("around="));
    act(() => root.render(<InboxDetail item={item} loaded />));
    await flush();
    expect(host.querySelector('[data-event-body="embedded"]')).not.toBeNull();
    expect(buttons()).not.toContain("看原文");
    expect(around().at(-1)).toContain("around=102"); // the chat, at the first new message
    await press("展开全部（3）");
    const received = host.querySelector('[data-chain-entry="received"]')!;
    await act(async () => received.querySelector("button")!.click());
    await flush();
    expect(host.querySelector('[data-event-body="embedded"]')).not.toBeNull(); // the page stays
    expect(around().at(-1)).toContain("around=100");
  });
  it("an older hub's item: no event body, no 看原文, the AI 分身's draft as before", () => {
    const old = renderToStaticMarkup(<InboxDetail item={plain(legacy(reply))} loaded />);
    expect(old).not.toContain("data-event-body");
    expect(old).not.toContain("看原文");
    expect(old).toContain("查看草稿");
    expect(old).toContain(reply.preview);
    // with a suggestion the draft is the suggestion: not offered twice
    expect(renderToStaticMarkup(<InboxDetail item={plain(reply)} loaded />)).not.toContain("查看草稿");
  });
});

describe("收件箱事件：已处理", () => {
  it("says the manager filed it, and draws the chain's 已处理 entry with its check, no link", async () => {
    act(() => root.render(<EventBody item={managerDone} onSource={() => {}} />));
    expect(host.querySelector("[data-event-manager-done]")?.textContent).toBe("助手处理了");
    expect(host.querySelector("[data-suggestion]")).toBeNull();
    const done = host.querySelector('[data-chain-entry="done"]')!;
    expect(done.textContent).toContain("管理者看完，不用管");
    expect(done.querySelector("button")!.disabled).toBe(true); // no link, nothing to expand
    expect(done.querySelector("svg path")?.getAttribute("d")).toBeTruthy();
  });
  it("a conclusion in 知悉即可 stays listed with its card until 知道了", () => {
    act(() => root.render(<EventBody item={conclusion} onSource={() => {}} />));
    expect(host.querySelector('[data-suggestion="conclusion"]')).not.toBeNull();
  });
});

describe("收件箱事件：列表", () => {
  it("has 进行中 between 需要你 and 知悉即可, marks the rows, and draws a suggestion instead of the draft", async () => {
    calls.items = fixture.items;
    act(() => root.render(<InboxList />));
    await flush();
    const titles = [...host.querySelectorAll("[data-section-title]")].map((s) => s.textContent);
    expect(titles).toEqual(["需要你", "进行中", "知悉即可", "已折叠", "已处理"]);
    const row = (id: string) => host.querySelector(`[data-item="${id}"]`)!;
    expect(row(reply.id).querySelector('[data-event-mark="suggestion"]')?.textContent).toContain("建议回复");
    expect(row(reply.id).textContent).not.toContain("AI 分身草拟的回复"); // the mirrored draft is not drawn too
    expect(row(urgent.id).querySelector('[data-event-mark="preparing"]')?.textContent).toContain("AI 正在准备建议");
    expect(row(waiting.id).querySelector('[data-event-mark="working"]')?.textContent).toBe("等对方回复");
    expect(row(failed.id).querySelector('[data-event-mark="failed"]')).not.toBeNull();
    expect(row(managerDone.id).querySelector('[data-event-mark="manager-done"]')?.textContent).toBe("助手处理了");
    expect(row(conclusion.id).closest("[data-section]")?.getAttribute("data-section")).toBe("fyi");
    // In progress never folds: no toggle on its title, its rows always shown
    const working = host.querySelector('[data-section="working"]')!;
    expect(working.querySelector("[aria-expanded]")).toBeNull();
    expect(working.querySelector(`[data-item="${waiting.id}"]`)).not.toBeNull();
    expect(host.querySelector('[data-section="action"] [aria-expanded]')).not.toBeNull();
  });
  it("an older hub's list: the four sections and the draft card as before", async () => {
    calls.items = fixture.items.map(legacy);
    act(() => root.render(<InboxList />));
    await flush();
    const titles = [...host.querySelectorAll("[data-section-title]")].map((s) => s.textContent);
    expect(titles).toEqual(["需要你", "知悉即可", "已折叠", "已处理"]);
    expect(host.querySelector("[data-event-mark]")).toBeNull();
    expect(host.querySelector(`[data-item="${reply.id}"]`)?.textContent).toContain("AI 分身草拟的回复");
  });
});
