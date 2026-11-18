// @vitest-environment jsdom
// The composer's permission picker (#5324, cindy's, after #5584's menu): a bare chip with the mode's symbol, cindy's name
// and a chevron; Full access in red. Its menu offers the modes the agent can switch to without Plan mode (the + panel's
// check row), each with its symbol, its description on hover, the current one checked; Full access asks again (#1379).
// A mode not known yet is named by no value (#5756).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// jsdom has no matchMedia (the menu reads the phone-width query)
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({
    matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as typeof window.matchMedia;
});
const api = vi.hoisted(() => vi.fn(async () => ({})));
vi.mock("../../lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("../../lib/api")>()), api }));
vi.mock("../../lib/store", async (importOriginal) => ({ ...(await importOriginal<typeof import("../../lib/store")>()), upsertSession: vi.fn() }));

import { PermissionPicker, usePlanMode, type PlanMode } from "./PermissionPicker";
import { requestPermissionPicker } from "../../lib/permissions-command";
import { PERMISSION_MODES, switchableModes } from "../../lib/session-actions";
import type { Session } from "../../lib/types";
import { t } from "../../i18n";
import { Icon } from "../../ui/icons";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// a Persona conversation
const session = { machine_id: "server", session_id: "s1", assistant: true, agent: "claude", permission_mode: "acceptEdits", effort: "high" } as unknown as Session;
const startWidth = window.innerWidth;
let host: HTMLDivElement;
let root: Root;

function setInnerWidth(w: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: w });
}

beforeEach(() => {
  setInnerWidth(1280);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<PermissionPicker session={session} assistant />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setInnerWidth(startWidth);
  api.mockClear();
  vi.unstubAllGlobals();
});

const chip = () => host.querySelector<HTMLButtonElement>('[data-testid="permission-picker"]')!;
const open = () => act(() => chip().click());
const rows = () => [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] [role="menuitemradio"]')];
const panel = () => document.querySelector<HTMLElement>('[role="menu"]')!;
const label = (id: string) => PERMISSION_MODES.find((m) => m.id === id)!.title;

describe("the chip (#5324)", () => {
  it("names each mode in cindy's words, with its symbol and a chevron, and says what it does on hover", () => {
    expect(PERMISSION_MODES.map((m) => m.title)).toEqual([
      t("lib-session-actions.mode_default"), t("lib-session-actions.mode_accept_edits"), t("lib-session-actions.mode_plan"), t("lib-session-actions.mode_auto"), t("lib-session-actions.mode_bypass_permissions"),
    ]);
    for (const m of PERMISSION_MODES) {
      const html = renderToStaticMarkup(<PermissionPicker session={{ ...session, permission_mode: m.id }} assistant />);
      expect(html).toContain(`>${m.title}<`);
      expect(html).toContain(`aria-label="${t("chat-sessiondialogs.header_mode", { mode: m.title })}"`);
      expect(html).toContain(`title="${m.id === "bypassPermissions" ? t("lib-session-actions.bypass_warning") : m.detail}"`);
      expect(html.match(/<svg/g)).toHaveLength(2); // the mode's symbol and the chevron
    }
    // a mode this page has no name for is named by its ID
    const odd = renderToStaticMarkup(<PermissionPicker session={{ ...session, permission_mode: "dontAsk" }} assistant />);
    expect(odd).toContain(">dontAsk<");
    expect(odd).toContain(`aria-label="${t("chat-sessiondialogs.header_mode", { mode: "dontAsk" })}"`);
  });

  it("is bare until hovered, tints Full access red, and keeps its symbol in a very narrow box", () => {
    const cls = chip().className.split(/\s+/);
    expect(cls).toEqual(expect.arrayContaining(["h-[30px]", "rounded-full", "border-transparent", "hover:border-line-strong", "hover:bg-hover", "min-w-0", "text-ink"]));
    expect(cls).not.toContain("bg-field");
    const bypass = renderToStaticMarkup(<PermissionPicker session={{ ...session, permission_mode: "bypassPermissions" }} assistant />);
    expect(bypass).toMatch(/data-testid="permission-picker"[^>]*class="[^"]*text-red/);
    const words = chip().querySelector("span")!;
    expect(words.className).toContain("truncate");
    expect(words.className).toContain("@max-[300px]/composer:hidden");
  });

  it("shows Plan mode while the session plans, which has no row of its own", () => {
    act(() => root.render(<PermissionPicker session={{ ...session, permission_mode: "plan" }} assistant />));
    expect(chip().textContent).toBe(label("plan"));
    open();
    expect(rows().some((r) => r.textContent?.includes(label("plan")))).toBe(false);
    expect(rows().filter((r) => r.getAttribute("aria-checked") === "true")).toHaveLength(0);
  });
});

