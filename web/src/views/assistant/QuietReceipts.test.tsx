// A task's card comes once in a Persona conversation, and how it goes on is its row (#8237, docs/assistant/unified-entry.md
// "Receipts"): no receipt, review or report is drawn; the hand-off card says a failed start in plain words (the raw error
// is the task's details') and a turn the model service cut off, in place.
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import { t } from "../../i18n";
import { type FeedItem } from "../../lib/feed";
import { getState } from "../../lib/store";
import type { Todo } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { FeedLineView } from "./Tasks";

const now = new Date().toISOString();
const todo = (o: Partial<Todo>): Todo => ({ id: "todo_1", title: "修连接", origin: "c1", created_at: now, updated_at: now, done_at: null, agents: [], ...o }) as unknown as Todo; // i18n-ignore
const item = (kind: string, body: Record<string, unknown> = {}): FeedItem => ({ id: "f1", conversation: "c1", at: now, kind, ref: "todo_1", body }) as FeedItem;
const draw = (it: FeedItem) => renderToStaticMarkup(<FeedLineView item={it} simplified />);

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };
afterEach(() => seed(before));

describe("a task's one card in the chat", () => {
  it("draws no receipt, review or report, whatever state they say", () => {
    seed({ todos: [todo({})] });
    for (const state of ["done", "failed", "interrupted"]) expect(draw(item("receipt", { title: "修连接", state, summary: "测试补好了。" }))).toBe(""); // i18n-ignore
    expect(draw(item("review", { verdict: "accepted", note: "OK" }))).toBe("");
    expect(draw(item("report", { text: "50%" }))).toBe("");
  });

  it("says why a start failed in plain words on the hand-off card, the raw error left to the task", () => {
    seed({ todos: [todo({ start_error: "rpc: connection closed" })] });
    const html = draw(item("task"));
    expect(html).toContain(t("assistant-tasks.start_failed", { error: t("assistant-tasks.start_cause_connection_lost") }));
    expect(html).not.toContain("rpc: connection closed");
    seed({ todos: [todo({ start_error: "exit status 1" })] });
    expect(draw(item("task"))).toContain(t("assistant-tasks.start_failed_details"));
  });

  it("says a turn the model service cut off on the hand-off card while the task is open", () => {
    const agent = { machine_id: "box", session_id: "s1", linked_at: now };
    const cut: Session = { machine_id: "box", session_id: "s1", title: "修连接", state: "idle", status: "idle", updated_at: now, turn_error: "API Error: 529 Overloaded" } as Session; // i18n-ignore
    seed({ todos: [todo({ agents: [agent] })], sessions: { box: [cut] } });
    expect(draw(item("task"))).toContain("data-handoff-interrupted");
    seed({ todos: [todo({ agents: [agent], done_at: now })], sessions: { box: [cut] } });
    expect(draw(item("task"))).not.toContain("data-handoff-interrupted");
  });
});
