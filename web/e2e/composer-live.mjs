// Live-hub acceptance in one explicitly created side conversation, never the main chat.
// node web/e2e/composer-live.mjs <origin> <hub-config-json> <artifacts> [private-test-session-json]
// Reuse the saved test-session.json on later runs; never upload that private record.
// Credentials and conversation identifiers stay out of console output and screenshots.
import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "playwright-core";
import { spawnSync } from "node:child_process";
const [address, config, artifacts, sessionFile] = process.argv.slice(2);
assert.ok(address && config && artifacts);
const base = new URL(address), output = resolve(artifacts);
await mkdir(output, { recursive: true });
const { token } = JSON.parse(await readFile(config, "utf8"));
assert.ok(token);
const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json", "Accept-Language": "en" };
const request = async (path, body) => {
  // Independent connections avoid carrying a stalled start into later requests; credentials stay on stdin.
  const settings = `url = ${JSON.stringify(new URL(`/api/v1${path}`, base).href)}\n` +
    Object.entries(headers).map(([k, v]) => `header = ${JSON.stringify(`${k}: ${v}`)}\n`).join("");
  const args = ["--config", "-", "--silent", "--show-error", "--fail", "--max-time", "120"];
  if (body) args.push("--request", "POST", "--data-binary", JSON.stringify(body));
  const r = spawnSync("curl", args, { input: settings, encoding: "utf8", maxBuffer: 10 * 1024 * 1024 });
  assert.equal(r.status, 0, `live API setup failed: ${r.stderr}`);
  return JSON.parse(r.stdout);
};
const assistant = await request("/assistant");
const main = assistant.main;
const created = sessionFile ? JSON.parse(await readFile(sessionFile, "utf8"))
  : await request("/assistant/conversations", { main: false, prompt: "This is a dedicated composer UI verification conversation. Reply only with OK. Do not run tools or change anything." });
