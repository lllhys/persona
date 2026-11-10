# Lessons: Tern engine (`node/hosttern.go`)

Read before changing the Tern host (`node/hosttern.go`), its command line client (`server/internal/terncli`), or testing against a real Tern.
Design: docs/integrations/engines.md ("Tern").

- **`--window` goes right after the command's first word** (tern 0.9.13, 2026-11-09). Every command works on one window's sessions, so the client
  names the window on each call: `tern send --window "" 11 keys enter`. Before the command (`tern --window "" ls`) tern takes `""` for a file to open
  and fails with "No such file or directory"; after the positional arguments, `tern run` would type it as more of the command and `tern send … keys`
  would take it for a key.
- **A block's `program` is the shell it started, not what runs in it**: `tern inspect` says `/bin/zsh` for a block where Claude runs. Only
  `tern process <block> --json` names the program in front (`foreground`: pid, name, argv). Claude's process name is its version (`2.1.316`); its
  `argv[0]` is `claude`.
- **The host's own calls are events**: every `tern` invocation connects as a client, so an unfiltered `tern events` reports a `client_connected` for
  each call the host makes, and a host refreshing on every event would keep refreshing itself. Filter by event name (`--filter pane_spawned,…`); an
  unknown name is refused (`no event is called …`), which is how the names in `ternEvents` were checked.
- **Colored captures end rows in `\n`**: `tern capture --ansi` keeps SGR escapes but not `\r\n`, which the node expects of colored reads; the host
  converts them.
- **Closing a session's last block leaves the session**: an empty session with no tabs stays until `tern kill session <id>`. The host ends it only when
  no window shows it (one the node opened with `tern new session`, which no window switches to).
- **Errors are text only**: every failure exits 1. No running daemon is "no Tern is running (no session daemon on …)", a missing block "no block is
  called `<id>`", an unknown window "no window has key …" (`terncli.classify`).
- **Tern's `working` is about output, not the agent**: Claude thinking quietly reads as not working, so agent state keeps coming from transcripts and
  hooks.
- **Each call is a process and a round trip** (25–35 ms on an M3 Pro): a look runs `tern process` for at most four blocks at a time, and Agents right
  after a Snapshot reuses that look.
