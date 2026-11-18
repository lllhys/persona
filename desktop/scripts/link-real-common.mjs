import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import { homedir, platform } from "node:os";
import { dirname, posix, win32 } from "node:path";

export const PAIR_INPUT = "persona://link?";
export const SERVICE_SENTINEL = "real Link smoke: systemctl access is unavailable";

const receiptPath = value => /^(?:[a-z]:[\\/]|\\\\)/i.test(value) ? win32 : posix;

/** Classify lexical path identity only; this never authorizes recursive deletion through symlinks. */
export function classifyOwnedPath(run, candidate) {
  if (typeof run !== "string" || typeof candidate !== "string") return "outside";
  const paths = receiptPath(run);
  if (!paths.isAbsolute(run) || !paths.isAbsolute(candidate)) return "outside";
  const descendant = paths.relative(paths.resolve(run), paths.resolve(candidate));
  if (descendant === "") return "root";
  if (descendant === ".." || descendant.startsWith(`..${paths.sep}`) || paths.isAbsolute(descendant)) return "outside";
  return "descendant";
}

/** Map only the run root and its descendants. Siblings and string prefixes are private, not owned paths. */
export function redactPaths(run, paths = []) {
  const root = receiptPath(run).resolve(run);
  const unique = [...new Set([root, ...paths.filter(Boolean).map(path => receiptPath(path).resolve(path)), homedir()])]
    .sort((a, b) => b.length - a.length || a.localeCompare(b));
  return unique.map(path => {
    const kind = classifyOwnedPath(root, path);
    const descendant = receiptPath(root).relative(root, path).split(receiptPath(root).sep).join("/");
    const replacement = kind === "root" ? "<run>" : kind === "descendant" ? `<run>/${descendant}` : "<path>";
    return { path, replacement };
  });
}

