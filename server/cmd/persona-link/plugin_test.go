package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func pluginEnv(t *testing.T) (*fakeEnv, *[]string) {
	t.Helper()
	f := newFakeEnv(t)
	t.Setenv("PERSONA_LANG", "en")
	var ran []string
	f.output = func(name string, args ...string) (string, string, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return "", "", nil
	}
	return f, &ran
}

func TestPluginNeedsTheNode(t *testing.T) {
	f, ran := pluginEnv(t)
	err := pluginCmd(f.env, []string{"status"})
	if err == nil || !strings.Contains(explainLanguage("en", err), "persona-link install") || len(*ran) != 0 {
		t.Fatalf("no node: %v %v", err, *ran)
	}
	if err := pluginCmd(f.env, []string{"wipe"}); err != errUsage {
		t.Fatalf("unknown op: %v", err)
	}
	if err := pluginCmd(f.env, nil); err != errUsage {
		t.Fatalf("no op: %v", err)
	}
}

func TestPluginRunsTheNodesCommand(t *testing.T) {
	f, ran := pluginEnv(t)
	bin := f.personadBin()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("node"), 0o755); err != nil {
		t.Fatal(err)
	}
	st := proto.ClaudePlugin{State: proto.ClaudePluginInstalled, Auto: true, Version: "0.1.0", Bundled: "0.1.0", Changed: true,
		ConfigDirs: []proto.ClaudePluginDir{{Path: "/home/pi/.claude", State: proto.ClaudePluginInstalled, Version: "0.1.0"},
			{Path: "/home/pi/profile", State: proto.ClaudePluginNotInstalled, OptedOut: proto.ClaudePluginOptedOutUninstalled}}}
	b, _ := json.Marshal(st)
	f.output = func(name string, args ...string) (string, string, error) {
		*ran = append(*ran, name+" "+strings.Join(args, " "))
		return string(b), "claude plugin: installed persona@persona", nil
	}
	if err := pluginCmd(f.env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if got := (*ran)[0]; got != bin+" claude-plugin install" {
		t.Fatalf("ran %q", got)
	}
	text := f.out.String()
	for _, want := range []string{"installed, version 0.1.0", "Automatic install and updates: on", "/home/pi/profile: removed in Claude Code",
		"the change reaches new sessions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	f.out.Reset()
	if err := pluginCmd(f.env, []string{"status", "--json"}); err != nil || strings.TrimSpace(f.out.String()) != string(b) {
		t.Fatalf("--json: %v %s", err, f.out)
	}
	// A node from before the plugin answers with its usage, not a status.
	f.output = func(string, ...string) (string, string, error) { return "usage: …", "", nil }
	if err := pluginCmd(f.env, []string{"status"}); err == nil || !strings.Contains(explainLanguage("en", err), "update the node") {
		t.Fatalf("old node: %v", err)
	}
}

func TestStatusShowsTheLastPluginCheck(t *testing.T) {
	f, ran := pluginEnv(t)
	st := proto.ClaudePlugin{State: proto.ClaudePluginDisabled, Auto: true, Version: "0.1.0"}
	b, _ := json.Marshal(st)
	if err := os.MkdirAll(f.personaDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.personaDir(), claudePluginStatusFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	r := gather(f.env)
	if r.ClaudePlugin == nil || r.ClaudePlugin.State != proto.ClaudePluginDisabled {
		t.Fatalf("report %+v", r.ClaudePlugin)
	}
	if text := r.text(f.now(), "pi"); !strings.Contains(text, "turned off in Claude Code") {
		t.Fatalf("status text:\n%s", text)
	}
	if len(*ran) != 0 {
		t.Fatalf("status ran %v", *ran)
	}
}
