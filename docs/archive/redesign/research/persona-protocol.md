# Persona hub protocol research (for a new Android client)

> Scope: read-only research, no repository changes, builds, or tests. Evidence uses `relative path:line` from the Persona repository root, or `docs/<topic>.md` ("Section").
> Privacy: no hostnames, IPs, domains, accounts, key / project IDs; examples use placeholders (`<hub address>`, `<token>`).
> Performance: cite only explicitly documented measurements with sample counts; other values are code constants / timeouts, labeled constants, not measurements.
> Status: complete (sections 0–12). Android details marked "Verify" were not checked online.

---

## 0. Quick conclusions

- Phone connects only to hub: HTTP (REST) + one WebSocket (`/api/v1/ws`), all JSON, snake_case, Bearer authentication. Protocol is platform-independent;
  Android can reproduce iOS `ios/Shared/APIClient.swift` + `ios/Shared/LiveConnection.swift` + `ios/Shared/Models.swift` one-for-one.
- **Push is the only hard gap**: hub sends only APNs (all of `server/internal/hub/push.go`); device registration has no platform field, only `apns_token`
  (`server/internal/db/db.go:122-133`, `devices` in migration `0001_init.sql`). Without hub changes, Android receives no push and cannot wake to execute `phone_*`.
- **One global phone-tool registry**: `register_phone` overwrites the hub tool table (`hub.go:647-656`), routing calls to the most recently registered real device
  (`http.go:542-556`). Simultaneous iPhone/Android connections overwrite each other and make routing uncertain; using both requires changes.
- Other platform dependencies (iOS-type Gmail OAuth client, iPhone-hardcoded assistant prompts/errors, APNs-only Live Activities) are in section 11.

---

## 1. Authentication and pairing

| Item | Finding | Evidence |
| --- | --- | --- |
| QR content | `persona://pair?url=<public_url>&token=<pairing token>` (query encoded with `url.Values`) | `server/internal/hub/config.go:144-147` |
| Generator | `personad hub pair` on the hub computer prints address, token, terminal QR (qrterminal) | `config.go:149-158`, `server/cmd/personad/main.go:25-56` |
| Pairing token | `token` in `hub.json`: random 24-byte hex on first run, **permanent, not single-use**, full role (all operations) | `config.go:82-92`, `server/internal/hub/devicetokens.go:69-71` |
| Client parsing | Scheme must be `persona`; read nonempty query `url`, `token` | `ios/Shared/APIClient.swift:7-16` |
| Per-device credentials | Immediately after pairing, `POST /api/v1/auth/tokens {"name","kind"}` → `{"id":"dev_…","token":"jd_…","role","name"}`; use new token thereafter, discard pairing token | `devicetokens.go:126-168`, `ios/Persona/App/DeviceCredentials.swift:15-31` |
| kind → role | `phone` → `full`; `watch` → `answer`; all others `kind:"other"`, `role:"full"`; only full can create tokens (otherwise 403); 503 without database | `devicetokens.go:137-152` |
| Default names | Empty name or over 80 characters: phone "iPhone", watch "Apple Watch", other "设备" (Device); **Apple names hardcoded**, Android must supply name | `devicetokens.go:153-159` |
| Storage | Hub stores token sha256 only (`device_tokens.token_hash`), plaintext returned only once at creation | `devicetokens.go:59-62,160-167`, `server/internal/db/migrations/hub/0031_device_tokens.sql` |
| Validation cache | Database result cached 30 seconds; `last_seen` written at most every 5 minutes | `devicetokens.go:57,80-107` |
| Revoke | `DELETE /api/v1/auth/tokens/{id}` clears cache and immediately disconnects device WS; `GET /api/v1/auth/tokens` → `{"tokens":[{id,name,kind,role,created_at,last_seen,revoked_at}],"current":"<this device id>"}` | `devicetokens.go:169-222` |
| Revoke self | iOS revoking itself unpairs (`model.unpair()`) | `DeviceCredentials.swift:127-136` |
| answer role | All GET/HEAD + WebSocket; write allowlist regex: `interactions/*/resolve`, `sessions/*/*/(messages\|seen\|interrupt)`, `inbox/*/(done\|reply\|draft)`, `inbox/refresh`, `devices(/.*)?`; WS `register_phone`, `tool_result` ignored | `devicetokens.go:111-124`, `server/internal/hub/http.go:699-718` |
| Token transport | `Authorization: Bearer <token>` or query `?token=` (**all** `/api/` paths, including WS/media) | `http.go:163-176` |
| Unauthenticated API | Only `GET /api/v1/health` → `{"ok":true,"version","features"}` | `http.go:21-23` |
| Old-hub compatibility | Without `/auth/tokens`, retain pairing token and retry next launch | `DeviceCredentials.swift:4-7` |
| Re-pair | Scan same QR again (unchanged token unless hub.json edited); revoked devices must rescan | Same as above |

Notes:
- **Push device_id and credential id are separate**: push / notification settings use the app-generated UUID (`PushRegistrar.deviceID`, `ios/Persona/App/PersonaApp.swift:239-245`),
  credentials use hub-issued `dev_…` (`DeviceCredentials.swift:10-13`). Android can follow this, but they are unlinked: hub cannot associate a push device with a credential (revocation does not remove push registration).
- Security: QR contains permanent master token, granting full access to anyone who obtains it; rotation requires changing hub.json and re-pairing all clients still using the pairing token. Optional improvement in section 11.

---

## 2. General conventions (common pitfalls for new clients)

- snake_case fields; RFC3339 UTC times (`docs/protocol/protocol.md` opening). Go emits **up to 9 fractional-second digits** (special handling in iOS, `ios/Shared/JSONValue.swift:100-120`).
- **Zero time**: Go `omitempty` does not omit `time.Time`; absent times become `0001-01-01T00:00:00Z` (e.g. `seen_at`, `state_since`, `last_run`, `next_run`).
  iOS treats all dates before 2000-01-01 as empty (`Date.realDate`, `ios/Shared/APIClient.swift:743-746`); Android parser must too.
- Errors: `{"error":"…"}` + 4xx/5xx. Node errors with code become **409**, with `code` and additional fields flattened at top level (`delivered`, `draft`, `clearable`…);
  machine offline 503; other node errors 502 (`http.go:253-274`). Lark read-only proxy may forward gateway `{"error":{"code","message"}}` (`APIClient.swift:49-61`).
  **Unknown paths / wrong methods** on the main API get Go ServeMux default plain text (only mail subroutes rewrite 404/405 to JSON, `server/internal/mail/http.go:28-55`); clients must tolerate non-JSON error bodies.
- Request ID: every response has `X-Request-Id`; requests may supply `X-Client-Request-Id` (4–32 hex characters or `-`), reused by hub logs (`http.go:196-218`). iOS appends request id to 5xx errors.
- Caching: all API responses `Cache-Control: no-store` (`http.go:179-181`), except session images `private, max-age=31536000, immutable` (`http.go:355`) and Lark emoji 1 hour (`server/internal/hub/lark.go:163`).
  iOS lesson: fetch app-disk-cached bytes (chat images, Lark media) through an HTTP-cache-free client to avoid two copies (`APIClient.swift:69-76`).
- Feature negotiation `features` (`server/internal/proto/proto.go:42-54`): `client_msg_id`, `send_refusals`, `screen_dialog`, `resumable_interactions`, `secret_prompts`,
  `request_id`, `terminal_stream`, `terminal_control`, `terminal_scroll`, `terminal_click`, `file_upload`. Delivered in `/health` and WS `hello`; send new requests only when feature fields exist.
- Size limits (constants): POST body forwarded to node 32 MB (`http.go:364`, including base64 message images); WS read limit 64 MB (`http.go:563`, same iOS limit, `LiveConnection.swift:220`);
  file uploads 200 MB (`proto.go:57-63`).
- Hub timeouts (constants, not measurements): forwarded GET 30 s, POST 90 s (offline sessions require `--resume` first), session creation 100 s, assistant chat creation 110 s (`http.go:283,371,417`, `assistant.go:296`).
  Corresponding iOS timeouts: default 30 s, sending 100 s, session/chat creation 110 s, run scheduled task now 130 s, upload 120 s (`APIClient.swift`).
- Transport: hub defaults to plaintext HTTP `:18660` (`config.go:86-88`), iOS uses `NSAllowsArbitraryLoads` (`ios/project.yml:84-86`). **Android 9+ blocks cleartext by default**;
  user-configured hub addresses require network security config allowing cleartext (or HTTPS reverse proxy before hub). WebSocket `ws://` / `wss://` follows http/https.

---

## 3. HTTP API catalog (route registration is authoritative)

