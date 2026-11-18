import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { appendFile, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createUpdateTestProcessRegistry, finishUpdateTestCleanup, runUpdateTestOperation, UnsafeUpdateTestFinalization, waitForUpdateTest } from "./update-test-lifecycle.mjs";
import { finishUpdateTest } from "./update-test-receipts.mjs";

const tick = () => new Promise(resolve => setTimeout(resolve, 20));

test("an aborted sleep rejects immediately and leaves no armed timer", async () => {
  const controller = new AbortController();
  const pending = waitForUpdateTest(60_000, controller.signal);
  controller.abort(new Error("deadline"));
  await assert.rejects(pending, /deadline/);
  assert.equal(process.getActiveResourcesInfo().filter(resource => resource === "Timeout").length, 0, JSON.stringify(process.getActiveResourcesInfo()));
});

test("operation deadline aborts main and waits for its late continuation before cleanup/report", async t => {
  const directory = await mkdtemp(join(tmpdir(), "persona-update-test-lifecycle-"));
  t.after(async () => { await rm(directory, { recursive: true, force: true }); });
  const trace = join(directory, "trace.log");
  const calls = [];
  let releaseMain;
  const mainContinued = new Promise(resolve => { releaseMain = resolve; });
  let mainResumed;
  const resumed = new Promise(resolve => { mainResumed = resolve; });
  let reported = false;
  const operation = runUpdateTestOperation(async signal => {
    signal.addEventListener("abort", releaseMain, { once: true });
    await mainContinued;
    calls.push("main:resumed");
    await appendFile(trace, "main:resumed\n");
    mainResumed();
    // Deliberately keep the losing continuation alive after its late write. The deadline must not return yet.
    await tick();
    calls.push("main:stopped");
  }, {
    timeoutMilliseconds: 20,
    stopGraceMilliseconds: 500,
    timeoutMessage: "injected deadline",
  });
  const observed = operation.catch(error => error);
  await resumed;
  assert.deepEqual(calls, ["main:resumed"], "late main work has started but the operation must still await its actual stop");
  const error = await observed;
  assert.equal(error.message, "injected deadline");
  assert.deepEqual(calls, ["main:resumed", "main:stopped"]);
  assert.equal(await readFile(trace, "utf8"), "main:resumed\n");
  // Only after the real continuation stopped may the ordinary receipt path remove it and report.
  await rm(trace);
  reported = true;
  calls.push("cleanup", "report");
  await tick();
  assert.equal(reported, true);
  assert.deepEqual(calls, ["main:resumed", "main:stopped", "cleanup", "report"]);
  await assert.rejects(readFile(trace, "utf8"), { code: "ENOENT" });
});

test("a main continuation that ignores cancellation blocks unsafe cleanup and report", async () => {
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const calls = [];
  const operation = runUpdateTestOperation(async () => {
    await pending;
    calls.push("late-main");
  }, { timeoutMilliseconds: 10, stopGraceMilliseconds: 20, abortGraceMilliseconds: 20, timeoutMessage: "deadline" });
  await assert.rejects(operation, error => {
    assert.ok(error instanceof UnsafeUpdateTestFinalization);
    assert.equal(error.safeToFinalize, false);
    assert.match(error.message, /cleanup and result reporting were skipped/);
    return true;
  });
  assert.deepEqual(calls, []);
  release();
  await pending;
  assert.deepEqual(calls, ["late-main"]);
});

