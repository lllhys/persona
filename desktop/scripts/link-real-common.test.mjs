import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { getSystemErrorName } from "node:util";
import { basename, dirname, join } from "node:path";
import test from "node:test";
import {
  PAIR_INPUT,
  assertSensitiveTextAbsent,
  atomicWriteJSON,
  classifyOwnedPath,
  digest,
  redactPaths,
  readProvenance,
  receiptVerdict,
  runCaptured,
  sanitize,
  sanitizeJSON,
  serviceVerdict,
  writeFailureReceipt,
  writeSanitizedJSON,
} from "./link-real-common.mjs";

async function temporary(t, ...expected) {
  const root = await mkdtemp(join(tmpdir(), "persona-link-real-common-"));
  let bodySucceeded = false;
  t.after(async () => {
    if (!bodySucceeded) {
      await writeFile(join(root, "FAIL"), "The test failed or did not complete; its fixture and evidence are preserved.\n", { flag: "wx", mode: 0o600 }).catch(() => {});
      return;
    }
    // Remove only the explicitly declared, successfully exercised fixture. Any unexpected artifact preserves the root.
    for (const path of expected) await rm(join(root, path), { recursive: true, force: false });
    const remaining = (await readdir(root, { recursive: true })).filter(path => !expected.some(artifact => path === artifact || artifact.startsWith(`${path}/`)));
    if (remaining.length) {
      await writeFile(join(root, "FAIL"), `Unexpected fixture artifacts preserved: ${remaining.join(", ")}\n`, { flag: "wx", mode: 0o600 });
      throw new Error(`test fixture cleanup was incomplete: ${remaining.join(", ")}`);
    }
    await rm(root, { recursive: true, force: false });
  });
  return { root, succeed: () => { bodySucceeded = true; } };
}

test("receipt sanitization removes pairing input and distinguishes owned paths", async t => {
  const { root, succeed } = await temporary(t);
  const value = sanitize(`${PAIR_INPUT} ${root}/home ${PAIR_INPUT}token=example`, root, [`${root}/home`]);
  assert.equal(value, "<pairing-link> <run>/home <pairing-link>");
  assert.equal(sanitize(`${root}-sibling/file ${root}/child/file ${root}`, root), "<path> <run>/child/file <run>");
  assert.equal(sanitize("Bearer example.credential", root), "Bearer <redacted>");
  assert.equal(digest(PAIR_INPUT), "8403bc0bf9056e497bab3fa9f9ce4400e2310cee5bdd003290bf60f305495b19");
  assert.ok(redactPaths(root).some(entry => entry.path === root && entry.replacement === "<run>"));
  succeed();
});

test("sanitized JSON never persists an owned path or a pairing input", async t => {
  const { root, succeed } = await temporary(t, "nested/result.json");
  const file = join(root, "nested", "result.json");
  await writeSanitizedJSON(file, { argv: ["git", "status"], cwd: root, input: PAIR_INPUT, bearer: "Bearer example.credential" }, root);
  const text = await readFile(file, "utf8");
  assert.deepEqual(JSON.parse(text), { argv: ["git", "status"], cwd: "<run>", input: "<pairing-link>", bearer: "Bearer <redacted>" });
  assert.equal(text.includes(root), false);
  succeed();
});

test("owned-path classification is lexical, boundary-aware and not deletion authorization", async t => {
  const { root, succeed } = await temporary(t);
  assert.equal(classifyOwnedPath(root, root), "root");
  assert.equal(classifyOwnedPath(root, join(root, "child")), "descendant");
  assert.equal(classifyOwnedPath(root, `${root}-sibling`), "outside");
  assert.equal(classifyOwnedPath(root, join(root, "..", "sibling")), "outside");
  assert.equal(classifyOwnedPath(root, "/tmp"), "outside");
  assert.equal(classifyOwnedPath(root, "child"), "outside");
  assert.equal(classifyOwnedPath("", root), "outside");
  assert.equal(sanitize(`path=${root}; next=${root}, end=${root} error=${root}:`, root), "path=<run>; next=<run>, end=<run> error=<run>:");
  assert.equal(sanitize(join(dirname(root), `private-${basename(root)}`, "artifact"), root), "<path>");
  succeed();
});

