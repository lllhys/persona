// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  g.matchMedia = (q: string) => ({
    matches: false,
    media: q,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  });
});
import { getState, reset, savePins } from "../../lib/store";
import type { Machine, Session } from "../../lib/types";
import { AgentsList } from "../Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const machines: Machine[] = [
  { id: "gcp", name: "Cloud Dev", online: true },
  { id: "mac", name: "Mac Studio", online: true },
];

const sessions: Session[] = [
  {
    machine_id: "gcp",
    session_id: "s1",
    title: "Implement Agents Tab Filter Bar",
    cwd: "/workspace/persona",
    repo_root: "/workspace/persona",
    status: "working",
    state: "working",
    agent: "claude",
    model: "claude-opus-5-5",
    updated_at: "2026-11-09T11:30:00Z",
    last_text: "Updated composable filter bar and chat-list rows.",
  },
  {
    machine_id: "mac",
    session_id: "s2",
    title: "Fix WebSocket Reconnect",
    cwd: "/workspace/orca",
    repo_root: "/workspace/orca",
    status: "blocked",
    state: "needs_you",
    agent: "codex",
    model: "gpt-5",
    updated_at: "2026-11-09T11:00:00Z",
    asks: "Should we retry after 5 seconds?",
    turn_error: "Socket closed unexpectedly",
  },
];

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  reset();
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (String(url).includes("/pins")) {
        return Promise.resolve(new Response(JSON.stringify({ spaces: [], sessions: ["mac/s2"] })));
      }
      return Promise.resolve(new Response("{}"));
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
});

describe("Agents Tab Chat-List & Composable Filter Bar", () => {
  it("renders IM Chat-List rows with pinned sessions at the top, metadata chips, and Search as Code", async () => {
    Object.assign(getState(), {
      loaded: true,
      machines,
      sessions: { gcp: [sessions[0]], mac: [sessions[1]] },
      pins: { spaces: [], sessions: ["mac/s2"] },
      todosLoaded: true,
    });

    await act(async () => {
      root.render(<AgentsList route={{ name: "agent-list" }} onNewAgent={() => {}} />);
    });

    const rows = [...host.querySelectorAll<HTMLElement>('[data-testid="agent-chatlist-row"]')];
    expect(rows).toHaveLength(2);
    // Pinned session s2 appears first in the Pinned section
    expect(rows[0].getAttribute("data-session-id")).toBe("s2");
    expect(rows[0].getAttribute("data-pinned")).toBe("true");
    expect(rows[1].getAttribute("data-session-id")).toBe("s1");

    // Metadata chips show machine, project, harness, model
    expect(rows[0].textContent).toContain("Mac Studio");
    expect(rows[0].textContent).toContain("orca");
    expect(rows[0].textContent).toContain("Codex");
    expect(rows[0].textContent).toContain("gpt-5");

    // Toggle Pinned filter pill
    const pinnedPill = host.querySelector<HTMLButtonElement>('[data-testid="filter-pill-pinned"]')!;
    await act(async () => {
      pinnedPill.click();
    });
    expect(host.querySelector('[data-testid="scoped-filter-strip"]')).not.toBeNull();
    const filteredRows = [...host.querySelectorAll<HTMLElement>('[data-testid="agent-chatlist-row"]')];
    expect(filteredRows.map((r) => r.getAttribute("data-session-id"))).toEqual(["s2"]);

    // Reset via Scoped Filter Strip
    const resetBtn = host.querySelector<HTMLButtonElement>('[data-testid="scoped-filter-reset"]')!;
    await act(async () => {
      resetBtn.click();
    });
    expect(host.querySelectorAll('[data-testid="agent-chatlist-row"]')).toHaveLength(2);

    // Activate Search as Code mode and enter query
    const sacToggle = host.querySelector<HTMLButtonElement>('[data-testid="agent-search-as-code-toggle"]')!;
    await act(async () => {
      sacToggle.click();
    });
    expect(host.querySelector('[data-testid="search-as-code-plan"]')).not.toBeNull();

    // Update pins dynamically
    await act(async () => {
      savePins({ spaces: [], sessions: ["gcp/s1"] });
    });
    const updatedRows = [...host.querySelectorAll<HTMLElement>('[data-testid="agent-chatlist-row"]')];
    expect(updatedRows[0].getAttribute("data-session-id")).toBe("s1");
    expect(updatedRows[0].getAttribute("data-pinned")).toBe("true");
  });
});
