package node

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fileBirth reads when a file was made where the system keeps it (macOS always; Linux where statx reports it), and is
// never after the file's last change.
func TestFileBirth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.md")
	before := time.Now().Add(-time.Minute)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	born, ok := fileBirth(path, info)
	if !ok {
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			t.Fatalf("%s keeps a file's birth time; none read", runtime.GOOS)
		}
		t.Skip("this file system reports no birth time")
	}
	if born.Before(before) || !born.Before(later) {
		t.Fatalf("born %v, made after %v, changed at %v", born, before, later)
	}
}
