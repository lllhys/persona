// A Persona conversation's page (`/persona/<id>`). With several assistants the conversation may be another assistant's
// than the current one (an inbox item's hand-off, a link, the palette, a to-do's origin), and the page must open it on
// the computer it runs on: what this tab noted at a hand-off (lib/outbox.ts adoptConversation) is gone after a reload
// and in a new tab, and a hub with the paged sessions list pushes no rows to look it up in. So the address names the
// computer (`?machine=`) and, when it is another manager's than the default, the manager (`?manager=`); the page asks the
// hub for that exact row, which the rest (its manager's offline state, its plan buttons) then reads (docs/web/web.md).
// One assistant: the plain address, as before.
import { defaultManager, otherManager } from "./manager-choice";
import { currentRoster, multiManager } from "./managers";
import type { Route } from "./router";

/** The page of conversation sid, on computer mid, of manager (none: the default's). Without the computer (a to-do names
 *  only its origin conversation) the manager is named even when it is the default: the page reads where its assistant
 *  runs. */
export function conversationPage(sid: string, mid?: string, manager?: string, at?: string): Route {
  const msg = at ? { at } : {};
  if (!multiManager(currentRoster())) return { name: "persona", sid, ...msg };
  if (!mid) return { name: "persona", sid, manager: manager || defaultManager(), ...msg };
  const other = otherManager(manager);
  return { name: "persona", sid, mid, ...(other ? { manager: other } : {}), ...msg };
}
