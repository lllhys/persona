package assistantskills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalCatalogIsBoundedAndReadsMetadataOnly(t *testing.T) {
	root := t.TempDir()
	write := func(dir, body string) {
		t.Helper()
		dir = filepath.Join(root, dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("review", "---\nname: review\ndescription: |\n  Review a document\n  for clarity.\n---\n!`touch should-not-run`\n")
	write("hidden", "---\nname: hidden\nuser-invocable: false # internal skill\n---\nInstructions\n")
	write("nested/not-a-root", "---\nname: hidden\n---\n")
	entries, err := LocalCatalog([]Root{{Path: root, Source: "project"}})
	if err != nil || len(entries) != 1 || entries[0].Description != "Review a document for clarity." {
		t.Fatalf("catalog: %+v %v", entries, err)
	}
	if _, err := os.Stat("should-not-run"); !os.IsNotExist(err) {
		t.Fatal("metadata was executed")
	}
	entries, err = LocalCatalog([]Root{{Path: filepath.Join(root, "missing"), Source: "user"}})
	if err != nil || len(entries) != 0 {
		t.Fatalf("absent optional root: %+v %v", entries, err)
	}
	if _, err := LocalCatalog([]Root{{Path: filepath.Join(root, "review", "SKILL.md"), Source: "user"}}); err == nil {
		t.Fatal("unreadable catalog reported as empty")
	}
}
