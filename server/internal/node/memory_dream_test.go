package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryDreamKeepsOneIndexedNoteAndRejectsStaleEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	if _, err := dreamMemory(dir, memoryOp{Op: "dream-write", Content: "Prefers tea."}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "MEMORY.md")
	index, err := os.ReadFile(path)
	if err != nil || strings.Count(string(index), "(dreaming.md)") != 1 {
		t.Fatalf("index = %q, err = %v", index, err)
	}
	notePath := filepath.Join(dir, dreamNoteName)
	note, err := os.ReadFile(notePath)
	if err != nil || string(note) != dreamNoteHeader+"Prefers tea.\n" {
		t.Fatalf("note = %q, err = %v", note, err)
	}
	previous := string(note)
	update := memoryOp{Op: "dream-write", Previous: &previous, PreviousIndex: string(index), Content: "Prefers coffee."}
	if _, err := dreamMemory(dir, update); err != nil {
		t.Fatal(err)
	}
	if _, err := dreamMemory(dir, update); err == nil {
		t.Fatal("stale write succeeded")
	}
	note, err = os.ReadFile(notePath)
	if err != nil || string(note) != dreamNoteHeader+"Prefers coffee.\n" {
		t.Fatalf("note = %q, err = %v", note, err)
	}
	index, err = os.ReadFile(path)
	if err != nil || string(index) != update.PreviousIndex {
		t.Fatalf("changed the existing index link: %q, err = %v", index, err)
	}
}