test("unsafe main rejection bypasses the real finishUpdateTest snapshot, cleanup and report", async t => {
  const directory = await mkdtemp(join(tmpdir(), "persona-update-test-unsafe-main-"));
  t.after(async () => { await rm(directory, { recursive: true, force: true }); });
  const trace = join(directory, "trace.log");
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  let mainFinished;
  const finished = new Promise(resolve => { mainFinished = resolve; });
  const calls = [];
  const verdict = finishUpdateTest({
    runMain: () => runUpdateTestOperation(async () => {
      await pending;
      await appendFile(trace, "late main\n");
      mainFinished();
    }, { timeoutMilliseconds: 10, stopGraceMilliseconds: 10, abortGraceMilliseconds: 10, timeoutMessage: "deadline" }),
    async readEvents() { calls.push("snapshot"); return []; },
    async cleanup() { calls.push("cleanup"); await rm(trace, { force: true }); },
    message: error => error.message,
    async report() { calls.push("report"); },
  });
  await assert.rejects(verdict, UnsafeUpdateTestFinalization);
  assert.deepEqual(calls, []);
  release();
  await finished;
  assert.equal(await readFile(trace, "utf8"), "late main\n", "the fixture retains its path instead of deleting under live main");
});

test("cleanup gets a fresh deadline after a long main and does not start already expired", async () => {
  await new Promise(resolve => setTimeout(resolve, 30));
  const failures = await finishUpdateTestCleanup(async signal => {
    await waitForUpdateTest(30, signal);
    return [];
  }, { timeoutMilliseconds: 50, settleGraceMilliseconds: 20 });
  assert.deepEqual(failures, []);
});

test("cleanup defaults support the production call and clear the deadline", async t => {
  const timers = [];
  const original = globalThis.setTimeout;
  globalThis.setTimeout = (...args) => {
    const timer = original(...args);
    timers.push(timer);
    return timer;
  };
  t.after(() => { globalThis.setTimeout = original; });
  let cleanupCalled = false;
  const failures = await finishUpdateTestCleanup(async signal => {
    cleanupCalled = true;
    assert.equal(signal.aborted, false);
    return [];
  });
  assert.equal(cleanupCalled, true);
  assert.deepEqual(failures, []);
  assert.equal(timers.length, 1);
  assert.equal(timers[0]._destroyed, true, "the three-minute default deadline must be cleared after cleanup");
});

test("cleanup timeout returns failure only after the cancelled operation actually stops", async () => {
  let cleaned = false;
  const failures = await finishUpdateTestCleanup(async signal => {
    try { await waitForUpdateTest(60_000, signal); }
    finally { cleaned = true; }
    return [];
  }, { timeoutMilliseconds: 10, settleGraceMilliseconds: 100 });
  assert.equal(cleaned, true);
  assert.match(failures, /cleanup timed out/);
});

test("an uncancellable cleanup survivor blocks the final report rather than racing it", async () => {
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const calls = [];
  const finishing = finishUpdateTest({
    async runMain() { return { install: "fixture" }; },
    async readEvents() { return []; },
    cleanup: () => finishUpdateTestCleanup(async () => {
      await pending;
      calls.push("late cleanup");
      return [];
    }, { timeoutMilliseconds: 10, settleGraceMilliseconds: 20 }),
    message: error => error.message,
    async report() { calls.push("report"); },
  });
  await assert.rejects(finishing, UnsafeUpdateTestFinalization);
  assert.deepEqual(calls, []);
  release();
  await pending;
  assert.deepEqual(calls, ["late cleanup"]);
});

test("a stop-triggered early main rejection preserves the original deadline and stop failure", async () => {
  let stops = 0;
  let rejectMain;
  await assert.rejects(runUpdateTestOperation(async () => {
    await new Promise((_, reject) => { rejectMain = reject; });
  }, {
    timeoutMilliseconds: 10,
    stopGraceMilliseconds: 10,
    abortGraceMilliseconds: 10,
    timeoutMessage: "original 70-minute deadline",
    onTimeout: () => { stops++; rejectMain(new Error("child stop failed")); },
    stopFailures: ["taskkill exited 1"],
  }), error => error.message === "original 70-minute deadline; taskkill exited 1");
  assert.equal(stops, 1);
  // The receipt composer's independent cleanup signal is not this main controller.
  const cleanup = await finishUpdateTestCleanup(async signal => {
    assert.equal(signal.aborted, false);
    await waitForUpdateTest(10, signal);
    return [];
  }, { timeoutMilliseconds: 100 });
  assert.deepEqual(cleanup, []);
});

