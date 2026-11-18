// @vitest-environment jsdom
// Full view's header has a gear whose menu is only Appearance (the terminal's light / dark, which herdr is told): the
// same row and the same setting as Single pane's Terminal settings.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FullView } from "./full";

const view: FullView = {
  ready: true,
  status: "live",
  note: "",
  cols: 80,
  rows: 24,
  control: "active",
  mods: { ctrl: "off", alt: "off", shift: "off" },
  selection: false,
  find: null,
  palette: null,
  failedToStart: false,
  uploading: null,
  dropping: false,
  elsewhere: null,
};

vi.mock("./full-registry", () => ({
  acquireFeed: () => ({
    fresh: false,
    feed: {
      subscribe: () => () => {},
      snapshot: () => view,
      claim() {},
      yield() {},
      focus() {},
      reconnect() {},
      hasSelection: () => false,
      driving: true,
    },
  }),
  releaseFeed: () => {},
}));

import { t } from "../../../i18n";
import { FullTerminal } from "./FullTerminal";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let slot: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("persona.terminal.keyBar", "0");
  host = document.body.appendChild(document.createElement("div"));
  slot = document.body.appendChild(document.createElement("div"));
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  document.body.innerHTML = "";
  localStorage.clear();
});

describe("Full view's gear", () => {
  it("opens a menu with only Appearance, which sets the terminal's light / dark", () => {
    act(() => root.render(<FullTerminal mid="m1" sid="s1" cwd="" slot={slot} live delay={0} onFallback={() => {}} onFailed={() => {}} />));
    const gear = slot.querySelector<HTMLButtonElement>(`button[aria-label="${t("terminal-full.settings")}"]`);
    expect(gear).not.toBeNull();
    act(() => gear!.click());
    const menu = document.querySelector(`[role="dialog"][aria-label="${t("terminal-full.settings")}"]`)!;
    expect(menu).not.toBeNull();
    expect(menu.querySelectorAll('[role="radiogroup"]')).toHaveLength(1);
    expect(menu.querySelector('[role="radiogroup"]')?.getAttribute("aria-label")).toBe(t("session-terminal-terminaloptions.appearance"));
    expect(menu.querySelectorAll('[role="switch"], input')).toHaveLength(0);
    const dark = menu.querySelector<HTMLButtonElement>(`[role="radio"][aria-label="${t("session-terminal-terminaloptions.appearance_dark")}"]`)!;
    act(() => dark.click());
    expect(localStorage.getItem("persona.terminal.appearance")).toBe("dark");
    // the screen box takes the scheme
    expect(host.querySelector('[data-terminal-scheme="dark"]')).not.toBeNull();
    act(() => gear!.click());
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });
});
