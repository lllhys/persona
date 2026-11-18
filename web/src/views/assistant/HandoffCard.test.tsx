// @vitest-environment jsdom
// A hand-off conversation's write-back as a card in the chat (#5932), and the "Wrap up" chips over its composer.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedItem } from "../../lib/feed";
import type { InboxItem } from "../../lib/inbox";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const box = vi.hoisted(() => ({ items: [] as unknown[], acted: [] as unknown[][], handed: [] as unknown[][] }));
vi.mock("../../lib/inbox", async (load) => {
  const real = await load<typeof import("../../lib/inbox")>();
  const state = () => ({ items: box.items as InboxItem[], history: [], loaded: true, routing: [], sending: [] });
  return {
    ...real,
    useInbox: <T,>(select: (s: ReturnType<typeof state>) => T) => select(state()),
    loadRoutedItem: () => undefined,
    suggestionAct: (...a: unknown[]) => {
      box.acted.push(a);
      return Promise.resolve(true);
    },
    handOffToMain: (...a: unknown[]) => {
      box.handed.push(a);
      return Promise.resolve({ machine_id: "server", session_id: "m1" });
    },
  };
});

import { HandoffCard, WrapUpChips } from "./HandoffCard";

const feed: FeedItem = {
  id: "f1", conversation: "m1", at: "2026-11-13T09:00:00Z", kind: "inbox_suggestion", ref: "lark:f1:100",
  body: { version: 2, kind: "reply", text: "周五上午给你", recap: "示例客户要报价，已定周五", title: "示例客户", source: "lark", account: "飞书" },
};
const open = (patch: Record<string, unknown> = {}) => ({
  id: "lark:f1:100", source: "lark", kind: "dm", title: "示例客户", preview: "", count: 1, tier: "action",
  suggestion: { version: 2, kind: "reply", text: "周五上午给你", conversation: { machine_id: "server", session_id: "m1" } },
  handoffs: [{ machine_id: "server", session_id: "m1", at: "2026-11-13T09:00:00Z" }],
  ...patch,
});

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  box.items = [];
  box.acted = [];
  box.handed = [];
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const draw = (node: React.ReactNode) => act(() => root.render(node));

describe("the hand-off card", () => {
  it("shows the summary and an editable reply while the item waits for it, and Send confirms the item's suggestion with the edit", async () => {
    box.items = [open()];
    draw(<HandoffCard item={feed} />);
    expect(host.textContent).toContain("示例客户要报价，已定周五");
    expect(host.textContent).toContain("以你的名义回复「示例客户」");
    const area = host.querySelector("textarea")!;
    expect(area.value).toBe("周五上午给你");
    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
    act(() => {
      set.call(area, "周五上午发你报价");
      area.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const send = host.querySelector<HTMLButtonElement>('[data-testid="handoff-card-confirm"]')!;
    expect(send.textContent).toContain("发送");
    await act(async () => send.click());
    expect(box.acted).toHaveLength(1);
    expect(box.acted[0][1]).toBe("confirm");
    expect(box.acted[0][2]).toBe("周五上午发你报价");
  });

  it("says what went out, without Send, once the reply was sent", () => {
    box.items = [open({ done_at: "2026-11-13T10:00:00Z", done_by: "replied", suggestion: undefined, chain: [{ id: "c1", kind: "confirmed", text: "", detail: "周五上午给你" }] })];
    draw(<HandoffCard item={feed} />);
    expect(host.querySelector('[data-testid="handoff-card"]')?.getAttribute("data-state")).toBe("sent");
    expect(host.textContent).toContain("已以你的名义回复");
    expect(host.querySelector("textarea")).toBeNull();
    expect(host.querySelector('[data-testid="handoff-card-confirm"]')).toBeNull();
  });

  it("offers nothing to send once a newer suggestion replaced it", () => {
    box.items = [open({ suggestion: { version: 3, kind: "reply", text: "新的", conversation: { machine_id: "server", session_id: "m1" } } })];
    draw(<HandoffCard item={feed} />);
    expect(host.textContent).toContain("已不是当前建议，没有发出");
    expect(host.querySelector('[data-testid="handoff-card-confirm"]')).toBeNull();
  });

  it("offers Got it for a conclusion worth knowing", () => {
    box.items = [open({ suggestion: { version: 2, kind: "conclusion", text: "只是通知", conversation: { machine_id: "server", session_id: "m1" } } })];
    draw(<HandoffCard item={{ ...feed, body: { ...feed.body, kind: "conclusion", outcome: "fyi", text: "只是通知" } }} />);
    expect(host.querySelector("textarea")).toBeNull();
    expect(host.querySelector('[data-testid="handoff-card-confirm"]')?.textContent).toBe("知道了");
  });
});

describe("Wrap up chips", () => {
  it("ask the conversation for the final write-back through the item's hand-off", async () => {
    box.items = [open()];
    draw(<WrapUpChips conversation="m1" />);
    const chip = host.querySelector<HTMLButtonElement>('[data-testid="wrap-up-chip"]')!;
    expect(chip.textContent).toBe("收尾：示例客户待发送");
    await act(async () => chip.click());
    expect(box.handed).toHaveLength(1);
    expect((box.handed[0][0] as InboxItem).id).toBe("lark:f1:100");
    expect(box.handed[0][1]).toBe("可以收尾了：把最终的总结和回复写回这条收件箱事项。");
  });

  it("say a reply waits to be sent, another suggestion waits to be confirmed, and nothing while it is worked on", () => {
    const wait = () => host.querySelector('[data-testid="wrap-up-wait"]')?.textContent ?? null;
    const redraw = () => {
      act(() => root.unmount()); // the chips are memoized on the conversation: start over for each item state
      root = createRoot(host);
      draw(<WrapUpChips conversation="m1" />);
    };
    box.items = [open({ handling: { state: "done" } })];
    redraw();
    expect(wait()).toBe("待发送");
    box.items = [open({ suggestion: { version: 3, kind: "conclusion", text: "已处理" } })];
    redraw();
    expect(wait()).toBe("待确认");
    box.items = [open({ suggestion: undefined, handling: { state: "looking" } })];
    redraw();
    expect(host.querySelector('[data-testid="wrap-up-chip"]')?.textContent).toBe("收尾：示例客户");
    expect(wait()).toBeNull();
  });

  it("are gone once the item is handled with nothing waiting for the owner", () => {
    box.items = [open({ suggestion: undefined, handling: { state: "done" } })];
    draw(<WrapUpChips conversation="m1" />);
    expect(host.querySelector('[data-testid="wrap-up-chip"]')).toBeNull();
  });

  it("are not drawn in a conversation nothing was handed to", () => {
    box.items = [open()];
    draw(<WrapUpChips conversation="m2" />);
    expect(host.querySelector('[data-testid="wrap-up-chip"]')).toBeNull();
  });
});
