// Cooperative cancellation and bounded process shutdown for the test-only desktop update harness.
// Aborting a wait is not completion: main and every owned child must actually settle before destructive cleanup.

export class UnsafeUpdateTestFinalization extends Error {
  constructor(message) {
    super(message);
    this.name = "UnsafeUpdateTestFinalization";
    this.safeToFinalize = false;
  }
}

export function abortReason(signal) {
  return signal?.reason instanceof Error ? signal.reason : new Error("Update-test operation aborted");
}

export function throwIfUpdateTestAborted(signal) {
  if (signal?.aborted) throw abortReason(signal);
}

/** Sleep without leaving a timer or abort listener behind. */
export function waitForUpdateTest(milliseconds, signal) {
  if (signal?.aborted) return Promise.reject(abortReason(signal));
  return new Promise((resolve, reject) => {
    const done = () => { signal?.removeEventListener("abort", aborted); resolve(); };
    const aborted = () => { clearTimeout(timer); signal?.removeEventListener("abort", aborted); reject(abortReason(signal)); };
    const timer = setTimeout(done, milliseconds);
    signal?.addEventListener("abort", aborted, { once: true });
  });
}

/** Return after an ordinary timeout; never detach a rejecting timer promise. */
async function boundedSettlement(promise, milliseconds) {
  let timer;
  try {
    return await Promise.race([
      promise.then(() => true, () => true),
      new Promise(resolve => { timer = setTimeout(() => resolve(false), milliseconds); }),
    ]);
  } finally { clearTimeout(timer); }
}

/**
 * Give cleanup a cooperative deadline, then require its actual promise to settle before a final report.
 * A timed-out rm cannot be rolled back; if it remains live, finalization is unsafe and paths/reports are not touched.
 */
export async function finishUpdateTestCleanup(cleanup, {
  timeoutMilliseconds = 180_000,
  settleGraceMilliseconds = 30_000,
  timeoutMessage = "cleanup timed out after 3 minutes",
} = {}) {
  const controller = new AbortController();
  let timer;
  let timedOut = false;
  const outcome = Promise.resolve().then(() => cleanup(controller.signal)).then(
    value => ({ status: "fulfilled", value }),
    reason => ({ status: "rejected", reason }),
  );
  const deadline = new Promise(resolve => {
    timer = setTimeout(() => {
      timedOut = true;
      controller.abort(new Error(timeoutMessage));
      resolve(true);
    }, timeoutMilliseconds);
  });
  const first = await Promise.race([outcome, deadline]);
  clearTimeout(timer);
  if (!timedOut) {
    if (first.status === "rejected") throw first.reason;
    return first.value;
  }
  if (!await boundedSettlement(outcome, settleGraceMilliseconds)) {
    throw new UnsafeUpdateTestFinalization(`${timeoutMessage}; cleanup did not stop, so result reporting was skipped`);
  }
  return `${timeoutMessage}; its remaining operation was not confirmed complete`;
}

/**
 * Stop children first, then abort cooperative waits, and finally require main's own promise to settle.
 * Refusal to stop is a nonzero, unsafe-finalization verdict: no run files may then be deleted.
 */
export async function runUpdateTestOperation(operation, {
  timeoutMilliseconds,
  stopGraceMilliseconds = 10_000,
  abortGraceMilliseconds = 20_000,
  timeoutMessage,
  onTimeout,
  onAbort,
  stopFailures,
}) {
  const controller = new AbortController();
  let timer;
  let timedOut = false;
  let timeoutError;
  const outcome = Promise.resolve().then(() => operation(controller.signal)).then(
    value => ({ status: "fulfilled", value }),
    reason => ({ status: "rejected", reason }),
  );
  const deadline = new Promise(resolve => {
    timer = setTimeout(() => {
      timedOut = true;
      timeoutError = new Error(timeoutMessage);
      try { onTimeout?.(); }
      catch (error) { timeoutError.message += `; stop failed: ${error instanceof Error ? error.message : String(error)}`; }
      resolve(true);
    }, timeoutMilliseconds);
  });
  const first = await Promise.race([outcome, deadline]);
  clearTimeout(timer);
  if (timedOut) {
    for (const failure of stopFailures ?? []) timeoutError.message += `; ${failure}`;
  }
  if (!timedOut) {
    if (first.status === "fulfilled") return first.value;
    throw first.reason;
  }

  if (!await boundedSettlement(outcome, stopGraceMilliseconds)) {
    controller.abort(timeoutError);
    try { onAbort?.(); }
    catch (error) { timeoutError.message += `; stop failed: ${error instanceof Error ? error.message : String(error)}`; }
    if (!await boundedSettlement(outcome, abortGraceMilliseconds)) {
      // outcome remains observed above; a later rejection is not unhandled and cannot trigger finalization.
      throw new UnsafeUpdateTestFinalization(`${timeoutError.message}; main did not stop, so cleanup and result reporting were skipped`);
    }
  }
  throw timeoutError;
}

