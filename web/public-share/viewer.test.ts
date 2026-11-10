import { afterEach, describe, expect, it, vi } from "vitest";
import { PublicReadError, publicPage, readPublic, retryAfter } from "./api";
import { Viewer } from "./viewer";
import type { PublicPage } from "../src/sharing/types";
const page = (patch: Partial<PublicPage> = {}): PublicPage => ({ revision: 1, content_version: 1, expires_at: new Date(Date.now() + 600000).toISOString(), publication_status: "frozen", poll_after_seconds: 30, messages: [{ message_id: "public-1", role: "assistant", text: "Synthetic result" }], next_cursor: null, ...patch });
const deferred = <T>() => { let resolve!: (x: T) => void; let reject!: (e: unknown) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b; }); return { resolve, reject, promise }; };
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });
describe("public sharing transport", () => {
  it("uses a credential-free bounded request and projects only allowed content", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>(async () => new Response(JSON.stringify(page()), { headers: { "Content-Type": "application/json" } })); vi.stubGlobal("fetch", fetch);
    const p = await readPublic("A".repeat(64), undefined, new AbortController().signal);
    expect(p.messages).toHaveLength(1);
    expect(fetch.mock.calls[0][1]).toMatchObject({ credentials: "omit", cache: "no-store", redirect: "error", referrerPolicy: "no-referrer" });
    const input = { ...page(), token: "not-public", machine: "not-public", messages: [{ ...page().messages[0], attachment: { url: "https://example.test/image" }, execution: [{ kind: "tool", name: "Synthetic tool", tool_id: "work-1", arguments: "safe", results: [{ text: "safe", child_payload: "hidden" }], raw: "hidden" }] }] };
    expect(JSON.stringify(publicPage(input))).not.toMatch(/not-public|image|child_payload|hidden/);
    expect(() => publicPage({ ...page(), messages: [{ ...page().messages[0], text: "x".repeat(32769) }] })).toThrow();
  });
  it("never consumes arbitrary errors and honors numeric/date Retry-After", async () => {
    const response = new Response("private arbitrary body", { status: 429, headers: { "Retry-After": "90" } }); const read = vi.spyOn(response, "text"); vi.stubGlobal("fetch", vi.fn(async () => response));
    await expect(readPublic("A".repeat(64), undefined, new AbortController().signal)).rejects.toMatchObject({ status: 429, retryAfter: 90000 });
    expect(read).not.toHaveBeenCalled(); expect(retryAfter(new Date(120000).toUTCString(), 60000)).toBe(60000);
  });
});
describe("public viewer lifetime and race fences", () => {
  it("serializes polling and fences pre-hide responses until resume revalidation", async () => {
    vi.useFakeTimers(); const first = deferred<PublicPage>(); const second = deferred<PublicPage>(); const read = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); viewer.retry(); expect(read).toHaveBeenCalledTimes(1);
    viewer.visibility(true); viewer.visibility(false); expect(viewer.state.page).toBeNull(); expect(read).toHaveBeenCalledTimes(1);
    first.resolve(page()); await vi.advanceTimersByTimeAsync(0); expect(viewer.state.page).toBeNull(); expect(read).toHaveBeenCalledTimes(2);
    second.resolve(page()); await vi.advanceTimersByTimeAsync(0); expect(viewer.state.phase).toBe("ready"); viewer.dispose();
  });
  it("clears on 404 and local expiry, ignoring later responses", async () => {
    vi.useFakeTimers(); const late = deferred<PublicPage>(); const read = vi.fn().mockResolvedValueOnce(page({ expires_at: new Date(Date.now() + 1000).toISOString() })).mockReturnValueOnce(late.promise);
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); await vi.advanceTimersByTimeAsync(0); viewer.retry(); await vi.advanceTimersByTimeAsync(1000); expect(viewer.state).toMatchObject({ phase: "unavailable", page: null });
    late.resolve(page()); await vi.advanceTimersByTimeAsync(0); expect(viewer.state.page).toBeNull();
    const notFound = new Viewer("A".repeat(64), () => {}, vi.fn().mockRejectedValue(new PublicReadError(404, "unavailable"))); notFound.start(); await vi.advanceTimersByTimeAsync(0); expect(notFound.state.phase).toBe("unavailable");
  });
  it("keeps a 30-day link alive past the platform timeout ceiling and expires at its actual deadline", async () => {
    vi.useFakeTimers(); const expires_at = new Date(Date.now() + 30 * 86400000).toISOString();
    const read = vi.fn().mockResolvedValue(page({ expires_at, poll_after_seconds: 30 * 86400 }));
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(2_147_483_647); expect(viewer.state.phase).toBe("ready");
    await vi.advanceTimersByTimeAsync(30 * 86400000 - 2_147_483_647); expect(viewer.state.phase).toBe("unavailable"); viewer.dispose();
  });
  it("pauses hidden polls and uses live/static minimums and Retry-After", async () => {
    vi.useFakeTimers(); const read = vi.fn().mockResolvedValueOnce(page({ publication_status: "live", poll_after_seconds: 1 })).mockRejectedValueOnce(new PublicReadError(429, "unavailable", 90000)).mockResolvedValue(page());
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); await vi.advanceTimersByTimeAsync(14999); expect(read).toHaveBeenCalledTimes(1); await vi.advanceTimersByTimeAsync(1); expect(read).toHaveBeenCalledTimes(2);
    viewer.retry(); await vi.advanceTimersByTimeAsync(89999); expect(read).toHaveBeenCalledTimes(2); await vi.advanceTimersByTimeAsync(1); expect(read).toHaveBeenCalledTimes(3);
    viewer.visibility(true); await vi.advanceTimersByTimeAsync(120000); expect(read).toHaveBeenCalledTimes(3); viewer.visibility(false); await vi.advanceTimersByTimeAsync(0); expect(read).toHaveBeenCalledTimes(4); viewer.dispose();
  });
  it("rejects a mixed-version page and revalidates without the stale cursor", async () => {
    vi.useFakeTimers(); const read = vi.fn().mockResolvedValueOnce(page({ next_cursor: "cursor-1" })).mockResolvedValueOnce(page({ revision: 2 })).mockResolvedValue(page({ revision: 2, messages: [{ message_id: "new-1", role: "assistant", text: "New policy" }] }));
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); await vi.advanceTimersByTimeAsync(0); viewer.more(); await vi.advanceTimersByTimeAsync(0);
    expect(read.mock.calls[1][1]).toBe("cursor-1"); expect(read.mock.calls[2][1]).toBeUndefined(); expect(viewer.state.page?.messages[0].message_id).toBe("new-1"); viewer.dispose();
  });
  it("reloads the whole share without its cursor after 409 share_changed", async () => {
    vi.useFakeTimers(); const read = vi.fn().mockResolvedValueOnce(page({ next_cursor: "cursor-1" })).mockRejectedValueOnce(new PublicReadError(409, "share_changed")).mockResolvedValue(page({ revision: 3, content_version: 2, messages: [{ message_id: "fresh-1", role: "user", text: "Reloaded" }] }));
    const states: string[] = []; const viewer = new Viewer("A".repeat(64), (s) => states.push(s.phase), read); viewer.start(); await vi.advanceTimersByTimeAsync(0);
    viewer.more(); await vi.advanceTimersByTimeAsync(0); expect(viewer.state.page).toBeNull(); expect(states.at(-1)).toBe("loading");
    await vi.advanceTimersByTimeAsync(1000); expect(read.mock.calls[1][1]).toBe("cursor-1"); expect(read.mock.calls[2][1]).toBeUndefined();
    expect(viewer.state).toMatchObject({ phase: "ready", page: { revision: 3, messages: [{ message_id: "fresh-1" }] } }); viewer.dispose();
  });
  it("hides content while a failed check backs off, then shows only verified content", async () => {
    vi.useFakeTimers(); const read = vi.fn().mockResolvedValueOnce(page({ poll_after_seconds: 30 })).mockRejectedValueOnce(new PublicReadError(503, "unavailable")).mockResolvedValue(page());
    const viewer = new Viewer("A".repeat(64), () => {}, read); viewer.start(); await vi.advanceTimersByTimeAsync(30000);
    expect(viewer.state).toMatchObject({ phase: "error", page: null }); await vi.advanceTimersByTimeAsync(4999); expect(read).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1); expect(read).toHaveBeenCalledTimes(3); expect(viewer.state.phase).toBe("ready"); viewer.dispose();
  });
});
