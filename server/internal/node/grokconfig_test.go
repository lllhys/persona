package node

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sampleGrokConfig = `# my grok
theme = "dark"

[models]
# picked in /settings
default = "grok-old"   # comment
hidden_models = [
  "a",
  "[ui]",
]

[ui]
yolo = false
note = """
[models]
default = "inside a string"
"""

[[marketplace.sources]]
url = "https://example.com/one"
default = "array entry"

[[marketplace.sources]]
url = "https://example.com/two"
`

func TestTomlValuesReadsTables(t *testing.T) {
	v := tomlValues(sampleGrokConfig)
	if v["theme"] != "dark" || v["models.default"] != "grok-old" || v["ui.yolo"] != false {
		t.Fatalf("%v", v)
	}
	for _, k := range []string{"default", "marketplace.sources.default", "ui.default"} {
		if _, ok := v[k]; ok {
			t.Fatalf("%s is not a key of the file's tables: %v", k, v)
		}
	}
	if got := tomlValues("ui.compact_mode = true\n")["ui.compact_mode"]; got != true {
		t.Fatalf("a top-level dotted key reads as its table's: %v", got)
	}
}

func TestTomlSetDottedReplacesInPlace(t *testing.T) {
	got, err := tomlSetDotted(sampleGrokConfig, "models.default", "grok-new")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sampleGrokConfig, `default = "grok-old"   # comment`, `default = "grok-new"`, 1)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTomlSetDottedAddsToExistingTable(t *testing.T) {
	got, err := tomlSetDotted(sampleGrokConfig, "ui.permission_mode", "ask")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sampleGrokConfig, "\"\"\"\n\n[[marketplace", "\"\"\"\npermission_mode = \"ask\"\n\n[[marketplace", 1)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if v := tomlValues(got); v["ui.permission_mode"] != "ask" || v["ui.yolo"] != false {
		t.Fatalf("%v", v)
	}
}

