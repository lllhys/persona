// @vitest-environment jsdom
// A streamed delta re-renders its SegmentView (the `streaming` prop changes), but not the steps of its turn: their
// props stay the same objects, so StepView and FreshAnswer skip. FreshWords still reaches the words that just came in.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptyTaskIndex } from "../../lib/tasks";
import type { Item } from "../../lib/types";
import type { Segment, Step } from "../../lib/turns";
import { ChatContext, FreshWords, type ChatContextValue } from "./chat-context";
import { SegmentView } from "./SegmentView";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const T0 = Date.parse("2026-11-04T10:00:00Z");
const at = (s: number) => new Date(T0 + s * 1000).toISOString();
const text = (n: number): Step => {
  const item = { id: `a${n}#0`, seq: n, role: "assistant", kind: "text", text: `words ${n}`, ts: at(n) } as Item;
  return { kind: "text", id: item.id, item };
};
const tool = (n: number): Step => ({
  kind: "tool",
  id: `t${n}#0`,
  use: { id: `t${n}#0`, seq: n, role: "assistant", kind: "tool_use", tool_name: "Bash", tool_use_id: `c-${n}`, tool_input: { command: `echo ${n}` }, ts: at(n) },
  result: { id: `t${n}-r#0`, seq: n, role: "user", kind: "tool_result", tool_use_id: `c-${n}`, text: "ok", ts: at(n) },
  images: [],
});

// a live turn: words between tool calls, every run of words a top-level StepView -> FreshAnswer
const steps = [1, 3, 5, 7, 9].flatMap((n) => [text(n), tool(n + 1)]);
const seg: Segment = { id: "seg", steps, tools: 5, thoughts: 0, replies: 5, folds: [], start: at(0) };
const ctx: ChatContextValue = { tasks: emptyTaskIndex(), subagents: [], scope: "" };
const expandFold = async () => {};

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

/** FreshAnswer asks FreshWords once per render: a spied `has` counts its renders. */
function freshSet(ids: string[]) {
  const set = new Set(ids);
  const has = vi.spyOn(set, "has");
  return { set, has };
}

function draw(fresh: ReadonlySet<string>, streaming?: string) {
  act(() =>
    root.render(
      <ChatContext.Provider value={ctx}>
        <FreshWords.Provider value={fresh}>
          <SegmentView seg={seg} live mid="m" sid="s" streaming={streaming} expandFold={expandFold} />
        </FreshWords.Provider>
      </ChatContext.Provider>,
    ),
  );
}

describe("StepView / FreshAnswer memo", () => {
  it("a streamed delta does not re-render the steps of its turn", () => {
    const { set, has } = freshSet([]);
    draw(set, "w");
    const first = has.mock.calls.length;
    expect(first).toBe(0); // old persisted narration is hidden while the single live text streams
    expect(host.querySelector("[data-work-detail]")).toBeNull();
    expect(host.textContent).not.toContain("words 9");
    for (let i = 0; i < 10; i++) draw(set, "w" + "x".repeat(i + 1));
    expect(has).toHaveBeenCalledTimes(first);
    expect(host.querySelector('[data-answer="streaming"]')).not.toBeNull();
  });

  it("words that just came in still learn it through FreshWords", () => {
    draw(freshSet([]).set);
    const next = freshSet(["a9#0"]);
    draw(next.set);
    // Only the latest persisted text is visible and reads freshness, even though the turn contains five text segments.
    expect(next.has).toHaveBeenCalledTimes(1);
    expect(next.has).toHaveBeenCalledWith("a9#0");
    expect(next.has.mock.results.some((r) => r.value === true)).toBe(true);
  });
});
