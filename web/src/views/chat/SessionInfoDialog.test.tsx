// @vitest-environment jsdom
// Session info leaves out counts nobody gives (#8261): a runtime's session with no transcript on its computer says so
// (`usage_unknown`, its zeros only standing in for older apps), and shows no "0 tokens" or "0" turns. A session that
// lists its own permission modes runs in one of them: no mode reported is "Launcher default". The chat's header draws
// it from SessionDialogs, which loads it on its first open (its own chunk, off the first screen).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { initI18n } from "../../i18n";
import { setToken } from "../../lib/api";
import { LAUNCHER_DEFAULT, modeTitle } from "../../lib/session-actions";
import type { Session } from "../../lib/types";
import { SessionInfoDialog as LazySessionInfo } from "./SessionDialogs";
import { SessionInfoDialog } from "./SessionInfoDialog";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let info: Record<string, unknown>;
let host: HTMLDivElement;
let root: Root;
const grok = { machine_id: "m", session_id: "paseo-g1", title: "Fix the build", agent: "paseo", agent_label: "Grok", source_kind: "runtime", status: "idle",
  capabilities: { read: true, send: true, modes: false, permission_modes: ["default", "bypassPermissions"] } } as unknown as Session;
const claude = { machine_id: "m", session_id: "c1", title: "Fix the build", agent: "claude", status: "idle" } as unknown as Session;

const rowsOf = () => Object.fromEntries([...document.querySelectorAll<HTMLElement>("[role=dialog] section div > div")].map((r) => {
  const [k, v] = r.querySelectorAll("span");
  return [k?.textContent ?? "", v?.textContent ?? ""];
}));

async function show(session: Session) {
  await act(async () => root.render(<SessionInfoDialog session={session} machineName="Mac" open onClose={() => {}} />));
  await act(async () => {});
}

beforeEach(async () => {
  await initI18n("en");
  setToken("synthetic-owner-a");
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(info), { status: 200, headers: { "Content-Type": "application/json" } })));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("Session info", () => {
  it("leaves out counts nobody gives, and names a listed session's own approval", async () => {
    info = { session_id: "paseo-g1", model: "grok-5", context_tokens: 0, output_tokens: 0, compactions: 0, turns: 0, usage_unknown: true, last_activity: new Date().toISOString() };
    await show(grok);
    const rows = rowsOf();
    expect(rows.Model).toBe("grok-5");
    expect(rows["Permission mode"]).toBe(LAUNCHER_DEFAULT.title);
    expect(Object.keys(rows)).not.toContain("Context");
    expect(Object.keys(rows)).not.toContain("Turns");
    expect(Object.keys(rows)).not.toContain("Output");
    expect(document.querySelector("[role=dialog]")!.textContent).not.toContain("0 tokens");
  });

  it("shows the counts a transcript gives, zeros too", async () => {
    info = { session_id: "c1", model: "claude-opus-5-5", permission_mode: "plan", context_tokens: 1200, output_tokens: 0, compactions: 0, turns: 0 };
    await show(claude);
    const rows = rowsOf();
    expect(rows.Context).toBe("1.2K tokens");
    expect(rows.Output).toBe("0 tokens");
    expect(rows.Turns).toBe("0");
    expect(rows["Permission mode"]).toBe(modeTitle("plan"));
  });

  // Millions read as millions, not "1235K" (#9205); a context of 0 (no reply yet, or just compacted: #9207) is not known.
  it("writes big counts in millions and leaves an unknown context out", async () => {
    info = { session_id: "c1", model: "claude-opus-5-5", context_tokens: 0, output_tokens: 1_234_567, compactions: 1, turns: 3 };
    await show(claude);
    const rows = rowsOf();
    expect(rows.Output).toBe("1.23M tokens");
    expect(Object.keys(rows)).not.toContain("Context");
  });

  it("is nothing until its first open, then the same dialog from its own chunk", async () => {
    info = { session_id: "c1", model: "claude-opus-5-5", context_tokens: 1200 };
    await act(async () => root.render(<LazySessionInfo session={claude} machineName="Mac" open={false} onClose={() => {}} />));
    expect(document.querySelector("[role=dialog]")).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
    await act(async () => root.render(<LazySessionInfo session={claude} machineName="Mac" open onClose={() => {}} />));
    for (let i = 0; i < 20 && !rowsOf().Context; i++) await act(async () => new Promise((r) => setTimeout(r, 10)));
    expect(rowsOf().Context).toBe("1.2K tokens");
  });
});
