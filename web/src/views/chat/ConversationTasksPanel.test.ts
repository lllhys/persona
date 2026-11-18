import { describe, expect, it } from "vitest";
import type { Todo } from "../../lib/todos";
import { conversationTasks } from "./ConversationTasksPanel";

function task(id: string, fields: Partial<Todo>): Todo {
  return { id, title: id, created_at: "", updated_at: "", ...fields };
}

describe("conversation tasks", () => {
  it("includes tasks dispatched by the main chat and tasks supervised by it, once each", () => {
    const supervised: NonNullable<Todo["supervision"]> = { supervisor: "main", done_when: [], round: 0, reviews: [] };
    const todos = [
      task("dispatched", { origin: "main", supervision: { ...supervised, supervisor: "background" } }),
      task("supervised", { origin: "other", supervision: supervised }),
      task("both", { origin: "main", supervision: supervised }),
      task("unrelated", { origin: "other" }),
      task("other-manager", { origin: "main", manager: "another" }),
    ];
    expect(conversationTasks(todos, "main", "home", "home").map((todo) => todo.id))
      .toEqual(["dispatched", "supervised", "both"]);
  });
});