function redactPathString(text, run, paths) {
  const mappings = redactPaths(run, paths);
  let output = "";
  for (let start = 0; start < text.length;) {
    const rest = text.slice(start);
    const match = /^(?:[a-z]:[\\/]|\\\\|\/)[^\r\n"'`;,:)}\]]*/i.exec(rest);
    if (!match) { output += text[start++]; continue; }
    // Spaces can belong to a path. Keep the whole field unless another absolute path starts;
    // unknown paths may redact extra words, but must not expose a private suffix after a space.
    const nextPath = /\s+(?=(?:[\w.-]+=)?(?:[a-z]:[\\/]|\\\\|\/))/i.exec(rest);
    const raw = rest.slice(0, Math.min(match[0].length, nextPath?.index ?? rest.length)).trimEnd();
    if (raw === "/") { output += raw; start += raw.length; continue; }
    const candidates = mappings.filter(({ path }) => classifyOwnedPath(path, raw) !== "outside").sort((a, b) => b.path.length - a.path.length);
    if (candidates.length) {
      const { path, replacement } = candidates[0];
      const descendant = receiptPath(path).relative(path, raw).split(receiptPath(path).sep).join("/");
      output += replacement === "<path>" && descendant ? "<path>" : `${replacement}${descendant ? `/${descendant}` : ""}`;
    } else {
      const kind = classifyOwnedPath(run, raw);
      output += kind === "root" ? "<run>" : kind === "descendant" ? `<run>/${receiptPath(run).relative(run, raw).split(receiptPath(run).sep).join("/")}` : "<path>";
    }
    start += raw.length;
  }
  return output;
}

/** Redact only the path-bearing text that this receipt owns; arbitrary encodings are outside this contract. */
export function sanitize(value, run, paths = []) {
  let text = String(value ?? "")
    .replace(/(?:persona|jarvis):\/\/(?:link|pair)\?[^\s"']*/gi, "<pairing-link>");
  text = redactPathString(text, run, paths)
    .replace(/\b(?:PERSONA_|JARVIS_)?(?:TOKEN|PASSWORD|SECRET|CODE|FP)=\S+/g, match => `${match.slice(0, match.indexOf("=") + 1)}<redacted>`)
    .replace(/([?&](?:token|code|fp|password)=)[^\s&"']*/gi, "$1<redacted>")
    .replace(/Bearer\s+[\x21-\x7e]+/g, "Bearer <redacted>");
  if (text.length > 128 * 1024) {
    const marker = "\n<evidence truncated after sanitization>";
    text = `${text.slice(0, 128 * 1024 - marker.length)}${marker}`;
  }
  return text.replace(/[\u001b\u009b][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d\/#&.:=?%@~_]+)*)?\u0007)|(?:(?:\d{1,4}(?:;\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]))/g, "");
}

export async function atomicWriteJSON(file, value) {
  await mkdir(dirname(file), { recursive: true });
  const temporary = `${file}.${process.pid}.tmp`;
  await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  await rename(temporary, file);
}

/** Redact each string before serializing; never truncate a complete JSON document into invalid syntax. */
export function sanitizeJSON(value, run, paths = []) {
  if (typeof value === "string") return sanitize(value, run, paths);
  if (Array.isArray(value)) return value.map(item => sanitizeJSON(item, run, paths));
  if (value && typeof value === "object") {
    const result = {};
    for (const [key, item] of Object.entries(value)) {
      const redactedKey = sanitize(key, run, paths);
      if (["__proto__", "prototype", "constructor"].includes(redactedKey) || Object.hasOwn(result, redactedKey)) throw new Error("redaction would merge or shadow distinct receipt keys");
      result[redactedKey] = sanitizeJSON(item, run, paths);
    }
    return result;
  }
  return value;
}

export async function writeSanitizedJSON(file, value, run, paths = []) {
  await atomicWriteJSON(file, sanitizeJSON(value, run, paths));
}

export async function assertSensitiveTextAbsent(files, secrets) {
  for (const file of files) {
    if (!await exists(file)) continue;
    const text = await readFile(file, "utf8");
    for (const secret of secrets.filter(Boolean)) {
      if (text.includes(secret) || text.includes(JSON.stringify(secret).slice(1, -1))) throw new Error(`Unredacted test-only or private value in ${file}`);
    }
  }
}

/**
 * Replace a previous verdict even when final provenance or privacy verification fails.
 * This helper only writes a failure receipt; the caller must retain its owned run root on failure.
 */
function provenanceFailure(provenance, expected) {
  if (!expected || typeof expected.runID !== "string" || !expected.runID || typeof expected.binary !== "string" || typeof expected.electron !== "string" || typeof expected.go !== "string" || !expected.dist || !expected.scripts || Object.keys(expected.dist).length === 0 || Object.keys(expected.scripts).length === 0) return true;
  if (!provenance || typeof provenance !== "object" || Array.isArray(provenance) || typeof provenance.runID !== "string" || !provenance.runID || !provenance.source) return true;
  if (expected.runID !== undefined && provenance.runID !== expected.runID) return true;
  for (const name of ["binary", "electron", "go"]) {
    if (expected[name] !== undefined && provenance[name] !== expected[name]) return true;
  }
  for (const group of ["dist", "scripts"]) {
    if (!provenance[group] || typeof provenance[group] !== "object" || Array.isArray(provenance[group]) || Object.keys(provenance[group]).length === 0) return true;
    for (const [name, digest] of Object.entries(expected[group] ?? {})) if (provenance[group][name] !== digest) return true;
  }
  const values = Object.values({ binary: provenance.binary, electron: provenance.electron, go: provenance.go, ...provenance.dist, ...provenance.scripts });
  if (values.some(value => typeof value !== "string" || !/^[a-f0-9]{64}$/.test(value))) return true;
  if (Object.keys(expected).some(key => !["runID", "binary", "electron", "go", "dist", "scripts"].includes(key))) return true;
  return false;
}

export function receiptVerdict({ commands = [], provenance, expectedProvenance, cleanup } = {}) {
  const commandFailure = !Array.isArray(commands) || commands.length === 0 || commands.some(command => !command || command.closed !== true || command.code !== 0 || command.signal !== null || command.error || command.timedOut || command.closeTimedOut || command.escalationError);
  // Original-PGID emptiness is not proof that a detached descendant exited. Generic captured commands cannot
  // authorize removal of the whole run root; a different, separately proven descendant-liveness contract is required.
  const descendantProofMissing = true;
  const invalidProvenance = provenanceFailure(provenance, expectedProvenance);
  return {
    ok: false,
    mayRemoveRunRoot: false,
    descendantProofMissing,
    commandFailure,
    provenanceFailure: invalidProvenance,
    cleanupFailure: cleanup?.ok !== true,
  };
}

export async function readProvenance(file, expected) {
  let value;
  try { value = JSON.parse(await readFile(file, "utf8")); } catch { return undefined; }
  return receiptVerdict({ provenance: value, expectedProvenance: expected }).provenanceFailure ? undefined : value;
}

export async function writeFailureReceipt(file, { schema = 1, runID, error, artifacts = [], run, paths = [] }) {
  if (!error) throw new Error("a failure receipt requires an actual error");
  const result = {
    schema,
    ok: false,
    verdict: "Development receipt contract: FAIL",
    error: String(error instanceof Error ? error.message : error),
    artifacts,
    cleanup: { ok: false, removed: false, reason: "failure receipts preserve the owned run root" },
    ...(runID ? { runID } : {}),
  };
  if (!run) {
    // No paths are available for a safe persisted failure message; retain only the fixed verdict.
    result.error = "Receipt failed before path redaction was available";
    result.artifacts = [];
  }
  await writeSanitizedJSON(file, result, run ?? file, paths).catch(async () => {
    // Never repeat an unsanitized exception in a fallback. A failed fallback write remains a thrown publication error.
    await atomicWriteJSON(file, { schema, ok: false, verdict: "Development receipt contract: FAIL", error: "Receipt redaction failed", cleanup: { ok: false, removed: false } });
  });
  return result;
}

export function digest(value) {
  return createHash("sha256").update(value).digest("hex");
}

export async function fileDigest(file) {
  return digest(await readFile(file));
}

export async function exists(file) {
  return stat(file).then(() => true, () => false);
}

function termination(child, signal) {
  // The leader can exit while a descendant retains stdout/stderr. Until close, its owned group is still relevant.
  if (process.platform === "win32" && (child.exitCode !== null || child.signalCode !== null)) return;
  if (!Number.isInteger(child.pid) || child.pid <= 0) throw new Error("no owned process group PID");
  try {
    if (process.platform === "win32") child.kill(signal);
    else process.kill(-child.pid, signal);
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
}

/**
 * POSIX-only observation of one newly created process group. /proc is read only for this exact PGID;
 * no private process environment or unrelated process metadata is read. This proves only that the original process
 * group is empty; it cannot detect a descendant that detached into another process group.
 */
async function observeOwnedGroup(child, timeout) {
  if (platform() !== "linux" || !Number.isInteger(child.pid) || child.pid <= 0) {
    return { groupEmpty: false, error: "a Linux owned-process-group receipt is required" };
  }
  const group = child.pid;
  const source = `
    import { readdir, readFile } from 'node:fs/promises';
    import { argv, exit } from 'node:process';
    const group = Number(argv[1]);
    const deadline = Number(argv[2]);
    if (!Number.isSafeInteger(group) || group <= 0 || !Number.isFinite(deadline)) exit(2);
    const sleep = ms => new Promise(resolve => setTimeout(resolve, Math.min(ms, 20)));
    try {
      for (;;) {
        const entries = await readdir('/proc');
        const remaining = [];
        for (const entry of entries) {
          if (!/^\\d+$/.test(entry)) continue;
          const stat = await readFile('/proc/' + entry + '/stat', 'utf8').catch(() => '');
          const end = stat.lastIndexOf(')');
          const fields = stat.slice(end + 2).split(' ');
          if (end < 0 || fields.length < 3 || Number(fields[2]) !== group || fields[0] === 'Z') continue;
          remaining.push(Number(entry));
        }
        if (remaining.length === 0) { console.log(JSON.stringify({ groupEmpty: true, observedGroup: group })); exit(0); }
        if (Date.now() >= deadline) { console.log(JSON.stringify({ groupEmpty: false, remaining })); exit(3); }
        await sleep(deadline - Date.now());
      }
    } catch (error) { console.error(JSON.stringify({ groupEmpty: false, error: error.message })); exit(2); }
  `;
  return new Promise(resolve => {
    let watcher;
    try {
      watcher = spawn(process.execPath, ["--input-type=module", "-e", source, String(group), String(Date.now() + timeout)], {
        stdio: ["ignore", "pipe", "pipe"], shell: false, detached: true,
      });
    } catch (error) {
      resolve({ groupEmpty: false, error: `owned-group observer: ${error.message}` });
      return;
    }
    let stdout = "", stderr = "", timer, spawnError;
    watcher.stdout.on("data", chunk => { stdout += chunk; });
    watcher.stderr.on("data", chunk => { stderr += chunk; });
    watcher.on("error", error => {
      clearTimeout(timer);
      spawnError = error.message;
      resolve({ groupEmpty: false, error: `owned-group observer: ${spawnError}` });
    });
    const stop = signal => {
      if (watcher.exitCode !== null || watcher.signalCode !== null) return;
      try { process.kill(-watcher.pid, signal); } catch { /* the close event below decides */ }
    };
    watcher.once("close", (code, signal) => {
      clearTimeout(timer);
      if (spawnError) return;
      let receipt;
      try { receipt = JSON.parse(stdout); } catch { receipt = { groupEmpty: false }; }
      resolve({ ...receipt, groupEmpty: receipt.groupEmpty === true && code === 0 && signal === null, watcher: { code, signal, stderr: stderr.slice(0, 1000) } });
    });
    timer = setTimeout(() => { stop("SIGTERM"); setTimeout(() => stop("SIGKILL"), 100).unref(); }, timeout + 1000);
  });
}

/**
 * Capture a real child close, including its nonzero code or signal. POSIX uses a new owned process group for bounded
 * termination, including a leader that exited while a descendant retained the captured pipes. Windows terminates only
 * the direct child and cannot approve a group receipt. Detached descendants and PGID-reuse are outside what this
 * observation can prove; closing captured pipes and an empty original group are not proof all descendants exited.
 */
export function runCaptured(executable, args, options = {}) {
  const {
    timeout = 120_000, terminateGrace = 1000, closeGrace = 1000,
    input, ...spawnOptions
  } = options;
  for (const [name, value] of Object.entries({ timeout, terminateGrace, closeGrace })) {
    if (!Number.isInteger(value) || value < 0 || value > 2_147_483_647) throw new RangeError(`${name} must be a nonnegative supported timer duration`);
  }
  return new Promise(resolve => {
    let stdout = "", stderr = "", error, spawnError, stdinError, timedOut = false, closeTimedOut = false, escalationError;
    let settled = false;
    let terminateTimer, killTimer, closeTimer;
    const startedAt = new Date().toISOString();
    let child;
    try {
      child = spawn(executable, args, { shell: false, stdio: ["pipe", "pipe", "pipe"], ...spawnOptions, ...(process.platform === "win32" ? {} : { detached: true }) });
    } catch (cause) {
      resolve({ executable, args: [...args], code: null, signal: null, error: cause.message, timedOut: false, closeTimedOut: false, closed: false, stdout, stderr, startedAt, endedAt: new Date().toISOString() });
      return;
    }
    const finish = async (code, signal) => {
      if (settled) return;
      settled = true;
      for (const timer of [terminateTimer, killTimer, closeTimer]) clearTimeout(timer);
      const group = await observeOwnedGroup(child, closeGrace);
      const groupError = group.error ?? (group.groupEmpty ? undefined : "owned process group was not observed empty");
      escalationError = [escalationError, groupError].filter(Boolean).join("; ") || undefined;
      resolve({ executable, args: [...args], pid: child.pid, code, signal, error, spawnError, stdinError, timedOut, closeTimedOut, escalationError, closed: !closeTimedOut, groupEmpty: group.groupEmpty === true, group: { groupEmpty: group.groupEmpty === true, remaining: group.remaining, error: group.error }, stdout, stderr, startedAt, endedAt: new Date().toISOString() });
    };
    const stop = (signal, field) => {
      try { termination(child, signal); } catch (cause) {
        const message = `${field}: ${cause.message}`;
        escalationError = escalationError ? `${escalationError}; ${message}` : message;
      }
    };
    terminateTimer = setTimeout(() => {
      timedOut = true;
      stop("SIGTERM", "terminate");
      killTimer = setTimeout(() => {
        stop("SIGKILL", "kill");
        closeTimer = setTimeout(() => { closeTimedOut = true; finish(null, null); }, closeGrace);
      }, terminateGrace);
    }, timeout);
    child.stdout.on("data", chunk => { if (stdout.length < 128 * 1024) stdout += chunk.toString("utf8").slice(0, 128 * 1024 - stdout.length); });
    child.stderr.on("data", chunk => { if (stderr.length < 128 * 1024) stderr += chunk.toString("utf8").slice(0, 128 * 1024 - stderr.length); });
    child.on("error", value => { spawnError = value.message; error = [spawnError, stdinError].filter(Boolean).join("; "); });
    child.once("close", finish);
    child.stdin.on("error", value => { stdinError = `stdin: ${value.message}`; error = [spawnError, stdinError].filter(Boolean).join("; "); });
    if (input !== undefined) {
      try { child.stdin.end(input); } catch (cause) {
        stdinError = `stdin: ${cause.message}`;
        error = [spawnError, stdinError].filter(Boolean).join("; ");
        try { child.stdin.destroy(); } catch { /* the real close receipt is still required */ }
      }
    } else child.stdin.end();
  });
}

/** systemctl is a service-manager double, never a Link executable double. */
export async function writeSystemctlDouble(file, receipt) {
  const script = `#!/bin/sh
set -eu
printf '%s\\n' "$*" >> ${JSON.stringify(receipt)}
printf '%s\\n' ${JSON.stringify(SERVICE_SENTINEL)} >&2
exit 42
`;
  await writeFile(file, script, { mode: 0o700 });
}

export async function readServiceCalls(file) {
  if (!await exists(file)) return [];
  return (await readFile(file, "utf8")).split("\n").filter(Boolean);
}

/** Every expected status must have reached the double; a missing receipt never proves interception. */
export function serviceVerdict(calls) {
  const expected = 4;
  const onlyRead = calls.every(call => call === "--user show-environment");
  return {
    ok: calls.length === expected && onlyRead,
    calls,
    expectedCalls: expected,
    mutationAttempts: calls.filter(call => call !== "--user show-environment"),
    boundary: "PATH-selected read-only systemctl double; not an OS sandbox or proof against direct host reads",
  };
}
