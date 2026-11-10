package kb

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Concurrent writers of one path must all succeed and leave a whole copy of one of them in place: a shared
// p+".tmp" let one writer truncate the other's copy, so one rename failed (no such file) or put a cut file in place.
func TestWriteFileConcurrentWritersOfOnePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chats", "c1", "meta.json")
	const writers, rounds = 4, 200
	payload := func(w int) []byte { return bytes.Repeat([]byte(fmt.Sprintf("writer %d\n", w)), 512) }
	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				if err := writeFile(p, payload(w)); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	failed := 0
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d of %d writes failed, first: %v", failed, writers*rounds, first)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	whole := false
	for w := range writers {
		whole = whole || bytes.Equal(got, payload(w))
	}
	if !whole {
		t.Fatalf("file holds no whole write (%d bytes)", len(got))
	}
	assertOnlyFile(t, filepath.Dir(p), "meta.json")
}

// A write that cannot be renamed into place (the target is a non-empty directory) reports the error and leaves no
// temporary file behind.
func TestWriteFileFailureLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "summary.md")
	if err := os.MkdirAll(filepath.Join(p, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(p, []byte("text\n")); err == nil {
		t.Fatal("writing over a non-empty directory succeeded")
	}
	assertOnlyFile(t, dir, "summary.md")
}

// A written file is private and its content is exactly what was written.
func TestWriteFileReplacesPrivately(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	for _, body := range []string{"a longer first version\n", "short\n"} {
		if err := writeFile(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != body {
			t.Fatalf("read %q, %v; want %q", got, err, body)
		}
	}
	if fi, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if mode := fi.Mode().Perm(); mode&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("mode %v, want no group or other access", mode)
	}
	assertOnlyFile(t, filepath.Dir(p), "x.json")
}

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s holds %v, want only %s", dir, names, name)
	}
}
