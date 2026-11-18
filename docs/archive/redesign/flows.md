# Main feature flow diagrams

> **Since 2026-10-31, the parts of this document about Android's look, layout and interaction are superseded by [Android interface styles and design system](../../android/android-design.md)**: the user asked for the visuals and interaction to be redone as a whole, as three interface styles: Glass (default), Morning Paper and Board. This document remains the spec for features, data, protocol and gates.

> Draft (2026-10-30), part of the [redesign proposal](README.md). This round only lands on Android and the hub; iOS does not change.
> The diagrams are written in Mermaid, which GitHub and most Markdown previews render directly; the same diagrams are also in the UI preview (`prototypes/android/flows.html`, removed in #9030).
> Under each diagram is the matching spec section; when a diagram and the spec disagree, the spec wins and the diagram gets fixed.

| # | Flow | Spec |
| --- | --- | --- |
| 1 | [Overview: who connects to whom](#1-overview-who-connects-to-whom) | README D8, D9 |
| 2 | [Pairing](#2-pairing) | ux.md 3.7, hub-changes.md 1.3 |
| 3 | [How pairing failures are reported](#3-how-pairing-failures-are-reported) | ux.md 3.7, android.md 5.1 |
| 4 | [Route selection: probing multiple addresses](#4-route-selection-probing-multiple-addresses) | android.md 5.1, hub-changes.md 1.2 |
| 5 | [Connection state](#5-connection-state) | ux.md 5.5 |
| 6 | [Persistent connection and heartbeat](#6-persistent-connection-and-heartbeat) | android.md 5.8, 5.9, hub-changes.md 1.6, 3.8 |
| 7 | [Finding the hub again after its address changes](#7-finding-the-hub-again-after-its-address-changes) | android.md 5.1, hub-changes.md 1.2, 1.7 |
| 8 | [Notification → approval → Agent continues](#8-notification--approval--agent-continues) | ux.md 3.4, 9, android.md 5.8 |
| 9 | ["Done" notification](#9-done-notification) | hub-changes.md 3.9 |
| 10 | [Sending a message: outbox and three result states](#10-sending-a-message-outbox-and-three-result-states) | ux.md 5.2, README D3 |
| 11 | [Opening a chat: sync and continuity](#11-opening-a-chat-sync-and-continuity) | README D4, `docs/protocol/protocol.md` ("Continuity of items", "Collapsed work") |
| 12 | [Inbox and AI twin](#12-inbox-and-ai-twin) | ux.md 3.6, `docs/assistant/inbox.md` |
| 13 | [Share to Persona](#13-share-to-persona) | android.md 6.3 |
| 14 | [First-run setup checklist (Chinese Android ROMs)](#14-first-run-setup-checklist-chinese-android-roms) | ux.md 3.7, android.md 5.18 |
| 15 | [In-app update](#15-in-app-update) | android.md section 9 |

## 1. Overview: who connects to whom

Android connects only to the hub: away from home over the public internet (IPv6 preferred) on the TLS port with a pinned public key; at home it can also use the LAN. The existing iOS keeps using the plaintext port (LAN or VPN), with push over APNs.

```mermaid
flowchart LR
  A["Android App<br/>foreground service keeps a WebSocket"] -- "HTTPS / WSS<br/>TLS port · pinned key<br/>public IPv6 first" --> H["personad hub<br/>always-on machine at home"]
  I["Existing iOS App (unchanged this round)<br/>push over APNs"] -- "HTTP / WS<br/>plaintext port<br/>LAN or VPN" --> H
  H --- P[("Postgres")]
  H -- "SSH" --> N1["personad node<br/>computer A"]
  H -- "SSH" --> N2["personad node<br/>computer B"]
  N1 --> C1["Claude Code / Codex<br/>in herdr"]
  N2 --> C2["Claude Code / Codex<br/>in herdr"]
  H --> X["Lark<br/>mail (IMAP / SMTP)"]
```

## 2. Pairing

The QR code format is unchanged (the existing iOS can still scan it); the token becomes a temporary one, valid for 10 minutes and usable once. New parameters are for Android.

```mermaid
sequenceDiagram
  autonumber
  actor U as You
  participant CLI as personad hub pair<br/>(on the hub machine)
  participant H as hub
  participant A as Android App
  participant O as Other paired devices
  U->>CLI: Run
  CLI->>H: Request a temporary token over local loopback
  H-->>CLI: Temporary token (stored, 10 minutes, exchangeable once)
  CLI-->>U: QR code url · token · hub · fp · tls · alt
  U->>A: Scan
  A->>A: Record hub_id, key fingerprint, candidate addresses
  par Staggered concurrent probes, 250 ms apart
    A->>H: TLS handshake + GET /health (public IPv6)
  and
    A->>H: TLS handshake + GET /health (domain / LAN / public IPv4)
  end
  H-->>A: Certificate + hub_id
  A->>A: Key fingerprint equals fp and hub_id matches: pick this path
  A->>H: POST /auth/tokens (with the temporary token)
  H-->>A: This device's token (temporary token revoked)
  H--)O: Notify: a new device paired
  A->>H: GET /machines, connect the WebSocket, send client_hello
  A-->>U: Setup checklist (flow 14)
```

## 3. How pairing failures are reported

No fake progress; failures give actionable hints by cause. A timeout on home Wi-Fi first rules out the local network permission (from Android 17, a missing grant shows up as a timeout).

```mermaid
flowchart TD
  S["Scan / paste a pairing link"] --> P{"Parses?"}
  P -- "No" --> E0["Not a Persona pairing QR code"]
  P -- "Yes" --> V{"Has fp and tls?"}
  V -- "No (legacy format / old hub)" --> LAN["Use only the plaintext LAN address<br/>Settings note: hub too old for direct public connection"]
  V -- "Yes" --> R["Probe all candidates (flow 4)"]
  LAN --> T
  R --> W{"Any candidate passes<br/>certificate + hub_id check?"}
  W -- "Yes" --> T["Exchange the temporary token for this device's token"]
  W -- "No" --> C{"Classify by cause"}
  C -- "DNS failure" --> E1["Can't find this address<br/>[Probe again]"]
  C -- "Refused" --> E2["The hub isn't listening on this port<br/>[Probe again]"]
  C -- "Timeout on home Wi-Fi<br/>without local network permission" --> E3["Connecting to the hub at home<br/>needs local network permission<br/>[Grant]"]
  C -- "Timeout" --> E4["At home: same network?<br/>Away: does the router allow<br/>connections from outside?<br/>[Connection diagnostics]"]
  C -- "Certificate mismatch" --> E5["Not the hub you paired<br/>or the hub changed its certificate<br/>[Scan again]"]
  T --> X{"hub response"}
  X -- "401 / 403" --> E6["Pairing code expired or already used<br/>[Scan again]"]
  X -- "code = incompatible" --> E7["Please update the hub"]
  X -- "Success" --> OK["Read the machine list → setup checklist"]
```

## 4. Route selection: probing multiple addresses

Modeled on Happy Eyeballs v2 (RFC 8305): staggered concurrent attempts; the first to pass the identity check wins. An old prefix may have been handed to someone else, so "reachable" is not enough: both the certificate and `hub_id` must match.

```mermaid
flowchart TD
  T["Triggers<br/>launch · disconnect · network change<br/>new candidates · periodic recheck"] --> K{"Remembered winner<br/>on this network?"}
  K -- "Yes" --> F["Try it first"]
  F -- "Success" --> D
  F -- "Failure" --> O
  K -- "No" --> O["Order<br/>public IPv6 → domain → LAN → public IPv4<br/>(public IPv4 only sent once the hub confirms it is reachable)"]
  O --> S["Start one more<br/>TLS handshake + GET /health every 250 ms"]
  S --> J{"Key fingerprint = fp<br/>and hub_id matches?"}
  J -- "First to pass" --> D["Pick the path, stop the other probes<br/>remember the winner per network (Wi-Fi / cellular)"]
  J -- "All failed" --> L{"On home Wi-Fi,<br/>local network permission not granted?"}
  L -- "Yes" --> PERM["State: needs local network permission"]
  L -- "No" --> B["State: Connecting…<br/>back off 1→20 s and retry"]
  D --> WS["Connect the WebSocket → client_hello<br/>→ resubscribe open sessions → reconcile pending interactions"]
```

## 5. Connection state

The four iOS states are copied as is; Android adds three states that need you (orange, no automatic retry: after granting it returns to "Connecting", after rescanning it pairs from scratch). Getting worse must last 1.5 seconds before it shows; recovery shows immediately.

```mermaid
flowchart LR
  U(["Not paired"]) -- "Pairing succeeds" --> C["Connecting…"]
  C -- "WebSocket connected" --> UP["Updating…"]
  UP -- "Pending interactions and open<br/>chats caught up (8 s max)" --> O(["Online"])
  O -. "Disconnect · no ping reply in 5 s<br/>· connection judged dead" .-> C
  O -- "System reports no network" --> W["Waiting for network"]
  C -- "System reports no network" --> W
  W -. "Network back, reconnect now" .-> C
  C -- "Timeout on home Wi-Fi<br/>without permission" --> P["Needs local network permission<br/>[Grant]"]
  C -- "Key fingerprint mismatch" --> X["The hub's certificate changed<br/>[Scan again]"]
  C -- "401" --> R["Pairing no longer valid<br/>[Scan again]"]
  classDef you fill:#FFF4E0,stroke:#D9922B,color:#1C1C1E
  class P,X,R you
```

## 6. Persistent connection and heartbeat

When Android is in deep sleep, client pings do not fire (OkHttp's timers use `System.nanoTime`, which stops during deep sleep), so background heartbeats are sent by the hub, at an interval negotiated per connection. Old connections (the existing iOS) do not negotiate and stay at 20 seconds.

```mermaid
sequenceDiagram
  participant S as Android foreground service
  participant H as hub
  Note over S,H: App in the foreground
  S->>H: client_hello (heartbeat 20 s)
  H-->>S: client_hello_ack (the value actually in effect)
  loop Every 20 s
    H->>S: WebSocket ping
    S-->>H: pong
  end
  S->>H: App-level ping (id)
  H-->>S: pong (same id), the network counts as good only if it returns within 5 s
  Note over S,H: App goes to the background
  S->>H: client_hello (heartbeat starts at 4 minutes, adjusted per network)
  H-->>S: client_hello_ack
  loop About every 4 minutes (jitter only downward)
    H->>S: WebSocket ping (network packet wakes the phone)
    S-->>H: pong
  end
  H->>S: interaction / notify event
  S->>S: Post a notification
  Note over S: On every wake-up, if more than interval + pong timeout<br/>has passed since the last data from the hub, reconnect now
  Note over S: On network change, reselect the route and reconnect immediately
```

## 7. Finding the hub again after its address changes

When home broadband redials, the IPv6 prefix changes and old connections drop on the spot. Android has no side channel like FCM, so when you are away the only way back is the domain name, which is why DDNS is recommended.

```mermaid
sequenceDiagram
  participant A as Android
  participant H as hub
  participant D as DNS (DDNS)
  Note over H: Home broadband redials, the IPv6 prefix changes
  H--xA: Old connection drops
  H->>H: netlink reports the address change, recompute candidates
  H->>D: Update the AAAA record (when DDNS is configured)
  A->>A: Disconnected, reselect the route (flow 4)
  alt Smooth prefix change, old connection still alive
    H->>A: endpoints event (new addresses)
  else Away from home, DDNS configured
    A->>D: Resolve the hub's domain
    D-->>A: New address (TTL at least 600 s, may still be the old one)
    A->>H: Probe the new address (pinned key + check hub_id)
  else Away from home, no domain
    A->>A: Wait until back home on the LAN
    Note over A: Diagnostics page suggests giving the hub a DDNS name
  end
```

## 8. Notification → approval → Agent continues

"Allow / Approve" requires unlocking first; a deny reason or plan feedback can be typed right in the notification. Once handled (wherever it was handled), the notification is withdrawn.

```mermaid
sequenceDiagram
  autonumber
  participant C as Claude Code (on the computer)
  participant N as personad node
  participant H as hub
  participant S as Android foreground service
  actor U as You
  C->>N: PermissionRequest hook (wants to run a command)
  N->>H: New interaction
  H->>S: WebSocket interaction event
  S->>U: Notification: command summary · Allow · Deny · Open
  alt Tap Allow
    U->>S: Unlock first (setAuthenticationRequired)
    S->>H: resolve allow (with client_op_id)
  else Tap Deny and write a reason
    U->>S: Type the reason in the notification (RemoteInput)
    S->>H: resolve deny + reason
  end
  H->>N: Pass on the decision
  N-->>C: hook returns, Agent continues
  H->>S: interaction_resolved
  S->>S: Withdraw this notification
  Note over H,S: When the result is unknown (timeout / disconnect)<br/>retry with the same op id, deduplicated by the hub<br/>or reread the interaction list first<br/>the card shows Result pending
```

## 9. "Done" notification

The hub decides whether to send it; iPhone and Android use the same rules, only the delivery channel differs. The phone only displays it and no longer computes it itself (with PR #84 the phone currently computes it itself).

```mermaid
sequenceDiagram
  participant N as node (Agent on the computer)
  participant H as hub
  participant I as iPhone
  participant S as Android foreground service
  N->>H: Session state: Working → Unread
  H->>H: notify.go rules: wait done_delay, only long tasks,<br/>push only after background tasks end, not if seen, not if muted
  par Same decision, two channels
    H->>I: APNs
  and
    H->>S: WebSocket notify (kind = done)
  end
  S->>S: Don't pop up if this session is open in the foreground
```

## 10. Sending a message: outbox and three result states

Write to the outbox first, then clear the input box; the same message always uses the same `client_msg_id`. "Sent but no reply" is no longer treated as offline and blindly resent.

```mermaid
flowchart TD
  S(["Tap Send"]) --> Q["Write to the outbox, then clear the input box<br/>generate client_msg_id"]
  Q --> N{"Online?"}
  N -- "No" --> WN["Send automatically once connected<br/>send in order once connected and Updating has finished"]
  N -- "Yes" --> SD["Sending"]
  WN -. "Connected" .-> SD
  SD --> R{"Result"}
  R -- "hub accepted it, node finished typing" --> OK(["Delivered"])
  R -- "Request never went out<br/>DNS / refused / no network" --> WN
  R -- "Definite rejection<br/>input_draft / dialog_open etc." --> RF["Not sent: reason<br/>[Clear and send] [Edit]"]
  R -- "Sent, no reply<br/>timeout / disconnect" --> UK{"hub supports<br/>client_ops?"}
  UK -- "Yes" --> RT["Retry automatically with the same id<br/>hub deduplicates and returns the original result"]
  RT -. "Retry" .-> SD
  UK -- "No" --> VF["Checking<br/>wait for it to appear in the transcript"]
  R -- "502 delivered = unknown<br/>error halfway through typing on the computer" --> VF
  VF -- "Appeared" --> OK
  VF -- "Past the deadline" --> NR["Couldn't confirm delivery<br/>[Resend] [Don't send]"]
  classDef pending fill:#FFF4E0,stroke:#D9922B,color:#1C1C1E
  class WN,RF,VF,NR pending
```

## 11. Opening a chat: sync and continuity

The rules follow `docs/protocol/protocol.md` ("Continuity of items" and "Collapsed work") and are written into `spec/fixtures/continuity/`, shared by the Android reducer and the Go simulation.

```mermaid
flowchart TD
  O["Open a session"] --> C["Draw the local cache first (last two turns)"]
  C --> Sub["subscribe: since_seq = covered, fold_work = true"]
  Sub --> B["Receive a batch of items (from_seq, to_seq, history)"]
  B --> H{"history changed?"}
  H -- "Yes (file replaced / rewritten)" --> RE["Drop local content, refetch the latest page"]
  H -- "No" --> G{"from_seq ≤ covered?"}
  G -- "Yes" --> M["Merge, advance covered to to_seq"]
  G -- "No (a batch was lost in between)" --> W["Wait 1.5 s (a resend may still be on the way)"]
  W --> F["after_seq = covered to fill the gap"]
  F -- "Filled" --> M
  F -- "More than 8 pages" --> RE
  M --> V["Render: one row per message, work collapsed into one row"]
  RE --> V
  V --> E{"Expand collapsed work?"}
  E -- "Yes" --> FW["Fetch this range by from_seq … through_seq<br/>replace the fold"]
  FW --> V
```

## 12. Inbox and AI twin

Lark messages and mail share one list, one row per item. The AI twin only drafts and never sends; sending takes a tap from you. Sends carry a clientId, so a retry never sends twice.

```mermaid
flowchart TD
  F["Lark messages"] --> H["hub collects"]
  M["Mail"] --> H
  H --> T["Model 1: triage<br/>Needs you / FYI / noise + one-line summary"]
  T --> J{"Model 2: hand it<br/>to an Agent?"}
  J -- "Yes, with a one- or two-line task" --> G["Model 3: a headless Agent looks into it<br/>returns a draft and its basis (inbox_draft)"]
  J -- "No" --> I
  G --> I["Inbox: one row per item<br/>draft attached under the row"]
  I --> U{"You"}
  U -- "Done" --> D["Move to Done<br/>can be moved back within 3 days"]
  U -- "Open" --> R["Open in the App<br/>Lark chat page / mail"]
  U -- "Tap Send" --> S["POST /reply (with clientId)<br/>mark done after sending"]
  U -- "Edit" --> E["Draft sheet: draft + basis<br/>+ view how the AI twin handled it"]
  E --> S
```

## 13. Share to Persona

An Android-only entry point: text, links, images and files from any App go into Persona, which fits "collect information from everywhere and hand it to AI" exactly.

```mermaid
flowchart LR
  X["Any App's share menu"] --> J["Persona (share target)"]
  J --> P{"Send where"}
  P -- "Default" --> M["Persona main chat"]
  P -- "Pick one" --> AG["An Agent session"]
  M --> OB["Through the outbox: store first, then send<br/>with client_msg_id (flow 10)"]
  AG --> OB
  OB --> UP["Images and files upload first,<br/>the message carries the path"]
```

## 14. First-run setup checklist (Chinese Android ROMs)

After pairing, check item by item; every item can be skipped, and says what you lose by skipping it. No Chinese Android ROM offers an API to check auto-start or the background allowlist, so what can be checked shows its real state, what can't gets steps for that ROM, and a lock-screen test notification is the backstop. Each vendor's entry points are in [android.md](android.md) 5.18.

```mermaid
flowchart TD
  S["Pairing complete"] --> ID["Read system properties: which ROM, which version"]
  ID --> A{"Everything checkable turned on?<br/>notification master switch · per-channel level<br/>battery optimization · background restrictions"}
  A -- "No" --> A1["Go to Settings item by item<br/>recheck when returning to the App"]
  A -- "Yes" --> C
  A1 --> C["Uncheckable ones: steps for this vendor and version<br/>auto-start · unrestricted · lock in recent tasks<br/>Huawei: turn off silent notifications, keep network during sleep<br/>Honor: turn off smart data saver"]
  C --> D["You tap Done<br/>if the jump fails, fall back to the app details page"]
  D --> T["Self-test: after 30 s on the lock screen<br/>the hub sends a test notification"]
  T --> Q{"Did it ring and light the screen?"}
  Q -- "No" --> C
  Q -- "Yes" --> L{"target 37: connecting to the hub at home<br/>needs local network permission"}
  L -- "Not granted" --> L1["Request it, if denied explain<br/>that at home only addresses not counted as local network work"]
  L -- "Granted / not needed" --> E
  L1 --> E["Can it connect from outside?<br/>Turn off Wi-Fi and try once on cellular"]
  E -- "Yes" --> OK(["Done<br/>afterwards the diagnostics page shows disconnect history"])
  E -- "No" --> DG["Connection diagnostics<br/>modem port allow / DDNS suggestion"]
  DG --> OK
```

## 15. In-app update

There is no Play Store. The APK lives on the hub, is downloaded over TLS (pinned to the same public key), and the system installer upgrades it. On HyperOS a silent-install request fails outright (`INSTALL_FAILED_ABORTED`, with no dialog at all), so Chinese ROMs always get the system confirmation. The first install has no device token: use the LAN download link printed by `personad hub pair --android`, or adb.

```mermaid
flowchart TD
  C["While the App is in the foreground, ask the hub<br/>whether there is a new version"] --> H{"New version?"}
  H -- "No" --> Z["Do nothing"]
  H -- "Yes" --> T["Settings shows Update available<br/>and release notes"]
  T --> D["Download the APK over TLS (pinned key)<br/>verify SHA-256"]
  D --> P{"Install unknown apps granted?"}
  P -- "No" --> P1["Jump to system settings to grant"]
  P1 --> K
  P -- "Yes" --> K{"Stock Android?"}
  K -- "Yes" --> SI["Try a silent install first"]
  K -- "No (Chinese ROM)" --> UI["Show the system install confirmation"]
  SI -- "Fails" --> UI
  SI -- "Succeeds" --> OK
  UI -- "Confirm" --> OK["Same signature, install over, data kept"]
  UI -- "Cancel" --> L["Prompt again next time it opens"]
  OK --> RS["Process killed<br/>MY_PACKAGE_REPLACED restarts the persistent service"]
```
