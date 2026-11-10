// Local synthetic HTTP/WS acceptance fixture. It never contacts a hub and never logs bodies, credentials or capabilities.
import { createServer } from "node:http";
import { createHash } from "node:crypto";
import { readFileSync, existsSync, statSync } from "node:fs";
import { dirname, resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { build } from "esbuild";
const web = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const [mainPort = "10305", publicPort = "10306"] = process.argv.slice(2);
const machine = "synthetic-computer", sessionID = "synthetic-session", manager = "assistant";
const token = "A".repeat(64);
const stamp = () => new Date().toISOString();
const until = () => new Date(Date.now() + 3 * 86400000).toISOString();
const human = "Review the fictional release checklist. Keep the summary concise.";
const answer = "The fictional release is ready for review.\n\nThe test plan covers fresh installation, reconnecting, and recovering an interrupted request. The remaining step is a human review of the release notes.\n\nLiteral text stays inert: <img src=x onerror=alert(1)> and https://example.test are not active content.";
const session = { machine_id: machine, session_id: sessionID, manager, title: "Fictional release checklist", cwd: "/tmp/synthetic-project", state: "idle", status: "idle", updated_at: stamp(), agent: "claude", capabilities: { read: true, send: true }, running: false };
const messages = [
  { message_id: "message-1", role: "user", text: human },
  { message_id: "message-2", role: "assistant", text: answer, execution: [{ kind: "text", text: "I checked the fictional test plan." }, { kind: "tool", tool_id: "work-1", name: "Synthetic check", arguments: '{"target":"fictional checklist"}', results: [{ text: "Three fictional cases are documented." }] }, { kind: "agent", tool_id: "work-2", text: "Synthetic child report: the checklist is consistent." }] },
  ...Array.from({ length: 99 }, (_, i) => ({ message_id: `message-${i + 3}`, role: i % 2 ? "assistant" : "user", text: `Synthetic historical message ${i + 3}.` })),
];
let shares = []; let selections = new Map(); let activePublicShare = null; let prepared = new Map(); let previewCounter = 0; let creation = 0; let mode = "ok";
const publicHeaders = { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, noarchive", "Content-Security-Policy": "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'; worker-src 'none'" };
const json = (res, data, status = 200) => { res.writeHead(status, { ...publicHeaders, "Content-Type": "application/json" }); res.end(JSON.stringify(data)); };
const project = (rows, tools = false) => rows.map(({ execution, ...m }) => ({ ...m, ...(tools && execution ? { execution } : {}) }));
const choose = (selection) => selection.mode === "all" ? messages : selection.mode === "from" ? messages.slice(messages.findIndex((m) => m.message_id === selection.from_message_id)) : messages.filter((m) => selection.message_ids.includes(m.message_id));
const paginate = (rows, cursor) => { const at = cursor ? Number(cursor.split("-").at(-1)) : 0; return { messages: rows.slice(at, at + 100), next_cursor: at + 100 < rows.length ? `cursor-${at + 100}` : null }; };
async function api(req, res, url, publicHost) {
  const p = url.pathname; let raw = ""; for await (const part of req) raw += part; const body = raw ? JSON.parse(raw) : {};
  if (p === "/fixture/mode" && !publicHost) { mode = body.mode || "ok"; return json(res, {}); }
  if (p === "/fixture/reset" && !publicHost) { shares = []; selections = new Map(); activePublicShare = null; prepared = new Map(); creation = 0; mode = "ok"; return json(res, {}); }
  if (p.startsWith("/api/public/agent-shares/")) {
    if (mode === "missing") return json(res, {}, 404);
    if (mode === "failed") return json(res, {}, 502);
    const share = shares.find((s) => s.share_id === activePublicShare);
    if (activePublicShare && (!share || share.status !== "active" || Date.parse(share.expires_at) <= Date.now())) return json(res, {}, 404);
    return json(res, { revision: share?.revision ?? 1, content_version: share?.content_version ?? 1, expires_at: share?.expires_at ?? until(), publication_status: share?.publication_status ?? "live", poll_after_seconds: share?.include_future === false ? 30 : 15, ...paginate(project(share ? selections.get(share.share_id) : messages, share?.show_tools ?? true), url.searchParams.get("cursor")) });
  }
  if (publicHost) return json(res, {}, 404);
  // Management requests name their assistant explicitly and never carry cookies.
  if ((p.includes("/share-") || p.startsWith("/api/v1/agent-shares")) && (req.headers["x-persona-manager"] !== manager || req.headers.cookie)) return json(res, { code: "manager_required" }, 400);
  if (p.includes("share-catalog")) return json(res, { can_share: true, reason: null, can_include_future: true, snapshot_id: "snapshot-1", snapshot_version: "snapshot-version-1", expires_at: until(), complete: true, total_messages: 101, omitted_messages: 2, omissions: ["unknown_provenance", "unfinished_turn"], ...paginate(project(messages).map((m) => ({ ...m, selectable: true })), url.searchParams.get("cursor")) });
  if (p.includes("share-preview")) {
    let id = p.split("/").at(-1), entry;
    if (req.method === "POST") { id = `preview-${++previewCounter}`; entry = { policy: body, expires_at: until(), rows: project(body.selection ? choose(body.selection) : messages.slice(0, 2), body.show_tools ?? shares.find((s) => s.share_id === body.share_id)?.show_tools) }; prepared.set(id, entry); }
    else entry = prepared.get(id);
    if (!entry) return json(res, { code: "preview_expired" }, 409);
    return json(res, { preview_id: id, policy_version: 1, expires_at: entry.expires_at, requires_insecure_transport_ack: true, transport_warning: "HTTP sends the bearer link and shared content without encryption. A network observer may obtain them. Acknowledge this risk before creating or widening the share.", selected_count: entry.rows.length, omitted_count: 2, ...paginate(entry.rows, url.searchParams.get("cursor")) });
  }
  if (p === "/api/v1/agent-shares") {
    if (req.method === "POST") {
      if (!body.acknowledgement?.insecure_transport || !prepared.has(body.acknowledgement?.preview_id)) return json(res, { code: "preview_required" }, 409);
      const rows = choose(body.selection); const share = { share_id: `share-${++creation}`, revision: 1, content_version: 1, selection: { mode: body.selection.mode, count: rows.length }, include_future: body.include_future, show_tools: body.show_tools, created_at: stamp(), updated_at: stamp(), expires_at: new Date(Date.now() + body.expires_in_days * 86400000).toISOString(), status: "active", publication_status: body.include_future ? "live" : "frozen", pause_reason: null };
      shares.push(share); selections.set(share.share_id, rows); activePublicShare = share.share_id; return json(res, { share, public_url: `http://127.0.0.1:${publicPort}/share/${token}` }, 201);
    }
    return json(res, { shares });
  }
  if (p.startsWith("/api/v1/agent-shares/")) {
    const id = p.split("/")[4], share = shares.find((s) => s.share_id === id); if (!share) return json(res, {}, 404);
    if (p.endsWith("/messages")) return json(res, { revision: share.revision, content_version: share.content_version, ...paginate(project(share.status === "active" ? selections.get(id) : [], share.show_tools), url.searchParams.get("cursor")) });
    if (req.method === "PATCH" || req.method === "DELETE") {
      if (body.expected_revision !== share.revision) return json(res, { code: "revision_conflict" }, 409);
      share.revision++; share.content_version++;
      if (req.method === "DELETE") { share.status = "revoked"; return json(res, { share_id: id, revision: share.revision, status: "revoked" }); }
      for (const key of ["show_tools", "include_future", "expires_at"]) if (key in body) share[key] = body[key];
      if (body.selection) { const rows = choose(body.selection); selections.set(id, rows); share.selection = { mode: body.selection.mode, count: rows.length }; }
      if (body.remove_message_ids) { const rows = selections.get(id).filter((m) => !body.remove_message_ids.includes(m.message_id)); selections.set(id, rows); share.selection.count = rows.length; }
      share.publication_status = share.include_future ? "live" : "frozen"; return json(res, { share });
    }
    return json(res, { share, can_widen: true, reason: null });
  }
  const path = p.replace(/^\/api\/v1/, "");
  if (path === "/machines") return json(res, { machines: [{ id: machine, manager, name: "Fictional computer", online: true, os: "linux" }] });
  if (path === "/sessions") return json(res, { sessions: [session] });
  if (path === `/sessions/${machine}/${sessionID}`) return json(res, { session });
  if (path.endsWith("/items")) return json(res, { items: [human, answer].map((text, i) => ({ id: `synthetic-item-${i + 1}`, seq: i + 1, role: i ? "assistant" : "user", kind: "text", text, ts: stamp() })), has_more: false });
  if (path === "/assistant") return json(res, { machine_id: machine, main: "synthetic-main", conversations: [], tasks: [], memory: [] });
  if (path === "/managers") return json(res, { default: manager, managers: [{ id: manager, name: "Persona", online: true }] });
  if (path === "/extensions/ui") return json(res, { slots: [] });
  if (path === "/extensions") return json(res, { extensions: [] });
  if (path === "/todos") return json(res, { todos: [] });
  if (path === "/interactions") return json(res, { interactions: [] });
  return json(res, {});
}
// Compile the actual pre-sharing worker and its actual transitive helpers, not a stand-in algorithm.
const legacy = await build({ entryPoints: [resolve(web, "src/sw.ts")], bundle: true, write: false, format: "iife", target: "es2022", plugins: [{
  name: "baseline-worker",
  setup(build) {
    build.onLoad({ filter: /\.ts$/ }, ({ path }) => {
      if (!path.startsWith(resolve(web, "src") + "/")) throw new Error("Legacy worker left its source graph");
      const repoPath = `web/${path.slice(web.length + 1)}`;
      return { contents: execFileSync("git", ["-C", resolve(web, ".."), "show", `307790f7b:${repoPath}`], { encoding: "utf8" }), loader: "ts", resolveDir: dirname(path) };
    });
  },
}] });
const oldWorker = legacy.outputFiles[0].text;
const types = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".png": "image/png", ".woff2": "font/woff2" };
function server(publicHost) {
  const s = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, "http://fixture.invalid");
      if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/fixture/")) return await api(req, res, url, publicHost);
      if (!publicHost && url.pathname === "/legacy-worker.js") { res.writeHead(200, { "Content-Type": "text/javascript", "Cache-Control": "no-store", "Service-Worker-Allowed": "/" }); return res.end(oldWorker); }
      if (!publicHost && url.pathname === "/legacy-shell") { res.writeHead(200, { "Content-Type": "text/html", "Cache-Control": "no-store" }); return res.end('<!doctype html><h1 id="legacy">Legacy authenticated shell</h1>'); }
      if (!publicHost && url.pathname.startsWith("/share/")) {
        if (mode === "failed") { res.writeHead(502, publicHeaders); return res.end(); }
        if (mode === "offline") { req.socket.destroy(); return; }
      }
      const publicPath = url.pathname.startsWith("/share/") || url.pathname.startsWith("/share-assets/");
      if (publicHost && !publicPath) { res.writeHead(404, publicHeaders); return res.end(); }
      const base = resolve(web, publicPath ? "dist/public-share" : "dist");
      const filename = resolve(base, publicPath && url.pathname.startsWith("/share/") || !extname(url.pathname) ? "index.html" : `.${url.pathname}`);
      if (!filename.startsWith(base + "/") || !existsSync(filename) || !statSync(filename).isFile()) { res.writeHead(404, publicHeaders); return res.end(); }
      res.writeHead(200, { ...(publicPath ? publicHeaders : { "Cache-Control": "no-store" }), "Content-Type": types[extname(filename)] || "application/octet-stream" }); res.end(readFileSync(filename));
    } catch { json(res, {}, 500); }
  });
  if (!publicHost) s.on("upgrade", (req, socket) => {
    if (!req.url.startsWith("/api/v1/ws")) return socket.destroy();
    const accept = createHash("sha1").update(req.headers["sec-websocket-key"] + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest("base64");
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    const send = (data) => { const b = Buffer.from(JSON.stringify(data)); const h = Buffer.alloc(b.length < 126 ? 2 : 4); h[0] = 129; h[1] = b.length < 126 ? b.length : 126; if (h.length === 4) h.writeUInt16BE(b.length, 2); socket.write(Buffer.concat([h, b])); };
    send({ type: "hello", server_version: "synthetic", hub_name: "Fictional hub", features: [] });
    socket.on("data", () => send({ type: "pong" })); socket.on("error", () => {});
  });
  return s;
}
server(false).listen(Number(mainPort), "127.0.0.1", () => console.log("READY synthetic application fixture"));
server(true).listen(Number(publicPort), "127.0.0.1", () => console.log("READY isolated public fixture"));
const deadline = setTimeout(() => { console.log("TIMEOUT synthetic sharing fixture (60 minutes)"); process.exit(124); }, 3600000); deadline.unref();
