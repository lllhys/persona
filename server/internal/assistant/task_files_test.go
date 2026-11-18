package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestTaskFilePaths(t *testing.T) {
	reply := "做完了。\nfile: /tmp/report.md\n文件：`~/manual.pdf`\nfile: relative.txt\nfile: /tmp/report.md\nfile: \"/tmp/chart.png\"\n" +
		"<closeout status=\"done\" merged=\"n/a\" deployed=\"n/a\">\nartifact: file /tmp/extra.zip\n</closeout>"
	got := taskFilePaths(reply)
	want := []string{"/tmp/report.md", "~/manual.pdf", "/tmp/chart.png", "/tmp/extra.zip"}
	if len(got) != len(want) {
		t.Fatalf("paths: %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths: %q, want %q", got, want)
		}
	}
}

func TestTaskFilePathsMost(t *testing.T) {
	var b strings.Builder
	for i := 0; i < taskFilesMost+3; i++ {
		b.WriteString("file: /tmp/f" + string(rune('a'+i)) + "\n")
	}
	if got := taskFilePaths(b.String()); len(got) != taskFilesMost {
		t.Fatalf("paths: %d, want %d", len(got), taskFilesMost)
	}
}

// taskFileFake is what the two nodes answer: files on "box" (the Agent's computer), uploads on "server" (the
// conversation's).
type taskFileFake struct {
	mu      sync.Mutex
	on      map[string][]byte
	uploads []map[string]any
	upload  func(dest string) (string, error)
}

func newTaskFileFake(f *fakeHub, on map[string][]byte) *taskFileFake {
	ff := &taskFileFake{on: on}
	f.answerNode("file.chunk", func(machine string, raw json.RawMessage) (any, error) {
		var req proto.FileChunkRequest
		_ = json.Unmarshal(raw, &req)
		data, ok := ff.on[req.Path]
		if machine != "box" || !ok {
			return nil, errors.New("no such file")
		}
		out := proto.FileChunk{Path: req.Path, Name: req.Path[strings.LastIndex(req.Path, "/")+1:], Size: int64(len(data)), Revision: "r1"}
		if req.Length > 0 {
			out.Data = data[req.Offset:min(int64(len(data)), req.Offset+int64(req.Length))]
		}
		return out, nil
	})
	f.answerNode("file.upload.begin", func(machine string, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		p["machine"] = machine
		ff.mu.Lock()
		defer ff.mu.Unlock()
		ff.uploads = append(ff.uploads, p)
		if ff.upload != nil {
			if _, err := ff.upload(p["dest"].(string)); err != nil {
				return nil, err
			}
		}
		return map[string]any{"upload_id": "u" + p["name"].(string), "dest": p["dest"]}, nil
	})
	f.answerNode("file.upload.chunk", func(string, json.RawMessage) (any, error) { return map[string]any{}, nil })
	f.answerNode("file.upload.end", func(string, json.RawMessage) (any, error) {
		return proto.UploadedFile{Path: "/home/me/uploads/files/2026-11-18/ab12-file"}, nil
	})
	return ff
}

func TestDeliverTaskFilesCopies(t *testing.T) {
	f := newFakeHub().withMachine()
	f.sessions["box"] = []proto.Session{{SessionID: "s1", MachineID: "box"}}
	ff := newTaskFileFake(f, map[string][]byte{"/home/me/manual.md": []byte("# manual\n"), "/home/me/plot.png": {0x89, 'P', 'N', 'G'}})
	f.answerNode("file.upload.end", func(machine string, raw json.RawMessage) (any, error) {
		var p struct {
			UploadID string `json:"upload_id"`
		}
		_ = json.Unmarshal(raw, &p)
		return proto.UploadedFile{Path: "/home/me/.persona/uploads/files/2026-11-18/ab12-" + strings.TrimPrefix(p.UploadID, "u")}, nil
	})
	s := newStorelessService(t, f)
	todo := Todo{ID: "todo_1", Title: "取回手册", Origin: "conv1", Agents: []TodoAgent{{MachineID: "box", SessionID: "s1"}}}
	putTodo(s, todo)

	files := s.deliverTaskFiles(context.Background(), todo, "box", "server", "手册在这里：\nfile: /home/me/manual.md\nfile: /home/me/plot.png\n")
	if len(files) != 2 {
		t.Fatalf("files: %+v", files)
	}
	for _, x := range files {
		if x.Local == "" || x.Error != "" || !strings.HasPrefix(x.Local, "/home/me/.persona/uploads/files/") {
			t.Fatalf("file: %+v", x)
		}
	}
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if len(ff.uploads) != 2 {
		t.Fatalf("uploads: %+v", ff.uploads)
	}
	for _, u := range ff.uploads {
		if u["dest"] != proto.UploadDestFile || u["machine"] != "server" {
			t.Fatalf("upload: %+v", u)
		}
	}
	// A second turn names the same files: nothing is copied again.
	if again := s.deliverTaskFiles(context.Background(), todo, "box", "server", "file: /home/me/manual.md"); len(again) != 2 {
		t.Fatalf("files after a second turn: %+v", again)
	}
	if len(ff.uploads) != 2 {
		t.Fatalf("copied again: %+v", ff.uploads)
	}
}

