package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// writeNode is a node whose home is a fresh temp directory with a project folder in it.
func writeNode(t *testing.T) (*Node, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Node{dir: t.TempDir(), home: home, logger: log.New(io.Discard, "", 0)}, home
}

// Only folders in the home are writable; Persona's own folders, the Library, secret folders and everything outside
// stay read-only, however the path gets there (../, a link).
func TestWritableDir(t *testing.T) {
	n, home := writeNode(t)
	outside := t.TempDir()
	for _, d := range []string{".persona/data", ".persona", "Library/Preferences", ".ssh", "proj/sub"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(home, "proj", "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "proj", "sub"), filepath.Join(home, "inlink")); err != nil {
		t.Fatal(err)
	}
	ok := []struct{ path, cwd string }{
		{"~", ""}, {"~/proj", ""}, {home + "/proj/sub", ""}, {"sub", home + "/proj"}, {"~/inlink", ""},
		{"~/proj/sub/..", ""},
	}
	for _, c := range ok {
		if _, _, err := n.writableDir(c.path, c.cwd); err != nil {
			t.Errorf("%q (cwd %q): %v", c.path, c.cwd, err)
		}
	}
	refused := []struct{ path, cwd, why string }{
		{outside, "", "outside the home"},
		{"~/proj/out", "", "a link out of the home"},
		{"~/proj/../..", "", "../ above the home"},
		{"../../..", home + "/proj", "relative ../ above the home"},
		{"~/.persona", "", "Persona's folder"},
		{"~/.persona/data", "", "inside Persona's folder"},
		{"~/.PERSONA", "", "Persona's folder in another case"},
		{"~/.persona", "", "the old Persona folder"},
		{"~/Library/Preferences", "", "the Library"},
		{"~/.ssh", "", "a secret folder"},
		{"/", "", "the root"},
		{"", home, "no path"},
	}
	for _, c := range refused {
		if _, _, err := n.writableDir(c.path, c.cwd); err == nil {
			t.Errorf("%s (%q) was writable", c.why, c.path)
		}
	}
	if _, _, err := n.writableDir(outside, ""); rpcCode(err) != proto.CodeReadOnly {
		t.Errorf("outside the home: code %q, want %q", rpcCode(err), proto.CodeReadOnly)
	}
	if err := os.WriteFile(filepath.Join(home, "proj", "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := n.writableDir("~/proj/a.txt", ""); err == nil {
		t.Error("a file was taken as a folder")
	}
	if _, _, err := (&Node{}).writableDir("/tmp", ""); err == nil {
		t.Error("a node with no home wrote somewhere")
	}
}

// dirUpload runs a whole dest=dir upload through the RPC the hub speaks.
func dirUpload(t *testing.T, n *Node, dir, cwd, conflict, name string, data []byte) (proto.UploadedFile, error) {
	t.Helper()
	call := func(method string, p map[string]any) (any, error) {
		raw, _ := json.Marshal(p)
		return n.uploadCall(method, raw)
	}
	res, err := call("file.upload.begin", map[string]any{"name": name, "size": len(data), "dest": "dir", "dir": dir, "cwd": cwd, "conflict": conflict})
	if err != nil {
		return proto.UploadedFile{}, err
	}
	if d := res.(map[string]any)["dest"]; d != "dir" {
		t.Fatalf("begin did not echo dest=dir: %v", res)
	}
	id := res.(map[string]any)["upload_id"]
	if len(data) > 0 {
		if _, err := call("file.upload.chunk", map[string]any{"upload_id": id, "offset": 0, "data": data}); err != nil {
			return proto.UploadedFile{}, err
		}
	}
	out, err := call("file.upload.end", map[string]any{"upload_id": id})
	if err != nil {
		return proto.UploadedFile{}, err
	}
	return out.(proto.UploadedFile), nil
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// A file sent into a folder keeps its own name there, is readable like a file made by hand, and the path comes back
// as the app knows the folder; no part file stays behind.
func TestDirUploadKeepsItsName(t *testing.T) {
	n, home := writeNode(t)
	out, err := dirUpload(t, n, "~/proj", "", "", "设计稿 v2.pdf", []byte("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "proj", "设计稿 v2.pdf"); out.Path != want || out.Name != "设计稿 v2.pdf" || out.Size != 4 {
		t.Fatalf("got %+v, want path %s", out, want)
	}
	st, err := os.Stat(out.Path)
	if err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("stat %v, %v", st, err)
	}
	if names := dirNames(t, filepath.Join(home, "proj")); len(names) != 1 {
		t.Fatalf("folder has %v", names)
	}
	// Relative to the session's folder, and a name that tries to leave it stays inside.
	out, err = dirUpload(t, n, ".", filepath.Join(home, "proj"), "", "../../escape.txt", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(out.Path) != filepath.Join(home, "proj") {
		t.Fatalf("landed at %s", out.Path)
	}
}

// A name the file system cannot take (over 255 bytes on Linux, 255 characters on a Mac) is shortened as a numbered
// copy's is, so the upload lands instead of failing once every byte has been sent.
func TestDirUploadShortensANameTooLong(t *testing.T) {
	n, home := writeNode(t)
	long := strings.Repeat("长", 300)
	out, err := dirUpload(t, n, "~/proj", "", "", long+".pdf", []byte("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if want := long[:180] + ".pdf"; out.Name != want || out.Path != filepath.Join(home, "proj", want) {
		t.Fatalf("got %+v, want name %s", out, want)
	}
	if b, err := os.ReadFile(out.Path); err != nil || string(b) != "%PDF" {
		t.Fatalf("content %q %v", b, err)
	}
}

// A taken name: refused before any byte with no policy, "name 2.ext" with keep, overwritten (keeping its
// permissions) with replace; a folder of that name is never replaced.
func TestDirUploadConflicts(t *testing.T) {
	n, home := writeNode(t)
	proj := filepath.Join(home, "proj")
	if err := os.WriteFile(filepath.Join(proj, "a.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := dirUpload(t, n, proj, "", "", "a.txt", []byte("new"))
	if rpcCode(err) != proto.CodeExists {
		t.Fatalf("no policy: %v (code %q)", err, rpcCode(err))
	}
	var re *rpc.Error
	if errors.As(err, &re); !strings.Contains(string(re.Data), `"a.txt"`) {
		t.Fatalf("data %s", re.Data)
	}
	if names := dirNames(t, proj); len(names) != 1 {
		t.Fatalf("a refused upload left %v", names)
	}
	for _, want := range []string{"a 2.txt", "a 3.txt"} {
		out, err := dirUpload(t, n, proj, "", "keep", "a.txt", []byte("new"))
		if err != nil || out.Name != want {
			t.Fatalf("keep: %+v %v, want %s", out, err, want)
		}
	}
	out, err := dirUpload(t, n, proj, "", "replace", "a.txt", []byte("newer"))
	if err != nil || out.Name != "a.txt" {
		t.Fatalf("replace: %+v %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "a.txt")); string(b) != "newer" {
		t.Fatalf("content %q", b)
	}
	if st, _ := os.Stat(filepath.Join(proj, "a.txt")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want the old file's 0600", st.Mode())
	}
	if err := os.Mkdir(filepath.Join(proj, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := dirUpload(t, n, proj, "", "replace", "d", []byte("x")); rpcCode(err) != proto.CodeExists {
		t.Fatalf("replacing a folder: %v", err)
	}
	if st, err := os.Stat(filepath.Join(proj, "d")); err != nil || !st.IsDir() {
		t.Fatal("the folder is gone")
	}
}

// A file that takes the name while the upload is under way is not overwritten: the upload ends with exists and
// leaves nothing behind.
func TestDirUploadNameTakenMeanwhile(t *testing.T) {
	n, home := writeNode(t)
	proj := filepath.Join(home, "proj")
	raw, _ := json.Marshal(map[string]any{"name": "b.txt", "size": 3, "dest": "dir", "dir": proj})
	res, err := n.uploadCall("file.upload.begin", raw)
	if err != nil {
		t.Fatal(err)
	}
	id := res.(map[string]any)["upload_id"]
	raw, _ = json.Marshal(map[string]any{"upload_id": id, "offset": 0, "data": []byte("new")})
	if _, err := n.uploadCall("file.upload.chunk", raw); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "b.txt"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"upload_id": id})
	if _, err := n.uploadCall("file.upload.end", raw); rpcCode(err) != proto.CodeExists {
		t.Fatalf("end: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "b.txt")); !bytes.Equal(b, []byte("theirs")) {
		t.Fatalf("overwritten: %q", b)
	}
	if names := dirNames(t, proj); len(names) != 1 {
		t.Fatalf("folder has %v", names)
	}
}

// Uploads into read-only places are refused at begin and write nothing there.
func TestDirUploadRefused(t *testing.T) {
	n, home := writeNode(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".persona"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{outside, filepath.Join(home, ".persona"), filepath.Join(home, "missing")} {
		if _, err := dirUpload(t, n, dir, "", "", "x.txt", []byte("x")); err == nil {
			t.Errorf("%s took the upload", dir)
		}
	}
	if names := dirNames(t, outside); len(names) != 0 {
		t.Fatalf("outside has %v", names)
	}
	// A private key's name is refused even in a writable folder (the read guard would hide it anyway).
	if _, err := dirUpload(t, n, "~/proj", "", "", "id_ed25519", []byte("x")); err == nil {
		t.Error("wrote a private key's name")
	}
	if _, err := dirUpload(t, n, "~/proj", "", "maybe", "x.txt", []byte("x")); err == nil {
		t.Error("an unknown conflict policy was taken")
	}
	raw, _ := json.Marshal(map[string]any{"name": "big.bin", "size": proto.MaxDirUploadBytes + 1, "dest": "dir", "dir": "~/proj"})
	if _, err := n.uploadCall("file.upload.begin", raw); rpcCode(err) != proto.CodeTooLarge {
		t.Fatalf("over the cap: %v", err)
	}
}

// The data folder PERSONA_HOME names is Persona's own folder too, whatever it is called: its extensions, knowledge base
// and managers are as read-only as ~/.persona's, by its own path or a link to it. With PERSONA_HOME=~ the data folder
// is the home itself, and the home stays writable.
func TestWritableDirCustomDataDir(t *testing.T) {
	n, home := writeNode(t)
	state := filepath.Join(home, "persona-state")
	t.Setenv("PERSONA_HOME", state)
	for _, d := range []string{"persona-state/extensions/demo", "persona-state/data"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(state, filepath.Join(home, "proj", "state")); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"~/persona-state", "~/persona-state/extensions/demo", "~/persona-state/data", "~/proj/state/extensions"} {
		if _, _, err := n.writableDir(dir, ""); rpcCode(err) != proto.CodeReadOnly {
			t.Errorf("%s: %v, want %s", dir, err, proto.CodeReadOnly)
		}
	}
	if _, err := dirUpload(t, n, "~/persona-state/extensions/demo", "", proto.UploadConflictReplace, "run", []byte("x")); err == nil {
		t.Error("an upload replaced a file in the data folder")
	}
	if names := dirNames(t, filepath.Join(state, "extensions", "demo")); len(names) != 0 {
		t.Fatalf("the data folder got %v", names)
	}
	if _, _, err := n.writableDir("~/proj", ""); err != nil {
		t.Errorf("a project folder beside the data folder: %v", err)
	}

	n, _ = writeNode(t)
	t.Setenv("PERSONA_HOME", "~")
	for _, dir := range []string{"~", "~/proj"} {
		if _, _, err := n.writableDir(dir, ""); err != nil {
			t.Errorf("PERSONA_HOME=~: %s: %v", dir, err)
		}
	}
}

// node.info's home_real (#8095): the home with its links followed when that is another path, else nothing.
func TestRealHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(base, "data", "home", "me")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "home-me")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for home, want := range map[string]string{link: real, link + "/": real, real: "", "": "", "relative/home": "",
		filepath.Join(base, "missing"): ""} {
		if got := realHome(home); got != want {
			t.Errorf("realHome(%q) = %q, want %q", home, got, want)
		}
	}
}
