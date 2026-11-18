// @vitest-environment jsdom
// The Sources row (#9132): under a finished reply that used the web, in Persona conversations and Agent sessions alike;
// never while the turn runs, never in a supervision trace, never for a turn without web calls; three pages, then +N.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
});

import { emptyTaskIndex } from "../../lib/tasks";
import { segments } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { ChatContext, type ChatContextValue } from "./chat-context";
import { SegmentView } from "./SegmentView";
import { siteOf } from "./TurnSources";
import { t } from "../../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let seq = 0;
const item = (over: Partial<Item>): Item => ({ id: `i${++seq}#0`, seq, role: "assistant", kind: "text", ts: "2026-11-16T10:00:00Z", ...over });
function call(tool: string, input: Record<string, unknown>, text: string): Item[] {
  const id = `c${seq + 1}`;
  return [item({ kind: "tool_use", tool_use_id: id, tool_name: tool, tool_input: input }), item({ role: "user", kind: "tool_result", tool_use_id: id, text })];
}
const ask = () => item({ role: "user", text: "What's the weather in Kyoto next week?" });
const done = () => item({ text: "Mostly sunny, 12–18 °C." });
const search = (n: number) => call("WebSearch", { query: "kyoto weather" }, `Web search results for query: "kyoto weather"\n\nLinks: ${JSON.stringify(Array.from({ length: n }, (_, i) => ({ title: `Forecast ${i + 1}`, url: `https://www.site${i + 1}.example.com/kyoto` })))}\n\nSummary.`);

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  seq = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const ctx = (lineStyle: ChatContextValue["lineStyle"]): ChatContextValue => ({ tasks: emptyTaskIndex(), subagents: [], scope: "t:", lineStyle });
async function draw(items: Item[], o: { lineStyle?: ChatContextValue["lineStyle"]; live?: boolean; trace?: boolean } = {}) {
  const seg = segments(items)[0];
  await act(async () =>
    root.render(
      <ChatContext.Provider value={ctx(o.lineStyle ?? "assistant")}>
        <SegmentView seg={seg} live={o.live ?? false} trace={o.trace} mid="m" sid="s" expandFold={async () => {}} />
      </ChatContext.Provider>,
    ),
  );
  // the row's chunk
  await act(async () => { await import("./TurnSources"); for (let i = 0; i < 10; i++) await Promise.resolve(); });
}
const row = () => host.querySelector<HTMLElement>("[data-turn-sources]");
const chips = () => [...host.querySelectorAll<HTMLAnchorElement>("[data-source]")];

describe("the Sources row", () => {
  it("follows a finished reply in a Persona conversation: each page by its site, its title spoken and in the tooltip, opening in a new tab", async () => {
    await draw([ask(), ...search(2), done()]);
    expect(row()).not.toBeNull();
    expect(row()!.getAttribute("aria-label")).toBe(t("chat-turnsources.label"));
    expect(chips().map((a) => a.textContent)).toEqual(["site1.example.com", "site2.example.com"]);
    expect(chips()[0].getAttribute("aria-label")).toBe("Forecast 1, site1.example.com");
    expect(chips()[0].title).toBe("Forecast 1\nhttps://www.site1.example.com/kyoto");
    expect(chips()[0].getAttribute("href")).toBe("https://www.site1.example.com/kyoto");
    expect(chips()[0].target).toBe("_blank");
    expect(row()!.previousElementSibling?.hasAttribute("data-latest-text")).toBe(true);
  });

  it("is in Agent sessions too; a page without a title is spoken by its site", async () => {
    await draw([ask(), ...call("WebFetch", { url: "https://docs.example.org/guide", prompt: "summarize" }, "A summary."), done()], { lineStyle: "agents" });
    expect(chips().map((a) => a.textContent)).toEqual(["docs.example.org"]);
    expect(chips()[0].getAttribute("aria-label")).toBe("docs.example.org");
  });

  it("tells two pages of one site apart by their titles", async () => {
    const two = call("WebSearch", { query: "q" }, `Links: ${JSON.stringify([{ title: "Tuesday", url: "https://w.example.com/tue" }, { title: "Wednesday", url: "https://w.example.com/wed" }, { title: "Other", url: "https://o.example.com/" }])}`);
    await draw([ask(), ...two, done()]);
    expect(chips().map((a) => a.textContent)).toEqual(["Tuesday", "Wednesday", "o.example.com"]);
  });

  it("counts www. and letter case as the same site when telling pages apart", async () => {
    const mixed = call("WebSearch", { query: "q" }, `Links: ${JSON.stringify([{ title: "With www", url: "https://www.example.com/a" }, { title: "Bare", url: "https://example.com/b" }, { title: "Upper", url: "HTTPS://WWW.Example.COM/c" }])}`);
    await draw([ask(), ...mixed, done()]);
    expect(chips().map((a) => a.textContent)).toEqual(["With www", "Bare", "Upper"]);
  });

  it("shows three, then +N, which shows the rest and Show less", async () => {
    await draw([ask(), ...search(5), done()]);
    expect(chips()).toHaveLength(3);
    const more = [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === t("chat-turnsources.more", { count: 2 }))!;
    expect(more.getAttribute("aria-label")).toBe(t("chat-turnsources.show_more", { count: 2 }));
    await act(async () => more.click());
    expect(chips()).toHaveLength(5);
    const less = [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === t("chat-turnsources.show_less"))!;
    await act(async () => less.click());
    expect(chips()).toHaveLength(3);
  });

  it("is not there while the turn runs, in a supervision trace, or after a turn without web calls", async () => {
    await draw([ask(), ...search(2), done()], { live: true });
    expect(row()).toBeNull();
    await draw([ask(), ...search(2), done()], { trace: true });
    expect(row()).toBeNull();
    await draw([ask(), ...call("Read", { file_path: "/notes.md" }, "notes"), done()]);
    expect(row()).toBeNull();
    // a failed search names nothing
    const failed = call("WebSearch", { query: "x" }, "Request failed");
    failed[1].is_error = true;
    await draw([ask(), ...failed, done()]);
    expect(row()).toBeNull();
  });

  it("reads folded work by its fold's sources", async () => {
    const fold = item({ id: "fold:x#0", kind: "fold", fold: { items: 6, tools: 3, thoughts: 0, through_seq: 3, sources: [{ url: "https://folded.example.com/a", title: "Folded page" }] } });
    await draw([ask(), fold, done()]);
    expect(chips().map((a) => a.textContent)).toEqual(["folded.example.com"]);
    expect(chips()[0].getAttribute("aria-label")).toBe("Folded page, folded.example.com");
  });
});

describe("siteOf", () => {
  it("is the host without www.", () => {
    expect(siteOf("https://www.example.com/a?b")).toBe("example.com");
    expect(siteOf("HTTPS://Docs.Example.org/")).toBe("docs.example.org");
    expect(siteOf("https://WWW.Example.com/")).toBe("example.com");
  });
});
