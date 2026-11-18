import assert from "node:assert/strict";
import test from "node:test";
import { HubWindowLifecycle } from "../src/hub-window-lifecycle.js";

test("setup close and Continue share one hub-window creation across the asynchronous state read", async () => {
  const lifecycle = new HubWindowLifecycle();
  let created = 0;
  let release: (() => void) | undefined;
  const reading = new Promise<void>(done => { release = done; });
  const show = () => lifecycle.show(async () => {
    created++;
    await reading;
    return { id: created };
  });
  const fromClosed = show();
  const fromContinue = show();
  assert.equal(created, 1);
  release!();
  const [first, second] = await Promise.all([fromClosed, fromContinue]);
  assert.equal(first, second);
  assert.equal(created, 1);
});

test("replacement starts a new generation while the previous creation is pending", async () => {
  const lifecycle = new HubWindowLifecycle();
  let rejectOld: ((error: Error) => void) | undefined;
  const old = lifecycle.show(() => new Promise((_, reject) => { rejectOld = reject; }));
  const generation = lifecycle.generation;
  lifecycle.replace();
  const replacement = lifecycle.show(async () => "new hub");
  assert.equal(await replacement, "new hub");
  rejectOld!(new Error("the old window was destroyed"));
  await assert.rejects(old, /destroyed/);
  assert.ok(lifecycle.generation > generation);
});

test("a failed hub-window creation clears its pending state and permits a retry", async () => {
  const lifecycle = new HubWindowLifecycle();
  let calls = 0;
  await assert.rejects(lifecycle.show(async () => { calls++; throw new Error("synthetic load failure"); }));
  await lifecycle.show(async () => { calls++; });
  assert.equal(calls, 2);
});