Routes: `server/internal/hub/http.go:19-161` (main table) + each file's `xxxRoutes` (`liveactivity.go:682-720`, `usage.go:128-132`, `metrics.go:224-260`,
`assistant.go:192-312`, `assistant_schedule.go:691-805`, `pins.go:59-120`, `ai.go:38-81`, `kb.go:598-689`, `lark.go:15-217`, `inbox.go:662-734`,
`devicetokens.go:126-198`) + mail subroutes `server/internal/mail/http.go:15-30`.
**[Undocumented]** means absent from `docs/protocol/protocol.md` REST table (some mentioned in other documents, noted explicitly).
`{mid}` = machine id, `{sid}` = session id (subagents `<parent-session>@<agent id>`, `proto.go:429-441`). Node-proxied APIs (`proxy` / `proxyBody`) return node results unchanged.

### 3.1 System / authentication
| Method path | Purpose / key parameters |
| --- | --- |
| GET `/api/v1/health` | Unauthenticated: `{ok, version, features}` |
| POST `/api/v1/auth/tokens` | `{name, kind: phone\|watch}` → `{id, token, role, name}` (documented in Authentication, not REST table) |
| GET `/api/v1/auth/tokens` | `{tokens, current}` |
| DELETE `/api/v1/auth/tokens/{id}` | Revoke + disconnect WS |
| GET `/api/v1/ws` | WebSocket (section 4) |

### 3.2 Machines
| Method path | Purpose / key parameters |
| --- | --- |
| GET `/api/v1/machines` | `{machines:[Machine]}` (includes `system`, `node_version`, `node_mismatch`) |
| POST `/api/v1/machines/{mid}/reconnect` | Reconnect node |
| GET `/api/v1/machines/{mid}/launchers` | `{launchers:[{name, base_url, model}]}` |
| GET `/api/v1/machines/{mid}/dirs?path=` | Directory browsing + recent directories |
| POST `/api/v1/machines/{mid}/spaces/{wid}/rename` | `{name}`: rename herdr workspace, migrate machine/old-name entries in pins / mutes (`pins.go:122-169`) |
| GET / PUT `/api/v1/machines/{mid}/claude-settings` | Allowlisted Claude Code settings; PUT `{key, value}` (null resets default), returns `{settings:[{key,title,detail,type,options,default,value}]}` |
| GET `/api/v1/metrics` | Dashboard: latest hour per machine, one point/minute; `latest` only if sampled in the last 30 seconds |
| GET `/api/v1/machines/{id}/metrics?range=1h\|6h\|24h` | Detail chart; 404 unknown machine, 400 invalid range (`metrics.go:238-259`) |
| GET `/api/v1/usage` | `{usage:{tpm, tokens, output, requests, machines:[{id,tpm,tokens,load}]}}` (`usage.go:69-75`) |

### 3.3 Sessions (Agents)
| Method path | Purpose / key parameters |
| --- | --- |
| GET `/api/v1/sessions?machine_id=` | `{sessions:[Session]}`: running + offline from last 7 days |
| POST `/api/v1/sessions` | `{machine_id, cwd, launcher, prompt?, resume_session_id?, workspace_id?, permission_mode?, effort?}` → `{session}` (`workspace_id` **[Undocumented]**, `http.go:397-429`) |
| GET `/api/v1/sessions/{mid}/{sid}/items` | Pagination: `before_seq`, `turns` (node cap 10 turns / 2000 items), `limit` (item count without turns, default 80, max 500), `after_seq` (incremental, 500/page, `more_after`), `lazy_images=1`; returns `{items, has_more, more_after?, history?, from_seq?, to_seq?}` (`http.go:295-323`, `server/internal/node/node.go:367-397`) |
| GET `/api/v1/sessions/{mid}/{sid}/items/{iid}/image` | Original image bytes, permanently cacheable; percent-encode `#`, `:` in `iid` (iOS allows only alphanumerics and `-._~`, `APIClient.swift:253-261`) |
| GET `/api/v1/sessions/{mid}/{sid}/items/{iid}/full` | Full truncated-item text (≤1 MB, no images) |
| POST `/api/v1/sessions/{mid}/{sid}/messages` | `{text, images?:[{mime,data}], client_msg_id?, clear_draft?, launcher?}`; `launcher` **[Undocumented]**: switch model for a headless assistant session from this message onward (`APIClient.swift:283-299`, `node.go:272-281`); refusals in section 5 |
| POST `/api/v1/sessions/{mid}/{sid}/interrupt` | Send Esc (assistant: stop current headless-process turn, `node.go:304-308`) |
| POST `/api/v1/sessions/{mid}/{sid}/keys` | `{keys, text, pre_keys?}` |
| GET `/api/v1/sessions/{mid}/{sid}/screen?lines=&source=visible\|recent&format=ansi` | Read screen `{text, status}` |
| POST `/api/v1/sessions/{mid}/{sid}/screen/before` | `{anchor:[…], lines, loaded?, format?}` → `{text, has_more, at_limit}` (`loaded` **[Undocumented]**, `node.go:309-320`) |
| POST `/api/v1/sessions/{mid}/{sid}/close` | Exit Agent and close tab (assistant: stop process) |
| POST `/api/v1/sessions/{mid}/{sid}/rename` | `{tab_name?, title?}` |
| POST `/api/v1/sessions/{mid}/{sid}/modes` | `{permission_mode?, effort?}` |
| POST `/api/v1/sessions/{mid}/{sid}/queue` | **[Undocumented]** `{keep:[indices]}` → `{queue:[original queued messages]}`: retrieve all queued messages and reorder (edit / delete queued messages, `node.go:477-483`, `server/internal/node/control.go:544-583`) |
| GET `/api/v1/sessions/{mid}/{sid}/info` | **[Undocumented]** Details: `session_id, transcript_path, cwd, model, launcher, git_branch, version, permission_mode, effort, context_tokens, output_tokens, compactions, turns, started_at, last_activity` (`node.go:454-476`, `APIClient.swift:351-374`) |
| POST `/api/v1/sessions/{mid}/{sid}/seen` | **[Undocumented]** Mark seen (Unread → Seen), equivalent to WS `seen` (`http.go:76-79`, `server/internal/hub/state.go:80-97`) |
| GET / PUT `/api/v1/pins` | Pins `{spaces:["machine/space-name"], sessions:["machine/session"]}` |
| GET / PUT `/api/v1/mutes` | Mutes (same structure + `allowed`), affects push |

### 3.4 Files / Git (forwarded to the session computer's node)
| Method path | Purpose / key parameters |
| --- | --- |
| GET `/api/v1/machines/{mid}/file?path=&cwd=` | **[Undocumented in protocol.md, mentioned in `docs/apple/ios.md`]** Read file: `{path,name,size,mime,text?,data?(base64),truncated,is_dir,entries?}`, ≤2 MB, rejects iCloud paths (`server/internal/node/files.go:16-60`) |
| GET `/api/v1/machines/{mid}/fs?path=&cwd=` | File tree, at most 2000 entries |
| POST `/api/v1/machines/{mid}/files?name=` | Upload raw body, **Content-Length required** (otherwise 411), ≤200 MB (413 `too_large`), old node 409 `node_outdated` → `{path,name,size}` (`server/internal/hub/upload.go:17-94`) |
| GET `/api/v1/machines/{mid}/git/status?cwd=` | Read-only Git status |
| GET `/api/v1/machines/{mid}/git/diff?cwd=&path=&staged=0\|1` | diff, ≤512 KB |
| GET `/api/v1/machines/{mid}/git/show?cwd=&hash=` | Commit details |
| GET `/api/v1/machines/{mid}/git/log?cwd=&skip=&limit=` | Paginated commits |

### 3.5 Interactions (Needs you)
| Method path | Purpose |
| --- | --- |
| GET `/api/v1/interactions` | Pending, creation time ascending |
| POST `/api/v1/interactions/{id}/resolve` | Resolution body; no longer pending 410; changed screen dialog 409 `prompt_changed` (`http.go:88-103`) |
| GET `/api/v1/interactions/history?machine_id=&session_id=&limit=` | History (status, resolution included) |

### 3.6 Push devices / notification preferences / Live Activities
| Method path | Purpose |
| --- | --- |
| POST `/api/v1/devices` | `{device_id, name, kind:"iphone"\|"watch", apns_token?, apns_env?, topic?}` (**no platform field**, unknown fields ignored, `http.go:104-112`, `db.go:122-133`) |
| GET / PUT `/api/v1/devices/{id}/prefs` | `{input, done:off\|long\|always, live_activity, lock_details, island:{preset,left,right,machine}}`, response adds read-only `long_turn_seconds`, `done_delay_seconds`; works without push registration (`http.go:116-134`, `server/internal/hub/notify.go:52-137`) |
| POST `/api/v1/devices/{id}/live-activity` | `{push_to_start_token}` / `{activity_id, push_token}` / `{activity_id, ended:true}` (APNs-only, `liveactivity.go:682-712`) |
| GET `/api/v1/fleet` | Current Fleet summary (all stats), fetched on foregrounding |

