# Phone control (iPhone)

The assistant operates an iPhone through one of the user's computers (a Mac or a Linux computer): its node reaches the iPhones paired with it over
RemotePairing and gives the assistant the screen, taps, swipes and typing (#3452). There is no automation app on the
phone and no VPN on it. It is the built-in extension **`iphone`**, parallel to `cua` ([computer-use.md](computer-use.md)):
**off by default**, then turned on computer by computer, and an iPhone is operated only after **that iPhone itself** allowed it.

| Block | Issue | What |
| --- | --- | --- |
| A | #3452 | The device bridge (RemotePairing) and its wire schema ([iphone-bridge.md](iphone-bridge.md)) |
| B | #3452 | Pairing and sessions: the pairing record, finding the iPhone again, reconnecting without replaying input ([iphone-pairing.md](iphone-pairing.md)) |
| C | #3450 | The node's built-in program `iphone`: `iphone.status`, handing `iphone.call` to D, pair / forget / cancel |
| D | #3452 | The content: element numbers, reading the screen, timeouts, human-like input, which steps need approval |
| E | #3456 | The manager side (this page): the extension, its settings and switches, the assistant's tools, approvals, consent; the app |

Not verified on a real iPhone yet: everything here is tested against a fake node.

## Settings and switches

The manifest is written in English and translated into Simplified Chinese and Japanese from the hub's catalog
(`hub-iphone.manifest_*`; a built-in has no package to read `l10n/` from, `Registry.AddBuiltinTranslated`).

### Switches

Three things, all needed before the assistant can touch an iPhone:

1. **The extension** `iphone`: Settings › Extensions, or `personad plugin enable iphone` on the hub machine. Off by default.
   While it is off the manager asks no node about iPhones, the machines carry no `iphone` and the assistant has no `iphone_*` tools.
2. **The computer**: on each computer's page, **Phone Control** › **Allow the assistant to operate iPhones through this computer**
   (`PUT /api/v1/machines/{mid}/iphone`), kept in the extension's `machines`. The extension's page shows the same list, one toggle per computer (`x-os: darwin, linux`).
3. **The iPhone's own consent**: in the Persona app on that iPhone, the guided setup's last step (Settings › Connectors ›
   This iPhone › **Let the Assistant Operate This iPhone**, "Guided setup" below; `PUT /api/v1/iphone/consent`), kept in
   the extension's `phones` by the node's device id.
   Only the user's own signed-in apps can give it (a master token or an extension's token gets 403). The hub cannot tell
   which of the user's devices sent it (an app cannot prove it is a given iPhone), so the switch is offered only in the
   app on an iPhone, for the iPhone it identifies as itself (below). Taking it back works even while the extension is off.
   `phones` is in the extension's schema only so it can be stored: the settings form does not show it, the general
   settings route (`PUT /api/v1/extensions/iphone/-/config`, and `personad extension config` through it) keeps the stored
   value whatever it is sent, and the assistant's settings card refuses it. Only the consent route changes it.

An iPhone is **allowed** when a computer that reaches it is on and its id is in `phones`. The assistant's tools appear only while
some iPhone is allowed, and only allowed iPhones can be named. Everything else fails closed: an iPhone the app could not
identify never gets consent, a computer turned off takes its iPhones away, taking the consent back takes the iPhone away.

The extension's `confirm` (**Ask on the phone before every action**, off by default) goes down with every `iphone.call`
(`proto.IPhoneCall.Confirm`); the node core asks step by step, with the screen in view, and the manager asks nothing
first itself. Without it only the steps the core marks as needing approval are asked.

### Guided setup

