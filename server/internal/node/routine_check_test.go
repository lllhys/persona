package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestRoutineCheck(t *testing.T) {
	if loginShellProgram() == "" {
		t.Skip("node shell unavailable")
	}
	for _, tc := range []struct {
		name, command string
		code          int
		reason        string
		timeout       bool
	}{
		{name: "pass", command: "exit 0"},
		{name: "skip", command: "printf 'holiday\\nmore\\n'; exit 1", code: 1, reason: "holiday"},
		{name: "error", command: "printf 'failed\\n' >&2; exit 4", code: 4, reason: "failed"},
		{name: "timeout", command: "sleep 30; exit 0", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(proto.RoutineCheckParams{Command: tc.command, Timeout: 1})
			if err != nil {
				t.Fatal(err)
			}
			result, err := routineCheck(t.Context(), raw)
			if tc.timeout {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("timeout = %v", err)
				}
				return
			}
			if err != nil || result.ExitCode != tc.code || result.Reason != tc.reason {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

func TestRoutineCheckShellInitBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell startup files")
	}
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			program, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SHELL", program)
			t.Setenv("ZDOTDIR", home)
			tool := filepath.Join(home, "routine-path-check")
			if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'from configured PATH\\n'\nexit 3\n"), 0700); err != nil {
				t.Fatal(err)
			}
			// i18n-ignore: exact historical shell diagnostics, including Bash's zh_CN translation (#8876).
			diagnostics := "bash: cannot set terminal process group (-1): Inappropriate ioctl for device\n" +
				"bash: no job control in this shell\nzsh: can't set tty pgrp: inappropriate ioctl for device\n" +
				"bash: 无法设定终端进程组 (-1): 对设备不适当的 ioctl 操作\nbash: 此 shell 中无任务控制\nexit\nlogout"
			rc := "printf '%s\\n' " + shellQuote(diagnostics) + " >&2\n" +
				"printf 'startup stdout\\n'; printf 'startup stderr without newline' >&2\n" +
				"export PATH=" + shellQuote(home) + ":$PATH\n" +
				"synthetic_check() { printf 'from configured function\\n' >&2; return 1; }\n" +
				"alias synthetic_alias=" + shellQuote("printf 'from configured alias\\n'") + "\n"
			if err := os.WriteFile(filepath.Join(home, "."+shell+"rc"), []byte(rc), 0600); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, command, reason string
				code                  int
			}{
				{"silent pass", "exit 0", "", 0},
				{"stdout", "printf 'holiday\\nmore\\n'; exit 1", "holiday", 1},
				{"stderr", "printf 'failed\\n' >&2; exit 4", "failed", 4},
				{"stdout exit", "printf 'exit\\n'; exit 1", "exit", 1},
				{"stderr exit", "printf 'exit\\n' >&2; exit 4", "exit", 4},
				{"logout", "printf 'logout\\n'; exit 1", "logout", 1},
				{"literal warning", "printf 'bash: no job control in this shell\\n'; exit 1", "bash: no job control in this shell", 1},
				{"stderr warning", "printf '%s\\n' " + shellQuote("bash: cannot set terminal process group (-1): Inappropriate ioctl for device") + " >&2; exit 4", "bash: cannot set terminal process group (-1): Inappropriate ioctl for device", 4},
				{"zsh warning", "printf '%s\\n' " + shellQuote("zsh: can't set tty pgrp: inappropriate ioctl for device") + " >&2; exit 4", "zsh: can't set tty pgrp: inappropriate ioctl for device", 4},
				// i18n-ignore: a command may legitimately report these exact diagnostics on stderr.
				{"translated warning", "printf '%s\\n' " + shellQuote("bash: 无法设定终端进程组 (-1): 对设备不适当的 ioctl 操作") + " >&2; exit 4", "bash: 无法设定终端进程组 (-1): 对设备不适当的 ioctl 操作", 4},
				{"translated monitor warning", "printf '%s\\n' " + shellQuote("bash: 此 shell 中无任务控制") + " >&2; exit 4", "bash: 此 shell 中无任务控制", 4},
				{"function", "synthetic_check", "from configured function", 1},
				{"alias", "synthetic_alias", "from configured alias", 0},
				{"PATH", "routine-path-check", "from configured PATH", 3},
				{"no newline", "printf 'unterminated'", "unterminated", 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					raw, err := json.Marshal(proto.RoutineCheckParams{Command: tc.command, Timeout: 1})
					if err != nil {
						t.Fatal(err)
					}
					got, err := routineCheck(t.Context(), raw)
					if err != nil || got.ExitCode != tc.code || got.Reason != tc.reason {
						t.Fatalf("result = %+v, error = %v; want exit %d, reason %q", got, err, tc.code, tc.reason)
					}
				})
			}
			t.Run("startup failure", func(t *testing.T) {
				if err := os.WriteFile(filepath.Join(home, "."+shell+"rc"), []byte("printf 'startup failed\\n' >&2; exit 4\n"), 0600); err != nil {
					t.Fatal(err)
				}
				notRun := filepath.Join(home, "must-not-run")
				command := "printf 'must not run\\n' > " + shellQuote(notRun)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				control := loginShell(ctx, command)
				control.Dir = home
				killGroupOnCancel(control)
				output, controlErr := control.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(controlErr, &exit) || exit.ExitCode() != 4 {
					t.Fatalf("raw startup control: %q, %v", output, controlErr)
				}
				if _, err := os.Stat(notRun); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("raw startup control ran the command: %v", err)
				}
				// Bash can emit a platform-specific terminal diagnostic before the rc's own error.
				first, _, _ := strings.Cut(string(output), "\n")
				want := strings.TrimSpace(strings.ToValidUTF8(first[:min(512, len(first))], ""))
				raw, _ := json.Marshal(proto.RoutineCheckParams{Command: command, Timeout: 1})
				got, err := routineCheck(t.Context(), raw)
				if err != nil || got.ExitCode != 4 || got.Reason != want {
					t.Fatalf("startup error lost: %+v, %v; want reason %q", got, err, want)
				}
				if _, err := os.Stat(notRun); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("routine check ran the command after startup failed: %v", err)
				}
			})
		})
	}
}

