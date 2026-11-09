import { describe, expect, it, vi } from "vitest";
import { intercepted, probeEdge, reloadForSignIn, RELOAD_GUARD_MS } from "./edge-auth";

const res = (status: number, type = "basic", ct = "application/json", redirected = false) => ({ status, type, redirected, headers: new Headers({ "Content-Type": ct }) }) as unknown as Response;
const store = () => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
};

describe("what a layer in front of the hub answers with", () => {
  it("recognises redirects, status 0, refusals and sign-in HTML", () => {
    expect(intercepted(res(0, "opaqueredirect"))).toBe(true);
    expect(intercepted(res(302))).toBe(true);
    expect(intercepted(res(200, "basic", "text/html; charset=utf-8"))).toBe(true);
    expect(intercepted(res(401, "basic", "text/html"))).toBe(true);
    expect(intercepted(res(200, "basic", "application/json", true))).toBe(true);
    expect(intercepted(res(511, "basic", "text/html"))).toBe(true);
  });
  it("leaves the hub's own answers alone", () => {
    expect(intercepted(res(200))).toBe(false);
    expect(intercepted(res(401))).toBe(false); // the hub's JSON 401 is the token's business
    expect(intercepted(res(500, "basic", "text/plain"))).toBe(false);
  });
});

describe("reloading for the sign-in", () => {
  it("reloads once, not again inside the guard window, and again after it", () => {
    const s = store();
    const reload = vi.fn();
    expect(reloadForSignIn(1_000_000, s, reload)).toBe(true);
    expect(reloadForSignIn(1_000_000 + 5_000, s, reload)).toBe(false);
    expect(reloadForSignIn(1_000_000 + RELOAD_GUARD_MS + 1, s, reload)).toBe(true);
    expect(reload).toHaveBeenCalledTimes(2);
  });
  it("never reloads when the guard cannot be kept", () => {
    const reload = vi.fn();
    const broken = { getItem: () => null, setItem: () => { throw new Error("full"); } };
    expect(reloadForSignIn(1, broken, reload)).toBe(false);
    expect(reload).not.toHaveBeenCalled();
  });
});

describe("probing after the socket failed", () => {
  it("does nothing when the network is down or the hub answers", async () => {
    expect(await probeEdge((async () => { throw new TypeError("offline"); }) as typeof fetch)).toBe(false);
    expect(await probeEdge((async () => res(200)) as typeof fetch)).toBe(false);
  });
  it("asks without following redirects", async () => {
    const f = vi.fn(async (_u: RequestInfo | URL, _i?: RequestInit) => res(200));
    await probeEdge(f as unknown as typeof fetch);
    expect(f.mock.calls[0][1]).toMatchObject({ redirect: "manual" });
  });
});
