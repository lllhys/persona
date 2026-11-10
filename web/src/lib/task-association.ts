import type { Item } from "./types";

const LAUNCHED = /(?:running in background with ID:?|agentId:|Monitor started \(task:?|moved to the background \(ID:?)\s*([A-Za-z0-9_-]+)/;

export function launchedTaskID(text: string): string | undefined {
  return LAUNCHED.exec(text)?.[1];
}

/** Pure association for paging/cache code; importing it does not load task presentation or localization. */
export function taskCalls(items: Item[]): Map<string, string> {
  const byTask = new Map<string, string>();
  for (const it of items) {
    const call = it.tool_use_id || it.task?.tool_use_id;
    if (!call) continue;
    const task = it.task?.task_id || (it.kind === "tool_result" ? launchedTaskID(it.text ?? "") : undefined);
    if (task) byTask.set(task, call);
  }
  const calls = new Map<string, string>();
  for (const it of items) {
    if (!it.task) continue;
    const call = it.tool_use_id || it.task.tool_use_id || (it.task.task_id ? byTask.get(it.task.task_id) : undefined);
    if (call) calls.set(it.id, call);
  }
  return calls;
}

/** Only legacy agent notices whose launching call is loaded disappear into that call's disclosure. */
export function attachedAgentNotices(items: Item[]): Set<string> {
  const launches = new Set(items.filter((i) => i.kind === "tool_use" && (i.tool_name === "Agent" || i.tool_name === "Task")).map((i) => i.tool_use_id));
  const calls = taskCalls(items);
  return new Set(items.filter((i) => i.kind === "notice" && i.task?.kind === "agent" && Boolean(calls.get(i.id)) && launches.has(calls.get(i.id))).map((i) => i.id));
}
