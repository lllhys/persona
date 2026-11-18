package node

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The plugin goes into the user's ~/.claude and the directory the node's environment names, never a profile of
// Persona's own under its data directory.
func TestClaudePluginDirs(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "me")
	data := filepath.Join(home, ".persona")
	def := filepath.Join(home, ".claude")
	for _, c := range []struct {
		env  string
		want []string
	}{
		{"", []string{def}},
		{filepath.Join(home, "work-claude"), []string{def, filepath.Join(home, "work-claude")}},
		{"~/other", []string{def, filepath.Join(home, "other")}},
		{filepath.Join(data, "profiles", "x"), []string{def}},
		{data, []string{def}},
	} {
		if got := claudePluginDirs(home, data, c.env); !slices.Equal(got, c.want) {
			t.Errorf("env %q: %v, want %v", c.env, got, c.want)
		}
	}
}

// The desktop app's own CLIs, newest version first, in both layouts.
func TestClaudeDesktopCLIs(t *testing.T) {
	home := t.TempDir()
	dirs := desktopDataDirs("windows", home)
	for _, p := range []string{
		filepath.Join(dirs[1], "claude-code", "2.1.299", "f0584be0fbaf", "claude.exe"),
		filepath.Join(dirs[1], "claude-code", "2.1.308", "c17a819afb40", "claude.exe"),
		filepath.Join(dirs[0], "claude-code", "2.1.300", "claude.exe"),
		filepath.Join(dirs[1], "claude-code", "2.1.310", "notes.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := claudeDesktopCLIs("windows", home)
	var versions []string
	for _, p := range got {
		versions = append(versions, desktopCLIVersion(p))
	}
	if want := []string{"2.1.308", "2.1.300", "2.1.299"}; !slices.Equal(versions, want) {
		t.Fatalf("versions %v (%v), want %v", versions, got, want)
	}
	if got := claudeDesktopCLIs("linux", home); len(got) != 0 {
		t.Fatalf("linux: %v", got)
	}
}
