// Urgent calls in a Persona conversation (docs/assistant/urgent-call.md): the conversation's calls as feed items of kind
// `urgent_call`, so displayLines puts each at its time like the rest of the feed. Loaded with the conversation, outside
// the first screen; read again when a call there moves on (the `urgent_call` message) and after a connection that did
// not resume. A manager without urgent calls, or a device without full access, has none to show.
import { api } from "../../lib/api";
import { conversationHeaders, type FeedItem } from "../../lib/feed";
import { isResumed } from "../../lib/reconnect";
import { onHubEvent } from "../../lib/store";

export interface UrgentCall {
  id: string;
  title: string;
  body?: string;
  why?: string;
  happening?: string;
  rule?: { id: string; text: string };
  state: string;
  attempt?: number;
  created_at: string;
  rang_at?: string;
  answered_at?: string;
  ended_at?: string;
  seconds?: number;
  outcome?: string;
  transcript?: string;
}

/** A call as a feed item of its conversation, at the time it first rang; one that could not ring is not shown. */
export function callItems(conversation: string, calls: UrgentCall[]): FeedItem[] {
  return calls
    .filter((c) => c.state !== "failed")
    .map((c) => ({ id: "call-" + c.id, conversation, at: c.rang_at ?? c.created_at, kind: "urgent_call" as const, body: c as unknown as Record<string, unknown> }));
}

/** Reads conversation `sid`'s calls into `set` now and whenever they change; returns the stop. */
export function watchCalls(sid: string, set: (items: FeedItem[]) => void): () => void {
  let stopped = false;
  const load = async () => {
    try {
      const r = await api<{ calls?: UrgentCall[] }>(`/assistant/calls?conversation=${encodeURIComponent(sid)}`, { headers: conversationHeaders(sid) });
      if (!stopped) set(callItems(sid, r.calls ?? []));
    } catch {
      // an older manager (404), a device without full access (403), the hub away: no rows
    }
  };
  void load();
  const offCall = onHubEvent("urgent_call", (m) => {
    if (m.conversation === sid) void load();
  });
  const offHello = onHubEvent("hello", (m) => {
    if (!isResumed(m)) void load();
  });
  return () => {
    stopped = true;
    offCall();
    offHello();
  };
}
