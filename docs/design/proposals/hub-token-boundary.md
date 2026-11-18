# Proposal: a token on the hub machine is not the owner's approval

Status: not planned (#5525). The owner decided on 2026-11-12 that the change is too large for now. This page keeps the
findings and options for later; nothing here is built. Covers the hub's authentication, the managers' view of who
called, the command line on the hub machine and the agent sandbox. The clients change only where a step says so.

**Who it serves**: everyone who runs a hub. What Persona and its Agents change in the owner's name should be something
the owner approved on their own device. Everyday screens stay as they are. The visible additions are a card when a
program on the hub machine asks to change something, and a line in Settings › Devices saying how each device was added.

## 1. Problem

An assistant conversation on the hub machine read the hub's master token from `hub.json` and changed one of the
owner's source subscriptions through `PUT /api/v1/subscriptions/{id}`. No card was shown. The assistant's own tool for
the same change, `subscription_save` (#5240), shows a before → after card on the phone and applies only after the
owner approves it. The HTTP route the assistant called instead has no such step.

It worked because of four facts, each true on its own:

| Fact | Where |
| --- | --- |
| The master token is treated as the owner. It maps to a full principal with no device, and `auth` refuses it only from the internet: loopback and the LAN pass. Over loopback it counts as "the owner at this machine" and may install extensions | `lookupToken` in `server/internal/hub/devicetokens.go`; `auth` in `server/internal/hub/http.go`; `fromThisMachine` in `server/internal/hub/extensions_link.go` |
| Everything on the hub machine runs as one account: the hub, the built-in manager, the node, the assistant's Claude, the Agents in herdr and the extensions. `hub.json` is `0600`, which keeps other accounts out and nothing else (#813) | `docs/server/agent-sandbox.md`; `scripts/deploy/hub-systemd.sh` (user units, no `User=`) |
| The assistant starts in `bypassPermissions` with Bash, so a shell command that reads the file and calls the API asks nothing. The command gate classifies calls proxied for extensions, not a request to the hub | `docs/assistant/assistant.md`, "Assistant permission defaults" |
| The managers cannot tell the master token from the owner's phone. The hub relays `X-Persona-Role: full` and no caller, and the assistant and devices managers check only the role | `relayHTTP` in `server/internal/hub/bus_forward.go`; `fullRole` in `server/internal/assistant/service.go` |

The checks that do refuse the master token can be bypassed:

- **Secret hand-off** (#3962) refuses the hub token as an approver. But the master token can create a full device
  credential (`POST /api/v1/auth/tokens`), and a device credential passes `personSent`, `handoffApprover` and
  `byOwnDevice`. The only trace is a "new device paired" notification, or a log line when push is not set up.
- **Cards** can be answered with the master token: permission, plan and question cards, "Always" included. Only
  hand-off cards refuse it (`fleetRoutesRelayed` in `server/internal/hub/fleet_link.go`, `serveChoose` in
  `server/internal/hub/choose.go`). So an Agent with the token can approve the card its own action raised.
- **The admin UI** needs a password session, but `personad hub admin reset-password` writes a new password straight
  into Postgres with `database_url` from `hub.json`, reading it from standard input when there is no terminal
  (`adminResetPassword` in `server/internal/hub/admin_cli.go`).

So on the hub machine today, every "a person is here" check comes down to "can read `hub.json`".

## 2. What a process in the hub's account can reach

Read from the code and from a live hub machine, file names and modes only: no value was opened or printed.

### 2a. Credentials and what each opens

| Credential | What it opens | Through the API? |
| --- | --- | --- |
| `hub.json` `token`, the master token | The whole client API from loopback and the LAN, including pairing codes, admin setup codes, mesh peers and extension installs (table 2b) | Yes |
| `hub.json` `database_url`, which holds the database password | Every table: device credentials, the admin password (`hub admin reset-password`), bus tokens (`hub assistant-token`, `hub manager-code`), the owner's data | No: bypasses the API |
| `secret.key`, `handles.key` (at the top and in `managers/<id>/`) | Every sealed secret in the database: connector and extension secrets, mail passwords, model keys (#813) | No |
| A standalone manager's `manager.json` `token` | Connecting to the hub's bus as that manager (`server/internal/managerconf/binding.go`) | The bus |
| `launch.key` | Asking the node for any managed launcher's settings, provider keys among them (`launcher.env` on `node.sock`) | Node RPC |
| `launcher-settings/`, `tls/`, `link-tls/`, the APNs key, the relay and tunnel tokens | Launcher tokens; the hub's and Persona Link's TLS identities; push notifications to the owner's devices as Persona | No |
| A model key file named in config (`ai.api_key_file`) | The model provider account. The sandbox and the node's path guard cover it only if its name matches the credential list (`server/internal/identity/secrets.go`) | No |
| The environment of extension programs the built-in manager starts | Extensions defined in `hub.json`, and installed ones granted full `hub_api`, get the master token itself as `PERSONA_HUB_TOKEN` (`hubEnv` in `server/internal/extmgr/extensions.go`) | Yes, as the master token |
| `run/hub-edge.sock` | `switch` starts an executable as the hub (`server/internal/hubedge/control_edge.go`) | Hub control |
| `run/slot-*.sock` | The hub's API. The hub believes the PROXY header that starts each connection, so a process there can claim any source address, loopback included (`server/internal/hubedge/conn.go`) | Yes |
| `node.sock`, `runtime.sock` | The node's RPC for MCP tools, hooks and runtimes. This is by design; the caller is identified by pid | Node RPC |
| Membership of the `docker` group | A container that mounts the data directory, or a shell in the database container. Equivalent to root | No |
| Passwordless `sudo` | Root | No |
| A writable `personad` binary or user unit | Code the hub runs at its next start | No |
| `~/.ssh` keys | The other computers the hub reaches over SSH | No |
| A browser signed in to the web app on the hub machine | The device credential it keeps, which is a person's credential | Yes |

A value printed by an Agent also ends up in its transcript, which Persona syncs to every client: only public
sharing redacts (`server/internal/transcript/share_sanitize.go`).

### 2b. What the master token changes without a person

| Area | Routes (examples) | Guard today |
| --- | --- | --- |
| Source subscriptions | `POST`, `PUT`, `DELETE /api/v1/subscriptions…`, `…/run` | Full role only (`server/internal/extmgr/subscriptions.go`) |
| Assistant and hub settings, memory | `PUT /api/v1/assistant`, `…/assistant/memory/{name}`, `/hub/update/settings`, `/machines/{mid}/claude-settings` | Full role, or none |
| Model profiles, launchers and their keys | `…/model-profiles/{id}`, `…/apply`, `…/rollback`, `/launchers…`, `/launchers/import` | Full role; recorded as "hub" |
| Extensions | `…/extensions/{id}/-/enabled`, `/config`, `/secrets/{name}`; install, upgrade, uninstall, sandbox, dev from loopback | Full role; loopback grants the owner flag (`X-Persona-Owner`) |
| Devices and pairing | `POST /api/v1/auth/tokens`, `DELETE /api/v1/auth/tokens/{id}`, `POST /api/v1/auth/pair-codes` | Role only; pairing codes need loopback |
| Computers and mesh | Adding a computer (already confirmed), `…/confirm`, `…/reject`, managers, `/admin/cli/peers…` | `mayTrust` accepts it; peers need loopback |
| Agents | Start, message, change permission mode, interrupt, close | None beyond the role |
| Cards and rules | `…/interactions/{id}/resolve`, `/choose`, `/reanswer`; "Always" rules, standing authorizations | Only hand-off cards refuse it |
| Sending as the owner | Inbox replies, `POST /api/v1/mail/send`, extension messaging | None; a connector reply raises a card, which the master token can answer |
| To-dos, knowledge base | `…/todos…`, `PUT /api/v1/kb/{kind}/{id}` | Full role, or none |
| Files, git and terminals on computers | `…/machines/{mid}/files…`, `…/git/{commit,push,…}`, panes, node updates | None beyond the role |
| Push registration | `POST /api/v1/devices`, `…/prefs`, `/notify` | Bound to an empty credential |

What already refuses it: the internet, agent sharing (which needs an owner-bound device, `docs/server/agent-sharing.md`,
"Trusted ownership"), the secret hand-off card, recalling a sent message, browser cookies, iPhone control, and the
closed admin routes, which need the password session. The WebSocket message paths have not been checked.

### 2c. Who uses the master token

- **Command line**: `personad hub pair` (and `--legacy`, which puts the master token in the QR code),
  `hub peer …`, `hub admin access` and `setup-code`, `hub setup`, `hub doctor`, `personad extension …`
  (`server/internal/extmgr/extension_cli.go`) and `extension dev`.
- **Direct to Postgres, no token**: `hub admin reset-password`, `hub assistant-token`, `hub manager-code`.
- **Extensions**: through `PERSONA_HUB_TOKEN`, from the built-in manager only. A relayed manager already gives scoped
  `pext_` tokens (`hubStartEnv`).
- **Scripts**: `scripts/operations/hub_action.py`, `scripts/release/promote_hub_release.py`,
  `scripts/release/promote_node_release.py`, `scripts/app/sim.sh` and `scripts/app/ui-test.sh` (which pair the simulator
  with it), `scripts/app/personactl.py`,
  `scripts/test/hub_restart_seamless.py`, `scripts/ci/image-smoke.sh`. `scripts/deploy/deploy.sh` does not use it.
- **Tests**: about 97 test files in `server/internal/hub` use the config's token, most through `call()` and
  `newTestHub`; around 100 writes in 37 files.
- **Docs and the web**: about 20 maintained docs show `Authorization: Bearer` examples, many of them with the master
  token, and the web login page offers signing in with it (`web/src/views/Login.tsx`).

## 3. Threats and goals

| Level | Who | Example |
| --- | --- | --- |
| T1 | A helpful Agent takes a shortcut with a credential it finds | Today's incident |
| T2 | An Agent steered by content it read (mail, a web page, an issue) | Asked to "update the settings" by a message |
| T3 | A process set on getting round every check, with the account's full rights | Rewrites the database, replaces the hub's binary |

**Goals**

1. No credential a local process can read is, by itself, enough to change the owner's data or answer the owner's
   cards through the API (T1, T2).
2. Agents keep a supported way to make every change: their Persona tools, which ask on a card.
3. The owner's command line keeps working, with a plain story of what it asks and why.
4. Each step can be turned back by a setting the owner can see.

**Not a goal**: stopping T3 on a hub machine where Agents run in an account that can become root (the `docker` group,
passwordless `sudo`, a writable hub binary). Nothing the hub checks holds there; only a separate account (option A2)
does. The proposal says so plainly instead of claiming more.

## 4. Options

### A. Keep Agents away from the hub's files

**A1, the agent sandbox on by default on the hub machine.** It is already built (`docs/server/agent-sandbox.md`):
bubblewrap on Linux, `sandbox-exec` on macOS. It is off by default. Turning it on hides `hub.json`, the keys,
`managers/`, `launcher-settings/` and the `run/` sockets from the starts it wraps: the assistant, managed launchers
and `codex-start`.

- **Stops**: T1 for wrapped starts. The token, the database password and `secret.key` cannot be read, so the API,
  the database and the sealed secrets are out of reach the easy way.
- **Does not stop**:
  - the Docker socket, which the sandbox leaves open on purpose;
  - driving programs outside the sandbox: `systemd-run --user`, herdr's or tmux's sockets, `ssh localhost`;
  - files the hub runs later: the `personad` binary, user units, shell startup files;
  - starts it does not wrap (plain `claude` launchers, herdr resume, the assistant's Codex);
  - the master token in extension programs' environment.

  So it does not hold against T2 or T3.
- **Impact**:
  - Installs: Linux needs bubblewrap and unprivileged user namespaces. Machines without bubblewrap cannot use it.
  - Operations: `personad hub admin` and `personad extension` fail inside Agents (intended). Sandboxed Agents restart
    with the node on Linux.
  - Tests: the existing real-sandbox tests.
  - Users: nothing visible but the doctor line.
- **Migration and rollback**: a new default when the probe passes, with the doctor saying why when it does not; the
  existing switch turns it off.

**A2, separate accounts.** Two shapes:

- **The hub in a service account.** The hub, its managers and the data directory belong to a `persona` account, and
  the owner's account cannot read them. This holds only if the owner's account cannot become `persona`: not in the
  `docker` group, no passwordless `sudo`, no write access to the hub's binary or units. Many cloud machines give the
  login account passwordless `sudo`, so this shape protects nothing there. The Docker deployment is close to it already
  (no Agents in the container, `PERSONA_LOCAL_NODE=0`), except that `./data` and the `docker` group give the host
  account everything.
- **Agents in their own account** (`agents`) without `sudo` or `docker`. This holds even when the owner's account is
  all-powerful.

What A2 does:

- **Stops**: T1, T2 and T3 on that machine. The kernel keeps the files, sockets and processes apart.
- **Does not stop**: what the Agents' account is given on purpose. An API credential handed to it still works.
- **Impact**:
  - Installs: the biggest change. An account at setup, herdr and the engines started as it, separate Claude and Codex
    sign-ins, and git and SSH credentials for it.
  - Projects: shared through a group or ACLs, or kept as separate checkouts.
  - The node: starts panes as another account.
  - Users: the owner's terminal and their Agents no longer share a home directory.
- **Migration and rollback**: an install-time choice with a documented way back (run Agents in the owner's account
  again).

### B. The master token only for the owner's terminal

The hub stops taking a bearer master token over TCP. The command line talks to the hub over a Unix socket in `run/`.
The hub reads the peer's account with `SO_PEERCRED` (Linux) or `getpeereid` (macOS) and accepts operator requests
only from its own account. The token leaves `hub.json`.

- **Stops**:
  - copying: there is no value to read, so nothing lands in a transcript, a backup, a log or another machine;
  - use from the LAN;
  - use from any other account or from a container without the socket;
  - with A1, use from sandboxed Agents too, since the sandbox already hides `run/`.
- **Does not stop**: use from the same account. `SO_PEERCRED` reports an account, a group and a pid, and nothing
  that tells an Agent from the owner's terminal. An Agent can open the socket or run `personad hub admin` itself. Pid
  ancestry and cgroups do not help, because a double fork or `systemd-run --user` puts the process outside the Agent's
  tree. The slot sockets (`run/slot-*.sock`) must get the same check, because their source address comes from a header
  the caller writes. Direct database writes are untouched while `database_url` stays readable.
- **Impact**:
  - Command line: every command in section 2c moves to the socket.
  - Scripts that call the API with the token switch to a small `personad hub api` call over SSH, or to a credential
    scoped to what they do.
  - Pairing: `--legacy` pairing and the web login's token sign-in go; `sim.sh` and `ui-test.sh` pair with a code.
  - Docker: commands run inside the container.
  - Extensions: get scoped tokens (they need them anyway, step 4).
  - Tests: `call()` and `newTestHub` inject an operator principal, a change to the shared helpers more than to 97
    files.
  - Users: nothing visible.
- **Migration and rollback**: `security.master_token` moves from `accept` (today) to `loopback` (LAN refused, doctor
  warns) to `socket` as the default, one release apart. Setting it back restores the old behavior, and `hub.json`
  keeps the field during the window so older command lines work.

### C. Changes to the owner's data need a person on a device

The hub sorts callers into two classes:

- **People**: a trusted full device credential (a phone, a tablet, a signed-in browser), an answer-only watch within
  its allowlist, and the admin UI's password session.
- **Programs**: the master token, extension tokens, pairing codes, Link and peer credentials, and any device not yet
  trusted.

Rules:

1. **Only people answer cards.** `resolve`, `choose` and `reanswer` refuse programs with `403 person_required`,
   except that extension tokens keep the narrow answers `extensionResolveRefused` allows today.
2. **A device made by a program is not trusted until a person says so.** A credential minted with the master token,
   or redeemed from a pairing code made by it, starts untrusted. The owner's trusted devices get a card: "New device
   '<name>' wants to join, added from the hub machine. Allow / Deny". The first device of a hub with no trusted device
   is trusted when paired, so setup is unchanged. Password sign-in creates a trusted device. `personSent`,
   `handoffApprover`, `byOwnDevice` and `captureHumanSubmission` require a trusted device.
3. **Programs write only from an allowlist.** Like the watch's `answerWrites`: a short `programWrites` list of
   operational routes (health, reload, update status) plus each extension token's granted scopes. Any other write from
   a program gets `403 person_required`, saying which tool asks instead (`subscription_save` for subscriptions,
   `persona_settings` for settings). New routes are closed until listed. The hub tells the managers the caller's class
   in the relayed request, and the managers check it too. The assistant's and the Agents' Persona tools are not
   affected: they reach the manager through the node and the bus, not with a token, and this proposal does not change
   which of them ask.
4. **The command line asks on the phone.** A changing command (`personad extension install`, `hub peer add`,
   `hub pair --machine --add`) sends its request to the hub, which keeps it under an id and shows a card: "A program
   on the hub machine asks to install the extension 'X' (it may: …). Allow / Deny". The command waits with a time
   limit and ends with one line saying what happened. Approval runs exactly the request it was shown, once.
   `fromThisMachine` no longer grants the owner flag by itself.
5. **Refusals are visible.** Each `person_required` is recorded with the credential's kind and the route (never the
   body) and listed in the admin UI and `hub doctor`, so the owner sees when an Agent tried.

What C does:

- **Stops**: T1 and T2 through the API with any credential found on the machine. Today's incident would become a
  refusal naming `subscription_save`. Answering its own card, minting a person's device and changing settings from
  the LAN with the token are stopped too.
- **Does not stop**:
  - direct database writes (`database_url`, Docker);
  - reading the sealed secrets with `secret.key`;
  - replacing the hub's binary;
  - `sudo`;
  - a person's credential kept on the hub machine (a browser signed in there);
  - reads: programs still read mail, chats and settings;
  - an owner who approves without reading. Cards say before → after in plain words, and the Agents' own tool cards
    do not change.
- **Impact**:
  - Command line: the commands above wait for a card, with a time limit and a one-line verdict.
  - Scripts: the release scripts that change the hub's update settings or update nodes, and live checks that start a
    throwaway Agent with the token, need a card per run or a credential scoped to exactly those routes.
  - Tests: card, device and write tests use a trusted device fixture instead of the config's token. Most go through
    `call()`.
  - Clients: a "new device" card, the program-request card, and in Settings › Devices how each device was added. Older
    apps that signed in with the master token can no longer answer cards and must pair again.
  - Users: one card when they, or an Agent, change something from the hub machine's command line.
- **Migration and rollback**:
  - Every full device credential that exists at upgrade is trusted, and Settings › Devices lists them with their dates
    for a one-time review.
  - Each rule ships behind `security.program_writes`: `allow` (today), `ask` (the default after the rollout), and an
    interim `log` that records what would be refused.
  - Setting it back to `allow` restores today's behavior; the admin UI and the doctor then say so.
  - The new device column is ignored by an older hub.

## 5. Comparison

| | A1 sandbox | A2 separate account | B terminal-bound token | C person-confirmed |
| --- | --- | --- | --- | --- |
| A helpful Agent reads `hub.json` and calls the API (T1) | Stopped, for wrapped starts | Stopped | Copying stopped; using the socket or the command line is not | Stopped: refused, names the tool |
| An Agent answers its own card | Stopped while it has no credential | Stopped | Not stopped | Stopped |
| A program mints a person's device | As above | Stopped | Not stopped | Stopped |
| Reading the database password or `secret.key` | Stopped, except through Docker | Stopped | Not stopped | Not stopped |
| Writing the database through Docker or `sudo` | Not stopped | Stopped when that account has neither | Not stopped | Not stopped |
| Leaving through `systemd-run`, herdr's socket, `ssh localhost` | Not stopped | Stopped | Not stopped | Writes still refused |
| The token in transcripts, backups and logs | Reading stopped | Reading stopped | Stopped: no token | Stays, but cannot write |
| The token from the LAN | Not stopped | Not stopped | Stopped | Writes refused |
| Works on every install | Needs bubblewrap or `sandbox-exec` | Needs an account set up | Yes | Yes |
| The owner's day | Nothing | Much changes | Nothing visible | A card for command-line changes |
| Effort | Small: built | Large | Medium | Medium to large |

## 6. Recommendation

C, then B, with A1 on by default where it works, and A2 documented for owners who need T3.

C is the only option that changes what a token means, and it works on every install: Docker, Linux and macOS, with or
without bubblewrap. A and B make the token harder to get without changing what it can do. B removes the copyable
secret and the LAN path that C leaves. A1 covers the files C does not: the database password and `secret.key`.

| Step | What | Size |
| --- | --- | --- |
| 0 | The assistant's prompt says the hub's files and HTTP API are not its tools; deny rules for the data directory in the assistant's Claude settings (a speed bump, #813 option 4); protocol and config docs say the master token is the operator's, not an Agent's | Small |
| 1 | C rule 1: only people answer cards | Small |
| 2 | C rule 2: trusted devices, the new-device card, existing devices trusted at upgrade | Medium; touches the clients |
| 3 | C rule 3 in `log` mode, then `ask`: the `programWrites` allowlist, `person_required`, the caller's class relayed to the managers | Medium |
| 4 | The built-in manager gives extensions scoped tokens, never the master token | Small |
| 5 | C rule 4: the command line asks on the phone; `fromThisMachine` stops granting the owner flag | Medium |
| 6 | B: the operator socket with a peer check (slot sockets included), `security.master_token` phases, scripts, the web login and `--legacy` pairing | Medium |
| 7 | A1 on by default where the probe passes; the doctor warns when Agents on the hub machine can reach Docker or passwordless `sudo` | Small |
| 8 | A guide for running Agents in their own account (A2) | Docs |

Steps 1 and 4 stand on their own and close the two bypasses in section 1 first.

## 7. Decisions for the owner

| # | Decision | Recommended |
| --- | --- | --- |
| 1 | Direction | C, then B, with A1 where it works (section 6) |
| 2 | Changes from the hub machine's command line | Ask on the phone. The alternative, the command line stays the owner, leaves Agents a way to do through the command line what they did with the token |
| 3 | Devices that exist at the upgrade | Trusted, with a one-time review list. The alternative is to confirm each once |
| 4 | Reads by programs | Unchanged for now; a follow-up issue |
| 5 | The agent sandbox on by default on hub machines | Yes where the probe passes (Linux needs bubblewrap) |
| 6 | A way back to today's behavior | Yes: `security.program_writes: allow`, shown in the admin UI and the doctor |

## 8. Open questions

- **Commands that write the database directly** (`hub admin reset-password`, `hub assistant-token`,
  `hub manager-code`): move them behind the operator socket and notify every trusted device. A process that can read
  `database_url` can still do the same itself; only A closes that.
- **A lost phone**: with no trusted device left, recovery from the terminal is a path an Agent can also take. Make it
  loud: the hub stopped, every device notified, an entry in the record.
- **Pairing codes** act as a full credential for ten minutes for older iOS apps (`docs/protocol/protocol.md`, "Temporary pairing").
  They count as programs for writes; check what old apps do in those minutes.
- **Mesh members**: whose devices approve a member hub's program request.
- **The WebSocket's message paths** were not part of the route inventory.