### 3.7 Assistant (Persona tab)
| Method path | Purpose / key parameters |
| --- | --- |
| GET `/api/v1/assistant` | `{machine_id, launcher, timezone, notes?, main?, cwd, conversations:[Session], notes_effective}` |
| PUT `/api/v1/assistant` | `{machine_id?, launcher?, timezone?, notes?, main?}`; `main:"-"` clears main chat **[Undocumented in protocol.md, mentioned in `docs/assistant/knowledge.md`]** (`assistant.go:196-242`) |
| POST `/api/v1/assistant/conversations` | `{prompt, images?, launcher?, permission_mode?, effort?, main?}` → `{session}` (last four fields incomplete in protocol.md table, `assistant.go:283-311`) |
| POST `/api/v1/assistant/warm` | `{launcher?}` warmup |
| GET / PUT `/api/v1/assistant/routines` | Replace whole scheduled-task table (user-editable fields only) |
| POST `/api/v1/assistant/routines/{id}/run` | → `{session_id, machine_id}` |
| GET `/api/v1/assistant/followups` | Follow-up list |
| DELETE `/api/v1/assistant/followups/{id}` | Cancel |
| POST `/api/v1/assistant/followups/{id}/run` | Run now |
| GET `/api/v1/assistant/runs[?ref=]` | Run records, newest first |
| GET `/api/v1/assistant/memory` | `{files:[{name,content,updated_at}], dir}` |
| PUT / DELETE `/api/v1/assistant/memory/{name}` | `{content}` |
| POST `/api/v1/ai/mail-account` | `{text}` → identify email configuration |

### 3.8 Knowledge base (**[Undocumented in protocol.md]**, table in `docs/assistant/knowledge.md`)
| Method path | Purpose |
| --- | --- |
| GET `/api/v1/kb/{kind}?tag=` | kind ∈ projects / chats / people / docs / meetings / events → `{items:[Entry], tags}` |
| GET `/api/v1/kb/{kind}/{id}` | `{detail:{kind,id,path,meta,summary,log,recent,links}, agents?}` (projects include Agents running in their code directories) |
| PUT `/api/v1/kb/{kind}/{id}` | Same fields as `kb_update` (`server/internal/kb/update.go:102-120`) → `{id}` |
| GET `/api/v1/kb-search?q=&messages=0` | `{hits:[{path,line}]}` |
503 when knowledge base disabled (`kb.go:600-605`).

### 3.9 Inbox
| Method path | Purpose |
| --- | --- |
| GET `/api/v1/inbox` | `{items:[InboxItem]}` (unhandled + handled within 3 days) |
| GET `/api/v1/inbox/{id}` | One item with `context` |
| POST `/api/v1/inbox/{id}/done` | `{done}` |
| POST `/api/v1/inbox/{id}/draft` | `{instruction?}` → `{text}` |
| POST `/api/v1/inbox/{id}/reply` | `{text}` send and mark done |
| POST `/api/v1/inbox/refresh` | Collect now, return list |
InboxItem also has `agent` not listed in protocol.md (AI twin processing result, `server/internal/extmgr/inbox.go:54-84`).

### 3.10 Lark proxy (Lark gateway)
Already in protocol.md: conversations / pinned / labels / unread-count / flags / conversations/{id}/labels / settings / unread / messages (GET, POST) / read / thread / media / messages/{id}/media / emojis / card-actions.
Additional code APIs **[Undocumented in protocol.md]** (some in `docs/extensions/feishu.md`):

| Method path | Purpose |
| --- | --- |
| GET `/api/v1/lark/raw/{path...}` | Read-only gateway agent API window (mentioned in `docs/extensions/feishu.md`); JSON media paths rewritten to hub |
| POST `/api/v1/lark/raw/{path...}` | Same POST (body ≤1 MB; assistant tool docs use only for `search/page`) |
| GET `/api/v1/lark/reactions/quick` | `{keys}` common emoji reactions |
| POST `/api/v1/lark/messages/{id}/reactions` | `{key, remove?}` |
| GET `/api/v1/lark/users/{id}?feed_id=` | User profile (unchanged gateway JSON) |
| POST `/api/v1/lark/users/{id}/chat` | → `{feed_id}` open DM |
| GET `/api/v1/lark/messages/{id}/url-preview?url=` | Link preview |
| GET `/api/v1/lark/moments/posts/{id}`, `…/comments` | Post / comments |
`GET /api/v1/lark/emojis` also returns `groups` (`lark.go:157-165`). Media URLs are hub-relative (`/api/v1/lark/media/…`), requiring hub base URL and authentication (header or `?token=`);
video / audio depend on Range forwarding (`CLAUDE.md` lesson).

### 3.11 Mail (`server/internal/mail/http.go:15-27`, matches protocol.md)
accounts (GET / POST / DELETE / test / mailboxes), `oauth/google/start`, `oauth/google/finish`, messages (list / single / flags), send. Gmail login platform issues in section 9.

---

## 4. WebSocket `/api/v1/ws`

### 4.1 Connection
- URL: `ws(s)://<hub address>/api/v1/ws`, authentication as HTTP (header or `?token=`). Server `websocket.Accept(... InsecureSkipVerify: true)` does not validate Origin (`http.go:558-563`).
- On connection, server immediately sends in order: `hello{server_version, client_id, features}` → `machines` → one `sessions` per machine → one `interaction` per pending request (`http.go:616-623`).
  Thus reconnect replays full state; clients need not fetch lists again (iOS still calls `GET /interactions` to reconcile, `ios/Persona/App/AppModel.swift:456-474`).
- Heartbeat: hub sends protocol-level WebSocket ping every 20 s (15 s timeout, closes on failure, `http.go:589-613`). **No JSON ping/pong**: protocol.md's `ping` / `pong`
  do not exist in code (server emits no `{"type":"ping"}` and ignores received `{"type":"pong"}`, `http.go:663-718`). iOS also pings every 20 s, 5 s on weak network,
  and reconnects after 45 s without messages (`ios/Shared/LiveConnection.swift:167-170,230-265`). OkHttp `pingInterval(20s)` suffices (coder/websocket automatically pongs).
- Backpressure (constant): 512-message send queue per connection. `broadcast` / `toSubscribers` **silently drops the message** when full; `sendJSON` and terminal `sendTerm` **disconnect immediately**
  ("too slow"), requiring client reconnect/catch-up (`http.go:448-459,461-501`, `terminal.go:479-491`). → read promptly, decode off main thread (iOS callback-thread decoding, `LiveConnection.swift:267-277`).
- Disconnect cleanup: unsubscribe all connection sessions and terminals (`http.go:571-587`). iOS disconnects on backgrounding so a suspended socket does not make hub think the phone is online (`PersonaApp.swift:27-35`).

### 4.2 Server → client (all types actually emitted)
| type | Fields | Recipient | Evidence |
| --- | --- | --- | --- |
| `hello` | `server_version, client_id, features` | This connection | `http.go:616` |
| `machines` | `machines` | All | `hub.go:213-215` |
| `sessions` | `machine_id, sessions` (full machine snapshot; each includes `state`, `rev`, `suggestion`…) | All | `hub.go:247-251`, `state.go:94-96` |
| `revs` | `machine_id, revs:{sid: rev}` (changed only) | All | `machine.go:335-350` |
| `items` | `machine_id, session_id, items` + `history? from_seq? to_seq?`; replay also `backlog:true`, `has_more?`, `more_after?` | Subscribers | `http.go:503-540,725-771` |
| `delta` | `machine_id, session_id, text` (whole text being generated by headless assistant, empty=end of turn) | Subscribers | `machine.go:402-409`, `server/internal/node/assistant.go:429` |
| `screen` | `machine_id, session_id, text, status, ts` | Subscribers | `machine.go:374-384` (**legacy**: current node no longer emits `session.screen`) |
| `terminal` | Frame `epoch, seq, full, cols, rows, data(base64)`, or `error` + `unsupported` | Terminal subscribers | `terminal.go:70-73,471-476` |
| `terminal_control` | `state: active\|busy\|taken\|lost\|unsupported`, `cols/rows` when active, `error` (human-readable reason) | Controller | `terminal.go:145-154` |
| `interaction` | `interaction` | All | `hub.go:376-384` |
| `interaction_resolved` | `id` | All | `hub.go:572-580` |
| `tool_call` | `call_id, name, args` | Selected phone | `hub.go:711` |
| `fleet` | `fleet` (FleetState + all stats) | All | `liveactivity.go:457-463` |
| `inbox` | No contents; app fetches | All | `inbox.go:241` |
| `lark` | `feed_ids` (400 ms throttle) | All | `lark_events.go:12-44` |
| `assistant_schedule` | No contents: scheduled tasks / follow-ups changed **[Undocumented]** | All | `assistant_schedule.go:683-689` |
| `hook_event` | `machine_id, session_id, event, tool_name, message, last_assistant_message, cwd` **[Undocumented]** (iOS decodes then ignores) | All | `machine.go:434-441`, `server/internal/node/hooks.go:201-206` |
| `error` | `machine_id, session_id, error`: subscription replay failed **[Undocumented]** | This connection | `http.go:743-745` |

