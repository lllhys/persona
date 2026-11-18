package claudeplugin

import (
	"os"
	"path/filepath"
	"testing"
)

// The plugin a build carries (after go generate) is integrations/claude-code-plugin as committed: the contract of its
// README ("Packaging"), the plugin persona listed from its own folder, without its tests.
func TestEmbeddedPluginIsTheRepositorysOne(t *testing.T) {
	b, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Skip("built without go generate ./internal/runtimepkg")
	}
	if b.ID() != "persona@persona" || b.Version == "" {
		t.Fatalf("embedded %s %q", b.ID(), b.Version)
	}
	src, err := BundleFromDir(filepath.Join("..", "..", "..", "integrations", "claude-code-plugin"))
	if os.IsNotExist(err) {
		t.Skip("no integrations checkout")
	}
	if err != nil {
		t.Fatal(err)
	}
	if src.Version != b.Version {
		t.Fatalf("embedded %s, integrations/ %s: run go generate", b.Version, src.Version)
	}
	for _, f := range b.Files {
		if filepath.Dir(f.Path) == "tests" {
			t.Fatalf("test file carried: %s", f.Path)
		}
	}
	if _, ok := b.marketFiles()[".claude-plugin/marketplace.json"]; !ok {
		t.Fatal("no marketplace file")
	}
}