test("Windows drive and UNC receipt paths preserve ownership without leaking paths", () => {
  for (const root of [String.raw`C:\receipts\owned`, String.raw`\\server\share\owned`]) {
    assert.equal(classifyOwnedPath(root, root), "root");
    assert.equal(classifyOwnedPath(root, `${root}\\child\\file`), "descendant");
    assert.equal(classifyOwnedPath(root, `${root}-sibling\\file`), "outside");
    assert.equal(sanitize(`${root} ${root}\\child\\file ${root}-sibling\\file`, root), "<run> <run>/child/file <path>");
    assert.equal(sanitize(`path=${root}; next=${root}, error=${root}:`, root), "path=<run>; next=<run>, error=<run>:");
    assert.throws(() => sanitizeJSON({ [root]: 1, [`${root}\\`]: 2 }, root), /merge or shadow/);
  }
  assert.equal(classifyOwnedPath(String.raw`C:\receipts\owned`, String.raw`c:\RECEIPTS\owned\child`), "descendant");
  assert.equal(classifyOwnedPath(String.raw`C:\receipts\owned`, String.raw`D:\receipts\owned`), "outside");
});

test("paths with spaces are redacted through structural boundaries without leaking suffixes", () => {
  for (const { root, sibling, unknown, child } of [
    { root: String.raw`C:\Users\Synthetic Owner\AppData\Local\Temp\owned`, sibling: String.raw`C:\Users\Synthetic Owner\AppData\Local\Temp\owned-sibling\private file`, unknown: String.raw`D:\Other Owner\private file`, child: String.raw`\child folder\file` },
    { root: String.raw`\\sample-server\share\Synthetic Owner\owned`, sibling: String.raw`\\sample-server\share\Synthetic Owner\owned-sibling\private file`, unknown: String.raw`\\another-server\share\Other Owner\private file`, child: String.raw`\child folder\file` },
    { root: "/tmp/Synthetic Owner/owned", sibling: "/tmp/Synthetic Owner/owned-sibling/private file", unknown: "/tmp/Other Owner/private file", child: "/child folder/file" },
  ]) {
    assert.equal(sanitize(`path=${root}; child=${root}${child}`, root), "path=<run>; child=<run>/child folder/file");
    assert.equal(sanitize(`${sibling} ${unknown} ${root}`, root), "<path> <path> <run>");
    assert.equal(sanitize(`first="${root}", second='${unknown}'\n${sibling}`, root), 'first="<run>", second=\'<path>\'\n<path>');
    assert.deepEqual(sanitizeJSON({ root, child: root + child, sibling, unknown }, root), { root: "<run>", child: "<run>/child folder/file", sibling: "<path>", unknown: "<path>" });
    assert.equal(classifyOwnedPath(root, sibling), "outside");
    assert.equal(classifyOwnedPath(root, root + child), "descendant");
    assert.throws(() => sanitizeJSON({ [root]: 1, [root + "/"]: 2 }, root), /merge or shadow/);
  }
});

test("sanitization covers keys, collisions, punctuation and truncated credentials", async t => {
  const { root, succeed } = await temporary(t, "receipt.json");
  const file = join(root, "receipt.json");
  await writeSanitizedJSON(file, { [root]: `${PAIR_INPUT}token=secret`, [`${root}-other`]: "outside" }, root, [`${root}-other`]);
  const text = await readFile(file, "utf8");
  assert.equal(text.includes(root), false);
  assert.equal(text.includes("secret"), false);
  const value = JSON.parse(text);
  assert.equal(value["<run>"], "<pairing-link>");
  assert.equal(value["<path>"], "outside");
  await assert.rejects(writeSanitizedJSON(file, { [root]: 1, [`${root}/`]: 2 }, root), /merge or shadow distinct receipt keys/);
  const truncated = sanitize(`${"x".repeat(128 * 1024)} ${PAIR_INPUT}token=secret`, root);
  assert.equal(truncated.length <= 128 * 1024, true);
  assert.equal(truncated.includes("secret"), false);
  succeed();
});