protocol.md's `session` (single state change) has **no emitter** in code and need not be implemented.

### 4.3 Client → server
| type | Fields | Notes / evidence |
| --- | --- | --- |
| `subscribe` | `machine_id, session_id, since_seq?, lazy_images?` | First subscriber starts node `session.watch` (new lines every 250 ms); once `lazy_images` is true, **all later items on this connection omit image data** (connection-sticky, `http.go:673-689`) |
| `unsubscribe` | `machine_id, session_id` | Last subscriber triggers node `session.unwatch` |
| `seen` | `machine_id, session_id` | **[Undocumented]** mark seen (`http.go:709-712`) |
| `terminal_subscribe` | `machine_id, session_id, epoch?, after_seq?, control?:{cols,rows}, takeover?` | `takeover` is **top-level**, not inside `control` (`http.go:649-651`, `AppModel.swift:330-338`) |
| `terminal_input` | `machine_id, session_id, text?, control?, scroll?, at?:[col,row], click?:[col,row]` | Only controller input forwarded to node; keys forwarded as events in arrival order (`terminal.go:239-298`) |
| `terminal_unsubscribe` | `machine_id, session_id` | |
| `register_phone` | `device_name, tools:[{name,description,input_schema}], simulator` | Full role only; empty `tools` does not overwrite existing registry (`http.go:699-708`) |
| `tool_result` | `call_id, content:[{type:text,text}\|{type:image,data,mime_type}], is_error` | Full role only |

### 4.4 Subscription backlog details (`http.go:721-771`)
- Without `since_seq`: latest 2 turns (`turns: 2`), one `items`, `backlog:true` and `has_more`.
- With `since_seq`: page by `after_seq` (500 node items/page), at most 8 pages (4000 items); beyond that final message has `more_after:true`, client switches to latest page;
  **no `has_more` in replay** (only tail-relative pages know whether earlier data exists; 2026-10-26 pitfall, `CLAUDE.md` and `http.go:751-755`).
- Replay failure emits `{"type":"error", machine_id, session_id, error}`.

---

## 5. Synchronization model (push + pull)

- **Lists**: full `sessions` pushes for any field change; lightweight `revs` pushes when only transcript grows and visible fields stay unchanged (node checks every second, protocol.md "Hub ↔ node RPC").
  `rev` = transcript byte count (`proto.go:227-229`). Full session list is about 130 KB, so rev changes do not resend it (`CLAUDE.md` documented figure, not measured this round).
- **Contents**: unopened sessions pull `after_seq=<local covered>` increments (iOS `AgentSync`: live Agents + used within 24 hours, max 40, concurrency 3; client-policy constants,
  `ios/Persona/App/AgentSync.swift:13-15`). Open sessions use WS live subscription + `since_seq` gap repair on subscribing. **Do not subscribe to all sessions for background sync**: subscription makes node refresh transcript every 250 ms (`CLAUDE.md` lesson).
- **Continuity** (`docs/protocol/protocol.md`, "Item continuity"): batches carry `history` (file identity; change means discard/reload), `to_seq` (last read line, including lines with no items),
  `from_seq` (continuous batches only). Maintain `covered`: `from_seq <= covered` → `covered = to_seq`; otherwise gap, wait 1.5 s then repair through `after_seq=covered`, switching to latest above 8 pages
  (`AppModel.swift:402-439`). Process empty `items` with `to_seq` too (advance covered).
- **Pagination**: history by turn (`turns=2`, page begins with user input); iOS also sends `limit=80` for old hubs (`APIClient.swift:218-225`). Trust `has_more` only on tail-relative pages.
- **Lazy images**: after `lazy_images`, items contain `image:{mime, lazy:true, size, width, height}`; fetch `/items/{iid}/image` for display (permanent cache). Locally sent images remain inline base64.
- **Truncation**: tool output limited to 4 KB in pages/pushes, other text 24 KB; truncated items carry `full_size` and a trailing line "…（已截断…）" (…truncated…). Clients strip that line and offer Read full text (`ios/Shared/Models.swift:395-399`).
- **Send idempotency** (`docs/protocol/protocol.md`, "Sending messages"; `server/internal/node/sendsafe.go:53-74`, `node.go:272-281`): one UUID `client_msg_id` per pending message, reused for retries; node remembers 10 minutes.
  Three outcomes: success; 409 refusal (`code: dialog_open | input_draft`, `delivered:"none"`, retryable; `input_draft` includes `draft`, plus `clearable` if one line, user-approved retry carries `clear_draft=<clearable>` and same id);
  502 `delivered:"unknown"` (error mid-typing; retry same id returns same error without typing twice).
- **Offline outbox**: persist messages locally while disconnected, send in order after reconnect/catch-up; one-off approvals / answers are not queued (`docs/apple/ios.md`, "Connection state").
- **Seen**: send WS `seen` or `POST …/seen` when opening or watching a session complete. Hub stores `session_views`, affecting `state` (unread → idle) and completion notifications (`state.go:10-35,80-97`).

---

## 6. Interactions (Needs you)

- Types `question | plan | permission | screen`; structures and Resolution in `docs/protocol/protocol.md` ("Data types"), `proto.go:318-381`.
- `screen` cards: hub reads/parses `dialog{header, question, detail, options[{key,label,detail,kind,keys}], rev}` (`server/internal/screen/dialog.go:16-38`),
  answers use `option` (`type_something` also sends `text`); node rereads screen before keys and compares `rev`, returning 409 `prompt_changed` and a new card if changed. `secret:true` uses a password field.
  Card id includes `rev`, so changed dialogs create new cards. Screen answers are synchronous (hub waits until node finishes keystrokes).
- Expiry (constants): hook interactions default 30 minutes (`hook_timeout_minutes`, `config.go:131-133`); screen cards 24 hours (`hub.go:311`); hub-mediated `mail_send` / `lark_send` authorization 15 minutes (`hub.go:683-691`).
- `resolve`: success `{ok:true}`; no longer pending 410; screen card 409 `prompt_changed`; other proxy errors 502/503.
- Restart resilience: retain `resumable` interactions for 3 minutes after node disconnect (`hub.go:125-127`), restoring same id; app deduplicates by id.
- Assistant `ask_user` is a `question`, `tool_name:"ask_user"`, push title "Persona 在问你" (Persona is asking you) (`push.go:316-318`).
- iOS notification actions (`ios/Persona/App/NotificationActions.swift`): authorization Allow `{allow:true}` / Deny `{allow:false}`, plan Approve `{approve:true, mode:"acceptEdits"}` /
  Send back `{approve:false, feedback:<input>}`. Allow, Approve, and Send back for revision require unlocking (`.authenticationRequired`); only permission Deny does not. Questions/terminal cards have no buttons (open to handle). Failure (410 handled, disconnected) shows local notification guidance.
- Docs say hooks allow immediately if no app online (`docs/server/architecture.md`, "Answering questions / approving plans"), but node `ask` directly calls `interaction.create` and waits until expiry,
  with no no-app-online bypass found (`server/internal/node/hooks.go:312-336`, `hub.go:396-429`). Code is authoritative; docs may be stale.

---

## 7. Phone-tool (phone_*) protocol

### 7.1 Route and protocol (`docs/apple/phone-tools.md`, `server/internal/hub/hub.go:631-765`)
1. Registration: after WS connects, client sends `register_phone{device_name, tools, simulator}` (iOS `didConnect`, `AppModel.swift:456-492`). `tools` are MCP definitions
   `{name, description, input_schema}`, **only user-enabled data sources**. Hub stores in memory and replaces all Postgres `phone_tools` (Claude still sees tools when phone is offline, `hub.go:647-656`, migration `0001_init.sql`).
