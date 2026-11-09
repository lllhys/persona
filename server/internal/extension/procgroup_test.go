//go:build !windows

package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// A market git or a tool check whose program leaves a helper holding its output (git-remote-https or ssh hold
// git's): the call returns at its deadline instead of when the helper exits, and the helper does not outlive it.

// fakeWithHelper writes an executable script name into a new directory that starts a helper (sleep 6, sharing the
// script's output) in the background, writes the helper's pid down and then runs rest. It answers the directory and
// where the pid is written; the helper is killed when the test ends either way.
func fakeWithHelper(t *testing.T, name, rest string) (dir, pidFile string) {
	t.Helper()
	dir = t.TempDir()
	pidFile = filepath.Join(dir, "helper.pid")
	script := "#!/bin/sh\nsleep 6 &\necho $! > '" + pidFile + "'\n" + rest + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pid, err := readHelperPid(pidFile); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return dir, pidFile
}

func readHelperPid(pidFile string) (int, error) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err == nil && pid <= 0 {
		err = fmt.Errorf("pid %d", pid)
	}
	return pid, err
}

// helperRunning: pid is a process that has not exited. A killed orphan is a zombie until it is reaped, and stays one
// where nobody reaps (a container without an init), so a zombie counts as gone.
func helperRunning(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if i := bytes.LastIndexByte(b, ')'); i >= 0 && i+2 < len(b) && b[i+2] == 'Z' {
			return false
		}
	}
	return true
}

// waitHelperGone fails the test when the helper whose pid is in pidFile is still running after within.
func waitHelperGone(t *testing.T, pidFile string, within time.Duration) {
	t.Helper()
	pid, err := readHelperPid(pidFile)
	if err != nil {
		t.Fatalf("the fake never started its helper: %v", err)
	}
	for deadline := time.Now().Add(within); helperRunning(pid); time.Sleep(20 * time.Millisecond) { // sleep-ok: polls an orphaned process nothing signals the exit of, until the deadline
		if time.Now().After(deadline) {
			t.Fatalf("helper %d is still running %v after the call returned", pid, within)
		}
	}
}

// cancelledOnceStarted runs call, cancels it as soon as the fake has written its helper's pid, and answers how long
// the call took to return after the cancel, and its error. The production calls treat a cancel like their own
// deadline. Cancelling only once the helper exists, not on a fixed deadline from the start, keeps a busy machine
// that is slow to start the fake from cancelling it before there is a helper to stop.
func cancelledOnceStarted(t *testing.T, pidFile string, call func(ctx context.Context) error) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- call(ctx) }()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) { // sleep-ok: polls for the pid the fake writes, which nothing signals, until the deadline
		if _, err := readHelperPid(pidFile); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the call returned before the fake started its helper: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake never started its helper")
		}
	}
	cancel()
	start := time.Now()
	err := <-done
	return time.Since(start), err
}

func TestMarketGitTimeoutStopsItsHelpers(t *testing.T) {
	dir, pidFile := fakeWithHelper(t, "git", "exec sleep 30")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Market{Spec: MarketSpec{Kind: "git", URL: "https://example.invalid/x.git"}, Dir: t.TempDir()}
	took, err := cancelledOnceStarted(t, pidFile, func(ctx context.Context) error { return m.git(ctx, m.Dir, "fetch") })
	if err == nil || !strings.Contains(err.Error(), i18n.T(i18n.Default, "extension-status.git_timeout")) {
		t.Fatalf("want the timeout error, got %v", err)
	}
	if took > 2*time.Second {
		t.Fatalf("git returned %v after it was cancelled, waiting for its helper", took)
	}
	waitHelperGone(t, pidFile, 2*time.Second)
}

// git itself succeeded and only its helper keeps the output open: that is a success, and the helper is stopped.
func TestMarketGitSucceedsPastAHelperHoldingItsOutput(t *testing.T) {
	dir, pidFile := fakeWithHelper(t, "git", "exit 0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Market{Spec: MarketSpec{Kind: "git", URL: "https://example.invalid/x.git"}, Dir: t.TempDir()}
	start := time.Now()
	if err := m.git(context.Background(), m.Dir, "fetch"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("git returned after %v, waiting for its helper", took)
	}
	waitHelperGone(t, pidFile, 2*time.Second)
}

func TestToolCheckTimeoutStopsItsHelpers(t *testing.T) {
	dir, pidFile := fakeWithHelper(t, "tool", "exec sleep 30")
	took, err := cancelledOnceStarted(t, pidFile, func(ctx context.Context) error {
		_, err := runCheck(ctx, filepath.Join(dir, "tool"), &ToolCheck{}, os.Environ())
		return err
	})
	if err == nil {
		t.Fatal("a check that never finished passed")
	}
	if took > 2*time.Second {
		t.Fatalf("the check returned %v after it was cancelled, waiting for its helper", took)
	}
	waitHelperGone(t, pidFile, 2*time.Second)
}

func TestToolCheckSucceedsPastAHelperHoldingItsOutput(t *testing.T) {
	dir, pidFile := fakeWithHelper(t, "tool", "echo tool 1.2.3\nexit 0")
	start := time.Now()
	v, err := runCheck(context.Background(), filepath.Join(dir, "tool"), &ToolCheck{}, os.Environ())
	if err != nil || v != "1.2.3" {
		t.Fatalf("got %q, %v", v, err)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("the check returned after %v, waiting for its helper", took)
	}
	waitHelperGone(t, pidFile, 2*time.Second)
}

// A check that prints far more than a version is read for its first x.y.z without keeping all of it.
func TestToolCheckOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho tool 1.2.3\nhead -c 4000000 /dev/zero\necho 9.9.9\n"
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if v, err := runCheck(context.Background(), filepath.Join(dir, "tool"), &ToolCheck{}, os.Environ()); err != nil || v != "1.2.3" {
		t.Fatalf("got %q, %v", v, err)
	}
	var out headBuffer
	out.max = 8
	if n, err := out.Write([]byte("tool 1.2.3\n")); n != 11 || err != nil {
		t.Fatalf("a write past the limit answered %d, %v", n, err)
	}
	if n, err := out.Write([]byte("more")); n != 4 || err != nil || string(out.buf) != "tool 1.2" {
		t.Fatalf("kept %q (%d, %v)", out.buf, n, err)
	}
}