func TestRoutineCheckDeadlineAfterShellExit(t *testing.T) {
	if runtime.GOOS == "windows" || loginShellProgram() == "" {
		t.Skip("requires a Unix shell")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	raw, err := json.Marshal(proto.RoutineCheckParams{Command: "sleep 30 & exit 0", Timeout: 10})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := routineCheck(ctx, raw); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("background child kept the check waiting for %s", elapsed)
	}
}

func TestRoutineCheckOutputBoundary(t *testing.T) {
	const marker = "\x1etest-routine-boundary\x1f"
	for split := 0; split <= len(marker); split++ {
		var output = routineCheckOutput{marker: []byte(marker)}
		for _, part := range []string{"startup without newline", marker[:split], marker[split:] + "exit\nmore\n"} {
			if n, err := output.Write([]byte(part)); n != len(part) || err != nil {
				t.Fatalf("split %d: Write = %d, %v", split, n, err)
			}
		}
		if got := string(output.reason()); got != "exit" {
			t.Fatalf("split %d: reason = %q", split, got)
		}
	}
	for _, tc := range []struct {
		name, output, reason string
	}{
		{"no marker", "startup failed\nmore\n", "startup failed"},
		{"no newline", "startup" + marker + "unterminated", "unterminated"},
		{"silent command", "startup" + marker, ""},
		{"command cap", marker + strings.Repeat("x", 600), strings.Repeat("x", 512)},
		{"long startup", strings.Repeat("x", 10_000) + marker + "failed\n", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := routineCheckOutput{marker: []byte(marker)}
			for _, b := range []byte(tc.output) {
				_, _ = output.Write([]byte{b})
				if len(output.pending) >= len(marker) {
					t.Fatal("startup matching buffer is unbounded")
				}
			}
			if got := string(output.reason()); got != tc.reason {
				t.Fatalf("reason = %q, want %q", got, tc.reason)
			}
		})
	}
}

func TestRoutineCheckCancellationAfterCommandStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			program, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SHELL", program)
			t.Setenv("ZDOTDIR", home)
			started := filepath.Join(home, "started")
			raw, _ := json.Marshal(proto.RoutineCheckParams{Command: "printf started > " + shellQuote(started) + "; sleep 30", Timeout: 10})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := routineCheck(ctx, raw); done <- err }()
			waitFor(t, "the routine check to start", func() bool { _, err := os.Stat(started); return err == nil })
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation = %v", err)
				}
			case <-time.After(1500 * time.Millisecond):
				t.Fatal("cancelled check kept its child running")
			}
		})
	}
}
