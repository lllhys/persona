import { describe, expect, it } from "vitest";
import { closeoutCommandClaims, closeoutCommandEvidence } from "./closeout-commands";
import { segments, type Segment } from "./turns";
import type { Item, TaskNote } from "./types";

let seq = 0;
function item(kind: Item["kind"], extra: Partial<Item> = {}): Item {
  seq++;
  return { id: `i${seq}#0`, seq, role: kind === "tool_result" ? "user" : "assistant", kind, ...extra };
}

function exchange(commands: { command: string; result?: Partial<Item>; note?: TaskNote }[]): Segment {
  const items: Item[] = [item("text", { role: "user", text: "Check it" })];
  commands.forEach(({ command, result, note }, i) => {
    const call = `call-${seq}-${i}`;
    items.push(item("tool_use", { tool_use_id: call, tool_name: "Bash", tool_input: { command } }));
    if (result) items.push(item("tool_result", { tool_use_id: call, text: "recorded output", ...result }));
    if (note) items.push(item("notice", { tool_use_id: call, task: note }));
  });
  items.push(item("text", { text: "Done" }));
  return segments(items)[0];
}

describe("closeoutCommandClaims", () => {
  it("reads inline code and recognizable bare commands, not prose or echo text", () => {
    expect(closeoutCommandClaims("Ran `cd web && npm test` and `GOFLAGS=-count=1 go test ./...`.")).toEqual(["cd web && npm test", "GOFLAGS=-count=1 go test ./..."]);
    expect(closeoutCommandClaims("npm test passed; python3 scripts/privacy_check.py 12/12 passed")).toEqual(["npm test", "python3 scripts/privacy_check.py"]);
    expect(closeoutCommandClaims("make sure this works; go to the tests; `echo go test ./...`")).toEqual([]);
    expect(closeoutCommandClaims("echo go test ./...")).toEqual([]);
    expect(closeoutCommandClaims("`cat <<EOF\ngo test ./...\nEOF`")).toEqual([]);
  });

  it("unwraps a shell only to recognize a command, preserving the claim as written", () => {
    expect(closeoutCommandClaims("`bash -lc 'CI=1 npm test'`")).toEqual(["bash -lc 'CI=1 npm test'"]);
    expect(closeoutCommandClaims("`go test -run 'TestA|TestB' ./...`")).toEqual(["go test -run 'TestA|TestB' ./..."]);
  });
});

