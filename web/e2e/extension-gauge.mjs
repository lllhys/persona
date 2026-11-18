// UI version 9's gauge (#9337) in a real browser against the mock hub: a made-up gateway's "Quota" page, a row of status
// tiles and a card of three rings that jsdom cannot lay out. Synthetic data only; it writes nothing to the hub.
//
// Start the mock with MOCK_EXTENSIONS=1 MOCK_EXTENSION_GAUGE=1 and Vite (or `vite preview`) pointing at it
// (PERSONA_HUB_URL).
// node web/e2e/extension-gauge.mjs <loopback web URL> <artifact directory> [Chromium executable]
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [base, shots, executable] = process.argv.slice(2);
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/extension-gauge.mjs <loopback web URL> <artifact directory> [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: extension-gauge timed out (180s)");
  process.exit(124);
}, 180_000);
const root = base.replace(/\/$/, "");
const PAGE = "/extension/example-quota/quota";
const RINGS = [
  { title: "5 hours", value: "4.69", label: "5 hours: 4.69 USD, 94%", offset: 6, hue: "green" },
  { title: "This week", value: "38.20", label: "This week: 38.20 USD, 76%", offset: 24, hue: "green" },
  { title: "Fable this week", value: "0.62", label: "Fable this week: 0.62 USD, 12%", offset: 88, hue: "amber" },
];
const results = [];
const check = (name, ok, detail) => {
  results.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${detail === undefined ? "" : ` ${JSON.stringify(detail)}`}`);
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
async function page({ width, height, theme, textSize, reducedMotion = "no-preference" }) {
  const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme, reducedMotion, deviceScaleFactor: 2, serviceWorkers: "block" });
  await context.addInitScript(([theme, textSize]) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "en");
    if (textSize) localStorage.setItem("persona.text-size", textSize);
  }, [theme, textSize]);
  // nothing leaves the loopback origin
  await context.route("**/*", (route) => (new URL(route.request().url()).origin === new URL(root).origin ? route.continue() : route.abort()));
  const p = await context.newPage();
  await p.goto(`${root}/?token=mock`);
  await p.waitForFunction(() => localStorage.getItem("persona.token"));
  await p.goto(root + PAGE);
  await p.locator("[data-gauge]").nth(2).waitFor({ timeout: 20_000 });
  // the tiles' fade-up and the rings' arcs come to rest (the app's busy loops go on, and are not waited for)
  await p.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity));
  return p;
}

// Each ring's tile, ring, arc and texts as the browser laid them out, with the colors it resolved.
const measure = (p) =>
  p.evaluate(() => {
    const box = (e) => {
      const r = e.getBoundingClientRect();
      return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) };
    };
    const probe = document.createElement("span");
    document.body.append(probe);
    const resolve = (name) => {
      probe.style.color = `var(--hue-${name})`;
      return getComputedStyle(probe).color;
    };
    const rings = [...document.querySelectorAll("[data-gauge]")].map((face) => {
      const tile = face.closest("[role=meter], button");
      const ring = face.firstElementChild;
      const arc = face.querySelector("[data-gauge-arc]");
      const value = ring.querySelector(":scope > span > span");
      const title = face.children[1];
      return {
        title: title.textContent,
        value: value.textContent,
        label: tile.getAttribute("aria-label"),
        role: tile.getAttribute("role") ?? tile.tagName.toLowerCase(),
        now: tile.getAttribute("aria-valuenow"),
        tile: box(tile.closest(".rounded-card")),
        ring: box(ring),
        valueBox: box(value),
        cut: value.scrollWidth > value.clientWidth,
        titleBox: box(title),
        offset: Number(arc?.getAttribute("stroke-dashoffset")),
        stroke: arc ? getComputedStyle(arc).stroke : "",
        transition: arc ? getComputedStyle(arc).transitionDuration : "",
      };
    });
    const hues = { green: resolve("green"), amber: resolve("amber") };
    probe.remove();
    const status = [...document.querySelectorAll("[role=group][aria-label]")].filter((g) => !g.closest("nav")).map((g) => ({ label: g.getAttribute("aria-label"), box: box(g) }));
    const footer = [...document.querySelectorAll("p, div")].find((e) => e.children.length === 0 && e.textContent.startsWith("5 hours resets in"));
    return { rings, hues, status, footer: footer ? box(footer) : null };
  });

const near = (a, b, d = 2) => Math.abs(a - b) <= d;

function common(m, tag) {
  check(`${tag}: three rings in order`, m.rings.map((r) => r.title).join("|") === RINGS.map((r) => r.title).join("|"), m.rings.map((r) => r.title));
  for (const [i, want] of RINGS.entries()) {
    const r = m.rings[i];
    if (!r) continue;
    check(`${tag}: ${want.title} reads "${want.label}"`, r.label === want.label && r.value === want.value, { label: r.label, value: r.value, role: r.role });
    check(`${tag}: ${want.title}'s arc to ${100 - want.offset}% in ${want.hue}`, r.offset === want.offset && r.stroke === m.hues[want.hue], { offset: r.offset, stroke: r.stroke, hue: m.hues[want.hue] });
    const cx = r.ring.x + r.ring.w / 2;
    check(`${tag}: ${want.title}'s value centred in its ring, whole, its title below`,
      near(r.valueBox.x + r.valueBox.w / 2, cx) && r.valueBox.y > r.ring.y && r.valueBox.y + r.valueBox.h < r.ring.y + r.ring.h && !r.cut && r.titleBox.y >= r.ring.y + r.ring.h && near(r.titleBox.x + r.titleBox.w / 2, cx),
      { ring: r.ring, value: r.valueBox, cut: r.cut, title: r.titleBox });
    check(`${tag}: ${want.title}'s ring inside its tile`, r.ring.x >= r.tile.x && r.ring.x + r.ring.w <= r.tile.x + r.tile.w && r.ring.y >= r.tile.y, { ring: r.ring, tile: r.tile });
  }
  const [still, , pressable] = m.rings;
  check(`${tag}: a ring that does not press is a meter, one that does a button`, still?.role === "meter" && still.now === "94" && pressable?.role === "button", { still: still?.role, now: still?.now, pressable: pressable?.role });
  const bottom = Math.max(...m.rings.map((r) => r.tile.y + r.tile.h));
  check(`${tag}: the reset times under the rings`, m.footer && m.footer.y >= bottom, { footer: m.footer, bottom });
}

