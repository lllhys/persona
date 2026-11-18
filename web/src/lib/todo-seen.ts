// Seen to-dos (#5707, docs/assistant/todo-status-sop.md "Seen"): Got it on a Just finished row, or opening the to-do, files
// it under Done at once instead of after 24 hours. The hub keeps the mark (`seen_at`), so every device agrees; Undo takes
// it off. A hub without `todo_seen` has no mark: the page offers neither.
import { api, enc } from "./api";
import { getState, hubHas, putTodo } from "./store";
import type { Todo } from "./todos";

/** The hub keeps seen marks (hello `features`): `PUT` / `DELETE /todos/{id}/seen` and `seen_at` on a to-do. */
export const TODO_SEEN_FEATURE = "todo_seen";

/** Whether the page offers Got it and counts opening a to-do as seeing it: only when the hub says it keeps the mark. */
export function todoSeenOn(features: string[]): boolean {
  return hubHas(features, TODO_SEEN_FEATURE);
}

/**
 * Marks to-do `id` seen (`seen`) or takes the mark off (Undo). The list shows it at once (principle 7), the hub's answer
 * takes its place, and a failure puts back the mark it had and throws. `after`: the moment that put it in Just finished
 * (its triage's `last`), so the mark shown before the hub answers is not earlier than it on a clock behind the hub's.
 */
export async function setTodoSeen(id: string, seen: boolean, after = 0): Promise<void> {
  const cur = getState().todos.find((t) => t.id === id);
  if (!cur) return;
  // putTodo: a /todos answer asked for before this is older and must not put the old mark back
  putTodo({ ...cur, seen_at: seen ? new Date(Math.max(Date.now(), after)).toISOString() : null });
  try {
    const r = await api<{ todo: Todo }>(`/todos/${enc(id)}/seen`, { method: seen ? "PUT" : "DELETE" });
    putTodo({ ...r.todo, seen_at: r.todo.seen_at ?? null });
  } catch (e) {
    putTodo({ ...cur, seen_at: cur.seen_at ?? null });
    throw e;
  }
}
