#!/usr/bin/env node
// Opt-in real runtime smoke, not an offline test. Uses a disposable skill and no hard-coded model.
// stdout is allowlisted: never publish the runtime's environment, catalog, credentials or arbitrary replies.
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, writeFile, rm, readdir, readFile, realpath } from "node:fs/promises";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { randomBytes } from "node:crypto";
import { fileURLToPath } from "node:url";

const runtime = process.argv[2];
if (!["codex", "claude"].includes(runtime)) throw new Error("Usage: node scripts/test/skill-runtime-smoke.mjs codex|claude [--model] [--model-id ID] [--settings FILE]");
const runModel = process.argv.includes("--model");
const option = (name) => {
  const index = process.argv.indexOf(name);
  if (index < 0) return undefined;
  const value = process.argv[index + 1];
  if (!value || value.startsWith("--")) throw new Error(`Missing ${name} value`);
  return value;
};
const model = option("--model-id");
const settings = option("--settings");
if (settings && runtime !== "claude") throw new Error("--settings is only supported for Claude");
if (settings && await realpath(settings) !== await realpath(join(process.env.CLAUDE_CONFIG_DIR || join(homedir(), ".claude"), "settings.json"))) {
  throw new Error("Claude smoke --settings must name the current profile's existing settings file");
}
const dir = await mkdtemp(join(tmpdir(), "persona-skill-smoke-"));
const name = "persona-explicit-smoke";
const marker = "PERSONA_SKILL_5357_" + randomBytes(16).toString("hex");
const file = join(dir, runtime === "codex" ? ".agents" : ".claude", "skills", name, "SKILL.md");
await mkdir(join(file, ".."), { recursive: true });
await writeFile(file, `---\nname: ${name}\ndescription: Disposable explicit selection smoke\n---\nFor this smoke, reply with exactly ${marker} and nothing else. Do not use tools except reading this file. Do not write files or make network requests.\n`);
let child;
const launch = (command, args = []) => spawn(process.env.SHELL || "/bin/sh", ["-ic", command + ' "$@"', "persona-skill-smoke", ...args], { cwd: dir, stdio: ["pipe", "pipe", "ignore"], detached: true });
const stop = (signal) => { try { if (child && child.exitCode === null) process.kill(-child.pid, signal); } catch { /* only this smoke's process group */ } };
const timeout = setTimeout(() => { stop("SIGKILL"); process.exitCode = 1; }, 90_000);
try {
  if (runtime === "codex") {
    child = launch("codex app-server");
    const pending = new Map();
    let seq = 0;
    let complete;
    const completed = new Promise((resolve) => { complete = resolve; });
    let markerSeen = false;
    const lines = createInterface({ input: child.stdout });
    lines.on("line", (line) => {
      let m;
      try { m = JSON.parse(line); } catch { return; }
      if (m.id != null && pending.has(m.id)) {
        const { resolve, reject } = pending.get(m.id);
        pending.delete(m.id);
        if (m.error) reject(new Error("runtime RPC refused (details redacted)"));
        else resolve(m.result);
      }
      if (m.method === "item/completed" && m.params?.item?.type === "agentMessage") markerSeen ||= m.params.item.text?.trim() === marker;
      if (m.method === "turn/completed") complete(m.params?.turn?.status);
    });
    child.once("exit", () => {
      for (const p of pending.values()) p.reject(new Error("runtime exited"));
      complete("exited");
    });
    const call = (method, params) => new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      child.stdin.write(JSON.stringify({ id, method, params }) + "\n");
    });
    await call("initialize", { clientInfo: { name: "persona-skill-smoke", version: "1" }, capabilities: { experimentalApi: true } });
    child.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
    const extra = join(dir, ".claude", "skills");
    const duplicate = join(extra, name, "SKILL.md");
    await mkdir(join(duplicate, ".."), { recursive: true });
    await writeFile(duplicate, `---\nname: ${name}\ndescription: Same-name prepared extra root fixture\n---\nThis is the other source, not the selected project skill.\n`);
    await call("skills/extraRoots/set", { extraRoots: [extra] });
    const catalog = await call("skills/list", { cwds: [dir], forceReload: true });
    const entries = catalog.data?.find((d) => d.cwd === dir)?.skills ?? [];
    const skill = entries.find((s) => s.name === name && s.path === file && s.enabled);
    if (!skill) throw new Error("fixture missing from actual effective runtime catalog");
    if (!entries.some((s) => s.name === name && s.path === duplicate && s.enabled)) throw new Error("same-name prepared extra root missing from actual catalog");
    const oldDirs = await readdir(join(homedir(), ".persona", "skills"), { withFileTypes: true }).catch(() => []);
    console.log("PASS codex actual skills/list: disposable project skill enabled and exact path matched (other entries redacted)");
    console.log(`PASS actual same-name sources preserved; enabled runtime entries=${entries.filter((s) => s.enabled).length}; legacy user-root directories=${oldDirs.filter((d) => d.isDirectory()).length} (not equivalent inventories)`);
    if (runModel) {
      const thread = await call("thread/start", { cwd: dir, ephemeral: true, sandbox: "read-only", approvalPolicy: "never", ...(model ? { model } : {}) });
      await call("turn/start", { threadId: thread.thread.id, input: [{ type: "skill", name: skill.name, path: skill.path },
        { type: "text", text: "Use the explicitly selected skill and follow its smoke instruction.", text_elements: [] }] });
      const status = await completed;
      if (status !== "completed" || !markerSeen) throw new Error(`model marker not verified (turn status: ${status}; reply redacted)`);
      console.log("PASS codex actual typed skill turn: model returned exact disposable marker");
    }
    lines.close();
  } else {
    if (!runModel) throw new Error("Claude has no inventory RPC in this adapter; use --model for its exact-file delivery smoke");
    const root = fileURLToPath(new URL("../../", import.meta.url));
    child = spawn("go", ["-C", join(root, "server"), "test", "-p", "1", "./internal/node", "-run", "^TestClaudeSkillRuntimeSmokeContract$", "-count=1"], {
      cwd: root, stdio: ["ignore", "ignore", "ignore"], detached: true,
      env: { ...process.env, GOMAXPROCS: "2", PERSONA_SKILL_SMOKE_DIR: dir, PERSONA_SKILL_SMOKE_PROFILE: process.env.CLAUDE_CONFIG_DIR || "default" },
    });
    const contractStatus = await new Promise((resolve) => child.once("exit", resolve));
    if (contractStatus !== 0) throw new Error("Claude production catalog/reference smoke bridge failed (details redacted)");
    const delivery = JSON.parse(await readFile(join(dir, "delivery.json"), "utf8"));
    console.log("PASS claude production catalog: partial local roots -> exact opaque selection -> revalidated instruction");
    child = launch("claude", ["-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence",
      ...(model ? ["--model", model] : []), ...(settings ? ["--settings", settings] : [])]);
    let result;
    let actualModel;
    let readID;
    let exactRead = false;
    let fileResult = false;
    const lines = createInterface({ input: child.stdout });
    lines.on("line", (line) => {
      let message;
      try { message = JSON.parse(line); } catch { return; }
      if (message.type === "result") result = message;
      if (message.type === "system" && message.subtype === "init") actualModel = message.model;
      for (const item of message.message?.content ?? []) {
        if (item.type === "tool_use" && item.name === "Read" && item.input?.file_path === file) {
          readID = item.id;
          exactRead = true;
        }
        if (item.type === "tool_result" && item.tool_use_id === readID && !item.is_error && JSON.stringify(item.content).includes(marker)) fileResult = true;
      }
    });
    const done = new Promise((resolve) => child.once("exit", resolve));
    child.stdin.end(delivery.prompt);
    const status = await done;
    lines.close();
    if (!result) throw new Error("runtime returned no JSON result (details redacted)");
    if (status !== 0 || result.is_error || result.result?.trim() !== marker) {
      const detail = JSON.stringify(result);
      const category = /not logged|api.?key|authentication|oauth|log.?in/i.test(detail) ? "authentication"
        : /permission|denied|allowed/i.test(detail) ? "permission" : "runtime/model response";
      throw new Error(`Claude model marker not verified: exit=${status}, is_error=${Boolean(result.is_error)}, category=${category} (reply redacted)`);
    }
    if (model && actualModel !== model) throw new Error("Claude runtime did not confirm the requested per-call model (model metadata redacted)");
    if (!exactRead || !fileResult) throw new Error("Claude marker reply did not include a verified exact-file Read/tool_result pair (details redacted)");
    if (model) console.log("PASS claude model selection: runtime init confirms the exact requested per-call model ID");
    console.log("PASS claude actual Read: exact selected SKILL.md and successful tool result contain the isolated marker");
    console.log("PASS claude actual model: exit=0, is_error=false, exact random file-only marker under existing permissions");
  }
} finally {
  clearTimeout(timeout);
  stop("SIGTERM");
  await rm(dir, { recursive: true, force: true }); // only this process's mkdtemp fixture
}
