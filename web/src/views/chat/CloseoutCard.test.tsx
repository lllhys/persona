// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { loadPages } from "../../lib/lazy-page";
import { segments, type Segment } from "../../lib/turns";
import type { Item, Session } from "../../lib/types";
import { CloseoutCard } from "./CloseoutCard";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The evidence row is a chunk of its own (CloseoutEvidence, #8118); these tests draw it without waiting for it.
beforeAll(() => loadPages(), 60_000);

let host: HTMLDivElement;
let root: Root;
let seq = 0;
const session = {
  machine_id: "m", session_id: "s", cwd: "/x", title: "Check it", status: "done", state: "idle",
  work: { phase: "done", closeout: { status: "done", verified: "`npm test`" } },
} as Session;

function item(kind: Item["kind"], extra: Partial<Item> = {}): Item {
  seq++;
  return { id: `i${seq}#0`, seq, role: kind === "tool_result" ? "user" : "assistant", kind, ...extra };
}

function exchange(command = "npm test", result: Partial<Item> = { is_error: false, outcome: { status: "ok", exit_code: 0 } }): Segment {
  return segments([
    item("text", { role: "user", text: "Check it" }),
    item("tool_use", { tool_use_id: "call", tool_name: "Bash", tool_input: { command } }),
    item("tool_result", { tool_use_id: "call", text: "output", ...result }),
    item("text", { text: "Done" }),
  ])[0];
}

function folded(): { segment: Segment; fold: Item } {
  const fold = item("fold", { fold: { items: 2, tools: 1, thoughts: 0, through_seq: 4 } });
  return { segment: { id: "folded", steps: [], tools: 1, thoughts: 0, replies: 0, folds: [fold] }, fold };
}

async function draw(segment: Segment | undefined, expandFold = vi.fn(async () => true)) {
  await act(async () => root.render(<CloseoutCard session={session} segment={segment} expandFold={expandFold} />));
  return expandFold;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
});

describe("CloseoutCard command evidence", () => {
  it("shows the recorded command, numeric exit and error flag without a verification badge", async () => {
    await draw(exchange("cd web; npm test", { is_error: true, outcome: { status: "failed", exit_code: 1 } }));
    const line = host.querySelector("[data-closeout-command-evidence='exit']");
    expect(line?.textContent).toContain("cd web; npm test");
    expect(line?.textContent).toContain(t("chat-closeoutevidence.exit", { code: 1 }));
    expect(line?.textContent).toContain("is_error");
    expect(host.querySelector("[data-closeout-evidence] .text-green")).toBeNull();
  });

  it("keeps is_error false separate from a missing numeric exit", async () => {
    await draw(exchange("npm test", { is_error: false }));
    const line = host.querySelector("[data-closeout-command-evidence='error']");
    expect(line?.textContent).toContain("is_error");
    expect(line?.textContent).toContain(t("chat-closeoutevidence.no_exit"));
    expect(line?.textContent).not.toContain(t("chat-closeoutevidence.exit", { code: 0 }));
  });

  it("says not-found only for this segment, and unavailable without its segment", async () => {
    await draw(exchange("npm run build"));
    expect(host.querySelector("[data-closeout-command-evidence='not_found']")?.textContent).toContain(t("chat-closeoutevidence.not_found"));
    await draw(undefined);
    expect(host.querySelector("[data-closeout-command-evidence='unavailable']")?.textContent).toContain(t("chat-closeoutevidence.unavailable"));
    expect(host.querySelector("[data-closeout-command-evidence='not_found']")).toBeNull();
  });

  it("loads folds on demand, leaves a failure unavailable and retries it", async () => {
    const { segment, fold } = folded();
    const expand = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(true);
    await draw(segment, expand);
    expect(expand).toHaveBeenCalledTimes(1);
    expect(expand).toHaveBeenCalledWith(fold);
    expect(host.querySelector("[data-closeout-command-evidence='unavailable']")).not.toBeNull();
    expect(host.querySelector("[data-closeout-command-evidence='not_found']")).toBeNull();
    const retry = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === t("chat-chatview.retry"));
    expect(retry).toBeDefined();
    await act(async () => retry!.click());
    expect(expand).toHaveBeenCalledTimes(2);
    await draw(exchange(), expand);
    expect(host.querySelector("[data-closeout-command-evidence='exit']")).not.toBeNull();
  });

  it("keeps a capped read unavailable after the fold marker is gone, and retries the retained fold", async () => {
    const { segment, fold } = folded();
    const expand = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    await draw(segment, expand);
    await draw({ ...exchange("npm run build"), id: segment.id }, expand);
    expect(host.querySelector("[data-closeout-command-evidence='unavailable']")).not.toBeNull();
    expect(host.querySelector("[data-closeout-command-evidence='not_found']")).toBeNull();
    const retry = Array.from(host.querySelectorAll("button")).find((button) => button.textContent === t("chat-chatview.retry"));
    await act(async () => retry!.click());
    expect(expand).toHaveBeenLastCalledWith(fold);
    await draw(exchange(), expand);
    expect(host.querySelector("[data-closeout-command-evidence='exit']")).not.toBeNull();
  });

  it("does not read a fold for prose with no command claim", async () => {
    const prose = { ...session, work: { phase: "done", closeout: { status: "done", verified: "Looks good" } } } as Session;
    const expand = vi.fn(async () => true);
    await act(async () => root.render(<CloseoutCard session={prose} segment={folded().segment} expandFold={expand} />));
    expect(expand).not.toHaveBeenCalled();
    expect(host.querySelector("[data-closeout-evidence]")).toBeNull();
  });
});