2. Exposure to Claude: `personad mcp` → node.sock → node → hub `mcp.tools` → `mcpTools()` = phone + mail tools (+ additional assistant tools for assistant sessions) (`hub.go:658-669`, `machine.go:465-470`).
3. Call: `mcp.call` → `callTool` (`phone_` prefix to `callPhone`) → `phoneClient()` chooses **real devices before simulators, then most recently registered** (`http.go:542-556`) →
   no online phone triggers `wakePhone` (silent push, at most once per 30 s, wait up to 20 s, check connection every 300 ms, `hub.go:722-753`) →
   send `tool_call{call_id, name, args}` → wait at most 2 minutes (`hub.go:693-720`, constant).
4. Result: `tool_result{call_id, content:[{type:"text",text} | {type:"image",data,mime_type}], is_error}` (`proto.go:383-401`). All failures become `is_error` + explanation for the model;
   client never throws (`ios/Persona/PhoneTools/PhoneToolRegistry.swift:95-120`).
5. Write confirmation: **on phone, not through hub**. Tools carry a `confirmation` summary; app prompts before execution. Denial → `is_error`, content `The user declined on the phone.`;
   confirmation covers only that call (`PhoneToolRegistry.swift:107-112`, `AppModel.swift:570-583`). Disabled sources reject calls even from old sessions (`PhoneToolRegistry.swift:103-106`).
   (Hub-mediated confirmation is only `mail_send`, `lark_send`, creating `permission` interactions, `hub.go:683-691`.)
6. Output: pretty JSON text; ISO 8601 time with local time-zone offset, dates `yyyy-MM-dd`; `phone_photo_get` adds an image block (`docs/apple/phone-tools.md`, "Tool catalog").

Measurement (single sample, `docs/archive/assistant/assistant-records.md`, "Capability measurements", n=1, not a baseline): calling a phone tool while locked, APNs accepted silent push in 259 ms, phone connected after 900 ms, tool result in 3.2 seconds; another run returned in 2.8 seconds.

### 7.2 Existing iOS tools (`ios/Persona/PhoneTools/*.swift` specs) and Android equivalents
(Android column is platform knowledge, **not checked online this round**; Verify items require official documentation / real-device checks before implementation.)

| Tool (source) | Parameters (schema) | Confirmation | Android equivalent | Differences / risks |
| --- | --- | --- | --- | --- |
| `phone_contacts_search` (contacts) | Required `query` (empty = first contacts in address-book order), `limit` 1–50 default 10 | No | ContactsContract (`READ_CONTACTS`), birthdays in Event rows | Essentially equivalent |
| `phone_calendar_events` (calendar) | `start?` `end?` (ISO 8601, default 7 days from midnight today), `query?`, `limit` 1–200 default 50 | No | CalendarContract.Instances (`READ_CALENDAR`), participants in Attendees | Equivalent |
| `phone_calendar_create_event` (calendar) | Required `title` `start` `end`, optional `location?` `notes?` `all_day?` `calendar?` (calendar name) | **Yes** | CalendarContract.Events insertion (`WRITE_CALENDAR`); default writable calendar of primary account | All-day Android events require UTC midnight + `EVENT_TIMEZONE=UTC` (Verify); choose default calendar explicitly |
| `phone_reminders_list` (reminders) | `list?` `include_completed?` `limit` 1–200 default 50 | No | **No unified system reminder API** | Options: omit tools; Google Tasks (OAuth tasks scope, could live on hub like Gmail); or app-owned reminders (local DB + WorkManager/alarm notifications). Vendor to-do apps have no public API |
| `phone_reminders_create` (reminders) | Required `title`, `due?` `notes?` `list?`, `priority` 0–9 | **Yes** | Same as above | Same as above |
| `phone_reminders_complete` (reminders) | Required `id` | **Yes** | Same as above | Same as above |
| `phone_location_current` (location) | No parameters, wait at most 15 s, reverse geocoding | No | FusedLocationProviderClient.getCurrentLocation (GMS) or LocationManager.getCurrentLocation (API 30+); Geocoder address lookup | Background (push-woken) location needs `ACCESS_BACKGROUND_LOCATION`, analogous to iOS while-in-use-only failure; Geocoder may lack backend without GMS / on some Chinese ROMs (Verify), returning coordinates only |
| `phone_health_summary` (health) | `metrics?` ∈ steps / distance / active_energy / heart_rate / resting_heart_rate / sleep / weight (default steps), `days` 1–90 default 7; daily aggregates | No | Health Connect: StepsRecord.COUNT_TOTAL, DistanceRecord.DISTANCE_TOTAL, ActiveCaloriesBurnedRecord, HeartRateRecord BPM_AVG/MIN/MAX, RestingHeartRateRecord, SleepSessionRecord, WeightRecord; daily `aggregateGroupByPeriod` | ① distance: HealthKit walking+running distance vs HC DistanceRecord without activity distinction; ② sleep: iOS asleep stages only, verify whether HC excludes awake stages; ③ HC reports permission grants (better than iOS, which never reports reads); ④ HC default history is 30 days before first grant, older needs `READ_HEALTH_DATA_HISTORY` (Verify); ⑤ background push-woken reads need `READ_HEALTH_DATA_IN_BACKGROUND` (Verify); ⑥ source apps must write HC, vendor health-app sync varies (Verify) |
| `phone_photos_recent` (photos) | `limit` 1–100 default 20, `days?`, `screenshots_only?` | No | MediaStore (13+ `READ_MEDIA_IMAGES/VIDEO`; 14+ partial `READ_MEDIA_VISUAL_USER_SELECTED` ≈ iOS limited) | No screenshot flag, infer from album path / bucket name (vendor-specific); favorites `IS_FAVORITE` (API 30+); location needs `ACCESS_MEDIA_LOCATION` + EXIF |
| `phone_photo_get` (photos) | Required `id`, `max_size` 256–2048 default 1280; JPEG quality 0.7 | No | ContentResolver.loadThumbnail / decode and scale | Equivalent; MediaStore `_ID` or content URI id |
| `phone_device_info` (device) | None | No | Build, BatteryManager, PowerManager (power saving / thermal), StatFs, ConnectivityManager | Equivalent; iOS field `ios_version`, Android defines its own |
| `phone_clipboard_get` (clipboard) | None | No | ClipboardManager | **Android 10+ reads only when foreground and focused (or default IME)**; background push wake cannot read; 12+ shows Pasted notice, vs iOS Allow Paste prompt |
| `phone_clipboard_set` (clipboard) | Required `text` | No | setPrimaryClip | 13+ system copy notice; background restrictions need verification |
| `phone_notify` (notifications) | Required `title` `body`, `delay_seconds` 0–86400 | No | NotificationManager (13+ `POST_NOTIFICATIONS`); delay with WorkManager (inexact) or exact alarms (12+/14+ restricted permissions) | Essentially equivalent |

Clients provide `description` / `input_schema` (no hub changes); Android should write platform-appropriate descriptions, not copy wording such as "iOS may show an Allow Paste prompt,"
but **prefer identical tool names and parameters** so assistant prompts and existing session conventions remain unchanged.

Android-only optional capabilities (depend on distribution: Google Play limits SMS / call-log permissions to default SMS / dialer apps; sideloading or internal distribution is not bound by that policy):
SMS (`READ_SMS`), call logs (`READ_CALL_LOG`), other apps' notifications (NotificationListenerService, can expose IM messages, sensitive), alarms (AlarmClock intent),
app usage time (UsageStatsManager). These fill `docs/apple/phone-tools.md` ("iOS limitations") gaps, but are highly sensitive and need separate toggles/confirmation.

### 7.3 Hub issues affecting phone tools
- **One global tool table**: `register_phone` replaces all `h.phoneTools` and `phone_tools` (`hub.go:647-656`); empty `tools` in `register_phone` does not overwrite (`http.go:706-708`).
  With two online phones (iPhone + Android), the last connected tool set wins and receives calls (`http.go:542-556`); Android lacks `phone_reminders_*`, so such calls return Unknown tool.
- **APNs-only wakeup**: `wakePhone` → `push.wake("tool")` sends `content-available` to **all** APNs-token devices (`push.go:227-231`); no Android wake route.
- Hardcoded iPhone wording: tool failures (`hub.go:699,718`), assistant descriptions (`assistant.go:117,128,164`), live snapshots (`server/internal/hub/assistant_snapshot.go:127-132`),
  `ask_user` description (`server/internal/hub/assistant_tools.go:26`). Android users/model output will say iPhone.

---

## 8. Push

