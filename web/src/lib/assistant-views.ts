import { deletedConversationID, onConversationDeleted } from "./conversation-deleted";
// What the web knows about the assistant's cards in a conversation (docs/assistant/assistant-views.md "Persistent submission" and "Live cards"):
// the live cards (view_show / view_update, by version), the answers given from any card on any device (by revision),
// and which text blocks the hub has registered (only those take an answer). It comes from the feed's snapshot (views,
// view_actions, view_blocks) and the events assistant_view, assistant_view_action and assistant_view_block, each kept
// per hub and conversation, an older version or revision dropped.
//
// Also what is only here: each card's draft (the fields typed into, the page stack) — kept across a live update and a
// re-render, never sent until submitted — and the submit under way, with the client_msg_id it keeps across retries.
// A submit names the version the user saw (expected_version); a card that changed since answers 409 view_changed: the
// draft stays, the newer card shows, and the user confirms again. Never sent again by itself with another version.
import { useSyncExternalStore } from "react";
import { ApiError, api, enc } from "./api";
import { uuid } from "./format";
import { onHubEvent } from "./store";
import type { AssistantView, ViewAction, ViewActionInput, ViewChangedError } from "./extension-wire";

/** A card's key: `view_…` for a live card, `<item id>/<n>` for a text block. */
export type CardKey = string;

export interface ConversationCards {
  /** Live cards by id. */
  views: Record<string, AssistantView>;
  /** The answer of a card, by key. */
  actions: Record<CardKey, ViewAction>;
  /** Text blocks the hub registered from a trusted reply. */
  blocks: Record<CardKey, true>;
}

/** What is being done on one card here. */
export interface CardDraft {
  /** Field values typed in (by name), over the card's own. */
  values: Record<string, string>;
  /** Named pages pushed, the top last (empty: the root). */
  stack: string[];
  /** A submit under way or failed, kept for "Retry" with the same client_msg_id. */
  sending?: Sending;
  /** The last thing that went wrong, shown on the card. */
  error?: string;
  /** The card changed under a submit (409 view_changed): the next one asks again, at the version shown now. */
  changed?: boolean;
  /** Another device answered first (409 already_answered). */
  elsewhere?: boolean;
}

export interface Sending {
  action: string;
  page: string;
  values: Record<string, string>;
  expectedVersion: number;
  confirmed: boolean;
  clientMsgId: string;
  /** In flight now (a failed one waits for "Retry"). */
  inFlight: boolean;
}

export interface CardsState {
  /** Whose cards these are: the hub's id (hello), so one hub's never show for another's conversation. */
  hub: string;
  conversations: Record<string, ConversationCards>;
  drafts: Record<string, CardDraft>;
}

const EMPTY: ConversationCards = { views: {}, actions: {}, blocks: {} };
let st: CardsState = { hub: "", conversations: {}, drafts: {} };
const listeners = new Set<() => void>();

function set(next: CardsState) {
  st = next;
  listeners.forEach((l) => l());
}

export function useCards<T>(select: (s: CardsState) => T): T {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => select(st),
    () => select(st),
  );
}

export const getCards = () => st;

/** The draft's key: hub, conversation and card, never mixed up with another's. */
export const draftKey = (conversation: string, key: CardKey, hub = st.hub) => `${hub}\u0000${conversation}\u0000${key}`;

export function conversationCards(conversation: string, s = st): ConversationCards {
  return s.conversations[conversation] ?? EMPTY;
}

/** A different hub (another hub at this address, the hub reinstalled): nothing of the old one is kept. */
export function setHub(hub: string) {
  if (hub === st.hub) return;
  // the first hello names the hub whose page this is: what was read before it is that hub's
  if (!st.hub) return set({ ...st, hub, drafts: rekey(st.drafts, hub) });
  set({ hub, conversations: {}, drafts: {} });
}

function rekey(drafts: Record<string, CardDraft>, hub: string): Record<string, CardDraft> {
  return Object.fromEntries(Object.entries(drafts).map(([k, d]) => [hub + k.slice(k.indexOf("\u0000")), d]));
}

function patchConversation(conversation: string, f: (c: ConversationCards) => ConversationCards) {
  const old = conversationCards(conversation);
  const next = f(old);
  if (next !== old) set({ ...st, conversations: { ...st.conversations, [conversation]: next } });
}