describe("its menu (#5584, #5324)", () => {
  it("lists the modes without Plan mode, each with its symbol, its name and its description as the row's tooltip", () => {
    open();
    const modes = switchableModes(true, "claude").filter((m) => m.id !== "plan");
    expect(rows()).toHaveLength(modes.length);
    rows().forEach((row, i) => {
      // a Persona conversation takes Full access with its next message, which its row says under its name (#6476)
      if (modes[i].id === "bypassPermissions") {
        expect(row.textContent).toBe(modes[i].title + t("chat-sessiondialogs.bypass_next_message"));
        return;
      }
      expect(row.textContent).toBe(modes[i].title);
      expect(row.title).toBe(modes[i].detail);
      expect(row.getAttribute("aria-describedby")).toBeNull();
      expect(row.querySelectorAll("svg").length).toBeGreaterThanOrEqual(2); // the symbol and the check's place
    });
  });

  it("marks the current mode for the eye and for screen readers, and the check never shrinks away", () => {
    open();
    const current = rows().filter((r) => r.getAttribute("aria-checked") === "true");
    expect(current).toHaveLength(1);
    expect(current[0].textContent).toBe(label("acceptEdits"));
    for (const row of rows()) {
      const check = [...row.querySelectorAll("svg")].at(-1)!;
      expect(check.getAttribute("class")).toContain("shrink-0");
      expect(check.getAttribute("class")).toContain(row === current[0] ? "text-ink" : "invisible");
    }
  });

  // jsdom has no layout, so this checks only the placement; clipping inside the panel is checked in a real browser by
  // scripts/test/web_mode_menu_e2e.py
  it("is placed at its 300px on a wide window and inside a 320px one", () => {
    open();
    expect(panel().style.width).toBe("300px");
    act(() => root.unmount());
    setInnerWidth(320);
    root = createRoot(host);
    act(() => root.render(<PermissionPicker session={session} assistant />));
    open();
    const left = parseFloat(panel().style.left);
    const width = parseFloat(panel().style.width);
    expect(width).toBeLessThanOrEqual(304);
    expect(left).toBeGreaterThanOrEqual(8);
    expect(left + width).toBeLessThanOrEqual(312);
  });

  it("is chosen from the keyboard", async () => {
    open();
    const key = (k: string) => act(() => void window.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true })));
    key("ArrowDown");
    await act(async () => void window.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
    expect(api).toHaveBeenCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "default" } });
    expect(document.querySelector('[role="menu"]')).toBeNull();
  });
});

