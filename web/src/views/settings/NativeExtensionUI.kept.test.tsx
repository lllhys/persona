// @vitest-environment jsdom
// A page drawn over the app socket on reload (docs/web/web.md "Extension pages on reload"): its last view stands in for the
// loading state until the first snapshot, the first page's drawing is handed back to be kept (UI version 7, the roster's
// default), a pushed page's never, nor one whose fields hold what was typed and not sent. Injected transport only.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UIStreamRequest, UIStreamSnapshot, View } from "../../lib/extension-wire";
import type { AppUIStreamPage } from "../../lib/extension-ui-app-stream";
import type { UIView } from "../../lib/extension-ui";

const seam = vi.hoisted(() => ({
  entries: [] as Array<{ adapter: AppUIStreamPage; sent: UIStreamRequest[]; frame: (body: object) => void }>,
  roster: () => {}, // the roster answers when the test says
}));
vi.mock("../../lib/managers", () => ({ listManagers: () => new Promise((resolve) => { seam.roster = () => resolve({ default: "manager" }); }) }));
vi.mock("../../lib/extension-ui-app-stream", async (original) => {
  const mod = await original<typeof import("../../lib/extension-ui-app-stream")>();
  return { ...mod, appUIStreamPage: (options: Parameters<typeof mod.appUIStreamPage>[0], manager: string, effect: Parameters<typeof mod.appUIStreamPage>[2], failed: Parameters<typeof mod.appUIStreamPage>[3]) => {
    let receive: (raw: string) => void = () => {};
    const sent: UIStreamRequest[] = [];
    const adapter = new mod.AppUIStreamPage(options, manager, {
      connected: () => true, send: (r) => { sent.push(r); }, frames: (fn) => { receive = fn; return () => {}; }, connection: () => () => {},
    }, effect, failed);
    seam.entries.push({ adapter, sent, frame: (body) => receive(JSON.stringify({ type: "extension_ui_stream", manager, ...body })) });
    return adapter;
  } };
});
import { NativeExtensionUI } from "./NativeExtensionUI";
import { LegacyExtensionUI } from "./ExtensionView";

let root: Root, host: HTMLDivElement;
const onView = vi.fn<(view: UIView, version: number, manager?: string) => void>();
const view = (text: string): View => ({ sections: [{ key: "section", items: [
  { type: "text", key: "text", text },
  { type: "field", kind: "text", key: "note", name: "note", label: "Note" },
  { type: "button", key: "next", title: "Native next", action: { id: "next", push: { ui: "detail" } } },
] }] });
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true, matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) });
  seam.entries = [];
  onView.mockClear();
  history.replaceState({}, "", "/extension/example/main");
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); seam.entries.forEach((e) => e.adapter.dispose()); host.remove(); });

async function snapshot(index: number, text: string, version = 1) {
  const entry = seam.entries[index];
  const request = entry.sent.find((r) => r.request === "open")!;
  const s: UIStreamSnapshot = { protocol: 1, session: `session-${index}`, epoch: `epoch-${index}`, version, root_key: "root", view: view(text) };
  await act(async () => entry.frame({ kind: "snapshot", ...(version === 1 ? { request_id: request.request_id } : {}), snapshot: s }));
}
const top = () => [...host.children].findLast((node) => !(node as HTMLElement).hidden) as HTMLElement;

describe("a page drawn over the socket on reload", () => {
  it("shows the last view until the first snapshot, and hands the first page's drawing back to be kept", async () => {
    await act(async () => root.render(<NativeExtensionUI extension="example" ui="main" bare placeholder={<p data-kept>Last view</p>} onView={onView} legacy={(props) => <LegacyExtensionUI {...props} />} />));
    // before the roster names the default, and before the first snapshot
    expect(host.querySelector("[data-kept]")).not.toBeNull();
    await act(async () => seam.roster());
    expect(seam.entries).toHaveLength(1);
    expect(host.querySelector("[data-kept]")).not.toBeNull();
    expect(onView).not.toHaveBeenCalled();
    await snapshot(0, "Fresh entry");
    expect(host.querySelector("[data-kept]")).toBeNull();
    expect(top().textContent).toContain("Fresh entry");
    expect(onView).toHaveBeenCalledTimes(1);
    expect(onView.mock.calls[0][1]).toBe(7);
    expect(onView.mock.calls[0][2]).toBe("manager");
    expect(onView.mock.calls[0][0].sections[0].items[0]).toMatchObject({ type: "text", text: "Fresh entry" });

    // a pushed page is drawn without the last view and never kept
    await act(async () => [...top().querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === "Native next")!.click());
    expect(top().querySelector("[data-kept]")).toBeNull();
    await snapshot(1, "Detail entry");
    expect(top().textContent).toContain("Detail entry");
    expect(onView).toHaveBeenCalledTimes(1);
  });

  it("does not keep a drawing while a field holds what was typed and not sent", async () => {
    await act(async () => root.render(<NativeExtensionUI extension="example" ui="main" bare onView={onView} legacy={(props) => <LegacyExtensionUI {...props} />} />));
    await act(async () => seam.roster());
    await snapshot(0, "First");
    expect(onView).toHaveBeenCalledTimes(1);
    act(() => seam.entries[0].adapter.session.setDraft("note", "typed, not sent"));
    await snapshot(0, "Second", 2);
    expect(top().textContent).toContain("Second");
    expect(onView).toHaveBeenCalledTimes(1);
  });
});