/** A live card, unless one as new or newer is here. */
export function applyView(conversation: string, v: AssistantView | undefined) {
  if (deletedConversationID(conversation)) return;
  if (!conversation || !v?.id || typeof v.version !== "number") return;
  if (v.conversation && v.conversation !== conversation) return;
  patchConversation(conversation, (c) => {
    const old = c.views[v.id];
    if (old && old.version >= v.version) return c;
    return { ...c, views: { ...c.views, [v.id]: v } };
  });
}

/** An answer, unless the one here has a later revision; the draft's submit for it is over. */
export function applyAction(conversation: string, a: ViewAction | undefined) {
  if (deletedConversationID(conversation)) return;
  if (!conversation || !a?.key || !a.client_msg_id) return;
  patchConversation(conversation, (c) => {
    const old = c.actions[a.key];
    if (old && (old.revision ?? 0) >= (a.revision ?? 0) && old.client_msg_id === a.client_msg_id) return c;
    // one answer per card: a record of a different submit only replaces it when it is the later revision
    if (old && old.client_msg_id !== a.client_msg_id && (old.revision ?? 0) >= (a.revision ?? 0)) return c;
    return { ...c, actions: { ...c.actions, [a.key]: a } };
  });
  const k = draftKey(conversation, a.key);
  const d = st.drafts[k];
  if (d?.sending) set({ ...st, drafts: { ...st.drafts, [k]: { ...d, sending: undefined, error: undefined, elsewhere: d.sending.clientMsgId !== a.client_msg_id } } });
}

export function applyBlock(conversation: string, key: string | undefined) {
  if (deletedConversationID(conversation)) return;
  if (!conversation || !key) return;
  patchConversation(conversation, (c) => (c.blocks[key] ? c : { ...c, blocks: { ...c.blocks, [key]: true } }));
}

/** The feed's snapshot: every live card, answer and registered block of the conversation (nothing is taken away). */
export function applySnapshot(conversation: string, res: { views?: Record<string, AssistantView> | null; view_actions?: Record<string, ViewAction> | ViewAction[] | null; view_blocks?: string[] | null }) {
  if (deletedConversationID(conversation)) return;
  for (const v of Object.values(res.views ?? {})) applyView(conversation, v);
  // the hub sends the answers keyed by card (docs/protocol/protocol.md); Object.values also takes a list
  for (const a of Object.values(res.view_actions ?? {})) applyAction(conversation, a);
  for (const k of res.view_blocks ?? []) applyBlock(conversation, k);
}

// The events (docs/assistant/assistant-views.md): each names its conversation.
onHubEvent("assistant_view", (m) => applyView(String(m.conversation ?? ""), m.view as AssistantView | undefined));
onHubEvent("assistant_view_action", (m) => applyAction(String(m.conversation ?? ""), m.action as ViewAction | undefined));
onHubEvent("assistant_view_block", (m) => applyBlock(String(m.conversation ?? ""), typeof m.key === "string" ? m.key : undefined));
onHubEvent("assistant_feed", (m) => {
  const it = m.item as { conversation?: string; kind?: string; body?: { view?: AssistantView } } | undefined;
  if (it?.kind === "view" && it.conversation) applyView(it.conversation, it.body?.view);
});
onHubEvent("hello", (m) => {
  const self = (m.mesh as { hubs?: { hub_id?: string; self?: boolean }[] } | undefined)?.hubs?.find((h) => h.self)?.hub_id;
  const id = typeof m.hub_id === "string" && m.hub_id ? m.hub_id : self;
  if (id) setHub(id);
});

// ── drafts ──

export function getDraft(conversation: string, key: CardKey): CardDraft | undefined {
  return st.drafts[draftKey(conversation, key)];
}

export function updateDraft(conversation: string, key: CardKey, f: (d: CardDraft) => CardDraft) {
  const k = draftKey(conversation, key);
  set({ ...st, drafts: { ...st.drafts, [k]: f(st.drafts[k] ?? { values: {}, stack: [] }) } });
}

// ── submitting ──

/** What POST …/views/{key}/actions takes: the generated ViewActionInput, with expected_version always sent (the schema
 *  only requires it with confirm; the web names the version it showed on every submit). */
export type ViewActionRequest = ViewActionInput & { expected_version: number };

/** A version the hub takes: a safe positive integer (ViewActionInput.expected_version's minimum 1). */
export const isCardVersion = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 1;