describe("a mode not known yet (#5756, as iOS #5596)", () => {
  // Claude Code before its first prompt: no mode reported, and it can already be changed
  const fresh = { machine_id: "mac", session_id: "a2", agent: "claude", effort: "high" } as unknown as Session;
  const show = () => {
    act(() => root.unmount());
    root = createRoot(host);
    act(() => root.render(<PermissionPicker session={fresh} />));
  };

  it("is named by no value: just Permission mode, with no tooltip, and keeps the raised hand", () => {
    // absent, empty, or null as a hub may send it
    for (const permission_mode of [undefined, "", null as unknown as string]) {
      const html = renderToStaticMarkup(<PermissionPicker session={{ ...fresh, permission_mode }} />);
      expect(html).toContain(renderToStaticMarkup(<Icon name="palm" size={14} stroke={2} className="shrink-0" />));
      expect(html).toContain(`>${t("chat-sessiondialogs.permission_mode")}<`);
      expect(html).toContain(`aria-label="${t("chat-sessiondialogs.permission_mode")}"`);
      expect(html).not.toContain(label("default"));
      expect(html).not.toContain(" title=");
      expect(html).not.toContain("data-mode=");
      expect(html.match(/<svg/g)).toHaveLength(2);
    }
  });

  it("checks no row, and any mode can still be picked", async () => {
    show();
    open();
    expect(rows().map((r) => r.textContent)).toEqual(expect.arrayContaining([expect.stringContaining(label("default")), expect.stringContaining(label("acceptEdits"))]));
    expect(rows().filter((r) => r.getAttribute("aria-checked") === "true")).toHaveLength(0);
    const edits = rows().find((r) => r.textContent?.includes(label("acceptEdits")))!;
    await act(async () => edits.click());
    expect(api).toHaveBeenCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "acceptEdits" } });
  });
});

describe("Full access on a running agent (#1379)", () => {
  const agent = (over: Partial<Session>) => ({ machine_id: "mac", session_id: "a1", agent: "claude", permission_mode: "acceptEdits", effort: "high", ...over }) as unknown as Session;
  const show = (s: Session) => {
    act(() => root.unmount());
    root = createRoot(host);
    act(() => root.render(<PermissionPicker session={s} />));
    open();
  };
  const bypassRow = () => [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] [role^="menuitem"]')].find((r) => r.textContent?.includes(label("bypassPermissions")));

  it("is shown disabled with its reason where the agent wasn't started so it can reach it", () => {
    show(agent({}));
    expect(rows()).toHaveLength(3);
    const row = bypassRow()!;
    expect(row.disabled).toBe(true);
    expect(row.textContent).toContain(t("lib-session-actions.mode_bypass_permissions_detail"));
  });

  it("is offered in red when the node says it can reach it, and asks again before switching", async () => {
    show(agent({ bypass_available: true }));
    expect(rows()).toHaveLength(4);
    const row = bypassRow()!;
    expect(row.className).toContain("text-red");
    await act(async () => row.click());
    expect(api).not.toHaveBeenCalled();
    expect(panel().textContent).toContain(t("chat-sessiondialogs.bypass_confirm"));
    expect(panel().textContent).toContain(t("lib-session-actions.bypass_warning"));
    await act(async () => bypassRow()!.click());
    expect(api).toHaveBeenCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "bypassPermissions" } });
  });

  it("is the checked row, in red, while the agent runs in it, so it can be switched away and back", () => {
    show(agent({ permission_mode: "bypassPermissions" }));
    expect(rows()).toHaveLength(4);
    expect(bypassRow()!.getAttribute("aria-checked")).toBe("true");
    expect(bypassRow()!.className).toContain("text-red");
  });
});