Who it serves: an everyday user who wants the assistant to use their phone (#8199). The way in is the first row of Settings ›
Connectors › This iPhone (always there on an iPhone, its line saying how far setup is), the link `persona://iphone/setup` (what a
failed iPhone tool call has the assistant give, "Setup not finished"), and `-openIPhoneSetup YES` in DEBUG builds. It opens
`IPhoneSetupView`: an overview (what it does, what it needs, the five steps with their state, the safety rule), then one screen per
step, then the page the user comes back to. Every step's state (Done, Working, Needs you) comes from the hub, never from a tap
(`IPhoneSetupFlow`), so a step done elsewhere is skipped and a screen moves on by itself:

1. **Turn On Phone Control**: the extension, in place (`PUT /api/v1/extensions/iphone/-/enabled`).
2. **Choose a Computer**: the computers that are Macs and online; a Linux computer is listed as not available yet, since the
   developer disk image comes with Xcode (the owner's call: a Mac with Xcode for now). Choosing one turns its switch on and installs
   the iPhone helper there when it isn't yet (`POST …/iphone/install`). A Mac without the image (`ddi_present` false) says to
   install Xcode.
3. **Connect and Pair**: once this iPhone shows on that computer's cable the app starts pairing by itself, once, and the screen
   says what the job waits for (`job.code`: tap Trust and enter the passcode; unlock). The iPhone that paired from here is the one
   this app remembers as itself.
4. **Turn On Developer Mode**: when the computer reports it off, where to turn it on, that the iPhone restarts once, and that
   setup picks up after. Then **Prepare** (`POST …/iphone/prepare`), with what it does in plain words: nothing reinstalled or erased,
   a component that goes away at the next restart and comes back by itself; Details says it is Apple's developer disk image and
   that Apple's signing server receives the chip ID.
5. **Allow This iPhone**: this iPhone's consent as a switch on this screen ("Which iPhone is this one" below), with the safety
   rule. Below iOS 27 the overview and this step say the assistant can look and open apps but not tap or type.

Done, the page holds the consent switch (turning it off here), **Try It** (`POST /api/v1/iphone/try`: a screenshot and Weather
opened, without the model) and **Ask Me Before Every Step** (the extension's `confirm`), the only optional switch. The page asks
the hub again every two seconds while a step waits on the cable, the phone or a job, and only while it is in front.

### On the web (#8217)

Who it serves: someone setting up from a computer's browser, with the iPhone plugged into that computer. A computer's page has
**iPhone control** below Browser while the extension is on (`proto.Machine.iphone`), a Mac or a Linux computer
(`web/src/views/machines/IPhoneSection.tsx`, its states in `web/src/lib/iphone.ts`). It reads the same codes as the guided setup,
in the same words: the computer's switch; a Linux computer without the developer disk image says it can't operate iPhones yet,
and why; **Install the iPhone helper** with its progress and Cancel, and why an install failed; a Mac without Xcode says to install
it, with Check again; each iPhone in one row, with **Pair** for one on the cable (the row says what pairing waits for, and Cancel),
where to turn on Developer Mode, **Prepare** with what it does and doesn't do (and Details), Locked, Waiting for this iPhone to
allow it, Ready; Forget in a paired iPhone's menu, after asking. A row shows Pair or Prepare as working the moment it is clicked,
before the hub answers. The consent is only the iPhone's to give, so the section says where on the iPhone. It asks the hub again
every 2 s while a job runs and every 5 s while the next step is at the iPhone (the cable, Developer Mode, the lock, the consent),
only while the tab is shown, and shows whichever of its own read and the machine list's push is newer (`checked`).

### Which iPhone is this one

The app on an iPhone cannot read its own device id (iOS gives apps no UDID, and the device name is just "iPhone"). So the
guided setup lists what the computers report (`GET /api/v1/iphone`) and matches on what the app does know: its model identifier
(`utsname.machine`, such as `iPhone17,1`) and iOS version, against each device's `product_type` and `ios`.

- **Exactly one match**: that is this iPhone; the switch acts on it.
- **Several**: the last step lists them (name, the computers that reach it) and the user picks **This One**. The pick is
  remembered on the phone (by device id) until the hub no longer lists it; an iPhone paired from this app's guided setup is
  picked by itself.
- **None**: there is no switch, and the step says to keep the iPhone near its computer and unlocked.

A wrong pick can only allow the other iPhone the user also owns and paired; the consent is still the user's, from their own app.

## The assistant's tools

Descriptions in `server/internal/prompts/mcp/iphone/`; handlers in `server/internal/extmgr/iphone.go`. Assistant
conversations only, like `computer_*`.

| Tool | Arguments | Does |
| --- | --- | --- |
| `iphone_status` | `{}` | Each allowed iPhone: id, name, model, iOS, the computer it is reached through, connected, transport, locked, ready, note, plus what the node says of it now (`iphone.call` op `status`, as `detail`); `not_allowed` counts the rest |
| `iphone_look` | `{phone?, screenshot? = true}` | `iphone.call` op `look`: the app in front, a numbering, the screen as numbered elements and a screenshot |
| `iphone_do` | `{phone?, steps, numbering?, screenshot?, timeout_ms?}` | `iphone.call` op `do`: the steps in order, stopping at the first failure. Steps are the core's ([iphone-control.md](iphone-control.md)): a target is `{"n"}`, `{"label", "role"?}` or `{"x", "y"}`; actions tap, long_press, drag, swipe, type, key, button, launch, terminate, wait |

`phone` is an id or a name (several allowed iPhones with the same name must be named by id); without it, the only
allowed iPhone. The target comes from what the computers said last, and only when that does not settle it are they asked
again. A device several computers reach goes through one where it is
connected. Each call carries the conversation's session `persona-<first 8 characters of its id>`. The hub log records the
tool, the actions (`do [tap type]`), the time and ok / failed (with what was sent), never typed text or screenshots.

`iphone_do` takes `timeout_ms`, the node's time for the call (1–120 s, 30 s by default); the manager waits 5 s more.

**Setup not finished** (#8199): when a call fails because of a setup the user hasn't finished (no iPhone allowed yet; the
extension or the computer off; the iPhone not allowed, not paired or no longer paired, Developer Mode off, not prepared;
no bridge on the computer), its result also tells the model to give the user the link `persona://iphone/setup`, which the
iPhone app opens as the guided setup. `iphone_status` carries the same line (`setup`) while some iPhone the computers
reach isn't set up, and so does `persona_settings` on `extension.iphone` while no iPhone is allowed. It is only in these
results, never in the tools' descriptions or the assistant's prompt (`iphoneSetupHint`, `extmgr/iphone.go`).

### Failures

The call helper (`iphoneCall`) checks again, on every call, that the extension is on, the computer is on, online and has the
program `iphone`, and the iPhone is allowed there (a target picked a moment earlier can be stale). A failure is an `*rpc.Error` whose data is
`{"sent": "no" | "unknown"}` (`proto.IPhoneSent`):

- `no`: nothing reached the iPhone (the extension off, the computer off or offline, the iPhone not allowed, a node too old, or
  the node's own word). The model is told so; trying again once that changed is safe.
- `unknown`, or a failure that says neither (a timeout, a dropped connection): some of it may have happened. The model is
  told to look at the screen with `iphone_look` first and not to repeat the steps blindly.

The names are `iphone_*` (and the node's methods `iphone.*`), not `phone_*`: `phone_*` are the iPhone's own data tools,
which the hub hands to the app ([phone-tools.md](../apple/phone-tools.md)).

### Approval

`iphone.call` answers `{output?, text?, image?, needs_approval?, approval_summary?, approval_id?}` (`proto.IPhoneResult`),
`approval_summary` being the core's structured approval (`proto.IPhoneApproval`): the step, its action, why
(`needs`: payment, delete, send_message, password, confirm, unchecked), the app, the target or the point (`at`, when the
screen gives no elements), the text it types (never a password: only `text_len`), the key, button or app, and with
`confirm` the steps one answer covers; `evidence` is English facts for the log.

The manager words the usual permission card itself, in the user's language (`hub-iphone.card_*`, `extmgr/iphone_card.go`):
"On “<iPhone>”, in <app>: Tap “Pay ¥36.00”", the reasons, and the steps it also covers. A point reads "(x, y) on the
screen (target not identified)". The card's input is the approval without its evidence, never the model's arguments
(which can hold a password being typed). The card has no picture yet, so the screenshot the core keeps with an
unchecked approval is not on it.

**One do at a time per iPhone.** An `iphone_do` holds that iPhone's turn (`iphoneTurn`, across conversations) until its
approvals are answered and it is done; a look never waits. While a card waits, and it may show on that very iPhone (a
notification banner over another app), no other input reaches the phone, so the automation can never tap its own
approval. The core also never operates Persona's own app: each iPhone's app sends its bundle id and display name with
its consent (`app`), kept in `self_apps` / `self_names` (only the consent route changes them, as `phones`) and passed
down with every call.

**What typed text leaves through the manager.** A password is never in the card (its text or its input), which the hub
stores and pushes, nor in the manager's log: the core gives only its length. Other text a step types is quoted on the
card, so the owner sees what is sent, and so is in the card's history and its push. The log names the actions only. The
model's own call, in the assistant's transcript, holds what it wrote; it can only type a password it already has in the
conversation.

The core's refusals (`rpc.Error` codes, all with `sent: no`): `bad_args` (it could not read the arguments or steps: the
model is told to fix its call), `not_pending` (a resume of an approval it no longer holds: said as it is, not tried
again), `busy` (a resume while another call runs on the phone: the approval is still pending, so the manager tries the
same id again after 1, 2 and 3 seconds without asking the owner again, then tells the model to try again later).

Once allowed, the manager resumes the do with `approved: <approval_id>` and no arguments. The core answers a resume it
no longer holds (the id used before, the do replaced by a newer one, older than 15 minutes, the node restarted) with
"nothing was sent"; the model gets that as it is, told the approved step was not done and not to try it again without
asking. A refusal (or 15 minutes without an answer) tells the assistant the user declined and not to retry. One call
asks at most 8 times. Which steps need approval is the core's decision (block D); the manager only asks and passes the
answer on.

## Interfaces

- `GET /api/v1/machines/{mid}/iphone` → `IPhone` `{enabled, installed, version?, devices: [{id, name, product_type?, ios?, paired, usb?, state?, transport?, connected, locked, capabilities?, ready, note?, code?, developer_mode?, auto_prepare?, allowed}], note?, code?, ddi_present?, checked?, job?}` (fields are block C's, `proto/iphone.go`);
  `PUT` `{"enabled": bool}` is the computer's switch. Extension off, or a computer the bridge doesn't run on → 404.
- `POST /api/v1/machines/{mid}/iphone/{pair|forget|cancel|install|prepare}` `{"device"?}`: pair an iPhone plugged into this computer (progress in `job`),
  forget one's pairing record, cancel a pairing (no device), install the device bridge from the release (no device;
  [iphone-bridge.md](iphone-bridge.md), "Releases"), prepare an iPhone (its developer disk image, and from then on again by itself:
  [iphone-pairing.md](iphone-pairing.md), "Preparing the phone"). Pairing, installing and preparing only once the computer is on (404); forget and cancel
  need only the extension on; no device 400; offline or an old node 409; no release build of the bridge for that
  computer 404, the release site away 502. The log never says which iPhone.
- `GET /api/v1/iphone` → `{on, devices: [{…device, machines: [{id, name, enabled}], consented}]}`, every iPhone the computers reach, each once.
- `PUT /api/v1/iphone/consent` `{"device", "allowed", "app"?: {"bundle_id", "name"}}` → that device as above. Not the user's own app → 403; extension off → 404.
- `POST /api/v1/iphone/try` `{"device"}` → `{screenshot?, opened, app, error?}`: the guided setup's Try It (#8199), without the model.
  It takes a look (the screenshot) and then opens Weather (`com.apple.weather`), through the same checks and per-iPhone turn as
  the tools. A real iPhone asks before every input it can't check (`unchecked`), and the owner's tap on Try It answers exactly
  that one step: the Weather launch is approved at once, anything else it asks is not. `opened` is the launch step's delivery
  `sent`; `error` is in the user's words. Not the user's own app → 403; extension off → 404; the iPhone not allowed or its
  computer offline → 409; another action on it under way → 409.
- `proto.Machine.iphone` is the computer's `IPhone` while the extension is on (the manager's report, like `computer`).
- Node: `iphone.status` → `IPhone` (the manager sets `enabled` and `allowed` itself, never taking the node's word);
  a device is `ready` when it is connected, unlocked and has `screenshot` and `input`. The element tree is not needed: a
  real iPhone never has one (only the simulator's bridge reads one), so it shows as `capabilities.ax_tree: false` with no
  note, and the core works from the screenshot by points, each input asked on the phone first (#3891);
  `iphone.call` `{device, session, op: status|look|do, args, approved?, timeout_ms?, confirm?, self_apps?, self_names?}` (`proto.IPhoneCall`) → `IPhoneResult`;
  `iphone.pair` / `iphone.forget` / `iphone.prepare` / `iphone.cancel` `{device}` and `iphone.install` `{urls, size, sha256, binary_sha256}` → `IPhone`.
  Through `plugin.node.call` to the built-in
  program `iphone` (methods `status`, `call`, `pair`, `forget`, `prepare`, `cancel`, `install`) on a node that lists it, by these names otherwise.
  `op: status` is the node's own answer; `look` and `do` go to block D's package.
  The node passes `confirm` to the core as `Request.Confirm`, and the core's `Response` into `IPhoneResult` as it is:
  `approval_summary` is the core's `Approval` object (`proto.IPhoneApproval` mirrors it field for field) and
  `approval_id` its id. A resume is op `do` with only `approved` (no `args`); the core's "nothing was sent" for a resume
  it no longer holds stays an `*rpc.Error` with `sent: no` and the core's message.
  `self_apps` / `self_names` are Persona's own apps on the iPhone (bundle ids and names) for the core's
  `Rules.SelfApps` / `Rules.SelfNames`, as the iPhone app reports them; they only add to what the node already keeps
  out (the name `Persona` and the bundle ids its device's app list gives), for as long as it runs.

## Codes for apps (#8199)

Every note comes with its reason as a `code`, so an app (the iPhone's guided setup) acts on the state without reading
the words, and an older node that sends none still shows its note:

- The computer's: `bridge_missing` (no device bridge here), `none_paired` (nothing paired with it); and `ddi_present`,
  whether it has the developer disk image Prepare mounts (Xcode's on a Mac, or `PERSONA_IPHONE_DDI_DIR`): a Mac without
  Xcode and a Linux computer don't, and the guided setup says so before Prepare.
- Each iPhone's: `usb_unpaired` (on the cable, not paired), `connecting`, then the bridge's or the session's code
  (`developer_mode_off`, `ddi_not_mounted`, `unsupported` (input below iOS 27), `locked`, `unreachable`, `pairing_rejected`,
  `tunnel_failed`, `timeout`, …), or `failed` when there is no better word. `developer_mode` is what the iPhone last said of
  Developer Mode: at pairing, then whenever it answered (it follows a phone that turned it on after pairing).
- The install job's (`job.kind` `install`): its `step` the stage in the user's words (download, verify, install); once
  failed, `job.code` `download_failed`, `checksum_mismatch`, `bad_helper`, `install_failed` or `timeout`.
- The prepare job's (`job.kind` `prepare`, with `job.device`): once failed, `job.code` `ddi_missing` (this computer has no
  developer disk image), `ddi_unsupported_device`, `tss_failed` (Apple's signing server), `mount_failed`,
  `developer_mode_off`, `locked` or `timeout`. Each iPhone's `auto_prepare` says its owner prepared it once, so its computer
  mounts the image again by itself after a restart.
- The pairing job's (`job.code`, with `job.device`, the iPhone it is for): while it runs, what it waits for from the person
  at the phone (`trust_pending`: tap Trust and enter the passcode; `locked`: unlock it; empty while it waits for nothing),
  its `step` the matching note; once failed, why (`timeout` when nobody tapped Trust in its 3 minutes, `pairing_rejected`,
  …).

## Notes and their languages

The node writes its notes (the computer's, each device's, a job's error) as the fixed Simplified Chinese of
`hub-iphone.json`'s `rpc_*` keys; the hub translates them for each app (`i18n.IPhoneStatus`, the computer's note, the
job and every device's note), as Cua's are.
