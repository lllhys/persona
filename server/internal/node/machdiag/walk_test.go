//go:build linux || darwin

package machdiag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFile makes a file of n bytes (and its folders).
func writeFile(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func devOf(t *testing.T, p string) uint64 {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := statOf(info)
	if !ok {
		t.Fatal("no stat")
	}
	return st.dev
}

func TestWalkerCountsFilesOnceAndNeverFollowsLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "big"), 1<<20)
	writeFile(t, filepath.Join(root, "a", "one"), 64<<10)
	writeFile(t, filepath.Join(root, "a", "b", "two"), 64<<10)
	if err := os.Symlink(outside, filepath.Join(root, "a", "link-to-outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "big"), filepath.Join(root, "a", "link-to-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a", "one"), filepath.Join(root, "a", "b", "one-again")); err != nil {
		t.Fatal(err)
	}
	w := newWalker(devOf(t, root))
	n, complete := w.size(context.Background(), filepath.Join(root, "a"))
	if !complete {
		t.Fatal("not complete")
	}
	if n < 128<<10 || n >= 1<<20 {
		t.Fatalf("size = %d: want the two files (and folders) once, nothing behind the links", n)
	}
	// A root that is itself a link holds nothing.
	if n, _ := w.size(context.Background(), filepath.Join(root, "a", "link-to-outside")); n != 0 {
		t.Fatalf("a link root = %d", n)
	}
}

// Another filesystem (a different device) is not walked.
func TestWalkerStaysOnOneFilesystem(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "f"), 4096)
	if n, complete := newWalker(devOf(t, root)+1).size(context.Background(), root); n != 0 || !complete {
		t.Fatalf("other device: %d %v", n, complete)
	}
}

// A walk whose time is up stops and says so.
func TestWalkerStopsAtItsDeadline(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(root, d, "f"), 4096)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, complete := newWalker(devOf(t, root)).size(ctx, root)
	if complete {
		t.Fatal("a cancelled walk says complete")
	}
	full, _ := newWalker(devOf(t, root)).size(context.Background(), root)
	if n >= full {
		t.Fatalf("cancelled walk counted %d of %d", n, full)
	}
}