/**
 * Track close, not just exit or a kill request. stop() may start an owned taskkill through trackTermination().
 * The injected spawn/stop doubles let tests exercise these semantics without platform commands.
 */
export function createUpdateTestProcessRegistry({ spawn, stop }) {
  const owned = new Set();
  const stopFailures = [];
  const terminationSettlements = new Set();

  function track(child, { termination = false } = {}) {
    const record = {
      child,
      termination,
      error: undefined,
      stopError: undefined,
      stopRequested: false,
      terminationError: undefined,
      closed: false,
      close: undefined,
      terminationSettled: undefined,
      requestStop: undefined,
    };
    let resolveClose;
    record.close = new Promise(resolve => { resolveClose = resolve; });
    child.once("close", (code, signal) => {
      record.closed = true;
      record.exit = { code, signal };
      owned.delete(record);
      resolveClose(record.exit);
    });
    record.terminationSettled = record.close.then(async () => {
      const failure = await record.terminationError;
      if (!failure) return;
      const detail = failure instanceof Error ? failure.message : String(failure);
      if (!stopFailures.some(entry => entry.endsWith(detail))) stopFailures.push(`termination failed: ${detail}`);
    });
    terminationSettlements.add(record.terminationSettled);
    void record.terminationSettled.finally(() => { terminationSettlements.delete(record.terminationSettled); });
    child.once("error", error => { record.error ??= error; });
    record.requestStop = () => {
      if (record.stopRequested) return;
      record.stopRequested = true;
      try {
        const requested = stop(record.child, child => track(child, { termination: true }));
        if (requested && typeof requested.then === "function") {
          throw new Error("stop must synchronously register termination helpers and return no Promise");
        }
      } catch (error) {
        record.stopError = error;
        stopFailures.push(`stop failed: ${error instanceof Error ? error.message : String(error)}`);
      }
    };
    void record.terminationSettled;
    owned.add(record);
    return record;
  }

  function run(command, args, { seconds, signal, onOutput, ...options }) {
    return new Promise((resolve, reject) => {
      if (signal?.aborted) { reject(abortReason(signal)); return; }
      let record;
      let timer;
      let terminal;
      const finish = error => {
        terminal ??= error;
        clearTimeout(timer);
        signal?.removeEventListener("abort", aborted);
        // Keep diagnostic output bounded, but do not forward cancelled command output during finalization.
        record.child.stdout?.off("data", keep);
        record.child.stderr?.off("data", keep);
        record.requestStop();
      };
      const aborted = () => { void finish(abortReason(signal)); };
      try {
        record = track(spawn(command, args, options));
      } catch (error) { reject(error); return; }
      record.text = "";
      const keep = data => {
        record.text = (record.text + data).slice(-20_000);
        onOutput?.(data);
      };
      record.child.stdout?.on("data", keep);
      record.child.stderr?.on("data", keep);
      timer = setTimeout(() => { void finish(new Error(`${command} timed out after ${seconds}s`)); }, seconds * 1000);
      signal?.addEventListener("abort", aborted, { once: true });
      void record.close.then(exit => {
        clearTimeout(timer);
        signal?.removeEventListener("abort", aborted);
        record.child.stdout?.off("data", keep);
        record.child.stderr?.off("data", keep);
        if (terminal) reject(terminal);
        else if (record.error) reject(record.error);
        else resolve({ ...exit, text: record.text ?? "" });
      });
      if (signal?.aborted) void finish(abortReason(signal));
    });
  }

  /** Stop current children, then require every close, including taskkill. No new main child may spawn after abort. */
  async function drain(milliseconds) {
    const allClosed = (async () => {
      for (;;) {
        // Stop is a synchronous latch: taskkill is registered before this close snapshot is taken.
        for (const record of [...owned]) if (!record.termination) record.requestStop();
        const targets = [...owned];
        await Promise.all(targets.map(record => record.close));
        if (owned.size === 0) return;
      }
    })();
    if (await boundedSettlement(allClosed, milliseconds)) {
      await Promise.all([...terminationSettlements]);
      const failures = [...stopFailures];
      return { closed: failures.length === 0, failures };
    }
    const failures = [...stopFailures, `${owned.size} owned process(es) did not close`];
    return { closed: false, failures };
  }

  return { owned, track, run, drain, stopFailures };
}
