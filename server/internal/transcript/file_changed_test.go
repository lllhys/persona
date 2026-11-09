package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

// A watch stats its transcript on every tick (Changed) and reads it only when that says so, and a Refresh with nothing
// new answers from a stat too: both must see what a read would. New lines, a file replaced by another of the same
// size, and a file gone are changes; a file at rest is not, and Refreshing it keeps the span and history.
func TestChangedSeesWhatRefreshWould(t *testing.T) {
	dir := t.TempDir()
	one := `{"type":"user","uuid":"u1","message":{"role":"user","content":"one"}}`
	two := `{"type":"user","uuid":"u2","message":{"role":"user","content":"two"}}`
	path := writeFile(t, dir, []string{one}, "")
	f := NewFile(path)
	if !f.Changed() {
		t.Fatal("a file not read yet is a change")
	}
	_, first, err := f.RefreshSpan()
	if err != nil {
		t.Fatal(err)
	}
	if f.Changed() {
		t.Fatal("a file read to its end is no change")
	}
	fresh, rest, err := f.RefreshSpan()
	if err != nil || len(fresh) != 0 || rest != (Span{first.History, 1, 1}) {
		t.Fatalf("a refresh at rest = %v, %+v, %v; want nothing, {%s 1 1}", fresh, rest, err, first.History)
	}

	writeFile(t, dir, []string{one, two}, "")
	if !f.Changed() {
		t.Fatal("an appended line is a change")
	}
	if fresh, _ := f.Refresh(); len(fresh) != 1 || fresh[0].Text != "two" {
		t.Fatalf("want the appended line, got %+v", fresh)
	}

	// Another file of the same size put in its place (a rewrite through a rename): read again from the start.
	other := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(other, []byte(two+"\n"+one+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if inodeOf(t, path) != 0 { // a file system without inodes (Windows) sees no replacement, as before
		if !f.Changed() {
			t.Fatal("a file replaced by one of the same size is a change")
		}
		if fresh, _ := f.Refresh(); len(fresh) != 2 || fresh[0].Text != "two" || fresh[0].Seq != 1 {
			t.Fatalf("want the replacement read from its start, got %+v", fresh)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !f.Changed() {
		t.Fatal("a file gone is a change (Refresh says why)")
	}
	if _, err := f.Refresh(); err == nil {
		t.Fatal("want an error for the file gone")
	}
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return inode(st)
}
