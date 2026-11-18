// What is using a computer (#8037): how a failed diagnosis reads (an older node, an older hub, a device that isn't
// fully trusted, the computer away), and the capability index's tools read: three at a time, kept 10 minutes.
import { afterEach, describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { diagnosisFailure, requestTools, resetTools, retryable, toolsOf, type Diagnosis } from "./machine-diagnosis";

const err = (status: number, body: Record<string, unknown> = {}, json = true) => new ApiError(status, String(body.error ?? "failed"), body, json);

describe("diagnosisFailure", () => {
  it("tells an older node by its 409 code", () => {
    const f = diagnosisFailure(err(409, { error: "too old", code: "node_outdated" }));
    expect(f.kind).toBe("node_outdated");
    expect(f.text).toContain("更新这台电脑上的 Persona");
    expect(retryable(f)).toBe(false);
  });

  it("tells an older hub by a 404 without a code, or one that is not JSON", () => {
    expect(diagnosisFailure(err(404, { error: "not found" })).kind).toBe("hub_outdated");
    expect(diagnosisFailure(err(404, {}, false)).kind).toBe("hub_outdated");
    expect(diagnosisFailure(err(404, { error: "not found" })).text).toContain("更新 hub");
    // the manager's own 404 names a code: an unknown computer, not an old hub
    expect(diagnosisFailure(err(404, { error: "unknown machine", code: "not_found" })).kind).toBe("error");
  });

  it("says only a fully trusted device can see it on a 403", () => {
    expect(diagnosisFailure(err(403, { code: "forbidden" })).kind).toBe("forbidden");
  });

  it("says the computer is away or slow, and that those can be tried again", () => {
    const off = diagnosisFailure(err(503, { code: "machine_offline" }));
    expect(off.kind).toBe("offline");
    expect(retryable(off)).toBe(true);
    expect(diagnosisFailure(err(504, { code: "deadline" })).kind).toBe("deadline");
  });

  it("keeps the hub's own words for anything else", () => {
    const f = diagnosisFailure(err(400, { error: "unknown section x", code: "bad_request" }));
    expect(f).toEqual({ kind: "error", text: "unknown section x" });
  });
});

describe("the tools read", () => {
  afterEach(resetTools);
  const settle = () => new Promise((r) => setTimeout(r, 0));

  it("asks at most three computers at a time, and each once", async () => {
    let running = 0, most = 0;
    const asked: string[] = [];
    const answers: (() => void)[] = [];
    const fetcher = (mid: string) => {
      asked.push(mid);
      running++;
      most = Math.max(most, running);
      return new Promise<Diagnosis>((resolve) => answers.push(() => { running--; resolve({ machine_id: mid, at: 1, tools: [{ id: "git", version: "2.47" }] }); }));
    };
    for (const mid of ["a", "b", "c", "d", "e"]) requestTools(mid, fetcher);
    requestTools("a", fetcher);
    expect(asked).toEqual(["a", "b", "c"]);
    expect(toolsOf("a")).toEqual({ state: "loading" });
    while (answers.length) {
      answers.shift()!();
      await settle();
    }
    expect(asked).toEqual(["a", "b", "c", "d", "e"]);
    expect(most).toBe(3);
    expect(toolsOf("e")).toEqual({ state: "ok", tools: [{ id: "git", version: "2.47" }] });
  });

  it("keeps an answer 10 minutes, and an outdated node's too", async () => {
    let calls = 0;
    requestTools("old", () => { calls++; return Promise.reject(err(409, { code: "node_outdated" })); });
    await settle();
    expect(toolsOf("old")).toMatchObject({ state: "failed", failure: { kind: "node_outdated" } });
    requestTools("old", () => { calls++; return Promise.reject(err(409, { code: "node_outdated" })); });
    expect(calls).toBe(1);
    expect(toolsOf("old", Date.now() + 11 * 60_000)).toBeUndefined();
  });

  it("asks again soon after a failure that can be retried", async () => {
    requestTools("away", () => Promise.reject(err(503, { code: "machine_offline" })));
    await settle();
    expect(toolsOf("away")).toMatchObject({ state: "failed" });
    expect(toolsOf("away", Date.now() + 31_000)).toBeUndefined();
  });
});
