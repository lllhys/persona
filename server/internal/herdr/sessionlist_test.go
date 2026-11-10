package herdr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// With herdr installed and nothing running, `herdr session list` gave the same answer every 5 s from a new herdr
// process. It runs again only when a server may have started: a listed socket appeared, the sessions directory
// changed, herdr itself changed; and a list with a session running is never kept.
func TestSessionListRunsHerdrOnlyWhenItMayHaveChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in herdr is a shell script")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	config := filepath.Join(cfg, "herdr")
	sessions := filepath.Join(config, "sessions")
	if err := os.MkdirAll(filepath.Join(sessions, "build"), 0o700); err != nil {
		t.Fatal(err)
	}
	execs, answer := filepath.Join(dir, "execs"), filepath.Join(dir, "answer")
	bin := filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho x >> "+execs+"\ncat "+answer+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	list := func(running bool) string {
		r := map[bool]string{false: "false", true: "true"}[running]
		return `{"sessions":[{"default":true,"name":"default","running":false,"session_dir":"` + config +
			`","socket_path":"` + filepath.Join(config, "herdr.sock") + `"},{"default":false,"name":"build","running":` + r +
			`,"session_dir":"` + filepath.Join(sessions, "build") + `","socket_path":"` + filepath.Join(sessions, "build", "herdr.sock") + `"}]}`
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(answer, list(false))
	c := &Client{Bin: bin}
	ran := 0
	listN := func(n int, want string) {
		t.Helper()
		for range n {
			got, err := c.ListSessions(context.Background())
			if err != nil || len(got) != 2 || got[1].Name != "build" {
				t.Fatalf("list: %+v %v", got, err)
			}
		}
		b, _ := os.ReadFile(execs)
		if now := strings.Count(string(b), "x"); now-ran != len(want) {
			t.Fatalf("%d lists ran herdr %d times, want %d (%s)", n, now-ran, len(want), want)
		}
		ran += len(want)
	}
	listN(12, "x") // a minute of lists, nothing running: herdr once

	build := filepath.Join(sessions, "build", "herdr.sock")
	write(build, "") // the session's server started
	listN(1, "x")
	if err := os.Remove(build); err != nil {
		t.Fatal(err)
	}
	listN(3, "x")
	if err := os.Mkdir(filepath.Join(sessions, "new"), 0o700); err != nil { // a new session
		t.Fatal(err)
	}
	listN(3, "x")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho x >> "+execs+"\ncat "+answer+"\n# updated\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	listN(3, "x")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "elsewhere")) // another config directory: its sessions
	listN(1, "x")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	listN(1, "x")
	listN(2, "")
	write(answer, list(true)) // a running session: asked every time
	if err := os.Mkdir(filepath.Join(sessions, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	listN(3, "xxx")
	if _, err := c.SessionName(context.Background()); err != nil {
		t.Fatal(err)
	}
	listN(0, "x") // SessionName ran it too
}