test("stop-failure diagnostics never wait for a stuck child or taskkill before abort", async () => {
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  let aborted = false;
  const operation = runUpdateTestOperation(async signal => {
    signal.addEventListener("abort", () => { aborted = true; }, { once: true });
    await pending;
  }, {
    timeoutMilliseconds: 10,
    stopGraceMilliseconds: 10,
    abortGraceMilliseconds: 10,
    timeoutMessage: "deadline",
    stopFailures: ["taskkill did not close"],
  });
  await assert.rejects(operation, error => {
    assert.ok(error instanceof UnsafeUpdateTestFinalization);
    assert.match(error.message, /deadline; taskkill did not close/);
    return true;
  });
  assert.equal(aborted, true);
  release();
});

test("early operation failures clear their deadline without waiting for the stop grace", async t => {
  let timer;
  const original = globalThis.setTimeout;
  globalThis.setTimeout = (...args) => { timer = original(...args); return timer; };
  t.after(() => { globalThis.setTimeout = original; });
  await assert.rejects(runUpdateTestOperation(async () => {
    await tick();
    throw new Error("early failure");
  }, { timeoutMilliseconds: 60_000, stopGraceMilliseconds: 60_000, timeoutMessage: "late deadline" }), /early failure/);
  assert.equal(timer._destroyed, true, "the long deadline timer must be cleared before report");
});

test("a normal result clears the deadline and propagates the exact value", async () => {
  const value = await runUpdateTestOperation(async signal => {
    await waitForUpdateTest(10, signal);
    return { install: "fixture result" };
  }, { timeoutMilliseconds: 1000, timeoutMessage: "unexpected deadline" });
  assert.deepEqual(value, { install: "fixture result" });
});

test("stop is invoked at the deadline and again after the stop grace, preserving the original abort reason", async () => {
  let stops = 0;
  let observed;
  await assert.rejects(runUpdateTestOperation(async signal => {
    signal.addEventListener("abort", () => { observed = signal.reason; }, { once: true });
    await waitForUpdateTest(60_000, signal);
  }, { timeoutMilliseconds: 10, stopGraceMilliseconds: 10, abortGraceMilliseconds: 100, timeoutMessage: "deadline", onTimeout: () => { stops++; }, onAbort: () => { stops++; } }), /deadline/);
  assert.equal(stops, 2);
  assert.equal(observed.message, "deadline");
});

function childDouble(pid = 123) {
  const child = new EventEmitter();
  child.pid = pid;
  child.exitCode = null;
  child.signalCode = null;
  child.stdout = new EventEmitter();
  child.stderr = new EventEmitter();
  return child;
}

function processDouble() {
  const children = [];
  const stops = [];
  const registry = createUpdateTestProcessRegistry({
    spawn(command, args) {
      const child = childDouble();
      children.push({ child, command, args });
      return child;
    },
    stop(child, trackTermination) {
      stops.push(child);
      if (child.pid === 123) {
        const killer = trackTermination(childDouble(456));
        killer.emit("close", 0, null);
      }
    },
  });
  return { children, stops, registry };
}

const flush = () => new Promise(resolve => setImmediate(resolve));

test("command timeout stays pending through exit and rejects only after close", async () => {
  const { children, registry } = processDouble();
  const pending = registry.run("fixture-command", [], { seconds: 0.02 });
  const observed = pending.catch(error => error);
  await new Promise(resolve => setTimeout(resolve, 35));
  const [record] = registry.owned;
  assert.ok(record.stopRequested);
  record.child.exitCode = 0;
  record.child.emit("exit", 0, null);
  let settled = false;
  void observed.finally(() => { settled = true; });
  await flush();
  assert.equal(settled, false, "exit without close must not settle the command");
  record.child.stdout.emit("data", "owned output");
  record.child.emit("close", 0, null);
  assert.deepEqual([...registry.owned].map(record => record.termination), [true], "the target closed, but its taskkill is still owned");
  const killer = [...registry.owned][0];
  killer.child.emit("close", 0, null);
  const error = await observed;
  assert.match(error.message, /fixture-command timed out after 0.02s/);
  await flush();
  assert.equal(registry.owned.size, 0);
});

