// Keyboard sending against a dedicated synthetic mock hub (#8249).
// Start the repository mock and Vite on dedicated loopback ports, then:
// node web/e2e/composer-enter-sends.mjs <web origin> <artifacts> [before|after] [--executable <chromium>]
// "before" pins the reported failure; "after" requires Mod+Enter and mouse promotion. Touch-only behavior must not change.
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const [address, artifacts, phase = "after"] = args;
const executableFlag = args.indexOf("--executable");
const executable = executableFlag >= 0 ? args[executableFlag + 1] : undefined;
assert.ok(address && artifacts && ["before", "after"].includes(phase));
assert.match(address, /^https?:\/\/(127\.0\.0\.1|localhost|\[::1\])(:\d+)?\/?$/);
const base = new URL(address);
const output = resolve(artifacts);
await mkdir(output, { recursive: true });
const launch = executable ? { executablePath: executable } : { channel: "chrome" };
const falsePointerFlags = ["--blink-settings=primaryHoverType=2,availableHoverTypes=2,primaryPointerType=2,availablePointerTypes=2"];
const result = { status: "running", phase, runs: [] };
const deadline = setTimeout(() => {
  result.status = "timeout";
  console.log("TIMEOUT composer Enter sending");
  void writeFile(`${output}/results.json`, JSON.stringify(result, null, 2)).finally(() => process.exit(1));
}, 240_000);
const lines = (n) => Array.from({ length: n }, (_, i) => `Draft line ${i + 1}`).join("\n");
const after = phase === "after";
let desktop, touchBrowser;
try {
  desktop = await chromium.launch({ ...launch, args: falsePointerFlags });
  for (const colorScheme of ["light", "dark"]) for (const large of [false, true]) {
    const name = `forced-coarse-${colorScheme}${large ? "-large-text" : ""}`;
    const context = await desktop.newContext({ viewport: { width: 390, height: 844 }, locale: "en-US", colorScheme, reducedMotion: "reduce", serviceWorkers: "block" });
    await context.addInitScript(({ colorScheme, large }) => {
      localStorage.setItem("persona.token", "mock");
      localStorage.setItem("persona.language", "en");
      localStorage.setItem("persona.theme", colorScheme);
      if (large) localStorage.setItem("persona.text-size", "large");
    }, { colorScheme, large });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    const sends = [], sendStatuses = [], errors = [], steps = [];
    page.on("pageerror", (error) => errors.push(String(error)));
    page.on("request", (request) => {
      if (request.method() === "POST" && /\/api\/v1\/sessions\/[^/]+\/[^/]+\/messages$/.test(new URL(request.url()).pathname)) sends.push(request.postDataJSON()?.text);
    });
    page.on("response", (response) => {
      if (response.request().method() === "POST" && /\/api\/v1\/sessions\/[^/]+\/[^/]+\/messages$/.test(new URL(response.url()).pathname)) sendStatuses.push(response.status());
    });
    const run = { name, steps, sends, sendStatuses, errors };
    result.runs.push(run);
    await page.goto(new URL("/?token=mock", base).href);
    const collapsed = page.locator("textarea").first();
    await collapsed.waitFor();
    const media = await page.evaluate(() => ({
      anyFine: matchMedia("(any-pointer: fine)").matches,
      primaryFine: matchMedia("(pointer: fine)").matches,
      anyCoarse: matchMedia("(any-pointer: coarse)").matches,
    }));
    run.media = media;
    assert.equal(media.anyFine, false, `${name}: launch flags did not force any-pointer:fine false`);
    const record = async (key, field = collapsed) => {
      await page.waitForTimeout(400);
      const step = { key, sends: sends.length, value: await field.count() ? await field.inputValue() : null, expanded: await page.getByTestId("composer-expanded").count() > 0 };
      steps.push(step);
      console.log(`${phase} ${name} ${key}: ${JSON.stringify(step)}`);
      return step;
    };
    const openExpanded = async () => {
      if (await page.getByTestId("composer-expanded").count()) return;
      await collapsed.fill(lines(9));
      await page.getByTestId("composer-expand").focus();
      await page.keyboard.press("Enter"); // native button activation: no pointer event
      await page.getByTestId("composer-expanded").waitFor();
    };

    await collapsed.fill("main before mouse");
    await collapsed.focus();
    await page.keyboard.press("Enter");
    const mainPlain = await record("main plain Enter before mouse");
    assert.equal(mainPlain.value, "main before mouse\n", `${name}: initial plain Enter must add a line`);
    assert.equal(sends.length, 0, `${name}: initial plain Enter sent`);

    await collapsed.fill("main Ctrl+Enter");
    await page.keyboard.press("Control+Enter");
    await record("main Ctrl+Enter before mouse");
    assert.equal(sends.length, after ? 1 : 0, `${name}: main Ctrl+Enter regression`);

    await openExpanded();
    const expandedField = page.getByTestId("composer-expanded").locator("textarea");
    await expandedField.focus();
    await page.keyboard.press("Enter");
    const expandedPlain = await record("expanded plain Enter before mouse", expandedField);
    assert.equal(expandedPlain.value, `${lines(9)}\n`, `${name}: expanded plain Enter must add a line`);
    assert.equal(sends.length, after ? 1 : 0, `${name}: expanded plain Enter sent`);

    await page.keyboard.press("Control+Enter");
    await record("expanded Ctrl+Enter before mouse", expandedField);
    assert.equal(sends.length, after ? 2 : 0, `${name}: expanded Ctrl+Enter regression`);

    await openExpanded();
    await expandedField.fill(lines(9));
    await expandedField.focus();
    await page.mouse.move(300, 300); // a real mouse pointer event, not a compatibility MouseEvent
    await page.keyboard.press("Enter");
    const expandedAfterMouse = await record("expanded plain Enter after mouse", expandedField);
    assert.equal(expandedAfterMouse.value, `${lines(9)}\n`, `${name}: mouse promotion must not send plain Enter in the editor`);
    assert.equal(sends.length, after ? 2 : 0, `${name}: expanded mouse step sent unexpectedly`);
    await page.screenshot({ path: `${output}/${name}-expanded-${phase}.png` });

    await page.getByTestId("composer-collapse").focus();
    await page.keyboard.press("Enter");
    await page.setViewportSize({ width: 1280, height: 900 });
    await collapsed.fill("main after mouse");
    await collapsed.focus();
    await page.keyboard.press("Enter");
    const mainAfterMouse = await record("main plain Enter after mouse");
    assert.equal(sends.length, after ? 3 : 0, `${name}: mouse promotion regression`);
    if (after) assert.equal(mainAfterMouse.value, "", `${name}: sent main draft was not cleared`);
    else assert.equal(mainAfterMouse.value, "main after mouse\n", `${name}: unmodified mouse behavior was not reproduced`);
    await page.screenshot({ path: `${output}/${name}-main-${phase}.png` });

    assert.deepEqual(errors, [], `${name}: browser errors`);
    const responseDeadline = Date.now() + 5_000;
    while (sendStatuses.length < sends.length && Date.now() < responseDeadline) {
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    assert.equal(sendStatuses.length, sends.length, `${name}: missing send responses ${sendStatuses.length}/${sends.length}`);
    assert.ok(sendStatuses.every((status) => status === 200), `${name}: send failed ${sendStatuses}`);
    if (after) assert.deepEqual(sends, ["main Ctrl+Enter", lines(9), "main after mouse"], `${name}: wrong sent payloads`);
    console.log(`PASS ${phase} ${name}: any-pointer:fine=false; ${sends.length} sends; key-by-key behavior verified`);
    await context.close();
  }
  await desktop.close();
  desktop = undefined;

  // A real touch profile: taps only, never page.mouse. Compatibility mouse events must not promote Enter-to-send.
  touchBrowser = await chromium.launch(launch);
  const context = await touchBrowser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2, locale: "en-US", colorScheme: "light", reducedMotion: "reduce", serviceWorkers: "block" });
  await context.addInitScript(() => {
    localStorage.setItem("persona.token", "mock");
    localStorage.setItem("persona.language", "en");
    localStorage.setItem("persona.theme", "light");
  });
  const page = await context.newPage();
  page.setDefaultTimeout(20_000);
  const sends = [], steps = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && /\/api\/v1\/sessions\/[^/]+\/[^/]+\/messages$/.test(new URL(request.url()).pathname)) sends.push(request.postDataJSON()?.text);
  });
  const run = { name: "touch-only-taps", steps, sends };
  result.runs.push(run);
  await page.goto(new URL("/?token=mock", base).href);
  const input = page.locator("textarea").first();
  await input.waitFor();
  run.media = await page.evaluate(() => ({ anyFine: matchMedia("(any-pointer: fine)").matches, anyCoarse: matchMedia("(any-pointer: coarse)").matches }));
  assert.equal(run.media.anyFine, false, "touch profile unexpectedly reports a fine pointer");
  const record = async (key) => {
    await page.waitForTimeout(300);
    const step = { key, sends: sends.length, value: await input.inputValue() };
    steps.push(step);
    console.log(`${phase} touch-only ${key}: ${JSON.stringify(step)}`);
  };
  await input.tap();
  await page.keyboard.type("touch draft");
  await page.keyboard.press("Enter");
  await record("plain Enter after touch tap");
  assert.equal(await input.inputValue(), "touch draft\n");
  await input.fill(lines(9));
  await page.getByTestId("composer-expand").tap();
  const expanded = page.getByTestId("composer-expanded").locator("textarea");
  await expanded.tap();
  await expanded.evaluate((el) => el.setSelectionRange(el.value.length, el.value.length));
  await page.keyboard.press("Enter");
  assert.equal(await expanded.inputValue(), `${lines(9)}\n`, "touch expanded editor plain Enter must add a line");
  await page.getByTestId("composer-collapse").tap();
  await input.fill("touch after taps");
  await input.tap();
  await input.evaluate((el) => el.setSelectionRange(el.value.length, el.value.length));
  await page.keyboard.press("Enter");
  await record("plain Enter after touch and compatibility mouse events");
  assert.equal(await input.inputValue(), "touch after taps\n", "touch taps promoted Enter-to-send");
  assert.equal(sends.length, 0, "touch-only run sent from the keyboard");
  await page.screenshot({ path: `${output}/touch-only-${phase}.png` });
  await context.close();
  console.log(`PASS ${phase} touch-only taps: any-pointer:fine=false; plain Enter remains a newline; zero sends`);
  result.status = "pass";
} catch (error) {
  result.status = result.status === "timeout" ? "timeout" : "fail";
  result.error = String(error);
} finally {
  clearTimeout(deadline);
  await desktop?.close().catch(() => {});
  await touchBrowser?.close().catch(() => {});
  await writeFile(`${output}/results.json`, JSON.stringify(result, null, 2));
  console.log(`${result.status.toUpperCase()} composer Enter sending ${phase}${result.error ? `: ${result.error}` : ""}`);
  process.exit(result.status === "pass" ? 0 : 1);
}