// `/permissions` in the composer (#6476): the picker's menu with every mode, Plan mode included, the current one checked;
// a pick switches the running session. Full access for a terminal Claude Code started without a bypass flag restarts
// the session once its turn is over, which its row says, asking first.
describe("/permissions (#6476)", () => {
  const agent = (over: Partial<Session>) =>
    ({ machine_id: "mac", session_id: "c1", agent: "claude", status: "idle", state: "idle", pane_id: "w1:p1", permission_mode: "default", ...over }) as unknown as Session;
  const show = (s: Session) => {
    act(() => root.unmount());
    root = createRoot(host);
    act(() => root.render(<PermissionPicker session={s} />));
  };
  // Full access's row turns into its question once clicked ("Switch to Full access"), which still names it
  const row = (id: string) => [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] [role^="menuitem"]')].find((r) => (id === "bypassPermissions" ? r.textContent?.includes(label(id)) : r.textContent?.startsWith(label(id))));

  it("opens the menu with all five modes, Plan mode and Auto included, the current one checked", () => {
    show(agent({ permission_mode: "acceptEdits" }));
    let opened = false;
    act(() => void (opened = requestPermissionPicker("mac/c1")));
    expect(opened).toBe(true);
    expect(rows()).toHaveLength(5);
    for (const id of ["default", "acceptEdits", "plan", "auto", "bypassPermissions"]) expect(row(id)).toBeTruthy();
    expect(rows().filter((r) => r.getAttribute("aria-checked") === "true").map((r) => r.textContent)).toEqual([label("acceptEdits")]);
    // the chip's own click keeps Plan mode for the + panel
    show(agent({ permission_mode: "acceptEdits" }));
    open();
    expect(row("plan")).toBeUndefined();
  });

  it("switches at once to Plan mode and Auto", async () => {
    show(agent({}));
    act(() => void requestPermissionPicker("mac/c1"));
    await act(async () => row("plan")!.click());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/sessions/mac/c1/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "plan" } });
    act(() => void requestPermissionPicker("mac/c1"));
    await act(async () => row("auto")!.click());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "auto" } });
  });

  it("offers Full access to an agent started without a bypass flag, saying the session restarts, and asks first", async () => {
    show(agent({}));
    act(() => void requestPermissionPicker("mac/c1"));
    const bypass = row("bypassPermissions")!;
    expect(bypass.disabled).toBe(false);
    expect(bypass.textContent).toContain(t("chat-sessiondialogs.bypass_restart_detail"));
    await act(async () => bypass.click());
    expect(api).not.toHaveBeenCalled();
    expect(panel().textContent).toContain(t("chat-sessiondialogs.bypass_confirm"));
    expect(panel().textContent).toContain(t("chat-sessiondialogs.bypass_restart_detail"));
    await act(async () => row("bypassPermissions")!.click());
    expect(api).toHaveBeenCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "bypassPermissions" } });
  });

  it("says the restart waits once picked, on the chip's hover and on the row", () => {
    show(agent({ permission_mode: "acceptEdits", bypass_pending: true }));
    expect(chip().title).toBe(t("chat-sessiondialogs.bypass_pending"));
    expect(chip().dataset.pending).toBe("bypassPermissions");
    expect(chip().textContent).toBe(label("acceptEdits")); // the mode it still runs in
    act(() => void requestPermissionPicker("mac/c1"));
    expect(row("bypassPermissions")!.textContent).toContain(t("chat-sessiondialogs.bypass_pending"));
  });

  it("has no menu to open once the picker is gone: the composer sends the words", () => {
    show(agent({}));
    act(() => root.unmount());
    expect(requestPermissionPicker("mac/c1")).toBe(false);
    root = createRoot(host);
  });
});

describe("Plan mode, the + panel's check row (#5324)", () => {
  let plan: PlanMode | undefined;
  function Probe({ s, assistant }: { s: Session | undefined; assistant?: boolean }) {
    plan = usePlanMode(s, assistant);
    return null;
  }
  const agent = (over: Partial<Session>) => ({ machine_id: "mac", session_id: "p1", agent: "claude", permission_mode: "auto", ...over }) as unknown as Session;
  const probe = (s: Session | undefined, assistant?: boolean) => act(() => root.render(<Probe s={s} assistant={assistant} />));

  it("is offered only where the session has modes, checked while it plans", () => {
    probe(undefined);
    expect(plan).toBeUndefined();
    probe(agent({}));
    expect(plan?.on).toBe(false);
    probe(agent({ permission_mode: "plan" }));
    expect(plan?.on).toBe(true);
  });

  it("goes into Plan mode, and back to the mode this page saw before it", async () => {
    probe(agent({ permission_mode: "auto" }));
    await act(async () => plan!.toggle());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "plan" } });
    probe(agent({ permission_mode: "plan" }));
    await act(async () => plan!.toggle());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "auto" } });
  });

  it("goes back to Default permissions when it never saw another mode, and never into Full access", async () => {
    probe(agent({ session_id: "p2", permission_mode: "plan" }));
    await act(async () => plan!.toggle());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "default" } });
    probe(agent({ session_id: "p3", permission_mode: "bypassPermissions", bypass_available: true }));
    probe(agent({ session_id: "p3", permission_mode: "plan", bypass_available: true }));
    await act(async () => plan!.toggle());
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), { method: "POST", timeout: 95_000, json: { permission_mode: "default" } });
  });

  it("asks first when it leaves a Full access the agent can't come back to", async () => {
    const ask = vi.fn(() => false);
    vi.stubGlobal("confirm", ask);
    probe(agent({ session_id: "p4", permission_mode: "bypassPermissions" }));
    await act(async () => plan!.toggle());
    expect(ask).toHaveBeenCalledOnce();
    expect(String((ask.mock.calls[0] as unknown[])[0])).toContain(t("chat-sessiondialogs.leave_bypass_detail"));
    expect(api).not.toHaveBeenCalled();
  });
});