test("abort before a command prevents spawning; later abort waits for close", async () => {
  const { children, registry } = processDouble();
  const controller = new AbortController();
  controller.abort(new Error("stop before spawn"));
  await assert.rejects(registry.run("not-spawned", [], { seconds: 1, signal: controller.signal }), /stop before spawn/);
  assert.equal(children.length, 0);
  const second = new AbortController();
  const pending = registry.run("owned", [], { seconds: 60, signal: second.signal });
  const observed = pending.catch(error => error);
  assert.equal(children.length, 1);
  second.abort(new Error("stop running command"));
  await flush();
  assert.deepEqual([...registry.owned].map(record => record.termination), [false, true], "both target and taskkill are owned");
  children[0].child.exitCode = 0;
  children[0].child.signalCode = "SIGKILL";
  children[0].child.emit("close", null, "SIGKILL");
  await flush();
  assert.deepEqual([...registry.owned].map(record => record.termination), [true], "only taskkill may remain after target close");
  const killer = [...registry.owned].find(record => record.termination);
  killer.child.emit("close", 0, null);
  assert.match((await observed).message, /stop running command/);
  await flush();
  assert.equal(registry.owned.size, 0);
});

test("cancelled commands retain diagnostic text without forwarding late stdout", async () => {
  const { children, registry } = processDouble();
  const controller = new AbortController();
  const forwarded = [];
  const pending = registry.run("cancel-output", [], { seconds: 60, signal: controller.signal, onOutput: data => forwarded.push(String(data)) });
  const observed = pending.catch(error => error);
  controller.abort(new Error("cancel output"));
  children[0].child.stdout.emit("data", "late stdout");
  children[0].child.stderr.emit("data", "late stderr");
  assert.deepEqual(forwarded, []);
  children[0].child.emit("close", null, "SIGKILL");
  const killer = [...registry.owned].find(record => record.termination);
  killer.child.emit("close", 0, null);
  assert.match((await observed).message, /cancel output/);
});

test("spawn error retains ownership until close and preserves the original error", async () => {
  const { children, registry } = processDouble();
  const pending = registry.run("missing-fixture-command", [], { seconds: 60 });
  const child = children[0].child;
  child.pid = undefined;
  child.emit("error", Object.assign(new Error("spawn fixture failed"), { code: "ENOENT" }));
  await flush();
  assert.equal(registry.owned.size, 1);
  child.emit("close", -2, null);
  await assert.rejects(pending, /spawn fixture failed/);
  assert.equal(registry.owned.size, 0);
});

test("exit before close does not skip termination or settle early", async () => {
  const { children, stops, registry } = processDouble();
  const pending = registry.run("exit-before-close", [], { seconds: 60 });
  const child = children[0].child;
  child.exitCode = 0;
  child.emit("exit", 0, null);
  const result = registry.drain(20);
  assert.equal(stops.length, 1, "exit is not close; request the registered stop once");
  assert.equal((await result).closed, false);
  child.emit("close", 0, null);
  assert.deepEqual(await pending, { code: 0, signal: null, text: "" });
});

test("drain waits for both target close and taskkill close before permitting removal", async () => {
  const children = [];
  const registry = createUpdateTestProcessRegistry({
    spawn: () => { const child = childDouble(children.length + 1); children.push(child); return child; },
    stop(child, trackTermination) {
      const killer = trackTermination(childDouble(99));
      setTimeout(() => {
        killer.child.emit("close", 0, null);
        child.exitCode = 0;
        child.signalCode = "SIGKILL";
        child.emit("close", null, "SIGKILL");
      }, 30);
    },
  });
  const record = registry.track(childDouble(1));
  children[0] = record.child;
  let removed = false;
  const draining = registry.drain(500).then(result => { removed = true; return result; });
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(removed, false);
  const result = await draining;
  assert.deepEqual(result, { closed: true, failures: [] }, JSON.stringify([...registry.owned].map(record => ({ termination: record.termination, closed: record.closed, error: record.stopError?.message }))));
  assert.equal(registry.owned.size, 0);
});