### 8.1 Current: APNs only (all of `server/internal/hub/push.go`)
- Config: `~/.persona/apns.json` (`key_id, team_id, key_path, topic`) + p8 key; absent means push disabled (`push.go:28-92`). ES256 JWT cached 40 minutes (`push.go:107-126`).
- `devices` table: `device_id, name, kind, apns_token, apns_env, topic, prefs(jsonb), updated_at` (`0001_init.sql`, `0021_device_prefs.sql`);
  `POST /api/v1/devices` decodes directly to `db.Device` (`http.go:104-112`), **no platform / push-provider field**; registration preserves prefs (`push.go:94-105`).
  `apns_env` chooses sandbox / production host, per-device `topic` (`push.go:133-138,254-262`); 410 / BadDeviceToken / Unregistered removes device (`push.go:284-297`).
  APNs uses persistent HTTP/2, `CloseIdleConnections` on errors (`push.go:276-281`, `CLAUDE.md` lesson).
- Push types:

| Type | Trigger / policy | APNs payload notes | Device filter | Evidence |
| --- | --- | --- | --- | --- |
| Needs you (interaction) | Immediately on new interaction; muted Agents excluded | `alert{title, subtitle:"<session name> · <machine>", body}`, `sound`, `thread-id=<session_id>`, `category=INTERACTION_<KIND>`, `interruption-level=time-sensitive`; custom `interaction_id, machine_id, session_id`; `apns-collapse-id=<interaction id>`, priority 10 | `prefs.input` | `push.go:305-340`, `hub.go:376-384` |
| Complete | After working → done/idle, wait `done_delay_seconds` (60); resumed work is same turn. No push if seen (herdr idle or app `seen`), muted, assistant `notify_user` within 3 minutes, Inbox Agent, or background-woken turn. With background tasks, wait through their reporting turn (max `background_hold_minutes` 120) | `alert{title:"✅ <title>", subtitle:"machine · space › tab", body:<last reply>}`, `thread-id`; keys `machine_id, session_id` | `prefs.done`: off / long (≥`long_turn_seconds` 60) / always | `machine.go:500-536`, `notify.go:11-267` |
| Assistant `notify_user` | Explicit assistant call | Same as Complete, subtitle Persona | All devices | `assistant_tools.go:116-125` |
| Silent wake | `phone_*` call without connected phone; once per 30 s | `{"aps":{"content-available":1},"persona_wake":"tool"}`, background push-type, priority 5 | All token-bearing devices | `push.go:227-231`, `hub.go:722-753` |
| Live Activity (Fleet) | See 8.3 | liveactivity push-type, topic `<bundle>.push-type.liveactivity` | `prefs.live_activity` with activity / push-to-start token | `liveactivity.go` |

- Timing in `hub.json` `notify` (`notify.go:20-50`, defaults are constants); per-device push choices in `devices.prefs` (`notify.go:52-137`).
- `Pusher.clear(id)` is empty (`push.go:356`): already displayed notifications are not withdrawn after interaction resolution (current iOS).
- Muting: Agent in `mutes.sessions` → muted; in `allowed` → not muted; otherwise space rule (`pins.go:41-57`).

### 8.2 Android needs (FCM approach)
(FCM / Android limits are platform knowledge, **not checked online this research**, marked Verify.)
- **Use only FCM data messages**, not notification messages: app builds notifications in `onMessageReceived`, enabling Allow / Deny buttons (API 31+ `Action.setAuthenticationRequired(true)` ≈ iOS
  `.authenticationRequired`), plan Send back with RemoteInput comments, `session_id` grouping (≈ `thread-id`), `interaction_id` notification tag (≈ collapse-id),
  and withdrawal after resolution (unavailable on iOS, easy on Android: optional extra hub `interaction_resolved` push).
- Priority: Needs you, Complete, notify_user, and silent wake use `android.priority=HIGH`; Fleet count changes `NORMAL` (may delay in Doze, acceptable), matching existing `urgent`.
  High-priority messages without visible notifications may be downgraded by FCM (Verify details); silent wakes already limited to once per 30 s, so less concern.
- Suggested TTL: interaction expiry; Complete ~1 hour; wake ~20 s (hub waits only 20 s); Fleet ~15 minutes (≈ stale-date).
- Payload: data values must be strings, roughly 4 KB/message cap (Verify); FleetState JSON (max 3 entries) fits one string field.
- **Android wake-to-run phone_***: high-priority FCM data `{type:"wake", reason:"tool"}` → `onMessageReceived` (short processing window, Verify) → short-lived foreground service
  (Android 14 `shortService`, etc.; high-priority FCM exempts background foreground-service startup, Verify) → WS connect, `register_phone`, execute `tool_call`, return `tool_result` → stop service.
  Optional hub change: include `tool_call` directly in push and return via HTTP (new API such as `POST /api/v1/phone/calls/{call_id}/result`), saving background WS setup time.