func TestDeliverTaskFilesSameComputer(t *testing.T) {
	f := newFakeHub().withMachine()
	s := newStorelessService(t, f)
	todo := Todo{ID: "todo_2", Title: "本机", Origin: "conv1"}
	files := s.deliverTaskFiles(context.Background(), todo, "server", "server", "file: /tmp/a.md\n")
	if len(files) != 1 || files[0].Local != "" || files[0].local() != "/tmp/a.md" || files[0].Error != "" {
		t.Fatalf("files: %+v", files)
	}
}

func TestDeliverTaskFilesFailure(t *testing.T) {
	f := newFakeHub().withMachine()
	f.sessions["box"] = []proto.Session{{SessionID: "s1", MachineID: "box"}}
	newTaskFileFake(f, map[string][]byte{})
	s := newStorelessService(t, f)
	todo := Todo{ID: "todo_3", Title: "取回", Origin: "conv1"}
	files := s.deliverTaskFiles(context.Background(), todo, "box", "server", "file: /home/me/gone.md\n")
	if len(files) != 1 || files[0].Error == "" || files[0].Path != "/home/me/gone.md" {
		t.Fatalf("files: %+v", files)
	}
	if !strings.Contains(files[0].String(), "在原电脑") {
		t.Fatalf("String: %q", files[0].String())
	}
}

func TestDeliverTaskFilesOldNode(t *testing.T) {
	f := newFakeHub().withMachine()
	f.sessions["box"] = []proto.Session{{SessionID: "s1", MachineID: "box"}}
	ff := newTaskFileFake(f, map[string][]byte{"/home/me/a.md": []byte("a")})
	ff.upload = func(dest string) (string, error) {
		if dest == proto.UploadDestFile {
			return "", errors.New("unknown upload destination \"file\"")
		}
		return "", nil
	}
	f.answerNode("file.upload.end", func(string, json.RawMessage) (any, error) {
		return proto.UploadedFile{Path: "/home/me/.persona/uploads/terminal/2026-11-18/ab12-a.md"}, nil
	})
	s := newStorelessService(t, f)
	todo := Todo{ID: "todo_4", Title: "旧 node", Origin: "conv1"}
	files := s.deliverTaskFiles(context.Background(), todo, "box", "server", "file: /home/me/a.md\n")
	if len(files) != 1 || files[0].Local == "" || files[0].Error != "" {
		t.Fatalf("files: %+v", files)
	}
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if len(ff.uploads) != 2 || ff.uploads[0]["dest"] != proto.UploadDestFile || ff.uploads[1]["dest"] != proto.UploadDestTerminal {
		t.Fatalf("uploads: %+v", ff.uploads)
	}
}

func TestTaskFileLineInNotice(t *testing.T) {
	got := taskFileLines([]taskFile{{Name: "manual.md", Path: "/home/me/manual.md", Local: "/x/uploads/files/2026-11-18/ab12-manual.md"}})
	if len(got) != 1 || !strings.Contains(got[0], "manual.md：/x/uploads/files/") {
		t.Fatalf("lines: %q", got)
	}
}
