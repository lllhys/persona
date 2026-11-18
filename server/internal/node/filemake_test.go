package node

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func makeCall(t *testing.T, n *Node, method string, params map[string]any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := n.fileMake(context.Background(), method, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func TestMakeDir(t *testing.T) {
	n, home := testNode(t)
	makeFile(t, filepath.Join(home, "proj", "a.txt"), "a")
	out, err := makeCall(t, n, "file.mkdir", map[string]any{"dir": "~/proj", "name": "new"})
	if err != nil || out["path"] != filepath.Join(home, "proj", "new") {
		t.Fatalf("mkdir = %v, %v", out, err)
	}
	if st, err := os.Stat(filepath.Join(home, "proj", "new")); err != nil || !st.IsDir() {
		t.Fatalf("no folder: %v", err)
	}
	_, err = makeCall(t, n, "file.mkdir", map[string]any{"dir": "~/proj", "name": "new"})
	wantCode(t, err, proto.CodeExists)
	_, err = makeCall(t, n, "file.mkdir", map[string]any{"dir": "~/proj", "name": "a.txt"})
	wantCode(t, err, proto.CodeExists)
	for _, bad := range []string{"", ".", "..", "a/b"} {
		if _, err := makeCall(t, n, "file.mkdir", map[string]any{"dir": "~/proj", "name": bad}); err == nil {
			t.Fatalf("made a folder named %q", bad)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, ".persona"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = makeCall(t, n, "file.mkdir", map[string]any{"dir": "~/.persona", "name": "x"})
	wantCode(t, err, proto.CodeReadOnly)
	_, err = makeCall(t, n, "file.mkdir", map[string]any{"dir": t.TempDir(), "name": "x"}) // outside the home
	wantCode(t, err, proto.CodeReadOnly)
}

func TestWriteCreatesAFileOnlyWhereNoneIs(t *testing.T) {
	n, home := testNode(t)
	makeFile(t, filepath.Join(home, "proj", "a.txt"), "a")
	text := "hello\n"
	out, err := makeCall(t, n, "file.write", map[string]any{"path": "notes.txt", "cwd": filepath.Join(home, "proj"), "create": true, "text": text})
	if err != nil || out["path"] != filepath.Join(home, "proj", "notes.txt") || out["revision"] == "" {
		t.Fatalf("create = %v, %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "proj", "notes.txt")); string(b) != text {
		t.Fatalf("wrote %q", b)
	}
	if st, _ := os.Stat(filepath.Join(home, "proj", "notes.txt")); runtime.GOOS != "windows" && st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %o, want 644", st.Mode().Perm())
	}
	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/proj/a.txt", "create": true, "text": ""})
	wantCode(t, err, proto.CodeExists)
	if b, _ := os.ReadFile(filepath.Join(home, "proj", "a.txt")); string(b) != "a" {
		t.Fatal("create replaced a file")
	}
	if err := os.MkdirAll(filepath.Join(home, "Library"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/Library/x.txt", "create": true, "text": ""})
	wantCode(t, err, proto.CodeReadOnly)
}

// A save of the same size stamped with the opened version's mtime (a coarse clock) still gets a new revision.
func TestPastRevisionOnACoarseClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.sh")
	makeFile(t, path, "echo 1\n")
	old, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	makeFile(t, path, "echo 2\n")
	if err := os.Chtimes(path, time.Time{}, old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.Stat(path); fileRevision(now) != fileRevision(old) {
		t.Fatal("setup: the two versions should share a revision")
	}
	pastRevision(path, old)
	now, _ := os.Stat(path)
	if fileRevision(now) == fileRevision(old) || !now.ModTime().After(old.ModTime()) {
		t.Fatalf("revision %s, mtime %v: want past %s", fileRevision(now), now.ModTime(), fileRevision(old))
	}
	if b, _ := os.ReadFile(path); string(b) != "echo 2\n" {
		t.Fatalf("content %q", b)
	}
}

func TestWriteReplacesOnlyTheVersionOpened(t *testing.T) {
	n, home := testNode(t)
	path := filepath.Join(home, "proj", "run.sh")
	makeFile(t, path, "echo 1\n")
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	rev := fileRevision(st)

	out, err := makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": "echo 2\n", "revision": rev})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "echo 2\n" {
		t.Fatalf("wrote %q", b)
	}
	if st, _ := os.Stat(path); runtime.GOOS != "windows" && st.Mode().Perm() != 0o750 {
		t.Fatalf("mode %o, want 750 kept", st.Mode().Perm())
	}
	newRev := out["revision"].(string)
	if newRev == rev {
		t.Fatal("revision did not change")
	}

	// The opened version is gone: refused, nothing written, the current revision said.
	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": "echo 3\n", "revision": rev})
	wantCode(t, err, proto.CodeFileChanged)
	if b, _ := os.ReadFile(path); string(b) != "echo 2\n" {
		t.Fatalf("a stale save wrote %q", b)
	}

	// Touched but the same bytes: the hash says so, the save goes.
	sum := sha256.Sum256([]byte("echo 2\n"))
	if _, err := makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": "echo 4\n", "revision": "stale", "sha256": hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
	// A different hash: refused.
	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": "echo 5\n", "revision": "stale", "sha256": hex.EncodeToString(sum[:])})
	wantCode(t, err, proto.CodeFileChanged)
	// Overwrite Anyway.
	if _, err := makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": "echo 6\n", "force": true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "echo 6\n" {
		t.Fatalf("force wrote %q", b)
	}
	// No temporary file left beside it.
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Fatalf("left %d entries beside the file", len(ents))
	}

	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/proj/nope.txt", "text": "x", "force": true})
	wantCode(t, err, proto.CodeNotFound)
	big := strings.Repeat("x", maxWriteBytes+1)
	_, err = makeCall(t, n, "file.write", map[string]any{"path": "~/proj/run.sh", "text": big, "force": true})
	wantCode(t, err, proto.CodeTooLarge)
}

func TestWriteThroughALinkWritesItsTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	n, home := testNode(t)
	target := filepath.Join(home, "shared", "conf.yaml")
	makeFile(t, target, "a: 1\n")
	if err := os.MkdirAll(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared/conf.yaml", filepath.Join(home, "proj", "conf.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := makeCall(t, n, "file.write", map[string]any{"path": "~/proj/conf.yaml", "text": "a: 2\n", "force": true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "a: 2\n" {
		t.Fatalf("target holds %q", b)
	}
	if st, err := os.Lstat(filepath.Join(home, "proj", "conf.yaml")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
}

func zipNames(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		kind := "file:"
		if f.Mode()&os.ModeSymlink != 0 {
			kind = "link:"
		} else if f.FileInfo().IsDir() {
			kind = "dir"
			b = nil
		}
		out[f.Name] = kind + string(b)
	}
	return out
}

func TestZipForADownload(t *testing.T) {
	n, home := testNode(t)
	makeFile(t, filepath.Join(home, "proj", "a.txt"), "a")
	makeFile(t, filepath.Join(home, "proj", "src", "b.go"), "package b")
	makeFile(t, filepath.Join(home, "proj", "src", "id_ed25519"), "secret") // a key name: left out
	if runtime.GOOS != "windows" {
		if err := os.Symlink("../a.txt", filepath.Join(home, "proj", "src", "link")); err != nil {
			t.Fatal(err)
		}
	}
	out, err := makeCall(t, n, "file.zip", map[string]any{"paths": []string{"~/proj/a.txt", "~/proj/src"}, "name": "Archive.zip"})
	if err != nil {
		t.Fatal(err)
	}
	path := out["path"].(string)
	if !strings.HasPrefix(path, filepath.Join(os.TempDir(), "persona-zips")) || out["name"] != "Archive.zip" {
		t.Fatalf("zip at %v", out)
	}
	names := zipNames(t, path)
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if names["a.txt"] != "file:a" || names["src/b.go"] != "file:package b" || names["src/"] != "dir" {
		t.Fatalf("zip holds %v", keys)
	}
	if _, ok := names["src/id_ed25519"]; ok {
		t.Fatal("a key went into the zip")
	}
	if runtime.GOOS != "windows" && names["src/link"] != "link:../a.txt" {
		t.Fatalf("the link is %q, want a link to ../a.txt", names["src/link"])
	}
	// Its file can be read back through the guard (the download), and nothing of it is in the home.
	if _, _, err := n.statSafe(path); err != nil {
		t.Fatalf("the zip is not readable: %v", err)
	}
	_ = os.RemoveAll(filepath.Dir(path))
}

func TestZipIntoAFolderKeepsBoth(t *testing.T) {
	n, home := testNode(t)
	makeFile(t, filepath.Join(home, "proj", "a.txt"), "a")
	makeFile(t, filepath.Join(home, "proj", "a.zip"), "old")
	out, err := makeCall(t, n, "file.zip", map[string]any{"paths": []string{"a.txt"}, "cwd": filepath.Join(home, "proj"), "dir": "~/proj", "name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "a 2.zip" || out["path"] != filepath.Join(home, "proj", "a 2.zip") {
		t.Fatalf("zip = %v, want a 2.zip beside a.zip", out)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "proj", "a.zip")); string(b) != "old" {
		t.Fatal("the zip there was replaced")
	}
	if names := zipNames(t, filepath.Join(home, "proj", "a 2.zip")); names["a.txt"] != "file:a" {
		t.Fatalf("zip holds %v", names)
	}
	if err := os.MkdirAll(filepath.Join(home, "Library"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = makeCall(t, n, "file.zip", map[string]any{"paths": []string{"~/proj/a.txt"}, "dir": "~/Library", "name": "x"})
	wantCode(t, err, proto.CodeReadOnly)
	_, err = makeCall(t, n, "file.zip", map[string]any{"paths": []string{"~/proj/nope"}, "dir": "~/proj"})
	wantCode(t, err, proto.CodeNotFound)
	_, err = makeCall(t, n, "file.zip", map[string]any{"paths": []string{"~/.ssh"}, "dir": "~/proj"})
	if err == nil {
		t.Fatal("zipped ~/.ssh")
	}
}
