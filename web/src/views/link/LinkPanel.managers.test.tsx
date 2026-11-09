// @vitest-environment jsdom
// Several managers (docs/assistant/managers.md "Grouping"): a document or Moments post a chat's link opens in the panel
// is read as that chat's manager, and so is everything the panel loads for it and every link inside it; the default's
// names none.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readView } from "../../lib/extension-ui";
import { ItemManager, LinkManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { closePanel, getPanel } from "../../lib/panel";
import { Markdown } from "../../ui/Markdown";
import { INERT, ViewMarkdown, ViewSections } from "../extensions/ViewBody";
import { LinkPanel } from "./LinkPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const POST = "https://example.feishu.cn/client/moments/detail?postId=12345";

let host: HTMLDivElement;
let root: Root;
const calls: { url: string; header: string | null }[] = [];
const flush = async (n = 8) => {
  for (let i = 0; i < n; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  calls.length = 0;
  resetManagerChoice();
  setRosterDefault("assistant");
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Element.prototype.scrollIntoView = vi.fn();
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, header: new Headers(init?.headers).get("X-Persona-Manager") });
    let body: unknown = {};
    if (url.includes("/documents/read?")) body = { title: "Plan", markdown: "Body\n\n![pic](</api/v1/lark/raw/documents/media/k1>)\n\n[next](https://example.feishu.cn/docx/Next1)" };
    else if (url.includes("/documents/comments")) body = { threads: [] };
    else if (url.includes("/moments/posts/12345/comments")) body = { comments: [] };
    else if (url.includes("/moments/posts/12345")) body = { id: "12345", userName: "Sample", content: "Post" };
    return { ok: true, status: 200, text: async () => JSON.stringify(body), headers: new Headers() };
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  closePanel();
  vi.unstubAllGlobals();
});

const click = (el: Element) => act(() => void el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 })));
const linkTo = (href: string) => [...host.querySelectorAll("a")].find((a) => a.getAttribute("href") === href)!;

describe("a link in a chat", () => {
  it("opens the document in the panel as the chat's manager, an inbox item's when no chat says, the default's naming none", async () => {
    const doc = "https://example.feishu.cn/docx/Chat1";
    const text = `[the plan](${doc})`;
    act(() => root.render(<LinkManager.Provider value="m-2"><Markdown text={text} /></LinkManager.Provider>));
    click(linkTo(doc));
    expect(getPanel()).toEqual({ kind: "link", url: doc, title: "the plan", manager: "m-2" });

    act(() => root.render(<ItemManager.Provider value="m-3"><Markdown text={text} /></ItemManager.Provider>));
    click(linkTo(doc));
    expect(getPanel()).toMatchObject({ kind: "link", url: doc, manager: "m-3" });

    act(() => root.render(<LinkManager.Provider value="assistant"><Markdown text={text} /></LinkManager.Provider>));
    click(linkTo(doc));
    expect(getPanel()).toMatchObject({ kind: "link", url: doc });
    expect((getPanel() as { manager?: string }).manager).toBeUndefined();
  });

  it("opens an extension view's link rows and Markdown links as the chat's or inbox item's manager", () => {
    const doc = "https://example.feishu.cn/docx/Card1";
    const view = readView({ sections: [{ items: [{ type: "link", url: doc, title: "Card link" }] }] });
    act(() => root.render(<LinkManager.Provider value="m-2"><ViewSections view={view} {...INERT} /><ViewMarkdown text={`[in text](${doc}#t)`} /></LinkManager.Provider>));
    click(linkTo(doc));
    expect(getPanel()).toEqual({ kind: "link", url: doc, title: "Card link", manager: "m-2" });
    click(linkTo(`${doc}#t`));
    expect(getPanel()).toMatchObject({ kind: "link", url: `${doc}#t`, manager: "m-2" });

    act(() => root.render(<ItemManager.Provider value="m-3"><ViewSections view={view} {...INERT} /></ItemManager.Provider>));
    click(linkTo(doc));
    expect(getPanel()).toMatchObject({ kind: "link", url: doc, manager: "m-3" });
  });
});

describe("the panel of another manager's link", () => {
  it("reads the document, its comments and its media as that manager, and opens links inside it as that manager", async () => {
    const doc = "https://example.feishu.cn/docx/Panel1";
    act(() => root.render(<LinkPanel url={doc} manager="m-2" onClose={() => {}} />));
    await flush();
    const lark = calls.filter((c) => c.url.includes("/lark/"));
    expect(lark.map((c) => new URL(c.url, "http://hub").pathname)).toEqual(expect.arrayContaining(["/api/v1/lark/raw/documents/read", "/api/v1/lark/raw/documents/reader/info", "/api/v1/lark/raw/documents/ai-summary", "/api/v1/lark/raw/documents/comments"]));
    expect(lark.map((c) => c.header)).toEqual(lark.map(() => "m-2"));
    const img = host.querySelector("img")!;
    expect(new URL(img.getAttribute("src")!, "http://hub").searchParams.get("manager")).toBe("m-2");

    click(linkTo("https://example.feishu.cn/docx/Next1"));
    expect(getPanel()).toMatchObject({ kind: "link", url: "https://example.feishu.cn/docx/Next1", manager: "m-2" });
  });

  it("reads a Moments post and its comments as that manager", async () => {
    act(() => root.render(<LinkPanel url={POST} manager="m-2" onClose={() => {}} />));
    await flush();
    const moments = calls.filter((c) => c.url.includes("/lark/moments/"));
    expect(moments.map((c) => new URL(c.url, "http://hub").pathname).sort()).toEqual(["/api/v1/lark/moments/posts/12345", "/api/v1/lark/moments/posts/12345/comments"]);
    expect(moments.map((c) => c.header)).toEqual(["m-2", "m-2"]);
  });

  it("names none for the default's", async () => {
    const doc = "https://example.feishu.cn/docx/Default1";
    act(() => root.render(<LinkPanel url={doc} onClose={() => {}} />));
    await flush();
    const lark = calls.filter((c) => c.url.includes("/lark/"));
    expect(lark.length).toBeGreaterThan(0);
    expect(lark.map((c) => c.header)).toEqual(lark.map(() => null));
    expect(new URL(host.querySelector("img")!.getAttribute("src")!, "http://hub").searchParams.get("manager")).toBeNull();
  });
});