test("drain waits when the target closes before its synchronously registered termination helper", async () => {
  const child = childDouble();
  let killer;
  const registry = createUpdateTestProcessRegistry({
    spawn: () => child,
    stop(target, trackTermination) {
      killer = trackTermination(childDouble(456));
      target.exitCode = 0;
      target.emit("close", 0, null);
    },
  });
  registry.track(child);
  let settled = false;
  const pending = registry.drain(100).then(result => { settled = true; return result; });
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(settled, false, "drain must not abandon the remaining taskkill");
  assert.equal(killer.closed, false);
  killer.child.emit("close", 0, null);
  assert.deepEqual(await pending, { closed: true, failures: [] });
});

test("a production-style nonzero taskkill result remains a failed drain after both closes", async () => {
  const child = childDouble();
  let killer;
  const registry = createUpdateTestProcessRegistry({
    spawn: () => child,
    stop(target, trackTermination) {
      killer = trackTermination(childDouble(456));
      killer.terminationError = killer.close.then(exit => {
        if (exit.code !== 0) throw new Error(`taskkill exited ${exit.code}`);
      }).catch(error => error);
      target.emit("close", null, "SIGKILL");
      killer.child.emit("close", 1, null);
    },
  });
  registry.track(child);
  const result = await registry.drain(100);
  assert.equal(result.closed, false);
  assert.deepEqual(result.failures, ["termination failed: taskkill exited 1"]);
});

test("a remaining termination helper is awaited without spawning another termination helper", async () => {
  let stops = 0;
  const registry = createUpdateTestProcessRegistry({ spawn: () => childDouble(), stop() { stops++; } });
  const target = registry.track(childDouble());
  const killer = registry.track(childDouble(456), { termination: true });
  target.child.emit("close", 0, null);
  await target.close;
  let settled = false;
  const pending = registry.drain(100).then(result => { settled = true; return result; });
  await flush();
  assert.equal(settled, false);
  killer.child.emit("close", 0, null);
  assert.deepEqual(await pending, { closed: true, failures: [] });
  assert.equal(stops, 0);
});

test("a child that never closes produces a bounded drain failure, not permission to remove", async () => {
  const registry = createUpdateTestProcessRegistry({ spawn: () => childDouble(), stop: () => {} });
  registry.track(childDouble());
  const result = await registry.drain(20);
  assert.equal(result.closed, false);
  assert.match(result.failures.join("\n"), /did not close/);
});

test("a stop error remains a cleanup failure after the child closes", async () => {
  const child = childDouble();
  const registry = createUpdateTestProcessRegistry({
    spawn: () => child,
    stop() { child.emit("close", null, "SIGKILL"); throw new Error("termination confirmation failed"); },
  });
  registry.track(child);
  const result = await registry.drain(50);
  assert.equal(result.closed, false);
  assert.deepEqual(result.failures, ["stop failed: termination confirmation failed"]);
  assert.equal(registry.owned.size, 0);
});

test("a failed taskkill is observed and cannot be mistaken for successful close", async () => {
  const child = childDouble();
  const registry = createUpdateTestProcessRegistry({
    spawn: () => child,
    stop(target, trackTermination) {
      const killer = trackTermination(childDouble(456));
      killer.child.emit("close", 1, null);
      throw new Error("taskkill exited 1");
    },
  });
  registry.track(child);
  const result = await registry.drain(50);
  assert.equal(result.closed, false);
  assert.match(result.failures.join("\n"), /did not close/);
});
