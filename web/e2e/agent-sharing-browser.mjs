// Drive only newly created CDP contexts. No existing browser targets, credentials or real hub are accessed.
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import assert from "node:assert/strict";
const [app = "http://127.0.0.1:10305", publicOrigin = "http://127.0.0.1:10306", output] = process.argv.slice(2);
if (!output) throw new Error("Pass an evidence directory");
for (const origin of [app, publicOrigin]) if (new URL(origin).hostname !== "127.0.0.1") throw new Error("Synthetic loopback origins only");
assert.notEqual(new URL(app).origin, new URL(publicOrigin).origin);
mkdirSync(output, { recursive: true });
// A dedicated headless browser by default (PERSONA_SHARE_CDP); never an existing page of a shared one.
const cdp = process.env.PERSONA_SHARE_CDP || "http://127.0.0.1:9222";
if (new URL(cdp).hostname !== "127.0.0.1") throw new Error("Loopback CDP only");
const version = await (await fetch(`${cdp}/json/version`)).json();
const ws = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((resolve, reject) => { ws.addEventListener("open", resolve, { once: true }); ws.addEventListener("error", reject, { once: true }); });
let counter = 0; const calls = new Map(); const events = []; const owned = [];
ws.addEventListener("message", ({ data }) => { const result = JSON.parse(data); if (result.id) { const call = calls.get(result.id); if (call) { calls.delete(result.id); clearTimeout(call.timeout); result.error ? call.reject(new Error(result.error.message)) : call.resolve(result.result); } } else events.push(result); });
function send(method, params = {}, sessionId) { return new Promise((resolve, reject) => { const id = ++counter; const timeout = setTimeout(() => { calls.delete(id); reject(new Error(`CDP timeout: ${method}`)); }, 30000); calls.set(id, { resolve, reject, timeout }); ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) })); }); }
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function page() {
  const { browserContextId } = await send("Target.createBrowserContext", { disposeOnDetach: false });
  owned.push(browserContextId);
  const { targetId } = await send("Target.createTarget", { url: "about:blank", browserContextId });
  const { sessionId } = await send("Target.attachToTarget", { targetId, flatten: true });
  const call = (method, params) => send(method, params, sessionId);
  await call("Page.enable"); await call("Runtime.enable"); await call("Network.enable");
  const evaluate = async (expression) => { const r = await call("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }); if (r.exceptionDetails) throw new Error(r.exceptionDetails.text); return r.result.value; };
  const wait = async (expression, timeout = 30000) => { const deadline = Date.now() + timeout; while (Date.now() < deadline) { try { if (await evaluate(expression)) return; } catch {} await delay(100); } throw new Error(`Browser condition timed out: ${expression}`); };
  const click = async (selector) => {
    await call("Page.bringToFront");
    await wait(`!!document.querySelector(${JSON.stringify(selector)})`);
    const box = await evaluate(`(()=>{const el=document.querySelector(${JSON.stringify(selector)});el.scrollIntoView({block:'center'});const b=el.getBoundingClientRect();return {x:b.x+b.width/2,y:b.y+b.height/2};})()`);
    await call("Input.dispatchMouseEvent", { type: "mouseMoved", ...box });
    await call("Input.dispatchMouseEvent", { type: "mousePressed", ...box, button: "left", clickCount: 1 }); await call("Input.dispatchMouseEvent", { type: "mouseReleased", ...box, button: "left", clickCount: 1 });
  };
  const button = async (text) => { await wait(`Array.from(document.querySelectorAll('button,[role=menuitem]')).some(e=>e.textContent.trim()===${JSON.stringify(text)})`); await evaluate(`(()=>{const e=Array.from(document.querySelectorAll('button,[role=menuitem]')).find(e=>e.textContent.trim()===${JSON.stringify(text)});e.setAttribute('data-e2e-click','yes');})()`); await click('[data-e2e-click="yes"]'); await evaluate("document.querySelector('[data-e2e-click]')?.removeAttribute('data-e2e-click')"); };
  const navigate = async (url) => { await call("Page.bringToFront"); await call("Page.navigate", { url }); await wait("document.readyState==='complete'"); };
  const shot = async (name) => { const { data } = await call("Page.captureScreenshot", { format: "png", captureBeyondViewport: false }); writeFileSync(resolve(output, `${name}.png`), Buffer.from(data, "base64")); };
  return { call, evaluate, wait, click, button, navigate, shot, browserContextId, targetId, sessionId };
}
const capability = "A".repeat(64);
try {
  for (const scheme of ["light", "dark"]) for (const narrow of [false, true]) {
    await fetch(`${app}/fixture/reset`, { method: "POST" });
    const p = await page(); await p.call("Emulation.setDeviceMetricsOverride", { width: narrow ? 390 : 1280, height: narrow ? 844 : 900, deviceScaleFactor: 1, mobile: narrow });
    await p.call("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: scheme }] });
    const suffix = `${narrow ? "narrow" : "wide"}-${scheme}`;
    await p.navigate(`${publicOrigin}/share/${capability}`); await p.wait("document.querySelectorAll('.public-message').length===100");
    assert.equal(await p.evaluate("!!document.querySelector('a,img,iframe,video,audio,canvas,link[rel=manifest]')"), false);
    assert.equal(await p.evaluate("!!document.querySelector('details[open]')"), false);
    assert.equal(await p.evaluate("!!navigator.serviceWorker.controller"), false);
    assert.equal(await p.evaluate("localStorage.length"), 0);
    assert.equal(await p.evaluate("document.documentElement.scrollWidth>innerWidth"), false);
    await p.shot(`viewer-${suffix}`);
    await p.evaluate("document.documentElement.style.fontSize='32px'"); await p.shot(`viewer-large-${suffix}`);
    assert.equal(await p.evaluate("document.documentElement.scrollWidth>innerWidth"), false);
    await p.evaluate("document.documentElement.style.fontSize=''");
    await p.click("summary"); assert.equal(await p.evaluate("document.querySelector('details').open"), true);
    await p.evaluate("document.querySelector('details').closest('article').scrollIntoView({block:'start'})"); await p.shot(`viewer-tools-open-${suffix}`);
    await p.button("Load more"); await p.wait("document.querySelectorAll('.public-message').length===101");
    assert.equal(await p.evaluate("document.querySelector('details').open"), true);
    await p.call("Network.setCacheDisabled", { cacheDisabled: true });
    const requests = events.filter((e) => e.sessionId === p.sessionId && e.method === "Network.requestWillBeSent").map((e) => e.params.request);
    assert(requests.every((r) => new URL(r.url).origin === new URL(publicOrigin).origin));
    assert(requests.filter((r) => r.url.includes('/api/public/')).every((r) => !r.headers.Authorization && !r.headers.Cookie));
    const m = await page(); await m.call("Emulation.setDeviceMetricsOverride", { width: narrow ? 390 : 1280, height: narrow ? 844 : 900, deviceScaleFactor: 1, mobile: narrow });
    await m.call("Page.addScriptToEvaluateOnNewDocument", { source: `if(location.origin===${JSON.stringify(app)}){localStorage.setItem('persona.token','synthetic-credential');localStorage.setItem('persona.language','en');localStorage.setItem('persona.theme',${JSON.stringify(scheme)});}` });
    await m.call("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: scheme }] });
    await m.navigate(`${app}/s/synthetic-computer/synthetic-session`); await m.wait("!!document.querySelector('header button[aria-label=\"More\"]')");
    await m.click('header button[aria-label="More"]'); await m.button("Share…"); await m.wait("!!document.querySelector('[role=dialog]')"); await m.button("New link");
    await m.wait("document.querySelectorAll('.share-messages').length>0 || document.querySelectorAll('.share-stack article').length===100");
    assert.equal(await m.evaluate("document.querySelectorAll('[role=switch][aria-checked=true]').length"), 0);
    assert.equal(await m.evaluate("document.querySelector('select.share-field').value"), "3");
    await m.shot(`settings-${suffix}`);
    await m.evaluate("document.documentElement.style.setProperty('--text-scale','2')");
    await m.shot(`settings-large-${suffix}`);
    assert.equal(await m.evaluate("document.documentElement.scrollWidth>innerWidth"), false);
    await m.evaluate("document.documentElement.style.fontSize='';document.documentElement.style.removeProperty('--text-scale')");
    // The first page is not the selection authority: all 101 rows must appear in the prepared preview.
    await m.button("Preview"); await m.wait("document.body.innerText.includes('Sanitized preview')");
    await m.evaluate("(()=>{const buttons=[...document.querySelectorAll('button')].filter(b=>b.textContent.trim()==='Load more');buttons.at(-1).setAttribute('data-preview-more','yes')})()"); await m.click('[data-preview-more="yes"]');
    await m.wait("document.querySelector('.share-messages').querySelectorAll('article').length===101");
    const acks = await m.evaluate("[...document.querySelectorAll('.share-selection input[type=checkbox]')].map(e=>e.id)");
    assert.equal(acks.length, 2); await m.evaluate(`document.getElementById(${JSON.stringify(acks[1])}).scrollIntoView({block:'center'})`); await m.shot(`preview-http-warning-${suffix}`); await m.click(`[id="${acks[0]}"]`);
    assert.equal(await m.evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='Create link').disabled"), true);
    await m.click(`[id="${acks[1]}"]`); await m.button("Create link"); await m.wait("!!document.querySelector('[data-sharing-link]')"); await m.shot(`created-${suffix}`);
    assert.equal(await m.evaluate("document.querySelector('[data-sharing-link]').textContent"), `${publicOrigin}/share/${capability}`);
    await p.navigate(`${publicOrigin}/share/${capability}`); await p.wait("document.querySelectorAll('.public-message').length===100");
    assert.equal(await p.evaluate("!!document.querySelector('details')"), false);
    assert.equal(await p.evaluate("document.body.innerText.includes('Snapshot')"), true);
    await m.button("Existing links"); await m.wait("document.body.innerText.includes('101 messages shared')"); assert.equal(await m.evaluate("!!document.querySelector('[data-sharing-link]')"), false);
    await m.shot(`list-${suffix}`);
    await m.button("Edit"); await m.wait("document.body.innerText.includes('Shared messages')"); await m.shot(`editor-${suffix}`);
    await m.button("Revoke link"); await m.wait("document.querySelectorAll('[role=dialog]').length===2"); await m.shot(`revoke-${suffix}`);
    await m.evaluate("[...document.querySelectorAll('button')].filter(b=>b.textContent.trim()==='Revoke link').at(-1).setAttribute('data-revoke-confirm','yes')"); await m.click('[data-revoke-confirm="yes"]'); await m.wait("document.body.innerText.includes('Revoked')");
    await p.navigate(`${publicOrigin}/share/${capability}`); await p.wait("document.body.innerText.includes(\"This link doesn't exist or is no longer available\")"); assert.equal(await p.evaluate("document.querySelectorAll('.public-message').length"), 0); await p.shot(`viewer-unavailable-${suffix}`);
    assert.equal(events.filter((e) => [p.sessionId, m.sessionId].includes(e.sessionId) && e.method === "Runtime.exceptionThrown").length, 0);
    console.log(`PASS browser viewer/settings/list/editor/revoke ${suffix}`);
  }
  // Real baseline worker: prove the old first-navigation failure and separate-origin default, not merely new route rules.
  for (const mode of ["ok", "failed", "offline"]) {
    await fetch(`${app}/fixture/mode`, { method: "POST", body: JSON.stringify({ mode: "ok" }) });
    const old = await page(); await old.navigate(`${app}/legacy-shell`);
    await old.evaluate("navigator.serviceWorker.register('/legacy-worker.js',{scope:'/'}).then(()=>navigator.serviceWorker.ready)"); await old.wait("!!navigator.serviceWorker.controller");
    await old.evaluate("caches.open('persona-shell-v2').then(c=>c.put('/',new Response('<!doctype html><h1 id=poison>Cached authenticated shell</h1>',{headers:{'Content-Type':'text/html'}})))");
    await fetch(`${app}/fixture/mode`, { method: "POST", body: JSON.stringify({ mode }) });
    await old.navigate(`${app}/share/${capability}`);
    if (mode === "ok") { await old.wait("!!document.querySelector('#viewer')"); assert.equal(await old.evaluate("caches.match('/',{cacheName:'persona-shell-v2'}).then(r=>r.text()).then(t=>t.includes('id=poison'))"), true); }
    else { await old.wait("!!document.querySelector('#poison')"); }
    const isolated = await page(); await isolated.navigate(`${publicOrigin}/share/${capability}`); await isolated.wait("!!document.querySelector('#viewer')");
    assert.equal(await isolated.evaluate("!!navigator.serviceWorker.controller || !!document.querySelector('#poison')"), false);
    assert.equal(await isolated.evaluate("caches.keys().then(k=>k.length)"), 0);
    console.log(`PASS real legacy-worker first navigation ${mode}; isolated origin cannot use poisoned app cache`);
  }
  await fetch(`${app}/fixture/mode`, { method: "POST", body: JSON.stringify({ mode: "ok" }) });
  const current = await page(); await current.navigate(`${app}/legacy-shell`);
  await current.evaluate("navigator.serviceWorker.register('/sw.js',{scope:'/'}).then(()=>navigator.serviceWorker.ready)"); await current.wait("!!navigator.serviceWorker.controller");
  await current.evaluate("caches.open('persona-shell-v2').then(c=>c.put('/',new Response('<h1 id=poison>Cached shell</h1>',{headers:{'Content-Type':'text/html'}})))");
  await fetch(`${app}/fixture/mode`, { method: "POST", body: JSON.stringify({ mode: "failed" }) });
  await current.navigate(`${app}/share/${capability}`); assert.equal(await current.evaluate("!!document.querySelector('#poison')"), false);
  await fetch(`${app}/fixture/mode`, { method: "POST", body: JSON.stringify({ mode: "ok" }) });
  console.log("PASS current worker public first-navigation pass-through; no cached authenticated shell");
  console.log("PASS synthetic browser acceptance");
} catch (error) { console.error(`FAIL synthetic browser acceptance: ${error.message}`); process.exitCode = 1; }
finally {
  // Dispose only the contexts this run created.
  for (const browserContextId of owned) await send("Target.disposeBrowserContext", { browserContextId }).catch(() => {});
  ws.close();
}
