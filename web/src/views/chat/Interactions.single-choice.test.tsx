// @vitest-environment jsdom
// A single-question, single-choice card sends on the first option tap. Multiple questions still paginate, and a
// multi-select still waits for its send arrow.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
const calls = vi.hoisted(() => [] as { path: string; body: Record<string, unknown> }[]);
vi.mock("../../lib/ops", () => ({
  apiOp: vi.fn(async (path: string, init: { json: Record<string, unknown> }) => {
    calls.push({ path, body: init.json });
    return {};
  }),
}));
import { InteractionCard } from "./Interactions";
import { t } from "../../i18n";
import { getState } from "../../lib/store";
import type { Interaction, Session } from "../../lib/types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const single = {
  id: "q_single", machine_id: "m", session_id: "s", kind: "question", tool_name: "AskUserQuestion",
  questions: [{ question: "Which library?", multi_select: false, options: [{ label: "A" }, { label: "B" }] }],
} as Interaction;
const multiple = {
  id: "q_multiple", machine_id: "m", session_id: "s", kind: "question", tool_name: "AskUserQuestion",
  questions: [
    { question: "First question", multi_select: false, options: [{ label: "A" }, { label: "B" }] },
    { question: "Second question", multi_select: false, options: [{ label: "C" }, { label: "D" }] },
  ],
} as Interaction;
const multi = {
  id: "q_multi", machine_id: "m", session_id: "s", kind: "question", tool_name: "AskUserQuestion",
  questions: [{ question: "Which libraries?", multi_select: true, options: [{ label: "A" }, { label: "B" }] }],
} as Interaction;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const option = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("button[aria-pressed]")].find((b) => b.textContent?.includes(label))!;
const sendArrow = () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-interactions.send_answer")}"]`)!;
const customAnswer = () => host.querySelector<HTMLInputElement>(`input[aria-label="${t("chat-interactions.custom_answer")}"]`)!;

beforeEach(() => {
  calls.length = 0;
  Object.assign(getState(), { sessions: { m: [{ machine_id: "m", session_id: "s" } as Session] }, interactions: [], answered: {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("question card submission", () => {
  it("submits a single-question, single-choice answer on one option tap", async () => {
    act(() => root.render(<InteractionCard ix={single} />));
    act(() => option("A").click());
    await flush();
    expect(option("A").getAttribute("aria-pressed")).toBe("true");
    expect(calls).toEqual([{ path: "/interactions/q_single/resolve", body: { answers: { "Which library?": "A" } } }]);
  });

  it("clears a nonblank custom answer and waits for explicit submission when an option is picked", async () => {
    act(() => root.render(<InteractionCard ix={single} />));
    const field = customAnswer();
    act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(field, "  Custom library  ");
      field.dispatchEvent(new Event("input", { bubbles: true }));
    });
    act(() => option("A").click());
    await flush();
    expect(option("A").getAttribute("aria-pressed")).toBe("true");
    expect(calls).toEqual([]);
    expect(customAnswer().value).toBe("");
    act(() => sendArrow().click());
    await flush();
    expect(calls).toEqual([{ path: "/interactions/q_single/resolve", body: { answers: { "Which library?": "A" } } }]);
  });

  it("keeps a multi-select answer unsubmitted until the send arrow", async () => {
    act(() => root.render(<InteractionCard ix={multi} />));
    act(() => option("A").click());
    await flush();
    expect(option("A").getAttribute("aria-pressed")).toBe("true");
    expect(calls).toEqual([]);
    act(() => sendArrow().click());
    await flush();
    expect(calls).toEqual([{ path: "/interactions/q_multi/resolve", body: { answers: { "Which libraries?": "A" } } }]);
  });

  it("keeps multiple questions on their paginate-then-submit flow", async () => {
    vi.useFakeTimers();
    act(() => root.render(<InteractionCard ix={multiple} />));
    act(() => option("A").click());
    await flush();
    expect(calls).toEqual([]);
    act(() => vi.advanceTimersByTime(400));
    expect(host.textContent).toContain("Second question");
    act(() => option("C").click());
    await flush();
    expect(calls).toEqual([]);
    act(() => sendArrow().click());
    await flush();
    expect(calls).toEqual([{
      path: "/interactions/q_multiple/resolve",
      body: { answers: { "First question": "A", "Second question": "C" } },
    }]);
  });
});
