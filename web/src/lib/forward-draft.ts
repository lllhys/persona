// The picked message as a forward (#3899, #3838, #9469): who wrote it and where, its text as the chat holds it. Only the
// chunks that forward, copy or export picked messages load it (ForwardDialog, Picking, ExportImage), not the first screen:
// the chat hands Forward to its picks and itself (ForwardBatch.from). No DOM here.
import { tabLabel } from "./format";
import type { ForwardChat, ForwardDraft, ForwardPick, ForwardRole, ForwardSource } from "./forward";
import { agentLabel } from "./mentions";

/** The picked message as a forward (iOS ChatView.forwardDraft): who wrote it and where, its text as this chat holds it
 *  (the transcript's, quotes and all; the whole of it read when it goes, if cut) and your own message's pictures. */
export function forwardDraftOf(c: ForwardChat, p: ForwardPick): ForwardDraft {
  const s = c.session;
  const role: ForwardRole = p.isUser ? "user" : p.from ? "other" : c.assistant ? "persona" : "agent";
  const author = p.from ?? (role === "persona" ? "Persona" : s ? agentLabel(s) : "Claude");
  const project = c.assistant || !s ? "" : [s.space_name, tabLabel(s)].filter(Boolean).join(" › ");
  // a message of yours is its transcript line's text blocks (with the images that came with them); a reply its item
  const entry = p.itemId.split("#")[0];
  const texts = p.isUser
    ? c.items.filter((i) => i.role === "user" && i.kind === "text" && (i.id === p.itemId || i.id.split("#")[0] === entry))
    : c.items.filter((i) => i.id === p.itemId);
  const raw = texts.map((i) => i.text ?? "").join("\n") || p.text;
  const cut = texts.length === 1 && texts[0].full_size ? texts[0].id : undefined;
  const source: ForwardSource = {
    mid: c.mid,
    sid: c.sid,
    item: p.itemId,
    role,
    author,
    chat: c.title,
    ...(project ? { project } : {}),
    ...(!c.assistant && c.machine ? { machine: c.machine } : {}),
    ...(p.ts ?? texts[0]?.ts ? { time: p.ts ?? texts[0]?.ts } : {}),
  };
  return {
    source,
    raw,
    ...(cut ? { cut } : {}),
    ...(p.isUser && p.images?.length ? { images: p.images } : {}),
    ...(p.isUser && p.photos?.length ? { photos: p.photos } : {}),
    ...(s?.cwd ? { cwd: s.cwd } : {}),
  };
}
