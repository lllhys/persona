// Service worker + running page against a simulated sign-in layer in front of the hub (#2508).
//   npm run build && node e2e/edge-auth.mjs [artifact-directory]
// Uses the real built /sw.js (web/dist) behind a loopback server that plays the edge: it can pass the app through, answer
// with a cross-origin 302, a same-origin 302 to a sign-in sub-path, or a 200 sign-in HTML page. For each interception the
// page is loaded once (the worker installs and caches the shell), the edge starts intercepting, and the reload must end
// on the sign-in address, never on the cached shell. Offline must still show the cached shell. A running page must
// reload into the sign-in by itself once its socket fails and the probe sees the interception, and must not reload
// repeatedly. Exits nonzero on failure; the last line is the verdict. Needs Chrome (playwright-core, channel chrome).
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const dist = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../dist");
const out = process.argv[2];
if (out) fs.mkdirSync(out, { recursive: true });
const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".webmanifest": "application/manifest+json", ".json": "application/json", ".svg": "image/svg+xml", ".woff2": "font/woff2" };
const LOGIN_HTML = "<!doctype html><html><head><title>Edge Sign In</title></head><body>sign in here</body></html>";

let mode = "open";
const hits = { appRequestsWhileIntercepted: 0 };
const listen = (handler) => new Promise((res) => { const s = http.createServer(handler); s.on("upgrade", (_q, sock) => sock.destroy()); s.listen(0, "127.0.0.1", () => res(s)); });

const login = await listen((_q, r) => { r.writeHead(200, { "Content-Type": "text/html" }); r.end(LOGIN_HTML); });
const loginOrigin = `http://127.0.0.1:${login.address().port}`;
let appOrigin = "";

const edge = await listen((req, res) => {
  const url = new URL(req.url, "http://x");
  if (mode === "same" && url.pathname === "/auth/login") { res.writeHead(200, { "Content-Type": "text/html" }); return res.end(LOGIN_HTML); }
  if (mode === "cross") { hits.appRequestsWhileIntercepted++; res.writeHead(302, { Location: `${loginOrigin}/login` }); return res.end(); }
  if (mode === "same") { hits.appRequestsWhileIntercepted++; res.writeHead(302, { Location: "/auth/login" }); return res.end(); }
  if (mode === "html") { hits.appRequestsWhileIntercepted++; res.writeHead(200, { "Content-Type": "text/html" }); return res.end(LOGIN_HTML); }
  if (url.pathname.startsWith("/api/")) { res.writeHead(200, { "Content-Type": "application/json" }); return res.end("{}"); }
  let file = path.join(dist, url.pathname === "/" ? "index.html" : url.pathname);
  if (!file.startsWith(dist) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, "index.html");
  res.writeHead(200, { "Content-Type": types[path.extname(file)] ?? "application/octet-stream" });
  fs.createReadStream(file).pipe(res);
});
appOrigin = `http://127.0.0.1:${edge.address().port}`;

const browser = await chromium.launch({ channel: "chrome", headless: true });
const results = [];
const check = (name, ok, detail = "") => { results.push({ name, ok, detail }); console.log(`${ok ? "PASS" : "FAIL"} ${name}${detail ? " — " + detail : ""}`); };

async function fresh() {
  mode = "open";
  const ctx = await browser.newContext();
  await ctx.addInitScript(() => { try { if (!localStorage.getItem("persona.token")) localStorage.setItem("persona.token", "edge-test"); } catch {} });
  const page = await ctx.newPage();
  await page.goto(appOrigin + "/", { waitUntil: "load" });
  // the worker installs, claims the page and keeps the shell
  await page.waitForFunction(async () => {
    const reg = await navigator.serviceWorker.getRegistration();
    if (!reg?.active || !navigator.serviceWorker.controller) return false;
    const c = await caches.open("persona-shell-v2");
    return Boolean(await c.match("/"));
  }, null, { timeout: 30000 });
  await page.reload({ waitUntil: "load" });
  const title = await page.title();
  if (title !== "Persona") throw new Error("shell did not load before interception: " + title);
  return { ctx, page };
}

const shellCacheIsApp = (page) => page.evaluate(async () => {
  const r = await (await caches.open("persona-shell-v2")).match("/");
  const t = r ? await r.text() : "";
  return t.includes("/assets/");
});

try {
  for (const m of ["cross", "same", "html"]) {
    const { ctx, page } = await fresh();
    // 1. navigation: the service worker must not answer with the cached shell
    mode = m;
    await page.reload({ waitUntil: "load" }).catch(() => {});
    const url = page.url();
    const title = await page.title();
    const expected = m === "cross" ? url.startsWith(loginOrigin + "/login") : m === "same" ? url.endsWith("/auth/login") : url === appOrigin + "/";
    check(`${m}: reload shows the sign-in, not the cached shell`, expected && title === "Edge Sign In", `url=${url} title=${title}`);
    mode = "open";
    // 2. the intercepted answer must not have replaced the cached shell
    const p2 = await ctx.newPage();
    await p2.goto(appOrigin + "/", { waitUntil: "load" });
    check(`${m}: cached shell is still the app after the interception`, await shellCacheIsApp(p2));
    await ctx.close();
  }

  // offline still shows the cached shell
  {
    const { ctx, page } = await fresh();
    await ctx.setOffline(true);
    await page.reload({ waitUntil: "load" }).catch(() => {});
    check("offline: the cached shell is shown", (await page.title()) === "Persona", `title=${await page.title()}`);
    await ctx.setOffline(false);
    await ctx.close();
  }

  // a running page notices by itself: its socket fails, the probe sees the interception, one reload
  for (const m of ["cross", "same", "html"]) {
    const { ctx, page } = await fresh();
    let navs = 0;
    page.on("framenavigated", (f) => { if (f === page.mainFrame()) navs++; });
    await page.waitForTimeout(1500); // the socket has failed a few times against the open edge: no reload
    check(`${m}: a healthy edge makes the page reload nothing`, navs === 0, `navigations=${navs}`);
    mode = m;
    const ok = await page.waitForFunction((o) => !location.origin.startsWith(o) || document.title === "Edge Sign In", appOrigin, { timeout: 20000 }).then(() => true, () => false);
    check(`${m}: the running page reloads into the sign-in by itself`, ok, `url=${page.url()}`);
    // no reload loop once there (cross-origin pages have no app code; same-origin html/same stay on the login page)
    const before = hits.appRequestsWhileIntercepted;
    await page.waitForTimeout(3000);
    check(`${m}: no retry loop against the intercepting edge`, hits.appRequestsWhileIntercepted - before <= 2, `requests in 3s=${hits.appRequestsWhileIntercepted - before}`);
    if (out) await page.screenshot({ path: path.join(out, `running-${m}.png`) });
    mode = "open";
    await ctx.close();
  }
} catch (e) {
  check("runner", false, String(e?.stack ?? e));
} finally {
  await browser.close();
  edge.close();
  login.close();
}
if (out) fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 2));
const failed = results.filter((r) => !r.ok);
console.log(failed.length ? `edge-auth: FAIL (${failed.length} of ${results.length})` : `edge-auth: PASS (${results.length} checks)`);
process.exit(failed.length ? 1 : 0);