func TestTomlSetDottedCreatesMissingTable(t *testing.T) {
	text := "theme = \"dark\"\n\n[[marketplace.sources]]\nurl = \"x\"\n"
	got, err := tomlSetDotted(text, "ui.compact_mode", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := text + "\n[ui]\ncompact_mode = true\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// A missing file (empty text) gets just the table.
	got, err = tomlSetDotted("", "models.default", "m")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[models]\ndefault = \"m\"\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTomlSetDottedRemoves(t *testing.T) {
	for _, value := range []any{nil, ""} {
		got, err := tomlSetDotted(sampleGrokConfig, "models.default", value)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(sampleGrokConfig, "default = \"grok-old\"   # comment\n", "", 1)
		if got != want {
			t.Fatalf("got\n%s", got)
		}
	}
	got, err := tomlSetDotted(sampleGrokConfig, "ui.compact_mode", nil)
	if err != nil || got != sampleGrokConfig {
		t.Fatalf("removing a key that is not there changes nothing: %v\n%s", err, got)
	}
}

func TestTomlSetDottedTopLevelAndInline(t *testing.T) {
	got, err := tomlSetDotted(sampleCodexConfig, "model", "gpt-5.5")
	if err != nil || got != tomlSet(sampleCodexConfig, "model", "gpt-5.5") {
		t.Fatalf("a top-level key goes through tomlSet: %v\n%s", err, got)
	}
	got, err = tomlSetDotted("ui.yolo = true\n", "ui.yolo", false)
	if err != nil || got != "ui.yolo = false\n" {
		t.Fatalf("a top-level dotted key is replaced in place: %v %q", err, got)
	}
	if _, err := tomlSetDotted("ui = { yolo = true }\n", "ui.compact_mode", true); err == nil {
		t.Fatal("an inline table must not get a second [ui]")
	}
}

func TestGrokSettingsOnTheNode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", "")
	n := &Node{home: home}
	path := filepath.Join(home, ".grok", "config.toml")

	// No file: the whitelist with defaults, nothing set.
	list, err := n.getGrokSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(grokSettings) {
		t.Fatalf("%v", list)
	}
	for _, s := range list {
		if s.Value != nil {
			t.Fatalf("%s should be unset: %v", s.Key, s.Value)
		}
	}

	// The first write creates the file.
	if err := n.setGrokSetting("ui.permission_mode", "always-approve"); err != nil {
		t.Fatal(err)
	}
	if err := n.setGrokSetting("ui.yolo", true); err != nil {
		t.Fatal(err)
	}
	if err := n.setGrokSetting("models.default", "grok-x"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[ui]\npermission_mode = \"always-approve\"\nyolo = true\n\n[models]\ndefault = \"grok-x\"\n"; string(b) != want {
		t.Fatalf("got %q", b)
	}

	// Refused: unknown keys, values outside an enum, wrong types; the file is untouched.
	for _, c := range []struct {
		key   string
		value any
	}{{"ui.theme", "dark"}, {"models.allowed_models", "x"}, {"ui.permission_mode", "yolo"}, {"ui.yolo", "yes"}, {"models.default", true}} {
		if err := n.setGrokSetting(c.key, c.value); err == nil {
			t.Fatalf("%s = %v should be refused", c.key, c.value)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(b) {
		t.Fatalf("a refused change wrote the file: %q", after)
	}

	got := map[string]any{}
	list, _ = n.getGrokSettings()
	for _, s := range list {
		got[s.Key] = s.Value
	}
	if got["ui.permission_mode"] != "always-approve" || got["ui.yolo"] != true || got["models.default"] != "grok-x" || got["ui.compact_mode"] != nil {
		t.Fatalf("%v", got)
	}

	// Back to the default removes the key.
	if err := n.setGrokSetting("ui.yolo", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "yolo") {
		t.Fatalf("yolo should be gone: %q", b)
	}
}

func TestGrokSettingsFollowGrokHome(t *testing.T) {
	home := t.TempDir()
	grokHome := filepath.Join(t.TempDir(), "grok")
	t.Setenv("GROK_HOME", grokHome)
	n := &Node{home: home}
	if err := n.setGrokSetting("ui.compact_mode", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(grokHome, "config.toml")); err != nil {
		t.Fatalf("GROK_HOME's config.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok")); !os.IsNotExist(err) {
		t.Fatalf("~/.grok should be left alone when GROK_HOME is set: %v", err)
	}
}

func TestGrokPermissionDetailsSayTheyApproveEverything(t *testing.T) {
	for _, s := range grokSettings {
		switch s.Key {
		case "ui.permission_mode":
			if !strings.Contains(s.Detail, "always-approve approves every tool call") {
				t.Fatalf("%q", s.Detail)
			}
		case "ui.yolo":
			if !strings.Contains(s.Detail, "Always approves tool calls") {
				t.Fatalf("%q", s.Detail)
			}
		}
	}
}

func TestTomlQuotedHeaderEndsTheTable(t *testing.T) {
	in := "[ui]\nyolo = true\n\n[projects.\"/home/me/repo\"]\ntrust_level = \"trusted\"\ncompact_mode = true\n"
	v := tomlValues(in)
	if _, ok := v["ui.compact_mode"]; ok {
		t.Fatalf("a key under a quoted header is not the previous table's: %v", v)
	}
	if v["ui.yolo"] != true {
		t.Fatalf("%v", v)
	}
	got, err := tomlSetDotted(in, "ui.compact_mode", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "[ui]\nyolo = true\ncompact_mode = false\n\n[projects.\"/home/me/repo\"]\ntrust_level = \"trusted\"\ncompact_mode = true\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := tomlValues(got)["ui.compact_mode"]; got != false {
		t.Fatalf("read back %v", got)
	}
}

func TestTomlSetDottedKeepsDottedKeyTables(t *testing.T) {
	in := "theme = \"dark\"\nui.yolo = true\nui.note = \"\"\"\nx\n\"\"\"\n\n[models]\ndefault = \"a\"\n"
	got, err := tomlSetDotted(in, "ui.compact_mode", true)
	if err != nil {
		t.Fatal(err)
	}
	want := "theme = \"dark\"\nui.yolo = true\nui.note = \"\"\"\nx\n\"\"\"\nui.compact_mode = true\n\n[models]\ndefault = \"a\"\n"
	if got != want {
		t.Fatalf("a table defined by dotted keys gets a dotted key, not a second [ui]:\ngot\n%s\nwant\n%s", got, want)
	}

	in = "[tui]\nui.yolo = true\n"
	got, err = tomlSetDotted(in, "tui.ui.compact_mode", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[tui]\nui.yolo = true\nui.compact_mode = true\n"; got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestAgentSettingsSavedTogetherKeepEachOther(t *testing.T) {
	grokHome := filepath.Join(t.TempDir(), "grok")
	t.Setenv("GROK_HOME", grokHome)
	n := &Node{home: t.TempDir()}
	path := filepath.Join(grokHome, "config.toml")
	if err := os.MkdirAll(grokHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("theme = \"dark\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode goes through the umask (0600 under umask 077, #8723): set it, so the check below is of the save.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	keys := []string{"ui.yolo", "ui.compact_mode"}
	for round := 0; round < 50; round++ {
		want := round%2 == 0
		var wg sync.WaitGroup
		errs := make(chan error, len(keys))
		for _, k := range keys {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- n.setGrokSetting(k, want)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		v := tomlValues(string(b))
		for _, k := range keys {
			if v[k] != want {
				t.Fatalf("round %d: two saves at once lost %s:\n%s", round, k, b)
			}
		}
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("the file keeps its mode: %v %v", st.Mode(), err)
	}
	left, _ := filepath.Glob(filepath.Join(grokHome, "*.tmp"))
	if more, _ := filepath.Glob(filepath.Join(grokHome, ".*.tmp")); len(left)+len(more) > 0 {
		t.Fatalf("temp files left behind: %v %v", left, more)
	}
}
