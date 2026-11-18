// What the Feishu chat page has sent and not yet seen come back (docs/extensions/lark-attachments.md "Sending and confirmation"): text, pictures
// and files go one after another, each with a client_id of its own, drawn at once as a local message that the hub's
// copy replaces. A failed one can be sent again with the same client_id; a file whose upload broke off halfway is not
// sent again on its own: it is checked against the newest page, or its local copy removed.
import { useCallback, useEffect, useMemo, useRef } from "react";
import { errorText } from "../../lib/api";
import { type LarkMention, type LarkMessage } from "../../lib/inbox";
import { nextLarkPosition, replyLarkMessage, sendLarkMessage } from "../../lib/inbox-page";
import { fileOutcomeUnknown, releaseLarkAttachments, sendLarkAttachment, type LarkAttachment, type LarkAttachmentRoute } from "../../lib/lark-attachments";
import { useItemManager } from "../../lib/manager-choice";
import { t } from "../../i18n";
import { toast } from "../../ui/atoms";

export interface LarkOutgoing {
  clientID: string;
  text?: string;
  attachment?: LarkAttachment;
  route: LarkAttachmentRoute;
  /** Whom the text @s (a group's message); kept with it, so sending it again @s them too. */
  mentions?: LarkMention[];
  /** For the local message: what it quotes. */
  quote?: { id: string; sender_name?: string; text?: string };
}

export function newClientID(): string {
  return "wmsg-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 8);
}

/** The local message drawn until the hub's copy comes. */
export function localMessage(o: LarkOutgoing, position: string): LarkMessage {
  const a = o.attachment;
  const parts = a
    ? [a.kind === "image" ? { kind: "image", url: a.url, text: a.name } : { kind: "file", text: a.name, metadata: { size: a.size } }]
    : [{ kind: "text", text: o.text ?? "" }];
  return {
    id: "temp-" + o.clientID,
    client_id: o.clientID,
    position,
    is_mine: true,
    sender_name: t("inbox-larkthread.me"),
    created_at: new Date().toISOString(),
    type: a ? (a.kind === "image" ? "IMAGE" : "FILE") : undefined,
    text: a ? a.name : o.text,
    parts,
    sending: true,
    thread_id: o.route.thread ? o.route.threadRoot ?? o.route.replyTo : undefined,
    reply_to: o.quote && !o.route.thread ? o.quote : undefined,
  };
}

/** Sends `o` the way it is routed. */
export function deliver(feed: string, o: LarkOutgoing, ol?: number, manager?: string): Promise<{ message_id?: string }> {
  if (o.attachment) return sendLarkAttachment(feed, o.attachment, o.clientID, o.route, ol, manager);
  if (o.route.replyTo) return replyLarkMessage(o.route.replyTo, o.text ?? "", Boolean(o.route.thread), o.clientID, ol, o.mentions, manager);
  return sendLarkMessage(feed, o.text ?? "", o.clientID, ol, o.mentions, manager);
}

type Update = (fn: (messages: LarkMessage[]) => LarkMessage[]) => void;

/**
 * The sends of one list (the chat, or one thread under it): `update` changes the list's messages. `newest` reads the
 * list's newest messages again for "Check result".
 */
