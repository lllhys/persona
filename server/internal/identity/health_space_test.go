package identity

import (
	"path/filepath"
	"testing"
)

// A health conversation's hook or card can carry a subfolder of its directory: Claude Code reports the shell's
// directory after the model's `cd` (#2368). InHealthSpace counts those as the space; a path part named health
// elsewhere is not one. SpaceOf stays exact.
func TestInHealthSpace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", "")
	in := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	old := func(parts ...string) string { return in(append([]string{"." + "jar" + "vis"}, parts...)...) } // the former state directory
	for p, want := range map[string]bool{
		in(".persona", "health"):                                        true,
		in(".persona", "health", "reports"):                             true,
		in(".persona", "health", "reports", "2026"):                     true,
		in(".persona", "health-m-0b0b0b", "x"):                          true,
		old("health", "x"):                                              true,
		in(".persona", "assistant", ".claude", "skills", "health"):      false,
		in(".persona", "health-bin", "x"):                               false,
		filepath.Join(string(filepath.Separator), "tmp", "health", "x"): false,
		in(".persona", "assistant", "x"):                                false,
		in(".persona"):                                                  false,
		"":                                                              false,
		".":                                                             false,
	} {
		if got := InHealthSpace(p); got != want {
			t.Errorf("InHealthSpace(%q) = %v, want %v", p, got, want)
		}
	}
	// Classification stays exact: a subfolder is not the space's directory.
	if SpaceOf(in(".persona", "health", "reports")) != "" || IsAssistantDirOf(in(".persona", "health", "reports"), "") {
		t.Error("SpaceOf and IsAssistantDirOf take only the directory itself")
	}
}

// A card is placed in its conversation's directory, unless the hook's is inside a health space the conversation's is
// not: a mix-up can only hide more.
func TestCardCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", "")
	assistant, health := filepath.Join(home, ".persona", "assistant"), filepath.Join(home, ".persona", "health")
	tmp := filepath.Join(string(filepath.Separator), "tmp")
	for _, c := range []struct{ home, here, want string }{
		{"", filepath.Join(health, "x"), filepath.Join(health, "x")},
		{assistant, filepath.Join(health, "x"), filepath.Join(health, "x")},
		{health, tmp, health},
		{health, filepath.Join(health, "reports"), health},
		{health, assistant, health},
		{assistant, tmp, assistant},
		{assistant, filepath.Join(assistant, ".claude", "skills", "x"), assistant},
		{assistant, "", assistant},
	} {
		if got := CardCwd(c.home, c.here); got != c.want {
			t.Errorf("CardCwd(%q, %q) = %q, want %q", c.home, c.here, got, c.want)
		}
	}
}
