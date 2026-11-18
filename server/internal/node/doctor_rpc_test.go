package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// callDoctor runs node.doctor / node.doctor.fix through handle, as the hub's call would, and decodes the report.
func callDoctor(t *testing.T, e *doctorEnv, method, params string) DoctorReport {
	t.Helper()
	e.n.doctorAt = func() DoctorOptions { return e.opts }
	out, err := e.n.handle(context.Background(), method, json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep DoctorReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestDoctorRPCReportsHealthyNode(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	rep := callDoctor(t, e, "node.doctor", `{}`)
	if !rep.OK || len(rep.Fixed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	// The wire model is the CLI's --json: same field names, lists never null.
	out, err := e.n.handle(context.Background(), "node.doctor", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ok", "dir", "checks", "fixed"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("no %q in the report", k)
		}
	}
	if string(raw["fixed"]) != "[]" {
		t.Errorf("fixed = %s", raw["fixed"])
	}
}

// Without fix the RPC only reads: stale and missing files stay as they are.
func TestDoctorRPCWritesNothingUnlessAskedTo(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	mcp := filepath.Join(e.opts.Dir, "mcp.json")
	hooks := filepath.Join(e.opts.Dir, "claude-settings.json")
	if err := os.WriteFile(mcp, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hooks); err != nil {
		t.Fatal(err)
	}
	for _, params := range []string{``, `{}`, `{"fix":false}`} {
		rep := callDoctor(t, e, "node.doctor", params)
		wantStatus(t, rep, "mcp.json", DoctorWarn, "stale")
		wantStatus(t, rep, "claude-settings.json", DoctorFail, "missing")
		if len(rep.Fixed) != 0 {
			t.Errorf("%q: fixed %v", params, rep.Fixed)
		}
		if b, _ := os.ReadFile(mcp); string(b) != `{"old":true}` {
			t.Fatalf("%q: mcp.json was rewritten: %s", params, b)
		}
		if _, err := os.Stat(hooks); err == nil {
			t.Fatalf("%q: claude-settings.json was written", params)
		}
	}
}

// fix goes the --fix way (writeGeneratedConfig): the generated files come back, the report after it is clean, and
// the user's own Claude Code settings are left exactly as they were.
func TestDoctorRPCFixRewritesGeneratedFilesOnly(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	user := filepath.Join(e.opts.ClaudeConfigDir, "settings.json")
	const mine = `{"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"/usr/bin/other-tool"}]}]}}`
	if err := os.WriteFile(user, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.opts.Dir, "claude-settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.opts.Dir, "mcp.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, params string }{{"node.doctor.fix", `{}`}, {"node.doctor", `{"fix":true}`}} {
		rep := callDoctor(t, e, tc.method, tc.params)
		if len(rep.Fixed) != 2 {
			t.Errorf("%s: fixed = %v", tc.method, rep.Fixed)
		}
		wantStatus(t, rep, "claude-settings.json", DoctorOK, "current")
		wantStatus(t, rep, "mcp.json", DoctorOK, "current")
		if !rep.OK {
			t.Errorf("%s: not ok after fix: %+v", tc.method, rep.Checks)
		}
		if b, _ := os.ReadFile(user); string(b) != mine {
			t.Fatalf("%s: the user's settings.json changed: %s", tc.method, b)
		}
		// Break both again for the second spelling.
		_ = os.Remove(filepath.Join(e.opts.Dir, "claude-settings.json"))
		_ = os.WriteFile(filepath.Join(e.opts.Dir, "mcp.json"), []byte(`{}`), 0o600)
	}
	// The user's own synchronous hook is reported, as information.
	rep := callDoctor(t, e, "node.doctor", `{}`)
	c := wantStatus(t, rep, "global-permission-hooks", DoctorWarn, "")
	if len(c.Items) != 1 {
		t.Errorf("items = %v", c.Items)
	}
}

func TestDoctorRPCRejectsBadParams(t *testing.T) {
	e := newDoctorEnv(t)
	e.n.doctorAt = func() DoctorOptions { return e.opts }
	if _, err := e.n.handle(context.Background(), "node.doctor", json.RawMessage(`{"fix":"yes"}`)); err == nil {
		t.Error("a bad fix value was accepted")
	}
}

// The web page groups these checks into features by name (web/src/lib/node-health.ts FEATURES): a check added,
// renamed or dropped here needs that table looked at.
func TestDoctorRPCCheckNamesAreWhatTheWebGroups(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	rep := callDoctor(t, e, "node.doctor", `{}`)
	var got []string
	for _, c := range rep.Checks {
		got = append(got, c.Name)
	}
	want := []string{"node.sock", "runtime.sock", "claude-settings.json", "claude-hooks", "assistant-settings.json", "assistant-hooks",
		"mcp.json", "agent-plugin", "command-paths", "claude-version", "herdr", "global-permission-hooks", "agent-sandbox"}
	if !slices.Equal(got, want) {
		t.Errorf("checks = %v\nwant     %v", got, want)
	}
	// On a Mac, and wherever the preflight runs, two more (#5944): the web shows them under "Other checks" when they
	// are not fine, and offers no Fix for them (their state is "").
	e.opts.LaunchAgent, e.opts.LaunchdTarget = filepath.Join(e.n.home, "none.plist"), "gui/501/"+NodeLaunchdLabel
	e.opts.Preflight = func(context.Context, i18n.Lang) DoctorCheck {
		return DoctorCheck{Name: pathAccessCheck, Status: DoctorWarn, Detail: "x"}
	}
	rep = callDoctor(t, e, "node.doctor", `{}`)
	got = got[:0]
	for _, c := range rep.Checks {
		got = append(got, c.Name)
		if (c.Name == launchdCheck || c.Name == pathAccessCheck) && c.State != "" {
			t.Errorf("%s: state %q", c.Name, c.State)
		}
	}
	if want = append(want, launchdCheck, pathAccessCheck); !slices.Equal(got, want) {
		t.Errorf("checks = %v\nwant     %v", got, want)
	}
}

// {"preflight": false} (the assistant's readiness, which only wants Claude Code's version) leaves the path-access
// check out: nothing read, nothing waited for. Without it, or true, the check runs.
func TestDoctorRPCWithoutThePreflight(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	runs := 0
	e.opts.Preflight = func(context.Context, i18n.Lang) DoctorCheck {
		runs++
		return DoctorCheck{Name: pathAccessCheck, Status: DoctorOK, Detail: "x"}
	}
	has := func(rep DoctorReport) bool {
		return slices.ContainsFunc(rep.Checks, func(c DoctorCheck) bool { return c.Name == pathAccessCheck })
	}
	if rep := callDoctor(t, e, "node.doctor", `{"preflight":false}`); has(rep) || runs != 0 {
		t.Errorf("runs %d checks %+v", runs, rep.Checks)
	}
	for _, params := range []string{`{}`, `{"preflight":true}`} {
		if rep := callDoctor(t, e, "node.doctor", params); !has(rep) {
			t.Errorf("%s: no %s", params, pathAccessCheck)
		}
	}
	if runs != 2 {
		t.Errorf("runs %d", runs)
	}
}
