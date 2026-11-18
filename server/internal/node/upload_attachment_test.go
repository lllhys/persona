package node

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// callAttachment sends data as a chat attachment to keep (dest=attachment) the way the hub does.
func callAttachment(t *testing.T, n *Node, name string, data []byte) (proto.UploadedFile, map[string]any) {
	t.Helper()
	call := func(method string, p map[string]any) any {
		raw, _ := json.Marshal(p)
		out, err := n.uploadCall(method, raw)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return out
	}
	begin := call("file.upload.begin", map[string]any{"name": name, "size": len(data), "dest": proto.UploadDestAttachment}).(map[string]any)
	call("file.upload.chunk", map[string]any{"upload_id": begin["upload_id"], "offset": 0, "data": data})
	return call("file.upload.end", map[string]any{"upload_id": begin["upload_id"]}).(proto.UploadedFile), begin
}

// A chat attachment is kept beside the day's pictures in the node's uploads/<date>/ under its own name, the folder
// private to the user; the same name again is numbered, never written over; the hub is told the destination was
// understood; no part file is left; the temp directory gets nothing.
func TestAttachmentUploadKeptUnderItsOwnName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	n, _ := writeNode(t)
	out, begin := callAttachment(t, n, "Q3 report.pdf", []byte("one"))
	if begin["dest"] != proto.UploadDestAttachment {
		t.Fatalf("begin did not echo dest: %v", begin)
	}
	day := filepath.Dir(out.Path)
	if filepath.Dir(day) != filepath.Join(n.dir, "uploads") || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(filepath.Base(day)) {
		t.Fatalf("kept in %s, want %s/<date>", day, filepath.Join(n.dir, "uploads"))
	}
	if out.Name != "Q3 report.pdf" || filepath.Base(out.Path) != "Q3 report.pdf" || out.Size != 3 {
		t.Fatalf("got %+v", out)
	}
	if st, err := os.Stat(day); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("folder %v %v", st, err)
	}
	again, _ := callAttachment(t, n, "Q3 report.pdf", []byte("two"))
	if again.Path != filepath.Join(day, "Q3 report 2.pdf") {
		t.Fatalf("second: %s", again.Path)
	}
	if b, _ := os.ReadFile(out.Path); !bytes.Equal(b, []byte("one")) {
		t.Fatalf("first file was changed: %q", b)
	}
	odd, _ := callAttachment(t, n, "../../escape.txt", []byte("x"))
	if filepath.Dir(odd.Path) != day {
		t.Fatalf("a name left the folder: %s", odd.Path)
	}
	if ents, _ := os.ReadDir(day); len(ents) != 3 {
		t.Fatalf("folder has %v", ents)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Fatalf("temp directory has %v", ents)
	}
}

// Over the chat's limit it is refused before anything is written.
func TestAttachmentUploadLimit(t *testing.T) {
	n, _ := writeNode(t)
	raw, _ := json.Marshal(map[string]any{"name": "big.mov", "size": proto.MaxUploadBytes + 1, "dest": proto.UploadDestAttachment})
	if _, err := n.uploadCall("file.upload.begin", raw); err == nil {
		t.Fatal("over the limit was accepted")
	}
}
