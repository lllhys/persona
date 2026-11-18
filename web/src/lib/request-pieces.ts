// Whether the To-dos page offers "By request": kept out of lib/todos.ts, which every first screen loads, since only the
// To-dos page (loaded when opened) asks.
import type { Todo } from "./todos";

/** Whether any request is in pieces (docs/assistant/todos.md "Requests in pieces"): a to-do is a piece of another listed
 *  one, or two to-dos came from one task plan. The To-dos page offers "By request" only then. */
export function hasRequestPieces(todos: readonly Todo[]): boolean {
  const ids = new Set(todos.map((t) => t.id));
  const plans = new Set<string>();
  for (const t of todos) {
    if (t.part_of && t.part_of !== t.id && ids.has(t.part_of)) return true;
    if (t.plan_id) {
      if (plans.has(t.plan_id)) return true;
      plans.add(t.plan_id);
    }
  }
  return false;
}
