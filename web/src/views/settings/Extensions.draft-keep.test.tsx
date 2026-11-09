// @vitest-environment jsdom
// Settings › Extensions keeps an extension's unsaved configuration while its page is closed (#2800): coming back from
// another extension's page finds the edits, the save bar and the list's mark (beside what is still missing); Revert drops
// them, and so does the extension leaving the list. Only the edits come back: a field changed elsewhere meanwhile shows,
// and is saved with, its new value. With a market, the list's note does not contradict the market's.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubExtension, HubExtensionDetail } from "../../lib/extensions";

const hub = vi.hoisted(() => ({ list: [] as unknown[], details: {} as Record<string, unknown>, markets: false, saved: [] as unknown[] }));
vi.mock("../../lib/extensions", async (original) => ({
  ...(await original<typeof import("../../lib/extensions")>()),
  listExtensionsInfo: () => Promise.resolve({ extensions: hub.list, canInstall: false, markets: hub.markets }),
  extensionDetail: (id: string) => Promise.resolve(hub.details[id]),
  setExtensionConfig: (id: string, config: unknown) => {
    hub.saved.push(config);
    return Promise.resolve({ ...(hub.details[id] as object), config });
  },
}));
vi.mock("../../lib/store", async (original) => ({ ...(await original<typeof import("../../lib/store")>()), onHubEvent: () => () => {} }));
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { t } from "../../i18n";
import { setToken } from "../../lib/api";
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const SAVED = "https://notes.example.com";
const TYPED = "https://notes.example.org";
const ext = (id: string, name: string): HubExtension => ({ id, name, kind: "process", status: "running", source: "installed", enabled: true });
const notes = (every_minutes: number) => ({
  ...ext("example-notes", "Example Notes"),
  form: { fields: [{ name: "base_url", type: "string", title: "Service URL" }, { name: "every_minutes", type: "integer", title: "Check every" }] },
  config: { base_url: SAVED, every_minutes },
}) as HubExtensionDetail;

let host: HTMLDivElement;
let root: Root;
let login = 0;
beforeEach(() => {
  hub.markets = false;
  hub.saved = [];
  hub.list = [{ ...ext("example-notes", "Example Notes"), needs_setup: ["API key"] }, ext("example-quota", "Example Quota")];
  hub.details = { "example-notes": notes(30), "example-quota": { ...ext("example-quota", "Example Quota"), form: { fields: [] }, config: {} } as HubExtensionDetail };
  setToken(`token-${++login}`); // a new credential: nothing kept from the case before
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const show = async (open: string | null) => {
  act(() => root.render(<Extensions open={open} onOpen={() => {}} />));
  await flush();
};
const input = () => host.querySelector<HTMLInputElement>("input");
const minutes = () => host.querySelectorAll<HTMLInputElement>("input")[1];
const button = (label: string) => [...host.querySelectorAll("button")].find((b) => b.textContent === label);
const row = (name: string) => [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(name))!;
const typeInto = (el: HTMLInputElement, value: string) => {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
};

describe("an extension's unsaved configuration", () => {
  it.each([
    { name: "stays when another extension is opened and is there on return", revert: false, elsewhere: 30, back: TYPED, unsaved: true },
    { name: "is gone after Revert: the saved value is there on return", revert: true, elsewhere: 30, back: SAVED, unsaved: false },
    { name: "comes back on top of a change made elsewhere meanwhile, which shows and is saved", revert: false, elsewhere: 120, back: TYPED, unsaved: true },
  ])("$name", async ({ revert, elsewhere, back, unsaved }) => {
    await show("example-notes");
    expect(input()!.value).toBe(SAVED);
    typeInto(input()!, TYPED);
    expect(button(t("settings-extensions.revert"))).toBeDefined();
    if (revert) act(() => button(t("settings-extensions.revert"))!.click());

    await show("example-quota");
    expect(input()).toBeNull();
    hub.details["example-notes"] = notes(elsewhere); // another device, app or an upgrade, while the page is closed
    await show(null);
    expect(row("Example Notes").textContent?.includes(t("settings-extensions.unsaved"))).toBe(unsaved);
    expect(row("Example Notes").textContent).toContain(t("settings-extensions.needs_setup", { items: "API key" }));
    expect(row("Example Quota").textContent).not.toContain(t("settings-extensions.unsaved"));

    await show("example-notes");
    expect(input()!.value).toBe(back);
    expect(minutes().value).toBe(String(elsewhere));
    expect(!!button(t("settings-extensions.revert"))).toBe(unsaved);
    if (!unsaved) return;
    await act(async () => button(t("common.save"))!.click());
    expect(hub.saved).toEqual([{ base_url: TYPED, every_minutes: elsewhere }]);
  });

  it("is dropped once the list no longer has the extension (uninstalled elsewhere), so a reload stops asking", async () => {
    const reloadAsks = () => !dispatchEvent(new Event("beforeunload", { cancelable: true }));
    await show("example-notes");
    typeInto(input()!, TYPED);
    await show("example-quota");
    expect(reloadAsks()).toBe(true);
    hub.list = [ext("example-quota", "Example Quota")];
    await show(null);
    expect(reloadAsks()).toBe(false);
  });
});

describe("the list's note", () => {
  it.each([
    { markets: true, note: "settings-extensions.list_footer_market", other: "settings-extensions.list_footer" },
    { markets: false, note: "settings-extensions.list_footer", other: "settings-extensions.list_footer_market" },
  ])("with a market: $markets", async ({ markets, note, other }) => {
    hub.markets = markets;
    await show(null);
    expect(!!host.querySelector('[data-settings-group="market"]')).toBe(markets);
    expect(host.querySelector('[data-settings-group="extensions"]')!.textContent).toContain(t(note));
    expect(host.textContent).not.toContain(t(other));
  });
});
