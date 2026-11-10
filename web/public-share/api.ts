import type { Execution, PublicMessage, PublicPage } from "../src/sharing/types";
export class PublicReadError extends Error {
  constructor(readonly status: number, readonly code: string, readonly retryAfter = 0) { super("public_read_failed"); }
}
export function retryAfter(value: string | null, now = Date.now()): number {
  if (!value) return 0;
  const seconds = /^\d+$/.test(value) ? Number(value) * 1000 : Date.parse(value) - now;
  return Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
}
const record = (x: unknown): Record<string, unknown> => {
  if (!x || typeof x !== "object" || Array.isArray(x)) throw new PublicReadError(0, "invalid_response");
  return x as Record<string, unknown>;
};
const text = (x: unknown, max = 32768): string => {
  if (typeof x !== "string" || new TextEncoder().encode(x).length > max) throw new PublicReadError(0, "invalid_response");
  return x;
};
const integer = (x: unknown): number => {
  if (!Number.isSafeInteger(x) || Number(x) < 1) throw new PublicReadError(0, "invalid_response");
  return Number(x);
};
function execution(input: unknown): Execution {
  const e = record(input);
  if (e.kind === "text") return { kind: "text", text: text(e.text) };
  if (e.kind === "agent") return { kind: "agent", tool_id: text(e.tool_id, 256), text: text(e.text) };
  if (e.kind === "tool" && Array.isArray(e.results) && e.results.length <= 100) return {
    kind: "tool", tool_id: text(e.tool_id, 256), name: text(e.name, 256), arguments: text(e.arguments),
    results: e.results.map((r) => ({ text: text(record(r).text) })),
  };
  throw new PublicReadError(0, "invalid_response");
}
export function publicPage(input: unknown): PublicPage {
  const p = record(input);
  if (!Array.isArray(p.messages) || p.messages.length > 100 || !["frozen", "live", "paused"].includes(String(p.publication_status))) throw new PublicReadError(0, "invalid_response");
  const expires = text(p.expires_at, 64);
  if (!Number.isFinite(Date.parse(expires))) throw new PublicReadError(0, "invalid_response");
  const seen = new Set<string>();
  const messages: PublicMessage[] = p.messages.map((input) => {
    const m = record(input); const id = text(m.message_id, 256);
    if (!id || seen.has(id) || (m.role !== "user" && m.role !== "assistant")) throw new PublicReadError(0, "invalid_response");
    seen.add(id);
    if (m.execution !== undefined && (!Array.isArray(m.execution) || m.execution.length > 1000)) throw new PublicReadError(0, "invalid_response");
    return { message_id: id, role: m.role, text: text(m.text), ...(Array.isArray(m.execution) ? { execution: m.execution.map(execution) } : {}) };
  });
  return {
    revision: integer(p.revision), content_version: integer(p.content_version), expires_at: expires,
    publication_status: p.publication_status as PublicPage["publication_status"], poll_after_seconds: integer(p.poll_after_seconds),
    messages, next_cursor: p.next_cursor === null ? null : text(p.next_cursor, 256),
  };
}
export async function readPublic(token: string, cursor: string | undefined, signal: AbortSignal): Promise<PublicPage> {
  const query = new URLSearchParams({ limit: "100", ...(cursor ? { cursor } : {}) });
  const res = await fetch(`/api/public/agent-shares/${token}?${query}`, {
    credentials: "omit", cache: "no-store", redirect: "error", referrerPolicy: "no-referrer", signal,
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    // Do not read or retain arbitrary error bodies or URLs, even for malformed capabilities.
    await res.body?.cancel().catch(() => {});
    throw new PublicReadError(res.status, res.status === 409 ? "share_changed" : "unavailable", retryAfter(res.headers.get("Retry-After")));
  }
  if (!res.headers.get("Content-Type")?.includes("application/json")) throw new PublicReadError(0, "invalid_response");
  const reader = res.body?.getReader(); if (!reader) throw new PublicReadError(0, "invalid_response");
  const parts: Uint8Array[] = []; let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read(); if (done) break;
      size += value.length; if (size > 512 * 1024) throw new PublicReadError(0, "response_limit");
      parts.push(value);
    }
  } finally { await reader.cancel().catch(() => {}); }
  const bytes = new Uint8Array(size); let at = 0;
  for (const part of parts) { bytes.set(part, at); at += part.length; }
  try { return publicPage(JSON.parse(new TextDecoder().decode(bytes))); }
  catch (e) { if (e instanceof PublicReadError) throw e; throw new PublicReadError(0, "invalid_response"); }
}