- **No-GMS risk**: phones without Google Play services, including Chinese-market models, cannot receive FCM (confirm user's model). Alternatives: UnifiedPush (e.g. ntfy distributor, hub POSTs to app-registered endpoint URL),
  vendor SDKs (Xiaomi / Huawei / Honor / OPPO / vivo, separate heavy integrations), or persistent foreground-service WS (no hub changes, but power use and ROM termination risk).
  Hub push should support providers (apns / fcm / future unifiedpush).
- **Network**: hub connections to Google's IPv4 are unreliable; Gmail already forces `google.proxy` (`CLAUDE.md` lesson, `docs/extensions/mail.md`). FCM (fcm.googleapis.com, oauth2.googleapis.com) is also Google;
  both sending and token exchange need proxy (reuse `google.proxy` or separate config).
- **Credentials**: FCM HTTP v1 exchanges service-account JSON (private key) for OAuth2 access token (scope `firebase.messaging`, about 1-hour lifetime, cached). Store service-account file on hub (0600);
  `hub.json` contains only path and project id, not database/repository secrets (`CLAUDE.md` "Secrets never enter the repository").
- Errors: 404 `UNREGISTERED`, 400 `INVALID_ARGUMENT` (bad token) → remove device; 401 → refresh access token; 429 / 5xx → `Retry-After` backoff (Verify codes).

### 8.3 Current Fleet Live Activities and Android Live Updates
Current (`docs/apple/live-activity.md`, `liveactivity.go`):
- `FleetState` (Go `liveactivity.go:39-74,151-159` ↔ Swift `ios/Shared/FleetState.swift`, matching fields): `phase` (needsYou / working / done / empty), `needsYou`, `working`, `unread`,
  `items[{id:"machine/session", title, place, kind, detail, interaction?, since}]` (max 3), `workingSince`, `details`, `assistant?`, `left?`/`right?`, `stats?{tpm,tokens,requests,loads}`.
  **All times are numeric Unix seconds**. `FleetItem.interaction` (interaction id buttons resolve) is absent from `docs/apple/live-activity.md` example.
- Trigger: recalculate on every `sessions` / `interaction` / `interaction_resolved` broadcast (`http.go:466-471`); unchanged means no push. New Needs you pushes immediately (priority 10), others coalesce 2.5 s (priority 5);
  `stale-date` = now + 15 minutes, refresh unchanged content every 10 minutes; empty for 1 minute or only Unread for 5 minutes → `end`; activities over 8 hours considered gone; numeric-only changes separated by at least `live_metrics_seconds` (60) per device;
  trim to each device's preferences (`forDevice`), suppress unchanged payloads (constants, `liveactivity.go:29-37`).
- Start: existing activity token → update; otherwise push-to-start token → one `start` at turn beginning (once per turn).
- Foreground app also gets `{"type":"fleet"}` and updates locally (no push budget); fetch `GET /api/v1/fleet` on foregrounding.
- Tokens in `live_activities` (`device_id, activity_id, push_token, push_to_start_token, started_at`, `0022_live_activities.sql`), API `POST /api/v1/devices/{id}/live-activity`.

Android equivalent (Android 16 Live Updates, details to verify):
- Form: promoted ongoing notification (`Notification.Builder#setRequestPromotedOngoing(true)` + standard style / ProgressStyle), short status-bar capsule text (`setShortCriticalText`),
  requires `POST_PROMOTED_NOTIFICATIONS`; must be ongoing, titled, not colorized, no custom RemoteViews (Verify).
- **No server-direct activity channel** like APNs activity tokens / push-to-start: hub can only send FCM data (`{type:"fleet", state:"<FleetState JSON>"}`), and app locally updates / cancels notification.
- Map phase + counts → short capsule text (e.g. "💬2", "⚙3"), analogous to `island.left`; first actionable request → notification Allow / Deny / Approve buttons (as `FleetIntents`);
  `workingSince` → `setUsesChronometer(true)` + `setWhen()` for local timing; stale → `setTimeoutAfter(15 minutes)`; `lock_details` → `VISIBILITY_PRIVATE` + counts-only public version;
  tap → `persona://open?machine=&session=` for one item, otherwise `persona://open`.
- Below Android 16: regular ongoing notification (low-importance channel). Verify whether Samsung / Xiaomi / OPPO capsules/focus notifications support Live Updates APIs.
- Push budget: FCM lacks APNs activity budget but high priority can be downgraded; count changes NORMAL, new Needs you HIGH (reuse `urgent`).

---

## 9. Other platform dependencies

### 9.1 Images / files
- **Message images**: `images:[{mime, data(base64)}]` inline JSON in `POST …/messages` and `POST /assistant/conversations`, whole body ≤32 MB (`http.go:364`, `assistant.go:292`).
  Node writes temporary images, appending `<attached_images>\npaths…\n</attached_images>` to message (`server/internal/node/control.go:279-286`); app parses the tag,
  fetching thumbnails through `GET /machines/{mid}/file?path=` from the session computer (`docs/apple/ios.md`, "Sent images"). Android should compress before sending.
- **Send files**: `POST /machines/{mid}/files?name=`, raw bytes, required `Content-Length` (OkHttp known-length RequestBody, no chunked body, otherwise 411); cancellation → hub `file.upload.abort`
  deletes partial file. Success returns absolute path, inserted in composer (single-quoted for spaces/quotes), **not automatically sent** (`docs/protocol/protocol.md`, "Sending files"; `upload.go`).
- **Client-parsed message tags**: `<attached_images>`, `<persona-context kind="context|edit" title url>…</persona-context>` (in-app web text selection AI edit / Use as context),
  `<cross-session-message from-name>` (`docs/protocol/protocol.md`, "Message tags"); also remove Claude `[Image #N]` placeholders and long-paste `<pasted_content id=…>` wrappers (`CLAUDE.md` lesson).
- **Chat file paths**: open through `GET /machines/{mid}/file` (≤2 MB, binary base64, render PDFs/images locally).

### 9.2 In-app links
- Client-only (Telegram-style bottom card, preloaded WebView pool, selected-text menus); no hub involvement. Lark documents / knowledge use `GET /api/v1/lark/raw/documents/…` (`docs/extensions/feishu.md`),
  rewriting document images to hub raw paths. Android: BottomSheet + WebView pool, custom ActionMode adding AI edit / Use as context to selection menu.
- All hub media (Lark images/video, chat images) requires authentication: OkHttp / Coil interceptors add `Authorization`; ExoPlayer supports request headers; URL-only cases use `?token=` (hub logs omit queries, `http.go:196-198`).
  Video relies on Range forwarding (`CLAUDE.md` lesson).

### 9.3 Terminal (`docs/integrations/terminal.md`, `docs/protocol/protocol.md`, "Terminal stream"; `server/internal/hub/terminal.go`)
- Depends on hello `terminal_stream` / `terminal_control` / `terminal_scroll` / `terminal_click`; without `terminal_stream`, poll `GET …/screen`.
- Frames are herdr-redrawn VT bytes, with few sequences: CUP, ED(2J), SGR (16 / 256 / true color), DECTCEM, 2026, OSC 8 (`docs/integrations/terminal.md` section 1). Client implements subset + two-cell wide characters
  (right-half placeholder pitfall in `CLAUDE.md` "Write wide characters in two steps"). Changed `epoch` without full frame, or discontinuous `seq` → discard grid and resubscribe without epoch.
- **Takeover dimensions**: measure cols × rows fitting the font, then send `terminal_subscribe{control:{cols,rows}}`; keyboard / rotation / font changes debounce 250 ms into `terminal_input{control}`
  (client policy, `docs/integrations/terminal.md` section 11). Node takes control only if both >0 and herdr supports it, otherwise mirrors original pane dimensions (`server/internal/node/terminal.go:175-186`).
- Keys: map names to bytes (Enter `\r`, arrows `CSI A–D`, modified `CSI 1;m X`, ctrl+letter control character, alt ESC prefix, shift+enter `ESC \r`), bracketed paste for multiline text;
  one wheel event per row at the touched cell (`scroll` + `at`); click via `click`.
- One controller per terminal: newest control subscriber takes over, previous gets `taken`; lease: hub `terminal.keepalive` every 20 s, node releases after 75 s without it (constants).
- Phone scrollback is accumulated by comparing frames locally (herdr has no history during takeover, `docs/integrations/terminal.md` section 11, "Phone scrollback").
- Documented measurements (`docs/integrations/terminal.md` section 10, 2026-10-29, sample count unspecified): herdr observe as fast as 16–19 ms/frame; Claude Code 2.1.312 streaming redraw around 110 ms; node coalesces frames within 30 ms (constant).
- IME: iOS custom `UITextInput` (Pinyin marked text, `CLAUDE.md` lesson); Android custom View `onCreateInputConnection` / composing text.

### 9.4 Deep links
- `persona://pair?url=<hub>&token=<token>`: QR / external pairing (`PersonaApp.swift:15-25`).
- `persona://open` returns to app; `persona://open?machine=<id>&session=<id>` opens session (Live Activities, `ios/LiveActivity/PersonaActivityAttributes.swift:12-31`).
- Notification taps use payload `machine_id` / `session_id` (`PersonaApp.swift:195-206`).
- Android: `persona` intent-filter with pair / open hosts; scan pairing codes in-app (system camera custom-scheme behavior varies, Verify).

### 9.5 Watch
- No Apple Watch-specific APIs: answer-role token (`kind:"watch"`), pairing/summary pushed from iPhone over WatchConnectivity; while open, directly fetch hub every 8 seconds (sessions / interactions / machines / Inbox),
  and directly call answers / messages / interruption / Inbox done/reply / `seen` / `items?turns=2` (`ios/PersonaWatch/PersonaWatchApp.swift:75-133,461-469`, `docs/apple/ios.md`, "Data flow"). Watch does not register push.
- Wear OS can follow: answer token + Wearable Data Layer pairing transfer; system bridges phone notifications (buttons, RemoteInput included) to watch (Verify). Hub needs only platform-neutral default device names (section 11).

### 9.6 Siri / Shortcuts → Android
- Four App Intents use REST, not WS: Ask Claude (latest live session or `POST /sessions` with `prompt`), status, approve first pending (permission → `{allow:true}`, plan → `{approve:true, mode:"bypassPermissions"}`), open session (`docs/apple/intents.md`).
- Android counterparts: App Shortcuts, quick settings tiles, home-screen widgets; no hub changes.

### 9.7 Gmail Sign in with Google
- Hub Google OAuth client is **iOS type**: PKCE, no client secret, reverse-client-ID custom callback scheme `<reversed client id>:/oauth2redirect` (`server/internal/mail/oauth.go:23-26,107-154`);
  `docs/extensions/mail.md` explicitly says Google discontinued custom schemes for Android / Chrome apps but still supports iOS. Refresh uses saved `client_id`, no secret (`oauth.go:320-326`).
- Android route (Verify): Google Authorization API on phone requests `https://mail.google.com/` + offline access, returns server auth code to hub; hub exchanges with **Web-type** client (secret) for refresh token.
  Requires hub Web client config (secret file), new API (e.g. `POST /api/v1/mail/oauth/google/exchange {server_auth_code}`), and account-specific client-type secret on refresh.
  Without hub changes: Gmail app password as regular IMAP account (`POST /api/v1/mail/accounts`); Lark mail and other IMAP unaffected.

---

## 10. Android client protocol checklist

**A. Pairing and credentials**
1. Parse `persona://pair?url=&token=` (in-app scanner + deep links); `GET /api/v1/health` validation and `features`.
2. Exchange with `POST /api/v1/auth/tokens {name:<model name>, kind:"phone"}`, Keystore-encrypted storage, discard pairing token; 404 old hub retains pairing token and retries next launch.
3. Settings › Devices: `GET/DELETE /api/v1/auth/tokens`; revoke self = unpair. Wear OS `kind:"watch"` (answer role).

**B. HTTP foundation**
4. Bearer header, `X-Client-Request-Id` (8 hex characters), errors (`error` string/object, `code` + flattened fields, `X-Request-Id`), tolerate plain-text 404/405.
5. JSON: explicit names (internal keys `tool_input`, `answers`, `meta`, `args`, etc. cannot be changed by naming strategies, hence iOS explicit CodingKeys throughout, `ios/Shared/Models.swift:3-4`);
   0–9 fractional-second digits, pre-2000 times empty, unknown-enum fallback (iOS status / state / kind all have unknown).
6. Allow cleartext HTTP (network security config); timeouts per section 2.

**C. WebSocket**
7. `/api/v1/ws`, `type` dispatch for all section 4.2 events, ignore unknowns; background decoding; backoff reconnect (iOS 1→20 s), immediate network-recovery reconnect; `pingInterval(20s)`, dead after 45 s without messages (iOS policy).
8. On connect: `register_phone` (full role, phone data enabled); resubscribe open sessions with `subscribe{since_seq:covered, lazy_images:true}`; terminal with `epoch/after_seq`; reconcile `GET /interactions`.
9. Disconnect WS on backgrounding except foreground-service wake work, so hub knows phone is absent and uses push wakeup.

**D. Synchronization and sending**
10. `covered` / `history` / `from_seq` / `to_seq` continuity, gap repair after 1.5 s, latest above 8 pages, reload on `history` change; trust tail-relative `has_more` only.
11. Background sync: `sessions` `rev` + `revs` → `after_seq` increments, memory/disk cache (last 2 turns per session); lazy images + permanent cache; `full_size` full-text reading.
12. Outbox: `client_msg_id` per message; 409 `dialog_open` / `input_draft` (`clear_draft` retry), 502 `delivered:"unknown"`; offline queue, ordered send after reconnect/catch-up.
13. Send `seen` when opening / watching a session.

**E. Interactions**
14. question (multi-select labels joined with `", "`, or custom text), plan (approve `mode`: bypassPermissions / acceptEdits / default; send back `feedback`), permission (`allow`, `always`, deny `message`),
    screen (`dialog.options` + `option` / `text`, password field for `secret`; without dialog use raw screen + `keys`), `dismiss`; handle 410 (handled), 409 `prompt_changed`.

**F. Phone tools**
15. Registry (prefer iOS names/parameters), source toggles, write-confirmation UI, `tool_result` text/image blocks, simulator `simulator:true`.

**G. Push / Live Updates** (depends on section 11 hub changes)
16. FCM token → `POST /api/v1/devices` with new platform fields; channels: Needs you (high), Complete (default), ongoing Fleet (low), silent wake (invisible). Interaction buttons directly resolve; notification taps open by `machine_id/session_id`.
17. `GET/PUT /api/v1/devices/{id}/prefs` (same iOS fields, Android ignores Island-only semantics or maps to status-bar capsule); foreground WS `fleet`, background FCM `fleet` updates Live Update notification, foreground `GET /api/v1/fleet`.

**H. Terminal**
18. VT subset grid, mirror / control modes, dimension debounce, key names→bytes, bracketed paste, row-by-row wheel + `at`, `click`, `busy → takeover` dialog, `taken / lost / unsupported`, poll `/screen` without `terminal_stream`.

**I. Other domains** (section 3 API table)
19. Assistant (warm, conversations, routines, followups, runs, memory, `delta` text), Inbox, Lark (hub-relative authenticated media; open chat `around=<readPosition+1>`, report only genuinely on-screen messages, `CLAUDE.md` lesson),
    mail (Gmail 9.7), knowledge, monitoring, usage, pins / mutes, Claude Code settings, file viewer, Git.

---

## 11. Server changes

### 11.1 Required (otherwise Android receives no push / cannot wake for phone tools)
| # | Change | Files |
| --- | --- | --- |
| 1 | **Device platform and push provider**: add `platform` (ios / android), `push_service` (apns / fcm), `push_token` (or `fcm_token`) to `devices`; missing old-app fields default ios / apns; accept fields in `POST /api/v1/devices` | New migration (choose number using `CLAUDE.md` lane rules, current max `0061`), `server/internal/db/db.go:122-133` (Device + Upsert / queries), `server/internal/hub/http.go:104-112` |
| 2 | **Provider dispatch + FCM sender**: `sendAs` only builds APNs, immediately returns when `p.key == nil` (no apns.json), and `ready()` false—**Android-only deployments will not even try waking**. Add FCM HTTP v1 (service-account JWT/token cache, data messages, HIGH / NORMAL, TTL, remove UNREGISTERED / INVALID_ARGUMENT devices, refresh on 401, backoff 429/5xx), through `google.proxy` | `server/internal/hub/push.go:28-303` (`newPusher`, `ready`, `sendAs`), new e.g. `server/internal/hub/fcm.go`, `server/internal/hub/config.go` (`fcm{project_id, service_account_file}`), `docs/operations/config.md`, `docs/server/server.md` |
| 3 | **Android payloads**: interactions (type, interaction_id, machine_id, session_id, kind, title, subtitle, body), Complete, `notify_user`, wake (`{type:"wake"}`, HIGH, short TTL) | `push.go:227-231,305-354`, `server/internal/hub/assistant_tools.go:116-125` (notify_user calls `notifyDone`) |
| 4 | **Platform-aware wakeup**: `wakePhone` sends high-priority FCM data to Android, preferably only the device offering the tool | `server/internal/hub/hub.go:722-753`, `push.go:227-231` |
| 5 | **Per-device phone tools and routing** (required for simultaneous iPhone/Android; optional if Android fully replaces iPhone): `register_phone` includes `device_id` / `platform`, store tools by device (`phone_tools` with `device_id` or one row/device), merge/dedupe `mcpTools`, select `callPhone` by tool availability → foreground online → recent activity → wake | `hub.go:633-720`, `http.go:542-556,699-708`, `db.go` (ReplacePhoneTools / PhoneTools), migration |
| 6 | **Android Live Activity branch** (required for Live Updates): `flush` / `metrics` / `endAll` currently iterate only `live_activities` and APNs liveactivity; send FCM `fleet` data to `platform=android` with `prefs.live_activity` (same `forDevice` trimming, urgent distinction, empty end state) | `server/internal/hub/liveactivity.go:483-606,641-664` |

### 11.2 Optional (experience / consistency / security)
| # | Change | Files |
| --- | --- | --- |
| 7 | Platform-neutral wording (or based on online phone): failures, assistant descriptions, snapshots, `ask_user`, default device-token names | `hub.go:699,718`, `server/internal/hub/assistant.go:117,128,164`, `server/internal/hub/assistant_snapshot.go:127-132`, `assistant_tools.go:26`, `server/internal/hub/devicetokens.go:153-159` |
| 8 | Withdraw notifications after resolution: currently empty `Pusher.clear`; Android NORMAL `interaction_resolved` data lets app cancel | `push.go:356`, `hub.go:572-580` |
| 9 | Push carries `tool_call`, HTTP result (new e.g. `POST /api/v1/phone/calls/{call_id}/result`, reuse `toolResult`), avoiding background WS setup | `hub.go:693-765`, new `http.go` route |
| 10 | Android Gmail login: Web-type OAuth client (secret file), server-auth-code exchange, account-client-type refresh secret | `config.go:32-38`, `server/internal/mail/oauth.go:107-154,320-326`, `server/internal/mail/http.go`, `docs/extensions/mail.md` |
| 11 | Single-use pairing code instead of permanent QR master token (`personad hub pair` short-lived code, `POST /api/v1/auth/pair {code}` exchanges device token) | `config.go:144-158`, `devicetokens.go` |
| 12 | Link credentials to push devices (`device_tokens` platform / push device_id), remove push/tools on credential revocation | `devicetokens.go`, `db.go`, migration |
| 13 | No-GMS devices: UnifiedPush provider (hub POSTs to registered endpoint) | `push.go` abstraction |
| 14 | Align docs: add **[Undocumented]** APIs/WS events (`assistant_schedule`, `hook_event`, `error`, `seen`, `queue`, `info`, `file`, `lark/raw`, etc.) to protocol.md; remove absent `session` / `ping` / `pong`; check architecture.md claim of immediate hook allow without online app | `docs/protocol/protocol.md`, `docs/server/architecture.md` |

No changes needed: sessions / items / interactions / terminal / files / Inbox / Lark / mail (except Gmail login) / knowledge / monitoring / pins / mutes / per-device preferences—all platform-independent JSON usable directly by Android.
The `island` structure in `prefs` can be reused unchanged for status-bar capsule contents, with no new fields.

---

## 12. Unresolved / verification needed

- Android details marked Verify in sections 7, 8, 9 (FCM payload limits / downgrade rules / processing windows, exemptions for background foreground-service startup, Live Updates eligibility, Health Connect history/background permissions,
  background clipboard writes, Geocoder availability, Wear OS notification bridging, vendor capsule Live Updates support) were not checked online and are platform knowledge.
- Whether user's Android has Google Play services (FCM vs UnifiedPush / vendor push / persistent connection); simultaneous iPhone/Android use (whether item 5 is required); Android distribution channel (SMS/call-log tool eligibility).
- Only routes/types read, not detailed implementations: Inbox AI twin, `server/internal/lark` client, knowledge engine, node headless assistant (`delta` confirmed only through `server/internal/node/assistant.go:429`).
- `screen` WS: hub still forwards, but no node `session.screen` emitter found; treated as legacy, not every branch confirmed.
- No performance measurements this round; cited figures include sources and sample counts (explicitly noted where docs omitted count).
