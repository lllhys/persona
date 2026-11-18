// "Live link" (#7497): its own module so a card can offer it without loading the Agents list's menu actions (the
// outbox among them).
import { t } from "../i18n";
import { toast } from "../ui/atoms";
import { attachSession, errorText } from "./api";
import { sessionCan } from "./format";
import { SESSIONS_LOOKUP_FEATURE } from "./runtime-list";
import { exactSession } from "./runtime-session";
import { applyCompleteSessions, getSession, getState, refresh, rememberSessionRow } from "./store";
import type { Session } from "./types";
import { heldChild, rereadHeldChild } from "./use-child-sessions";

/** "Live link": link a session from another source (a Codex app-server thread…) live. The chat's ⋯, the row menu and a
 *  card the page can't answer call this; it reads the session again first, since the menu may have opened on an older
 *  copy, and a child's card on its parent's page reads the parent's held row (#5120). */
export async function attachLive(s: Pick<Session, "machine_id" | "session_id">) {
  const { machine_id: mid, session_id: sid } = s;
  if (!sessionCan(getSession(mid, sid) ?? heldChild(mid, sid), "attach")) return;
  try {
    await attachSession(mid, sid);
    await Promise.all([refresh(), readLinked(mid, sid)]);
    toast(t("chat-chatview.attached"), "ok");
  } catch (e) {
    toast(t("chat-chatview.attach_failed", { error: errorText(e) }), "error");
  }
}

/** The linked session's new row where its card reads it. A paging hub's refresh() reloads only the Agents list's bounded
 *  page, which may not hold this session (an off-page one, a child folded under its parent): its exact lookup reads
 *  the row, and a parent's open chat that holds it reads its children again. A failed read leaves the row to the hub's
 *  next hint; the link itself is made. */
async function readLinked(mid: string, sid: string) {
  const held = heldChild(mid, sid) ? rereadHeldChild(mid, sid) : undefined;
  if (getState().features.includes(SESSIONS_LOOKUP_FEATURE)) {
    const row = await exactSession(mid, sid).catch(() => undefined);
    if (row) {
      rememberSessionRow(row);
      applyCompleteSessions([row]);
    }
  }
  await held;
}
