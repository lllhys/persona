// @vitest-environment jsdom
// #2602: background continuation stays in the ordinary summary, never a second tool drawer.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chatLines } from "../../lib/turns";
import { emptyTaskIndex } from "../../lib/tasks";
import type { Item } from "../../lib/types";
import { ChatContext } from "./chat-context";
import { forgetExpanded } from "./expanded";
import { ChatLineView } from "./Lines";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
const item = (seq: number, kind: Item["kind"], text?: string): Item => ({ id: `i${seq}#0`, seq, role: kind === "notice" || kind === "recap" ? "system" : "assistant", kind, text, ts: new Date(Date.parse("2026-11-04T10:00:00Z") + seq * 1000).toISOString() });
const tool = (seq: number): Item => ({ ...item(seq, "tool_use"), tool_name: "Bash", tool_use_id: `call-${seq}`, tool_input: { command: `echo example-${seq}` } });
const user = { ...item(1, "text", "Check the example."), role: "user" as const };
const wake = { ...item(4, "notice", "Background task done"), wake: true };
const first = [user, tool(2), item(3, "text", "Earlier progress.")];
let host: HTMLDivElement;
let root: Root;
beforeEach(() => { forgetExpanded(); host = document.createElement("div"); document.body.append(host); root = createRoot(host); });
afterEach(() => { act(() => root.unmount()); host.remove(); });
const header = () => host.querySelector<HTMLButtonElement>("[data-work-group] > button")!;
const slot = () => host.querySelector("[data-latest-text]");

describe.each(["assistant", "agents"] as const)("%s background continuation", (style) => {
  function render(items: Item[], live: boolean) {
    const lines = chatLines(items, { live });
    act(() => root.render(<ChatContext.Provider value={{ scope: "wake-test:", tasks: emptyTaskIndex(), subagents: [], lineStyle: style }}>
      {lines.map((line) => <ChatLineView key={line.id} line={line} mid="machine" sid="session" expandFold={async () => {}} />)}
    </ChatContext.Provider>));
  }
  it("retains every wake tool in one manually inspectable summary during and after execution", () => {
    render(first, false);
    const originalSlot = slot();
    const live = [...first, wake, tool(5), item(6, "text", "New progress."), tool(7)];
    render(live, true);
    expect(slot()).toBe(originalSlot);
    expect(slot()?.textContent).toContain("New progress.");
    expect(host.querySelectorAll("[data-work-group]")).toHaveLength(1);
    expect(host.textContent).not.toContain(wake.text);
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(header().getAttribute("aria-expanded")).toBe("false");
    act(() => header().click());
    for (const seq of [2, 5, 7]) expect(host.querySelector("[data-work-detail]")?.textContent).toContain(`echo example-${seq}`);
    expect(host.querySelector("[data-work-detail]")?.textContent).toContain("Earlier progress.");
    const done = [...live, item(9, "text", "Final result.")];
    render(done, false);
    expect(slot()).toBe(originalSlot);
    expect(slot()?.textContent).toContain("Final result.");
    expect(header().getAttribute("aria-expanded")).toBe("false");
    act(() => header().click());
    expect(host.querySelector("[data-work-detail]")?.textContent).toContain("New progress.");
  });
  it("reopens the real live exchange before its recap, without an empty live placeholder", () => {
    const recap = item(4, "recap", "Earlier context.");
    const resumed = [...first, recap, { ...wake, id: "wake5#0", seq: 5 }, tool(6), item(7, "text", "Resumed progress.")];
    render(resumed, true);
    expect(host.querySelectorAll("[data-segment]")).toHaveLength(1);
    expect(host.querySelectorAll("[data-work-group]")).toHaveLength(1);
    expect(slot()?.textContent).toContain("Resumed progress.");
    act(() => header().click());
    expect(host.querySelector("[data-work-detail]")?.textContent).toContain("echo example-6");
    render([...resumed, item(8, "text", "Resumed final result.")], false);
    expect(header().getAttribute("aria-expanded")).toBe("false");
    expect(slot()?.textContent).toContain("Resumed final result.");
  });
});