test("credential-shaped fields and special JSON keys are redacted or rejected", async t => {
  const { root, succeed } = await temporary(t, "credentials.json");
  const file = join(root, "credentials.json");
  const secret = `${"a".repeat(31)}!private-suffix%`;
  await writeSanitizedJSON(file, { authorization: `Bearer ${secret}`, assignment: "PERSONA_TOKEN=secret", other: "PASSWORD=secret" }, root);
  const text = await readFile(file, "utf8");
  assert.equal(text.includes(secret), false);
  assert.equal(text.includes("PERSONA_TOKEN=secret"), false);
  assert.equal(text.includes("PASSWORD=secret"), false);
  assert.deepEqual(JSON.parse(text), { authorization: "Bearer <redacted>", assignment: "PERSONA_TOKEN=<redacted>", other: "PASSWORD=<redacted>" });
  await assert.rejects(writeSanitizedJSON(file, JSON.parse('{"__proto__":{"injected":true}}'), root), /merge or shadow/);
  succeed();
});

test("a collision during failure reporting replaces the old PASS with a minimal failure", async t => {
  const { root, succeed } = await temporary(t, "result.json");
  const file = join(root, "result.json");
  await atomicWriteJSON(file, { ok: true, verdict: "PASS" });
  await writeFailureReceipt(file, { error: "privacy verification failed", artifacts: [{ [root]: 1, [`${root}/`]: 2 }], run: root });
  const value = JSON.parse(await readFile(file, "utf8"));
  assert.equal(value.ok, false);
  assert.equal(value.error, "Receipt redaction failed");
  assert.equal(value.cleanup.removed, false);
  succeed();
});

function assertUnsupportedGroup(result) {
  assert.equal(result.groupEmpty, false);
  assert.equal(result.group.error, "a Linux owned-process-group receipt is required");
  assert.equal(result.escalationError, "a Linux owned-process-group receipt is required");
  const verdict = receiptVerdict({ commands: [result], cleanup: { ok: true } });
  assert.equal(verdict.commandFailure, true);
  assert.equal(verdict.mayRemoveRunRoot, false);
  assert.equal(verdict.descendantProofMissing, true);
}

test(process.platform === "linux"
  ? "an observer spawn failure produces a bounded failed command receipt"
  : "an unsupported platform refuses group evidence without launching the Linux observer", async t => {
  const original = process.execPath;
  Object.defineProperty(process, "execPath", { value: join(original, "does-not-exist"), configurable: true });
  t.after(() => { Object.defineProperty(process, "execPath", { value: original, configurable: true }); });
  const result = await runCaptured(original, ["-e", "process.exit(0)"], { closeGrace: 100 });
  assert.equal(result.code, 0);
  assert.equal(result.closed, true);
  assert.equal(result.groupEmpty, false);
  if (process.platform === "linux") assert.match(result.escalationError, /owned-group observer/);
  else assertUnsupportedGroup(result);
});

test("service verdict requires four intercepted reads and no mutation", () => {
  const calls = Array(4).fill("--user show-environment");
  assert.equal(serviceVerdict(calls).ok, true);
  assert.equal(serviceVerdict([]).ok, false);
  assert.equal(serviceVerdict(calls.slice(0, 3)).ok, false);
  assert.equal(serviceVerdict([...calls.slice(0, 3), "--user restart persona-link.service"]).ok, false);
  assert.equal(serviceVerdict(calls).mutationAttempts.length, 0);
});