// #6476: a pick that restarts the session is drawn as waiting for it, never as the mode it does not run in yet
describe("the pick drawn before the hub answers (#6476)", () => {
  it("draws Full access by a restart as waiting, and another mode as dropping the wait", async () => {
    const { upsertSession } = await import("../../lib/store");
    const { applyModes } = await import("./SessionDialogs");
    const up = vi.mocked(upsertSession);
    up.mockClear();
    const s = { machine_id: "mac", session_id: "r1", agent: "claude", status: "idle", state: "idle", pane_id: "w1:p1", permission_mode: "default" } as unknown as Session;
    await applyModes(() => s, { permission_mode: "bypassPermissions" });
    expect(up).toHaveBeenLastCalledWith(expect.objectContaining({ permission_mode: "default", bypass_pending: true }));
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/modes"), expect.objectContaining({ json: { permission_mode: "bypassPermissions" } }));
    await applyModes(() => ({ ...s, bypass_pending: true }), { permission_mode: "auto" });
    expect(up).toHaveBeenLastCalledWith(expect.objectContaining({ permission_mode: "auto", bypass_pending: undefined }));
    // where shift+tab reaches it, it is drawn at once
    await applyModes(() => ({ ...s, bypass_available: true }), { permission_mode: "bypassPermissions" });
    expect(up).toHaveBeenLastCalledWith(expect.objectContaining({ permission_mode: "bypassPermissions" }));
  });
});

// #8261: a session of an agent that is not Claude Code lists the modes it takes (Paseo's Grok): the chip names its own
// approval "Launcher default" (no mode reported is it), the menu offers that and Full access alone, and a pick goes to
// the session as any mode does.
describe("a session that lists its modes (#8261)", () => {
  const grok = (permission_mode?: string) => ({ machine_id: "mac", session_id: "g1", agent: "paseo", agent_label: "Grok", source_kind: "runtime", status: "idle",
    permission_mode, capabilities: { read: true, send: true, modes: false, permission_modes: ["default", "bypassPermissions"] } }) as unknown as Session;

  it("names its own approval, offers it and Full access alone, and switches", async () => {
    act(() => root.render(<PermissionPicker session={grok()} />));
    expect(chip().textContent).toBe(t("lib-session-actions.mode_launcher_default"));
    expect(chip().getAttribute("title")).toBe(t("lib-session-actions.mode_launcher_default_detail"));
    expect(chip().dataset.mode).toBe("default");
    open();
    expect(rows().map((r) => r.textContent)).toEqual([
      expect.stringContaining(t("lib-session-actions.mode_launcher_default")), expect.stringContaining(t("lib-session-actions.mode_bypass_permissions")),
    ]);
    // into Full access asks first, as Claude Code's does: the row turns into its question, a second choice runs it
    act(() => rows()[1].click());
    act(() => rows()[1].click());
    await act(async () => {});
    expect(api).toHaveBeenLastCalledWith(expect.stringContaining("/sessions/mac/g1/modes"), expect.objectContaining({ json: { permission_mode: "bypassPermissions" } }));
  });

  it("in Full access: the chip says so in red", () => {
    act(() => root.render(<PermissionPicker session={grok("bypassPermissions")} />));
    expect(chip().textContent).toBe(t("lib-session-actions.mode_bypass_permissions"));
    expect(chip().className).toContain("text-red");
  });
});
