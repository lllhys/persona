import { deleteConversation } from "./conversation-delete-action";
import { afterEach, expect, it, vi } from "vitest";
import { applyCompleteSessions, getSession, getState, markConversationDeleted, refreshAssistant, reset, upsertSession } from "./store";
import { conversationDeleteAllowed } from "./conversation-delete";
import type { Session } from "./types";

const row = (patch: Partial<Session> = {}): Session => ({ machine_id: "hub", session_id: "chat", status: "idle", title: "Trip", cwd: "", assistant: true, ...patch });
afterEach(() => { reset(); vi.unstubAllGlobals(); });

it("only offers deletion for ordinary inactive assistant conversations on a capable hub", () => {
  const opts = { supported: true, main: false, builtIn: false, pending: false };
  expect(conversationDeleteAllowed(row(), opts)).toBe(true);
  for (const patch of [{ supported: false }, { main: true }, { builtIn: true }, { pending: true }]) expect(conversationDeleteAllowed(row(), { ...opts, ...patch })).toBe(false);
  for (const patch of [{ status: "working" }, { status: "blocked" }, { starting: true }, { waiting: true }, { background_working: true }, { deleted: true }, { assistant: false }]) expect(conversationDeleteAllowed(row(patch), opts)).toBe(false);
});

it("removes cached rows and never lets a late complete read or push revive a deleted chat", () => {
  upsertSession(row());
  upsertSession(row({ machine_id: "other" }));
  markConversationDeleted("hub", "chat");
  expect(getState().sessions.hub).toEqual([]);
  expect(getSession("hub", "chat")?.deleted).toBe(true);
  expect(getSession("other", "chat")?.deleted).not.toBe(true);
  upsertSession(row());
  applyCompleteSessions([row()]);
  expect(getState().sessions.hub).toEqual([]);
  expect(getSession("hub", "chat")?.deleted).toBe(true);
  reset();
  upsertSession(row());
  expect(getSession("hub", "chat")?.deleted).not.toBe(true);
});

it("filters an assistant read which started before deletion", async () => {
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem() {}, removeItem() {} });
  let release!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((r) => { release = r; })));
  const reading = refreshAssistant();
  markConversationDeleted("hub", "chat");
  release(new Response(JSON.stringify({ machine_id: "hub", main: "main", conversations: [row()] })));
  await reading;
  expect(getState().assistant?.conversations).toEqual([]);
});

it("does not apply a deletion response from before an account reset", async () => {
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem() {}, removeItem() {} });
  let release!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((r) => { release = r; })));
  const deleting = deleteConversation(row());
  reset();
  upsertSession(row());
  release(new Response('{"ok":true,"deleted":true}'));
  await deleting;
  expect(getSession("hub", "chat")?.deleted).not.toBe(true);
});

it("recognizes an explicit tombstone DTO and denies all session capabilities", () => {
  upsertSession(row());
  applyCompleteSessions([row({ deleted: true, status: "offline", title: "Conversation deleted" })]);
  expect(getState().sessions.hub).toEqual([]);
  expect(getSession("hub", "chat")).toMatchObject({ deleted: true, title: "", capabilities: {} });
});

it("uses the row's manager and machine and keeps linked tasks when another device deleted it", async () => {
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem() {}, removeItem() {} });
  const fetcher = vi.fn(async (_url: string, _init?: RequestInit) => Response.json({ error: "Deleted", code: "conversation_deleted" }, { status: 410 }));
  vi.stubGlobal("fetch", fetcher);
  const tasks = getState().todos;
  await deleteConversation(row({ manager: "manager-other" }));
  expect(new Headers(fetcher.mock.calls[0][1]?.headers).get("X-Persona-Manager")).toBe("manager-other");
  expect(getSession("hub", "chat")?.deleted).toBe(true);
  expect(getState().todos).toBe(tasks);
});