export function useLarkOutbox(feed: string, ol: number | undefined, update: Update, current: () => LarkMessage[],
  newest?: () => Promise<LarkMessage[]>) {
  // the chat's manager (LarkThread provides it): a send queued here goes to it even after the page moved on
  const manager = useItemManager();
  const pending = useRef(new Map<string, LarkOutgoing>());
  // previews of pictures sent: their local messages still show them until the hub's copy comes
  const sent = useRef<LarkAttachment[]>([]);
  const queue = useRef<Promise<void>>(Promise.resolve());
  const updateRef = useRef(update);
  updateRef.current = update;
  const currentRef = useRef(current);
  currentRef.current = current;
  const newestRef = useRef(newest);
  newestRef.current = newest;

  // pictures' previews go with the page
  useEffect(() => {
    const map = pending.current;
    const done = sent.current;
    return () => {
      releaseLarkAttachments([...map.values()].flatMap((o) => (o.attachment ? [o.attachment] : [])));
      releaseLarkAttachments(done);
      map.clear();
      done.length = 0;
    };
  }, [feed, ol]);

  const mark = useCallback((clientID: string, patch: Partial<LarkMessage>) => {
    updateRef.current((list) => list.map((m) => (m.client_id === clientID && m.id.startsWith("temp-") ? { ...m, ...patch } : m)));
  }, []);

  const run = useCallback(async (o: LarkOutgoing) => {
    try {
      const r = await deliver(feed, o, ol, manager);
      // a file taken without a message id stays as it is: sent, nothing to send again
      const id = r.message_id;
      updateRef.current((list) => (id && list.some((m) => m.id === id)
        ? list.filter((m) => !(m.client_id === o.clientID && m.id.startsWith("temp-"))) // the hub's copy came first
        : list.map((m) => (m.client_id === o.clientID && m.id.startsWith("temp-")
          ? { ...m, sending: false, failed: false, local_state: undefined, ...(id ? { id } : {}) } : m))));
      if (r.message_id || o.attachment?.kind === "file") {
        pending.current.delete(o.clientID);
        if (o.attachment) sent.current.push(o.attachment);
      }
    } catch (e) {
      const unknown = o.attachment?.kind === "file" && fileOutcomeUnknown(e);
      mark(o.clientID, { sending: false, failed: true, local_state: unknown ? "unknown" : "failed" });
      toast(t("inbox-larkthread.send_failed", { error: errorText(e) }), "error");
    }
  }, [feed, ol, manager, mark]);

  /** Draws them at once and sends them in order, after whatever is still going out. */
  const send = useCallback((outs: LarkOutgoing[]) => {
    const list = currentRef.current();
    const local: LarkMessage[] = [];
    for (const o of outs) {
      pending.current.set(o.clientID, o);
      local.push(localMessage(o, nextLarkPosition([...list, ...local])));
    }
    updateRef.current((have) => [...have, ...local.filter((m) => !have.some((x) => x.id === m.id))]);
    for (const o of outs) queue.current = queue.current.then(() => run(o));
    return queue.current;
  }, [run]);

  const retry = useCallback((m: LarkMessage) => {
    const o = (m.client_id && pending.current.get(m.client_id)) || {
      clientID: m.client_id || newClientID(), text: m.text ?? "", route: { replyTo: m.reply_to?.id },
    };
    pending.current.set(o.clientID, o);
    mark(o.clientID, { sending: true, failed: false, local_state: undefined });
    queue.current = queue.current.then(() => run(o));
    return queue.current;
  }, [mark, run]);

  const discard = useCallback((m: LarkMessage) => {
    const o = m.client_id ? pending.current.get(m.client_id) : undefined;
    if (o?.attachment) releaseLarkAttachments([o.attachment]);
    if (m.client_id) pending.current.delete(m.client_id);
    updateRef.current((list) => list.filter((x) => x.id !== m.id));
  }, []);

  /** "Check result": the hub's copy (same client_id) replaces the local one; not there, it stays to be removed by hand. */
  const check = useCallback(async (m: LarkMessage) => {
    const read = newestRef.current;
    if (!read) return;
    try {
      const fresh = await read();
      const found = fresh.find((x) => x.client_id === m.client_id && !x.id.startsWith("temp-"));
      if (found) {
        discard(m);
        updateRef.current((list) => (list.some((x) => x.id === found.id) ? list : [...list, found]));
        toast(t("inbox-larkthread.check_found"), "ok");
      } else {
        toast(t("inbox-larkthread.check_missing"), "error");
      }
    } catch (e) {
      toast(errorText(e), "error");
    }
  }, [discard]);

  return useMemo(() => ({ send, retry, discard, check }), [send, retry, discard, check]);
}