export type SubmitOutcome = "sent" | "answered_elsewhere" | "changed" | "failed" | "busy";

/** The path of a card's actions: the key escaped once, as one segment. */
export const actionsPath = (conversation: string, key: CardKey) => `/assistant/conversations/${enc(conversation)}/views/${enc(key)}/actions`;

/**
 * Submits a reply action at the version the user saw. The same request again ("Retry" after a failure) keeps its
 * client_msg_id, so the hub answers with the record it has instead of a second message.
 */
export async function submitAction(conversation: string, key: CardKey, req: Omit<Sending, "clientMsgId" | "inFlight">): Promise<SubmitOutcome> {
  const d = getDraft(conversation, key);
  if (d?.sending?.inFlight) return "busy";
  if (conversationCards(conversation).actions[key]) return "answered_elsewhere";
  // never sent without the version shown: a card with none is not one the hub would take an answer for
  if (!isCardVersion(req.expectedVersion)) {
    updateDraft(conversation, key, (x) => ({ ...x, error: "invalid card version" }));
    return "failed";
  }
  const same = d?.sending && sameRequest(d.sending, req);
  const sending: Sending = { ...req, clientMsgId: same ? d!.sending!.clientMsgId : uuid(), inFlight: true };
  updateDraft(conversation, key, (x) => ({ ...x, sending, error: undefined, elsewhere: false }));
  const json: ViewActionRequest = {
    action: req.action,
    ...(req.page ? { page: req.page } : {}),
    ...(Object.keys(req.values).length ? { values: req.values } : {}),
    client_msg_id: sending.clientMsgId,
    expected_version: req.expectedVersion,
    ...(req.confirmed ? { confirmed: true } : {}),
  };
  try {
    const res = await api<{ action?: ViewAction }>(actionsPath(conversation, key), { method: "POST", json });
    if (res.action) applyAction(conversation, res.action);
    updateDraft(conversation, key, (x) => ({ ...x, sending: undefined, changed: false }));
    return "sent";
  } catch (e) {
    return failed(conversation, key, e);
  }
}

function sameRequest(a: Omit<Sending, "clientMsgId" | "inFlight">, b: Omit<Sending, "clientMsgId" | "inFlight">) {
  return a.action === b.action && a.page === b.page && a.expectedVersion === b.expectedVersion && a.confirmed === b.confirmed && JSON.stringify(sorted(a.values)) === JSON.stringify(sorted(b.values));
}

const sorted = (v: Record<string, string>) => Object.fromEntries(Object.entries(v).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));

function failed(conversation: string, key: CardKey, e: unknown): SubmitOutcome {
  if (e instanceof ApiError && e.status === 409 && e.code === "already_answered") {
    applyAction(conversation, e.body.action as ViewAction | undefined);
    updateDraft(conversation, key, (x) => ({ ...x, sending: undefined, elsewhere: true }));
    return "answered_elsewhere";
  }
  if (e instanceof ApiError && e.status === 409 && e.code === "view_changed") {
    const body = e.body as Partial<ViewChangedError>;
    // the newer live card, when the hub sent it (a text block's is its key at version 1: nothing to redraw)
    const latest = body.view;
    if (latest && latest.id === key && isCardVersion(latest.version)) applyView(conversation, latest);
    // the draft stays; the next submit asks again at the version shown then, with a new client_msg_id
    updateDraft(conversation, key, (x) => ({ ...x, sending: undefined, changed: true }));
    return "changed";
  }
  const message = e instanceof Error ? e.message : String(e);
  // a request the hub turned down for what it says (400, 403, 404) is not one to retry as it is
  const retry = !(e instanceof ApiError) || e.status >= 500 || e.status === 0;
  updateDraft(conversation, key, (x) => ({ ...x, error: message, sending: retry && x.sending ? { ...x.sending, inFlight: false } : undefined }));
  return "failed";
}

/** Clears everything (tests). */
export function resetCards(hub = "") {
  set({ hub, conversations: {}, drafts: {} });
}

onConversationDeleted((_mid, sid) => {
  const conversations = { ...st.conversations };
  delete conversations[sid];
  const drafts = Object.fromEntries(Object.entries(st.drafts).filter(([key]) => key.split("\u0000")[1] !== sid));
  set({ ...st, conversations, drafts });
});
