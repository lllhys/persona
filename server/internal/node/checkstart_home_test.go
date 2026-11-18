package node

import (
	"os"
	"path/filepath"
	"testing"
)

// A bare "~" is the machine's home, like "~/x" is x under it; the hub may not know the home yet and leave it to us.
func TestCheckStartExpandsHome(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := t.TempDir()
	n := &Node{home: home}
	for in, want := range map[string]string{
		"~":     home,
		"~/":    home,
		"~/src": filepath.Join(home, "src"),
		abs:     abs,
		"":      home,
	} {
		got, launcher, err := n.checkStart(in, "", "", "", "", "")
		if err != nil {
			t.Fatalf("checkStart(%q): %v", in, err)
		}
		if got != want || launcher != "claude" {
			t.Errorf("checkStart(%q) = %q %q, want %q claude", in, got, launcher, want)
		}
	}
	if _, _, err := n.checkStart("~/missing", "", "", "", "", ""); err == nil {
		t.Error("checkStart accepted a directory that does not exist")
	}
}