const session = created.session;
assert.ok(session?.machine_id && session.session_id && session.session_id !== main, "test conversation must not be the main chat");
assert.ok(!created.origin || created.origin === base.origin, "saved test session must belong to this hub");
await writeFile(`${output}/test-session.json`, JSON.stringify({ origin: base.origin, session: { machine_id: session.machine_id, session_id: session.session_id } }), { mode: 0o600 });
console.log(`PASS dedicated side conversation ${sessionFile ? "reused" : "created (main=false)"}; no main-chat start`);
const browser = await chromium.launch({ channel: "chrome" });
const result = { status: "running", dedicatedSideConversation: true, mockHub: false, runs: [] };
const deadline = setTimeout(() => {
  result.status = "timeout";
  console.log("TIMEOUT live composer acceptance");
  void writeFile(`${output}/results.json`, JSON.stringify(result, null, 2)).finally(() => process.exit(1));
}, 180_000);
const lines = (n) => Array.from({ length: n }, (_, i) => `Verification line ${i + 1}.`).join("\n");
try {
  for (const [width, height] of [[390, 844], [1280, 900]]) for (const theme of ["light", "dark"]) {
    const name = `${width}x${height}-${theme}`;
    const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme, isMobile: width < 500, hasTouch: width < 500, locale: "en-US", reducedMotion: "reduce", serviceWorkers: "block" });
    await context.addInitScript(({ token, theme }) => {
      localStorage.setItem("persona.token", token);
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", "en");
    }, { token, theme });
    const blockedWrites = [];
    await context.route("**/api/v1/**", (route) => {
      const req = route.request(), path = new URL(req.url()).pathname;
      if (main && path.includes(encodeURIComponent(main)) && /\/(items|feed|today|send)$/.test(path)) return route.abort();
      // Built-in metadata lookup only: the handler fetches and stores nothing (models_catalog_routes.go).
      if (req.method() === "POST" && path === "/api/v1/launchers/catalog") return route.continue();
      if (!["GET", "HEAD"].includes(req.method()) && !(path.includes(encodeURIComponent(session.session_id)) && /\/(seen|today)$/.test(path))) {
        blockedWrites.push("unexpected write blocked"); return route.abort();
      }
      return route.continue();
    });
    const page = await context.newPage();
    page.setDefaultTimeout(25_000);
    await page.goto(new URL(`/persona/${encodeURIComponent(session.session_id)}`, base).href);
    await page.getByText("This is a dedicated composer UI verification conversation.", { exact: false }).first().waitFor();
    assert.ok(new URL(page.url()).pathname.includes(encodeURIComponent(session.session_id)), "stay in the verified test conversation before typing");
    const input = page.locator("textarea").last();
    await input.waitFor();
    const measure = async () => {
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      return input.evaluate((el) => {
        const css = getComputedStyle(el), line = parseFloat(css.lineHeight), pad = parseFloat(css.paddingTop) + parseFloat(css.paddingBottom);
        return { height: el.getBoundingClientRect().height, rows: (el.clientHeight - pad) / line, line, scrollTop: el.scrollTop, scrollHeight: el.scrollHeight, clientHeight: el.clientHeight };
      });
    };
    const run = { name, growth: [] };
    result.runs.push(run);
    for (const n of [1, 3, 4, 6, 7, 30]) { await input.fill(lines(n)); run.growth.push({ lines: n, ...await measure() }); }
    const six = run.growth.find((g) => g.lines === 6), seven = run.growth.find((g) => g.lines === 7);
    assert.ok(Math.abs(six.rows - 6) < 0.1 && Math.abs(six.height - seven.height) < 1, `${name}: not capped at six lines`);
    await input.hover(); await page.mouse.wheel(0, -10000);
    await page.waitForFunction(() => document.querySelector("textarea").scrollTop < 1);
    run.beforeIdle = await measure();
    await page.waitForTimeout(3000);
    run.afterIdle = await measure();
    assert.ok(Math.abs(run.beforeIdle.scrollTop - run.afterIdle.scrollTop) < 1, `${name}: manual scroll moved while idle`);
    await input.screenshot({ path: `${output}/${name}-manual.png` });
    await input.focus(); await page.keyboard.press("Control+End"); await page.keyboard.insertText(" More text.");
    run.afterTyping = await measure();
    assert.ok(run.afterTyping.scrollTop + run.afterTyping.clientHeight >= run.afterTyping.scrollHeight - 3, `${name}: caret not visible after typing`);
    await input.fill(lines(6));
    await measure();
    await input.screenshot({ path: `${output}/${name}-six-lines.png` });
    if (width < 500) {
      await page.setViewportSize({ width, height: 500 });
      run.keyboard = await measure();
      assert.ok(Math.abs(run.keyboard.rows - 6) < 0.1, `${name}: keyboard-sized viewport did not show six lines`);
      await page.setViewportSize({ width, height });
      await input.fill(lines(30)); await measure();
      await page.getByTestId("composer-expand").click();
      const panel = page.getByTestId("composer-expanded");
      await panel.evaluate((el) => Promise.all(el.getAnimations({ subtree: true }).filter((a) => a.effect?.getTiming().iterations !== Infinity).map((a) => a.finished.catch(() => {}))));
      await page.keyboard.press("Control+Home");
      run.expanded = await panel.evaluate((el) => {
        const b = el.querySelector('[data-testid="composer-collapse"]'), f = el.querySelector("textarea");
        return { color: getComputedStyle(b).color, overlaps: f.getBoundingClientRect().top < b.getBoundingClientRect().bottom };
      });
      await panel.screenshot({ path: `${output}/${name}-expanded.png` });
    }
    assert.deepEqual(blockedWrites, [], `${name}: unexpected live write attempted`);
    console.log(`PASS live ${name}: six-line growth; manual scroll ${run.beforeIdle.scrollTop} -> ${run.afterIdle.scrollTop} after 3s; typing reveals caret; no main-chat writes`);
    await context.close();
  }
  result.status = "pass";
} catch (e) { result.status = "fail"; result.error = String(e); }
finally {
  clearTimeout(deadline); await browser.close();
  await writeFile(`${output}/results.json`, JSON.stringify(result, null, 2));
  console.log(`${result.status.toUpperCase()} live composer acceptance${result.error ? `: ${result.error}` : ""}`);
  process.exit(result.status === "pass" ? 0 : 1);
}
