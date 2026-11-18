// "Show scheduled task chats" in the sidebar's Recent (#8324): the assistant's `recent_task_chats` on the hub, one value
// for all of the user's devices (off by default; an older hub leaves it out, and then there is no switch). All chats'
// ⋯ menu saves it here: the sidebar shows the change at once, and it goes back if the hub says no (the caller says why).
// Settings › Assistant saves the same field for the assistant it shows.
import { api } from "./api";
import { getState, patchAssistant } from "./store";

export async function saveRecentTaskChats(on: boolean): Promise<void> {
  const was = getState().assistant?.recent_task_chats;
  patchAssistant({ recent_task_chats: on });
  try {
    const r = await api<{ recent_task_chats?: boolean }>("/assistant", { method: "PUT", json: { recent_task_chats: on } });
    patchAssistant({ recent_task_chats: r.recent_task_chats ?? on });
  } catch (e) {
    patchAssistant({ recent_task_chats: was });
    throw e;
  }
}