describe("closeoutCommandEvidence matching", () => {
  it.each([
    ["cd pkg && GOFLAGS=-count=1 go test ./... -race", true],
    ["go test ./... -v", true],
    ["bash -lc 'cd web && CI=1 npm test'", false],
    ["go test ./internal/...", false],
    ["go test ./... -run TestFoo", false],
    ["go test ./... --run=TestFoo", false],
    ["go test ./... -run TestFoo -v", false],
    ['echo "go test ./..."', false],
    ["cat <<EOF\ngo test ./...\nEOF", false],
  ])("go test ./... against %s => %s", (command, found) => {
    const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false } }]))[0];
    expect(result.state === "not_found").toBe(!found);
  });

  it("matches a valid environment and shell wrapper without inventing an earlier pipeline stage's result", () => {
    const command = "env -i CI=1 timeout 30s bash -lc 'cd web && npm test 2>&1 | tail -n 5'";
    const result = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command, result: { is_error: false } }]))[0];
    expect(result).toMatchObject({ state: "unavailable", command });
    expect(result.exitCode).toBeUndefined();
    expect(result.isError).toBeUndefined();
    const direct = "env -u UNUSED CI=1 timeout 30s bash -lc 'cd web && npm test'";
    expect(closeoutCommandEvidence("`cd web && npm test`", exchange([{ command: direct, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", command: direct, exitCode: 0, isError: false });
  });

  it("rejects selection-affecting GOFLAGS but keeps a non-narrowing assignment", () => {
    const narrowed = closeoutCommandEvidence("`go test ./...`", exchange([{ command: "GOFLAGS=-run=TestFoo go test ./...", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(narrowed).toMatchObject({ state: "not_found" });
    expect(narrowed.exitCode).toBeUndefined();
    const harmless = closeoutCommandEvidence("`go test ./...`", exchange([{ command: "GOFLAGS=-count=1 go test ./...", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(harmless).toMatchObject({ state: "exit", exitCode: 0 });
  });

  it("attributes a shell result only to its final command and successful && links", () => {
    for (const [command, exit_code, is_error] of [
      ["false && go test ./...; true", 0, false],
      ["go test ./...; true", 0, false],
      ["go test ./...; false", 1, true],
      ["go test ./... | tail -n 5", 0, false],
    ] as const) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error, outcome: { status: "ok", exit_code } } }]))[0];
      expect(result).toMatchObject({ state: "unavailable", command });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
    for (const [claim, command] of [
      ["go test ./...", "go test ./... && true"],
      ["go vet ./... && go test ./...", "go vet ./... && go test ./..."],
    ]) {
      expect(closeoutCommandEvidence(`\`${claim}\``, exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", exitCode: 0, isError: false });
    }
  });

  it("does not attribute a skipped final && command through shell wrappers", () => {
    for (const command of [
      "false && go test ./...",
      "true && false && go test ./...",
      "bash -lc 'false && go test ./...'",
      "bash -lc 'bash -c \"false && go test ./...\"'",
      "timeout 30s bash -lc 'false && go test ./...'",
    ]) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: true, outcome: { status: "failed", exit_code: 1 } } }]))[0];
      expect(result).toMatchObject({ state: "unavailable", command });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
  });

  it("does not attribute a pipeline skipped by a false guard", () => {
    const command = "false && tail -n 5 | go test ./...";
    const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: true, outcome: { status: "failed", exit_code: 1 } } }]))[0];
    expect(result).toMatchObject({ state: "unavailable", command });
    expect(result.exitCode).toBeUndefined();
    expect(result.isError).toBeUndefined();
  });

  it("does not attribute a skipped guarded pipeline through nested shells", () => {
    const command = "bash -lc 'bash -c \"false && tail -n 5 | go test ./...\"'";
    const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: true, outcome: { status: "failed", exit_code: 1 } } }]))[0];
    expect(result).toMatchObject({ state: "unavailable", command });
    expect(result.exitCode).toBeUndefined();
    expect(result.isError).toBeUndefined();
  });

  it("attributes a successful guarded pipeline's last stage", () => {
    const command = "true && tail -n 5 | go test ./...";
    expect(closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", command, exitCode: 0, isError: false });
  });

  it("does not invent an earlier guarded pipeline stage's result", () => {
    for (const [command, exit_code, is_error] of [
      ["true && go test ./... | tail -n 5", 0, false],
      ["false && go test ./... | tail -n 5", 1, true],
    ] as const) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error, outcome: { status: "ok", exit_code } } }]))[0];
      expect(result).toMatchObject({ state: "unavailable", command });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
  });

  it("attributes a successful && command before the final pipeline", () => {
    const command = "go test ./... && tail -n 5 | head -n 1";
    expect(closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", command, exitCode: 0, isError: false });
  });

  it("keeps direct failures and successful && chains attributable", () => {
    for (const command of ["go test ./...", "true && go test ./...", "go test ./... && true"]) {
      const exitCode = command === "go test ./..." ? 1 : 0;
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: exitCode !== 0, outcome: { status: "ok", exit_code: exitCode } } }]))[0];
      expect(result).toMatchObject({ state: "exit", command, exitCode });
    }
  });

  it("requires positive Go test counts in arguments and GOFLAGS", () => {
    for (const command of ["go test ./... -count=0", "GOFLAGS=-count=0 go test ./..."]) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
      expect(result).toMatchObject({ state: "not_found" });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
    expect(closeoutCommandEvidence("`go test ./...`", exchange([{ command: "go test ./... -count=1", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", exitCode: 0 });
  });

  it("fails closed after shell option changes but keeps an ordinary pipeline's last stage", () => {
    for (const command of [
      "set -o pipefail; false | go test ./...",
      "bash -lc 'set -o pipefail; false | go test ./...'",
      "bash -lc 'false | go test ./...'; set -o pipefail",
      "shopt -s pipefail; false | go test ./...",
      "setopt pipefail; false | go test ./...",
      "unsetopt pipefail; false | go test ./...",
    ]) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: true, outcome: { status: "failed", exit_code: 1 } } }]))[0];
      expect(result).toMatchObject({ state: "unavailable", command });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
    expect(closeoutCommandEvidence("`go test ./...`", exchange([{ command: "false | go test ./...", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", exitCode: 0 });
  });

  it("rejects short-circuited and conditional alternatives, not unconditional separators", () => {
    for (const command of ["true || go test ./...", "if true; then go test ./...; fi"]) {
      const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
      expect(result).toMatchObject({ state: "not_found" });
      expect(result.exitCode).toBeUndefined();
    }
    for (const command of ["true && go test ./...", "true; go test ./..."]) {
      expect(closeoutCommandEvidence("`go test ./...`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", exitCode: 0 });
    }
  });

  it("requires a claimed directory to be the recorded directory in effect", () => {
    const same = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command: "cd web && npm test", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(same).toMatchObject({ state: "exit", exitCode: 0 });
    for (const command of ["cd server && npm test", "cd web && cd server && npm test"]) {
      const different = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
      expect(different).toMatchObject({ state: "not_found" });
      expect(different.exitCode).toBeUndefined();
    }
    const changedByEnv = closeoutCommandEvidence("`cd server && npm test`", exchange([{ command: "cd server && env -C web npm test", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(changedByEnv).toMatchObject({ state: "not_found" });
    expect(changedByEnv.exitCode).toBeUndefined();
    expect(closeoutCommandEvidence("`npm test`", exchange([{ command: "cd app && npm test", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0]).toMatchObject({ state: "exit", exitCode: 0 });
  });

  it("does not let a skipped or failed cd prove a claimed directory", () => {
    for (const [claim, command] of [
      ["cd web && npm test", "cd server && false && cd web; npm test"],
      ["cd /definitely-missing && npm test", "cd server && cd /definitely-missing; npm test"],
      ["cd web && npm test", "bash -lc 'cd server && false && cd web; npm test'"],
      ["cd web && npm test", "timeout 30s bash -lc 'cd server && cd /definitely-missing; cd web; npm test'"],
    ]) {
      const result = closeoutCommandEvidence(`\`${claim}\``, exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
      expect(result).toMatchObject({ state: "unavailable", command });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
    const unscoped = closeoutCommandEvidence("`npm test`", exchange([{ command: "cd server && false && cd web; npm test", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(unscoped).toMatchObject({ state: "exit", exitCode: 0 });
    const wrapped = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command: "bash -lc 'cd web && npm test'", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(wrapped).toMatchObject({ state: "exit", exitCode: 0 });
    const failed = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command: "cd web && npm test", result: { is_error: true, outcome: { status: "failed", exit_code: 1 } } }]))[0];
    expect(failed).toMatchObject({ state: "unavailable", exitCode: undefined, isError: undefined });
  });

  it("rejects env directory changes hidden after valued unset options", () => {
    for (const command of [
      "cd server && env -u UNUSED -C web npm test",
      "cd server && env --unset UNUSED --chdir web npm test",
    ]) {
      for (const claim of ["cd server && npm test", "cd web && npm test"]) {
        const result = closeoutCommandEvidence(`\`${claim}\``, exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
        expect(result).toMatchObject({ state: "not_found" });
        expect(result.exitCode).toBeUndefined();
        expect(result.isError).toBeUndefined();
      }
    }
  });

  it("rejects directory-stack changes instead of guessing the restored directory", () => {
    for (const command of ["cd server && pushd web && popd && npm test", "cd server && popd && npm test"]) {
      const result = closeoutCommandEvidence("`cd web && npm test`", exchange([{ command, result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
      expect(result).toMatchObject({ state: "not_found" });
      expect(result.exitCode).toBeUndefined();
      expect(result.isError).toBeUndefined();
    }
  });

  it("rejects a different npm test selection and reversed command order", () => {
    for (const command of ["npm test -- src/one.test.ts", "npm test --testNamePattern=one", "npm test --runTestsByPath=src/one.test.ts"]) {
      expect(closeoutCommandEvidence("`npm test`", exchange([{ command, result: { is_error: false } }]))[0].state).toBe("not_found");
    }
    expect(closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test -- --runInBand", result: { is_error: false } }]))[0].state).toBe("error");
    expect(closeoutCommandEvidence("`go vet ./... && go test ./...`", exchange([{ command: "go test ./... && go vet ./...", result: { is_error: false } }]))[0].state).toBe("not_found");
  });

  it("does not borrow another exchange's command", () => {
    const earlier = exchange([{ command: "npm test", result: { is_error: false } }]);
    const later = exchange([{ command: "npm run build", result: { is_error: false } }]);
    expect(earlier.id).not.toBe(later.id);
    expect(closeoutCommandEvidence("`npm test`", later)[0].state).toBe("not_found");
  });
});

describe("closeoutCommandEvidence facts", () => {
  it("shows a numeric zero without turning an error flag into an exit code", () => {
    const zero = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: false, outcome: { status: "ok", exit_code: 0 } } }]))[0];
    expect(zero).toMatchObject({ state: "exit", exitCode: 0, isError: false });
    const flag = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: false } }]))[0];
    expect(flag).toMatchObject({ state: "error", isError: false });
    expect(flag.exitCode).toBeUndefined();
  });

  it("shows a nonzero exit and the error flag together", () => {
    const result = closeoutCommandEvidence("`go test ./...`", exchange([{ command: "go test ./...", result: { is_error: true, outcome: { status: "failed", exit_code: 2 } } }]))[0];
    expect(result).toMatchObject({ state: "exit", exitCode: 2, isError: true });
  });

  it("distinguishes background, running and a later numeric background result", () => {
    const background = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: false, outcome: { status: "background" } } }]))[0];
    expect(background).toMatchObject({ state: "background", background: true, isError: false });
    const running = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: false, outcome: { status: "background" } }, note: { kind: "shell", status: "running" } }]))[0];
    expect(running).toMatchObject({ state: "running", background: true });
    const done = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: false, outcome: { status: "background" } }, note: { kind: "shell", status: "completed", exit_code: 0 } }]))[0];
    expect(done).toMatchObject({ state: "exit", background: true, exitCode: 0 });
  });

  it("keeps a call without a result and a folded segment unavailable, not not-found", () => {
    expect(closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test" }]))[0]).toMatchObject({ state: "unavailable", reason: "no_result" });
    const folded = exchange([{ command: "npm test", result: { is_error: false } }]);
    folded.folds = [item("fold")];
    expect(closeoutCommandEvidence("`npm test`", folded)[0].state).toBe("unavailable");
    expect(closeoutCommandEvidence("`npm test`", undefined)[0].state).toBe("unavailable");
  });

  it("keeps known facts beside a truncated record without presenting it as complete evidence", () => {
    const result = closeoutCommandEvidence("`npm test`", exchange([{ command: "npm test", result: { is_error: true, full_size: 9000, outcome: { status: "failed", exit_code: 1 } } }]))[0];
    expect(result).toMatchObject({ state: "unavailable", reason: "truncated", exitCode: 1, isError: true });
  });
});
