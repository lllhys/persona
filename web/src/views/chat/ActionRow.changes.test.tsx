// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it } from "vitest";
import { closePanel, getPanel } from "../../lib/panel";
import type { Item } from "../../lib/types";
import type { ToolStep } from "../../lib/work-groups";
import { ActionRow } from "./ActionRow";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => closePanel());
});
afterEach(() => {
  act(() => closePanel());
  act(() => root.unmount());
  host.remove();
});

it("an edit row opens its call in the changes side panel, not a modal", async () => {
  const use: Item = {
    id: "e1#0", seq: 1, role: "assistant", kind: "tool_use", tool_name: "Edit", tool_use_id: "call-e1",
    tool_input: { file_path: "/work/a.ts", old_string: "old", new_string: "new" },
  };
  const result: Item = { id: "r1#0", seq: 2, role: "user", kind: "tool_result", tool_use_id: "call-e1", text: "ok" };
  const step: ToolStep = { kind: "tool", id: use.id, use, result, images: [] };
  act(() => root.render(<ActionRow step={step} mid="m" sid="s" />));
  const row = host.querySelector<HTMLButtonElement>("button[data-action-row], [data-action-row] > button")!;
  await act(async () => { row.click(); });

  expect(getPanel()).toEqual({ kind: "changes", mid: "m", sid: "s", at: { path: "/work/a.ts", call: "call-e1" } });
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
