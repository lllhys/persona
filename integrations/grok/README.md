# Grok assistant runtime

This companion connects the authenticated Grok CLI to Persona's local runtime socket. It exposes `grok-chat` as an
assistant launcher and runs conversations through Grok's ACP interface. The separate name avoids taking ownership of
the `grok` Agent provider already offered by a Paseo engine.

Requires Node 22+ and a Grok CLI supporting ACP client `pre_tool_use` hooks, session loading, model configuration and
HTTP MCP servers. Authenticate Grok normally before starting the companion. No credentials are copied into Persona.

```sh
node integrations/grok/companion.mjs \
  --socket "$HOME/.persona/runtime.sock" \
  --grok-bin /absolute/path/to/grok \
  --model <model-id-from-grok> --effort <supported-level> \
  --assistant-for grok   # optional: the Paseo provider running this same Grok CLI and account
```

Why two entries: a Paseo provider (for example `grok`, labelled "Grok · Official") starts Agents through Paseo's ACP
bridge, and the Paseo runtime does not declare the assistant capability, so the main chat cannot run on it. This companion
is the assistant runtime for the same CLI: it supplies the assistant's private context, its Persona tools, phone approval
and the permission modes (`docs/integrations/runtimes.md`, "Assistant").

Optional flags: `--grok-home` selects the native transcript directory; `--launcher` selects another unique launcher name;
`--display-name` is what the apps call it (default `Grok`); `--assistant-for <launcher>` names the Agent launcher on this
computer that runs the same Grok CLI and account but can't run assistant chats, such as a Paseo provider `grok`: the main
chat's menu then shows that one as "Can start Agents … For assistant chats, choose Grok" instead of only refusing it. Set it
only when the two really are the same account; nothing is inferred from names, and a message is never moved from one to
the other;
`--permission-mode` selects the default (`default`, `acceptEdits`, `auto`, `plan`, `bypassPermissions`). An explicit
conversation setting overrides it. `auto` uses the phone's permission decisions; it does not claim Grok's own automatic
approval classifier. To align an unattended Paseo profile, use its actual model ID and `thinkingOptionId`, and set
`--permission-mode bypassPermissions` only when that profile enables automatic acceptance.

Run this alongside the node, with the same operating-system account and environment. A service manager can keep it
running; reconnecting to the node is automatic. Stopping it stops its Grok process. A restart rediscovers native Grok
conversations in Persona's assistant and health directories, including manager-scoped directories, and resumes them
under the same `grok-chat-<native-id>` IDs.

Renaming a conversation uses Grok's native `_x.ai/session/rename` operation. The title is saved as a manual title in
Grok's own session record, remains after restarts and later replies, and can be changed while a reply is running.
Session discovery reads `generated_title` before the older `session_summary`, which can retain an earlier name.

Conversations started with the Grok CLI in a terminal are listed too, under their working directory, so they appear in
the Agents list and its repository grouping alongside the assistant's. One updated in the last seven days is listed;
one a running Grok process holds (Grok's own `active_sessions.json`) is listed however old it is. While a terminal holds
it, the conversation is read-only from Persona: its turns appear as the terminal writes them, and sending, renaming or
changing its settings is refused (`terminal_open`), since a second writer would interleave with the terminal's turns.
Once that terminal exits, the phone continues the conversation through ACP in the same directory. These conversations
are not the assistant's: they get Persona's Agent tools, not the assistant's.

The model catalog comes from the installed CLI. The companion verifies acknowledged settings and refuses unsupported
reasoning levels. It supplies assistant tools through a private loopback MCP endpoint backed by the runtime SDK.
The standard launcher Test runs a short isolated native conversation with the same model and effort, no Persona
tools or context, and a deadline. Its successfully closed native probe record is removed afterwards.
Private context is sent before the prompt and removed from displayed native history. Tool requests pass through the
phone's existing permission cards; plan mode denies writes, and shell commands belonging to Persona's command gates
must use their corresponding Persona tools even in bypass mode. Native Grok transcripts remain the durable source.
An atomic, owner-only `persona-runtime.json` beside each native transcript keeps the companion's permission mode,
assistant ownership and last terminal error with a prompt fingerprint. It contains no prompt text or credentials and
is removed with the native session. This is provider-owned runtime metadata; Persona server state remains in Postgres.
Conversations from an older companion without metadata use default approval until their persisted Persona mode is
applied, and do not claim the new-chat default as their observed permission mode.

Validation:

```sh
npm --prefix integrations/grok test
GROK_LIVE=1 GROK_BIN=/absolute/path/to/grok GROK_MODEL=<model-id> GROK_EFFORT=<level> \
  node integrations/grok/e2e/live.mjs
```

The opt-in live check makes real model requests through an isolated fake node. It verifies model/effort, a scoped Persona
MCP call, hidden context, native renaming, a follow-up, restart/resume with the renamed title, a denied write in plan
mode, persistence of that mode across another restart, and the standard launcher Test. It creates only a temporary
working directory and native Grok conversation history; it does not connect to the production hub.