test("spawn failure returns a bounded failed close without an uncaught stdin error", async () => {
  const result = await runCaptured("/tmp/persona-link-real-common-does-not-exist", [], { input: "x".repeat(128 * 1024), timeout: 1000 });
  assert.equal(getSystemErrorName(result.code), "ENOENT");
  assert.equal(result.closed, true);
  assert.match(result.spawnError, /ENOENT/);
  assert.match(result.error, /ENOENT/);
});

test("a captured command waits for close and preserves nonzero and signal exits", async () => {
  const normal = await runCaptured(process.execPath, ["-e", "setTimeout(() => { console.log('normal'); process.exit(0); }, 20)"]);
  assert.equal(normal.code, 0);
  assert.equal(normal.signal, null);
  assert.equal(normal.closed, true);
  assert.equal(normal.timedOut, false);
  assert.equal(normal.stdout, "normal\n");
  const result = await runCaptured(process.execPath, ["-e", "setTimeout(() => { console.log('closed'); process.exit(7); }, 20)"]);
  assert.equal(result.code, 7);
  assert.equal(result.signal, null);
  assert.equal(result.closed, true);
  assert.equal(result.stdout, "closed\n");
  const signaled = await runCaptured(process.execPath, ["-e", "process.kill(process.pid, 'SIGTERM')"]);
  // Windows reports a self-termination as exit 1; it does not expose a POSIX signal status.
  assert.equal(signaled.code, process.platform === "win32" ? 1 : null);
  assert.equal(signaled.signal, process.platform === "win32" ? null : "SIGTERM");
  assert.equal(signaled.closed, true);
});