let failed = false;
try {
  for (const theme of ["light", "dark"]) {
    // 1440 × 900: the four status tiles in one row, the three rings in one row under them, as wide as each other
    {
      const p = await page({ width: 1440, height: 900, theme });
      const m = await measure(p);
      common(m, `1440 ${theme}`);
      const status = m.status.slice(0, 4);
      check(`1440 ${theme}: four status tiles in one row above the rings`, status.length === 4 && new Set(status.map((s) => s.box.y)).size === 1 && status[0].box.y < m.rings[0].tile.y, status);
      check(`1440 ${theme}: the rings side by side, as wide as each other`, new Set(m.rings.map((r) => r.tile.y)).size === 1 && new Set(m.rings.map((r) => r.tile.w)).size === 1 && m.rings[0].tile.x < m.rings[1].tile.x, m.rings.map((r) => r.tile));
      check(`1440 ${theme}: the arc eases (no Reduce Motion)`, m.rings[0].transition === "0.5s", m.rings[0].transition);
      await p.screenshot({ path: join(shots, `quota-1440-${theme}.png`) });
      if (theme === "light") {
        await p.getByRole("button", { name: RINGS[2].label }).click();
        const toast = await p.getByText("Fable this week: 0.62 USD left").waitFor({ timeout: 10_000 }).then(() => true, () => false);
        check("1440 light: pressing the low ring does its action", toast);
      }
      await p.context().close();
    }
    // 390 × 844: two tiles a line, the third ring under the first, every ring whole inside its tile
    {
      const p = await page({ width: 390, height: 844, theme });
      const m = await measure(p);
      common(m, `390 ${theme}`);
      const [a, b, c] = m.rings.map((r) => r.tile);
      check(`390 ${theme}: two rings a line, the third under the first`, a.y === b.y && a.x < b.x && c.y > a.y && c.x === a.x, { a, b, c });
      await p.screenshot({ path: join(shots, `quota-390-${theme}.png`) });
      await p.context().close();
    }
  }
  // Settings › Appearance's Large text: the values still whole and centred
  for (const [width, height] of [[1440, 900], [390, 844]]) {
    const p = await page({ width, height, theme: "light", textSize: "large" });
    common(await measure(p), `${width} large text`);
    await p.screenshot({ path: join(shots, `quota-${width}-large-light.png`) });
    await p.context().close();
  }
  // Reduce Motion: the arc snaps
  {
    const p = await page({ width: 1440, height: 900, theme: "light", reducedMotion: "reduce" });
    const m = await measure(p);
    check("reduce motion: the arc does not ease", m.rings.every((r) => parseFloat(r.transition) <= 0.001), m.rings.map((r) => r.transition));
    await p.context().close();
  }
} catch (e) {
  failed = true;
  console.error(e);
} finally {
  await browser.close();
  clearTimeout(watchdog);
}
writeFileSync(join(shots, "results.json"), JSON.stringify(results, null, 2));
const bad = results.filter((r) => !r.ok);
console.log(`RESULT: ${failed || bad.length ? "FAIL" : "PASS"} (${results.length - bad.length}/${results.length} checks)`);
process.exit(failed || bad.length ? 1 : 0);