test(process.platform === "win32"
  ? "a native Windows timeout closes the direct child without claiming group cleanup"
  : "a real TERM-resistant child has a bounded SIGKILL close and a timeout failure", async t => {
  const source = "process.on('SIGTERM', () => {}); console.log('ready'); setTimeout(() => {}, 10000)";
  const closeGrace = 100;
  const startedAt = Date.now();
  const result = await runCaptured(process.execPath, ["-e", source], { timeout: 200, terminateGrace: 50, closeGrace });
  const elapsed = Date.now() - startedAt;
  assert.equal(result.stdout, "ready\n");
  assert.equal(result.timedOut, true);
  assert.equal(result.closed, !result.closeTimedOut);
  assert.equal(result.signal, result.closeTimedOut ? null : process.platform === "win32" ? "SIGTERM" : "SIGKILL");
  if (!result.closeTimedOut) {
    assert.equal(result.code, null);
    if (process.platform === "linux") {
      assert.equal(result.escalationError, undefined);
      assert.equal(result.groupEmpty, true);
    } else assertUnsupportedGroup(result);
  }
  assert.ok(elapsed < 10_000, "the bounded failure must return before the fixture's own 10-second timer");
  if (process.platform !== "linux") {
    assert.equal(result.closed, true, "the native direct child must close before this fixture completes");
    return;
  }
  t.after(async () => {
    // A close timeout does not identify the fixture's later exit; check its exact PID, PGID and start time.
    const initialStat = await readFile(`/proc/${result.pid}/stat`, "utf8").catch(() => "");
    if (!initialStat) return;
    const initialFields = initialStat.slice(initialStat.lastIndexOf(")") + 2).trim().split(/\s+/);
    assert.equal(Number(initialFields[2]), result.pid, "the fixture must belong to its own owned process group");
    const startTime = initialFields[19];
    const readMatchingStat = async () => {
      const stat = await readFile(`/proc/${result.pid}/stat`, "utf8").catch(() => "");
      if (!stat) return "";
      const fields = stat.slice(stat.lastIndexOf(")") + 2).trim().split(/\s+/);
      assert.equal(Number(fields[2]), result.pid, "the fixture PGID changed before cleanup");
      assert.equal(fields[19], startTime, "the fixture PID was reused before cleanup");
      return stat;
    };
    for (const signal of ["SIGTERM", "SIGKILL"]) {
      if (!await readMatchingStat()) return;
      try { process.kill(-result.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
      const deadline = Date.now() + 2000;
      while (await readMatchingStat()) {
        assert.ok(Date.now() < deadline, `the exact fixture did not exit within 2000 ms of ${signal}`);
        await new Promise(done => setTimeout(done, 10));
      }
    }
  });
});

// These fixtures depend on the Linux /proc PID/PGID/start-time cleanup proof. Do not start them on a host
// where that proof is unavailable; exercise the unsupported-platform receipt contract with a real child instead.
if (process.platform === "linux") {
  test("a descendant that holds inherited pipes produces a bounded close failure", async t => {
    const { root, succeed } = await temporary(t, "descendant.pid", "descendant.closed");
    const pidFile = join(root, "descendant.pid");
    const closedFile = join(root, "descendant.closed");
    const source = `
      const fs = require('node:fs');
      const child = require('node:child_process').spawn(process.execPath, ['-e', 'const fs=require("fs");process.on("SIGTERM",()=>{fs.writeFileSync(process.argv[1],"closed");process.exit(0)});console.log("descendant-ready");setTimeout(()=>{},30000)', ${JSON.stringify(closedFile)}], { stdio: ['ignore', 'inherit', 'inherit'] });
      child.once('spawn', () => { fs.writeFileSync(${JSON.stringify(pidFile)}, String(child.pid)); process.exit(0); });
    `;
    t.after(async () => {
      const pid = Number(await readFile(pidFile, "utf8").catch(() => "0"));
      if (pid > 0) {
        const initialStat = await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "");
        const fields = initialStat.slice(initialStat.lastIndexOf(")") + 2).trim().split(/\s+/);
        const startTime = fields[19];
        if (initialStat) assert.equal(Number(fields[2]), result.pid, "the descendant must remain in the exact owned group");
        for (const signal of ["SIGTERM", "SIGKILL"]) {
          const stat = await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "");
          if (!stat) break;
          assert.equal(stat.slice(stat.lastIndexOf(")") + 2).trim().split(/\s+/)[19], startTime, "the descendant PID was reused");
          try { process.kill(-result.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
          const deadline = Date.now() + 2000;
          while (await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "")) {
            assert.ok(Date.now() < deadline, `the exact descendant did not exit within 2000 ms of ${signal}`);
            await new Promise(done => setTimeout(done, 10));
          }
        }
      }
      await unlink(pidFile).catch(() => {});
    });
    const result = await runCaptured(process.execPath, ["-e", source], { timeout: 200, terminateGrace: 50, closeGrace: 100 });
    assert.equal(await readFile(pidFile, "utf8").then(value => Number(value) > 0), true, "the parent must publish the exact child PID before exiting");
    assert.deepEqual(new Set(result.stdout.split("\n").filter(Boolean)), new Set(["descendant-ready"]));
    assert.equal(result.timedOut, true);
    assert.equal(result.closeTimedOut, false, "the owned process group closes the captured pipes");
    assert.equal(result.closed, true);
    assert.equal(await readFile(closedFile, "utf8"), "closed");
    succeed();
  });

  test("an ignored-stdio descendant prevents a successful group receipt", async t => {
    const { root, succeed } = await temporary(t, "descendant.pid", "descendant.py", "out", "err");
    const pidFile = join(root, "descendant.pid");
    const fixture = join(root, "descendant.py");
    await writeFile(fixture, `import os,signal,time
def stop(*_):
    raise SystemExit(0)
signal.signal(signal.SIGTERM,stop)
with open(${JSON.stringify(pidFile)},'w') as f:f.write(str(os.getpid()))
time.sleep(30)
`);
    const source = `
      const fs = require('node:fs');
      const out = fs.openSync(${JSON.stringify(join(root, 'out'))}, 'w');
      const err = fs.openSync(${JSON.stringify(join(root, 'err'))}, 'w');
      const child = require('node:child_process').spawn('/usr/bin/python3', ['-I', ${JSON.stringify(fixture)}], { stdio: ['ignore', out, err] });
      child.on('error', error => { fs.writeFileSync(${JSON.stringify(join(root, 'error'))}, error.message); process.exit(2); });
      setTimeout(() => process.exit(fs.existsSync(${JSON.stringify(pidFile)}) ? 0 : 2), 200);
    `;
    const result = await runCaptured(process.execPath, ["-e", source], { closeGrace: 50 });
    try {
      assert.equal(result.code, 0);
      assert.equal(result.closed, true);
      assert.equal(result.groupEmpty, false);
      assert.equal(receiptVerdict({ commands: [result], provenance: {}, cleanup: { ok: true } }).mayRemoveRunRoot, false);
    } finally {
      const pid = Number(await readFile(pidFile, "utf8").catch(() => "0"));
      const stat = await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "");
      const fields = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
      if (pid > 0 && fields.length > 2) {
        const group = Number(fields[2]);
        assert.equal(group, result.pid, "the fixture must still belong to this run's owned group");
        for (const signal of ["SIGTERM", "SIGKILL"]) {
          try { process.kill(-group, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
          for (let attempt = 0; attempt < 100; attempt++) {
            await new Promise(done => setTimeout(done, 10));
            if (!await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "")) break;
          }
          if (!await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "")) break;
        }
        if (await readFile(`/proc/${pid}/stat`, "utf8").catch(() => "")) {
          throw new Error("the owned descendant fixture did not close");
        }
      }
    }
    succeed();
  });

  test("a detached descendant survives original-group observation and cannot authorize cleanup", async t => {
    const { root, succeed } = await temporary(t, "ready.json");
    const readyFile = join(root, "ready.json");
    const marker = "persona-link-detached-test-fixture";
    const source = `
      const fs = require('node:fs');
      const child = require('node:child_process').spawn(process.execPath, ['-e', "const fs=require('node:fs');const stat=fs.readFileSync('/proc/self/stat','utf8');const fields=stat.slice(stat.lastIndexOf(')')+2).trim().split(/\\\\s+/);fs.writeFileSync(process.argv[2],JSON.stringify({pid:process.pid,pgid:Number(fields[2]),start:fields[19]}));setInterval(()=>{},1000);", ${JSON.stringify(marker)}, ${JSON.stringify(readyFile)}], { detached: true, stdio: 'ignore' });
      child.unref();
      const deadline = Date.now() + 2000;
      const timer = setInterval(() => {
        if (fs.existsSync(${JSON.stringify(readyFile)})) { clearInterval(timer); process.exit(0); }
        else if (Date.now() > deadline) { clearInterval(timer); process.exit(2); }
      }, 10);
    `;
    let identity;
    let result;
    let verdict;
    let cleanupError;
    try {
      result = await runCaptured(process.execPath, ["-e", source], { closeGrace: 50 });
      identity = JSON.parse(await readFile(readyFile, "utf8"));
      const stat = await readFile(`/proc/${identity.pid}/stat`, "utf8");
      const fields = stat.slice(stat.lastIndexOf(")") + 2).trim().split(/\s+/);
      assert.equal(Number(fields[2]), identity.pgid);
      assert.equal(fields[19], identity.start);
      assert.equal(identity.pgid, identity.pid, "the fixture must detach into its own process group");
      assert.notEqual(identity.pgid, result.pid, "the fixture must not be found by the original-group scan");
      assert.equal(result.code, 0);
      assert.equal(result.closed, true);
      assert.equal(result.groupEmpty, true, "the original PGID is empty even though the detached descendant is alive");
      verdict = receiptVerdict({ commands: [result], cleanup: { ok: true } });
      assert.equal(verdict.ok, false);
      assert.equal(verdict.mayRemoveRunRoot, false);
      assert.equal(verdict.descendantProofMissing, true);
    } catch (error) {
      cleanupError = error;
    } finally {
      if (identity) {
        try {
          const stat = await readFile(`/proc/${identity.pid}/stat`, "utf8").catch(() => "");
          if (stat) {
            const fields = stat.slice(stat.lastIndexOf(")") + 2).trim().split(/\s+/);
            assert.equal(Number(fields[2]), identity.pgid, "fixture PGID changed before cleanup");
            assert.equal(fields[19], identity.start, "fixture PID was reused before cleanup");
            for (const signal of ["SIGTERM", "SIGKILL"]) {
              try { process.kill(-identity.pgid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
              for (let attempt = 0; attempt < 100; attempt++) {
                await new Promise(done => setTimeout(done, 10));
                if (!await readFile(`/proc/${identity.pid}/stat`, "utf8").catch(() => "")) break;
              }
              if (!await readFile(`/proc/${identity.pid}/stat`, "utf8").catch(() => "")) break;
            }
            assert.equal(await readFile(`/proc/${identity.pid}/stat`, "utf8").catch(() => ""), "", "the exact fixture must exit before ordinary test cleanup");
          }
        } catch (error) {
          cleanupError ??= error;
        }
      }
      if (cleanupError) {
        const evidence = join(root, "detached-counterexample.json");
        await writeFile(evidence, `${JSON.stringify({ identity, result, verdict, cleanupError: cleanupError.message, originalGroupObservation: "not proof of detached-descendant exit" }, null, 2)}\n`, { mode: 0o600 });
        throw cleanupError;
      }
    }
    succeed();
  });
} else {
  test("a real completed child cannot authorize cleanup without Linux group observation", async () => {
    const result = await runCaptured(process.execPath, ["-e", "console.log('real child closed')"]);
    assert.equal(result.code, 0);
    assert.equal(result.closed, true);
    assert.equal(result.timedOut, false);
    assert.equal(result.stdout, "real child closed\n");
    assertUnsupportedGroup(result);
  });
}

test("stdin failures remain observable beside a genuine child close", async () => {
  // 128 KiB can fit in a Linux socket buffer before fd 0 closes, producing no write error.
  // Keep a real write pending, and confirm that the child actually closed its input.
  const result = await runCaptured(process.execPath, ["-e", "require('node:fs').closeSync(0); console.log('stdin closed'); setTimeout(() => {}, 100)"], { input: "x".repeat(8 * 1024 * 1024), timeout: 2000 });
  assert.equal(result.stdout, "stdin closed\n");
  assert.equal(result.code, 0);
  assert.equal(result.closed, true);
  // A real closed-pipe write reports EOF on Windows libuv; POSIX reports EPIPE.
  assert.match(result.error, process.platform === "win32" ? /^stdin: write (?:EOF|EPIPE)$/ : /^stdin: write EPIPE$/);
  assert.equal(result.stdout.includes("private fixture input"), false);
});

test("a missing provenance failure replaces an old PASS and preserves the run root", async t => {
  const { root, succeed } = await temporary(t, "result.json");
  const file = join(root, "result.json");
  await atomicWriteJSON(file, { ok: true, error: root });
  await assert.rejects(readFile(join(root, "provenance.json"), "utf8"));
  const result = await writeFailureReceipt(file, { runID: "test-run", error: "missing provenance.json", artifacts: ["observer.json"], run: root });
  assert.equal(result.ok, false);
  assert.equal((await readFile(file, "utf8")).includes('"ok": true'), false);
  assert.equal((await readFile(file, "utf8")).includes(root), false);
  assert.deepEqual(result.cleanup, { ok: false, removed: false, reason: "failure receipts preserve the owned run root" });
  assert.equal(await readFile(join(root, "result.json"), "utf8").then(Boolean), true);
  succeed();
});

test("sanitized truncation does not expose a private path", async t => {
  const { root, succeed } = await temporary(t);
  const text = sanitize(`${"x".repeat(128 * 1024)} ${root}`, root);
  assert.equal(text.length <= 128 * 1024, true);
  assert.equal(text.includes(root), false);
  succeed();
});

test("sanitized JSON remains valid when a string exceeds the evidence limit", async t => {
  const { root, succeed } = await temporary(t, "large.json");
  const file = join(root, "large.json");
  await writeSanitizedJSON(file, { text: "x".repeat(200_000), paths: [root] }, root);
  const value = JSON.parse(await readFile(file, "utf8"));
  assert.equal(value.text.length, 128 * 1024);
  assert.deepEqual(value.paths, ["<run>"]);
  succeed();
});

test("missing, malformed and incomplete provenance cannot approve removal", async t => {
  const { root, succeed } = await temporary(t, "provenance.json");
  const file = join(root, "provenance.json");
  const validCommand = { closed: true, code: 0, signal: null, groupEmpty: true };
  const valid = { runID: "test-run", source: {}, binary: digest("binary"), electron: digest("electron"), go: digest("go"), dist: { "main.js": digest("main") }, scripts: { "smoke.mjs": digest("script") } };
  const expected = { runID: valid.runID, binary: valid.binary, electron: valid.electron, go: valid.go, dist: valid.dist, scripts: valid.scripts };
  for (const commands of [[], undefined]) assert.equal(receiptVerdict({ commands, provenance: valid, cleanup: { ok: true } }).mayRemoveRunRoot, false);
  for (const manifests of [{ dist: {}, scripts: valid.scripts }, { dist: valid.dist, scripts: {} }]) {
    assert.equal(receiptVerdict({ commands: [validCommand], provenance: { ...valid, ...manifests }, cleanup: { ok: true } }).mayRemoveRunRoot, false);
  }
  assert.equal(await readProvenance(file), undefined);
  await atomicWriteJSON(file, { incomplete: true });
  assert.equal(await readProvenance(file), undefined);
  await atomicWriteJSON(file, "{not json");
  assert.equal(await readProvenance(file), undefined);
  await atomicWriteJSON(file, valid);
  assert.deepEqual(await readProvenance(file, expected), valid);
  for (const expected of [{ runID: "other-run" }, { binary: digest("other") }, { dist: { "main.js": digest("other") } }, { scripts: { "smoke.mjs": digest("other") } }]) {
    assert.equal(await readProvenance(file, expected), undefined);
    assert.equal(receiptVerdict({ provenance: valid, expectedProvenance: expected }).mayRemoveRunRoot, false);
  }
  for (const command of [
    { closed: false, code: null, signal: null },
    { closed: true, code: null, signal: "SIGTERM" },
    { closed: true, code: 0, signal: null, closeTimedOut: true },
    { closed: true, code: 0, signal: null, timedOut: true },
    { closed: true, code: 0, signal: null, escalationError: "kill failed" },
  ]) {
    const verdict = receiptVerdict({ commands: [command], provenance: valid, expectedProvenance: expected, cleanup: { ok: true } });
    assert.equal(verdict.ok, false);
    assert.equal(verdict.mayRemoveRunRoot, false);
  }
  for (const command of [validCommand, { ...validCommand, descendantsClosed: true, descendantsClosedProof: true }]) {
    const verdict = receiptVerdict({ commands: [command], provenance: valid, expectedProvenance: expected, cleanup: { ok: true } });
    assert.equal(verdict.ok, false);
    assert.equal(verdict.mayRemoveRunRoot, false);
    assert.equal(verdict.descendantProofMissing, true);
  }
  succeed();
});

test("a redaction or provenance failure cannot leave a previous successful verdict", async t => {
  const { root, succeed } = await temporary(t, "result.json");
  const file = join(root, "result.json");
  await atomicWriteJSON(file, { ok: true });
  await assert.rejects(assertSensitiveTextAbsent([file], ["true"]), /Unredacted/);
  const result = await writeFailureReceipt(file, { error: `final privacy verification failed for ${root}/result.json`, run: root });
  assert.equal(result.ok, false);
  const text = await readFile(file, "utf8");
  assert.equal(text.includes('"ok": true'), false);
  assert.match(text, /final privacy verification failed for <run>\/result.json/);
  await assertSensitiveTextAbsent([file], [root, PAIR_INPUT]);
  succeed();
});
