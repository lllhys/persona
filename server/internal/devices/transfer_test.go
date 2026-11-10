package devices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// sourceNode answers file.chunk from memory, the way node/download.go does: the revision is pinned, and changing the
// file makes later chunks at the old revision fail with file_changed.
type sourceNode struct {
	mu       sync.Mutex
	data     []byte
	rev      int
	old      bool                     // predates file.chunk
	missing  bool                     // the file is not there
	size     int64                    // reported instead of len(data) when set
	onChunk  func(offset int64) error // called before each data chunk is answered (outside mu)
	requests []proto.FileChunkRequest
}

func (s *sourceNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	if s.old || method != "file.chunk" {
		return nil, errors.New("unknown method " + method)
	}
	var p proto.FileChunkRequest
	_ = json.Unmarshal(params, &p)
	s.mu.Lock()
	s.requests = append(s.requests, p)
	hook := s.onChunk
	s.mu.Unlock()
	if hook != nil && p.Length > 0 {
		if err := hook(p.Offset); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.missing {
		return nil, &rpc.Error{Message: "no such file"}
	}
	rev := fmt.Sprintf("r%d", s.rev)
	if p.Revision != "" && p.Revision != rev {
		return nil, &rpc.Error{Code: proto.CodeFileChanged, Message: "file changed during download; reopen it to try again"}
	}
	size := int64(len(s.data))
	if s.size != 0 {
		size = s.size
	}
	out := proto.FileChunk{Path: "/home/me/" + strings.TrimPrefix(p.Path, "/home/me/"), Name: "movie.mov", Size: size, Revision: rev, Offset: p.Offset}
	if p.Length > 0 {
		out.Data = append([]byte(nil), s.data[p.Offset:p.Offset+int64(p.Length)]...)
	}
	return out, nil
}

func (s *sourceNode) change() {
	s.mu.Lock()
	s.rev++
	s.mu.Unlock()
}

// targetNode answers file.upload.* in memory, the way node/upload.go does for dest=inbox.
type targetNode struct {
	mu      sync.Mutex
	calls   []string
	data    bytes.Buffer
	aborted bool
	begins  []map[string]any
	noDest  bool              // a node from before dest=inbox that says so: "unknown upload destination"
	noEcho  bool              // a node from before destinations: opens it in its temp directory and says nothing
	onBegin func()            // before an upload begins (outside mu)
	onChunk func(n int) error // the n-th chunk (from 0), before it is written (outside mu)
	chunks  int
}

func (f *targetNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	if method == "file.upload.begin" && f.onBegin != nil {
		f.onBegin()
	}
	if method == "file.upload.chunk" && f.onChunk != nil {
		f.mu.Lock()
		n := f.chunks
		f.chunks++
		f.mu.Unlock()
		if err := f.onChunk(n); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	var p struct {
		Dest string `json:"dest"`
		Data []byte `json:"data"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "file.upload.begin":
		var all map[string]any
		_ = json.Unmarshal(params, &all)
		f.begins = append(f.begins, all)
		if f.noDest {
			return nil, errors.New(`unknown upload destination "inbox"`)
		}
		if f.noEcho {
			return map[string]any{"upload_id": "u1"}, nil
		}
		return map[string]any{"upload_id": "u1", "dest": p.Dest}, nil
	case "file.upload.chunk":
		f.data.Write(p.Data)
		return map[string]any{"received": f.data.Len()}, nil
	case "file.upload.end":
		return proto.UploadedFile{Path: "/home/me/Downloads/Persona/movie.mov", Name: "movie.mov", Size: int64(f.data.Len())}, nil
	case "file.upload.abort":
		f.aborted = true
		return map[string]any{"ok": true}, nil
	}
	return nil, errors.New("unknown method " + method)
}

func (f *targetNode) state() (calls []string, data []byte, aborted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]byte(nil), f.data.Bytes()...), f.aborted
}

// fakeMachine runs machine id of d on a node that answers with handle (node.info and the session list it answers
// itself); the returned func cuts the connection for good (the machine cannot dial it again).
func fakeMachine(t *testing.T, d *Devices, id, name string, handle func(context.Context, string, json.RawMessage) (any, error)) (*Machine, func()) {
	t.Helper()
	var mu sync.Mutex
	var conns []net.Conn
	gone := false
	m := newMachine(d, MachineConfig{ID: id, Name: name, Kind: "ssh"})
	m.dialNode = func(context.Context) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if gone {
			return nil, errors.New("unreachable")
		}
		managerSide, nodeSide := net.Pipe()
		p := rpc.New("n", nodeSide)
		p.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "node.info":
				return proto.NodeInfo{Hostname: id, OS: "linux", Arch: "amd64", Home: "/home/me", Version: "test"}, nil
			case "sessions.list":
				return map[string]any{"sessions": []proto.Session{}}, nil
			}
			if strings.HasPrefix(method, "file.") {
				return handle(ctx, method, params)
			}
			return map[string]any{"ok": true}, nil
		}
		go func() { _ = p.Serve(context.Background(), nodeSide); nodeSide.Close() }()
		conns = append(conns, managerSide, nodeSide)
		return managerSide, nil
	}
	d.addEntry(m)
	runMachine(t, m)
	eventually(t, 5*time.Second, id+" connected", m.reachable)
	cut := func() {
		mu.Lock()
		defer mu.Unlock()
		gone = true
		for _, c := range conns {
			c.Close()
		}
	}
	return m, cut
}

func transferDevices(t *testing.T, src *sourceNode, dst *targetNode) (*Devices, *Machine, *Machine, func(), http.Handler) {
	t.Helper()
	d := newTestDevices(t)
	a, _ := fakeMachine(t, d, "a", "Laptop", src.handle)
	b, cutB := fakeMachine(t, d, "b", "Desktop", dst.handle)
	switchOn(t, d, "a", "b")
	return d, a, b, cutB, d.Handler(http.NotFoundHandler())
}

// switchOn turns file transfer on for the machines (it is off until the user does).
func switchOn(t *testing.T, d *Devices, machines ...string) {
	t.Helper()
	for _, id := range machines {
		if err := d.setFileTransferOn(context.Background(), id, true); err != nil {
			t.Fatal(err)
		}
	}
}

func postTransfer(t *testing.T, mux http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/files/transfer", strings.NewReader(body)))
	return rec
}

const transferBody = `{"from":{"machine":"a","path":"Movies/movie.mov","cwd":"/home/me"},"to":{"machine":"b","dest":"inbox"}}`

// waitTransfer waits for the transfer to end and answers it as GET does.
func waitTransfer(t *testing.T, d *Devices, mux http.Handler, id string) proto.FileTransfer {
	t.Helper()
	d.transfers.mu.Lock()
	tr := d.transfers.byID[id]
	d.transfers.mu.Unlock()
	if tr == nil {
		t.Fatalf("no transfer %s", id)
	}
	select {
	case <-tr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the transfer did not end")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/files/transfer/"+id, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %d %s", rec.Code, rec.Body)
	}
	var out proto.FileTransfer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func started(t *testing.T, rec *httptest.ResponseRecorder) proto.FileTransfer {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST %d %s", rec.Code, rec.Body)
	}
	var out proto.FileTransfer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.State != proto.TransferRunning {
		t.Fatalf("started %+v", out)
	}
	return out
}

// The bytes go from A's file into B's receive folder chunk by chunk, every chunk asked at the revision A first gave;
// the transfer ends done with B's path.
func TestTransferRelaysAFileIntoTheTargetsInbox(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), proto.FileDownloadChunkBytes/16*2+100) // two chunks and a bit
	src := &sourceNode{data: data}
	dst := &targetNode{}
	d, _, b, _, mux := transferDevices(t, src, dst)
	b.cfg.InboxDir = "~/Received"
	tr := started(t, postTransfer(t, mux, transferBody))
	if tr.From != "a" || tr.To != "b" || tr.Name != "movie.mov" || tr.Total != int64(len(data)) {
		t.Fatalf("started %+v", tr)
	}
	end := waitTransfer(t, d, mux, tr.ID)
	if end.State != proto.TransferDone || end.Bytes != int64(len(data)) || end.ResultPath != "/home/me/Downloads/Persona/movie.mov" || end.Error != "" {
		t.Fatalf("end %+v", end)
	}
	calls, got, aborted := dst.state()
	if !bytes.Equal(got, data) || aborted {
		t.Fatalf("got %d bytes, aborted %v, calls %v", len(got), aborted, calls)
	}
	if want := []string{"file.upload.begin", "file.upload.chunk", "file.upload.chunk", "file.upload.chunk", "file.upload.end"}; strings.Join(calls, " ") != strings.Join(want, " ") {
		t.Fatalf("calls %v", calls)
	}
	begin := dst.begins[0]
	if begin["dest"] != proto.UploadDestInbox || begin["dir"] != "~/Received" || begin["name"] != "movie.mov" || begin["size"] != float64(len(data)) {
		t.Fatalf("begin %v", begin)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	for i, r := range src.requests {
		if i > 0 && (r.Revision != "r0" || r.Path != "/home/me/Movies/movie.mov" || r.Cwd != "/home/me" || r.Length > proto.FileDownloadChunkBytes) {
			t.Fatalf("request %d %+v", i, r)
		}
	}
}

// An empty file is still a file: begin and end, no chunk.
func TestTransferEmptyFile(t *testing.T) {
	dst := &targetNode{}
	d, _, _, _, mux := transferDevices(t, &sourceNode{}, dst)
	end := waitTransfer(t, d, mux, started(t, postTransfer(t, mux, transferBody)).ID)
	if calls, _, _ := dst.state(); end.State != proto.TransferDone || strings.Join(calls, " ") != "file.upload.begin file.upload.end" {
		t.Fatalf("%+v %v", end, calls)
	}
}

// DELETE stops a running transfer: the target's upload is aborted, the state is cancelled, no more chunks go.
func TestTransferCancel(t *testing.T) {
	data := bytes.Repeat([]byte{7}, proto.FileDownloadChunkBytes*4)
	release := make(chan struct{})
	src := &sourceNode{data: data}
	src.onChunk = func(off int64) error {
		if off >= proto.FileDownloadChunkBytes { // the second chunk waits until the test cancels
			<-release
		}
		return nil
	}
	dst := &targetNode{}
	d, _, _, _, mux := transferDevices(t, src, dst)
	tr := started(t, postTransfer(t, mux, transferBody))
	eventually(t, 5*time.Second, "the first transfer chunk", func() bool {
		got, _ := d.getTransfer(tr.ID)
		return got.Bytes > 0
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/v1/files/transfer/"+tr.ID, nil))
	close(release)
	var out proto.FileTransfer
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.State != proto.TransferCancelled || out.Bytes != proto.FileDownloadChunkBytes {
		t.Fatalf("DELETE %d %s", rec.Code, rec.Body)
	}
	calls, _, aborted := dst.state()
	if !aborted || strings.Contains(strings.Join(calls, " "), "file.upload.end") {
		t.Fatalf("aborted %v, calls %v", aborted, calls)
	}
	// Over is over: a second DELETE changes nothing, and GET still reads it.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/v1/files/transfer/"+tr.ID, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"cancelled"`) {
		t.Fatalf("second DELETE %d %s", rec.Code, rec.Body)
	}
}

// A done transfer is not cancelled by DELETE.
func TestTransferCancelAfterDone(t *testing.T) {
	d, _, _, _, mux := transferDevices(t, &sourceNode{data: []byte("hi")}, &targetNode{})
	tr := started(t, postTransfer(t, mux, transferBody))
	waitTransfer(t, d, mux, tr.ID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/v1/files/transfer/"+tr.ID, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"done"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// The source file changes while it is sent: the next chunk at the pinned revision is refused, the transfer fails
// with file_changed and the target's part is aborted, never ended with a mix of two versions.
func TestTransferSourceChangesMidway(t *testing.T) {
	data := bytes.Repeat([]byte{1}, proto.FileDownloadChunkBytes*3)
	src := &sourceNode{data: data}
	src.onChunk = func(off int64) error {
		if off == proto.FileDownloadChunkBytes {
			src.change()
		}
		return nil
	}
	dst := &targetNode{}
	d, _, _, _, mux := transferDevices(t, src, dst)
	end := waitTransfer(t, d, mux, started(t, postTransfer(t, mux, transferBody)).ID)
	if end.State != proto.TransferFailed || end.Code != proto.CodeFileChanged || !strings.Contains(end.Error, "Laptop") {
		t.Fatalf("end %+v", end)
	}
	calls, _, aborted := dst.state()
	if !aborted || strings.Contains(strings.Join(calls, " "), "file.upload.end") {
		t.Fatalf("aborted %v, calls %v", aborted, calls)
	}
}

// A source whose chunk does not match what was asked (another size, a short read) fails the transfer too.
func TestTransferSourceAnswersAnotherFile(t *testing.T) {
	src := &sourceNode{data: bytes.Repeat([]byte{1}, proto.FileDownloadChunkBytes*2)}
	src.onChunk = func(int64) error {
		src.mu.Lock()
		src.data = src.data[:len(src.data)-1] // the file shrank, its revision did not say so
		src.mu.Unlock()
		return nil
	}
	dst := &targetNode{}
	d, _, _, _, mux := transferDevices(t, src, dst)
	end := waitTransfer(t, d, mux, started(t, postTransfer(t, mux, transferBody)).ID)
	if end.State != proto.TransferFailed || end.Code != proto.CodeFileChanged {
		t.Fatalf("end %+v", end)
	}
	if _, _, aborted := dst.state(); !aborted {
		t.Fatal("not aborted")
	}
}

// The target goes away mid-way (its connection drops): the transfer fails naming it, and the source is asked no more.
func TestTransferTargetDisconnects(t *testing.T) {
	data := bytes.Repeat([]byte{2}, proto.FileDownloadChunkBytes*5)
	src := &sourceNode{data: data}
	dst := &targetNode{}
	var cutB func()
	dst.onChunk = func(n int) error {
		if n == 1 {
			cutB()
			return errors.New("gone")
		}
		return nil
	}
	d := newTestDevices(t)
	fakeMachine(t, d, "a", "Laptop", src.handle)
	_, cutB = fakeMachine(t, d, "b", "Desktop", dst.handle)
	switchOn(t, d, "a", "b")
	mux := d.Handler(http.NotFoundHandler())
	end := waitTransfer(t, d, mux, started(t, postTransfer(t, mux, transferBody)).ID)
	if end.State != proto.TransferFailed || !strings.Contains(end.Error, "Desktop") || end.Bytes != proto.FileDownloadChunkBytes {
		t.Fatalf("end %+v", end)
	}
	src.mu.Lock()
	asked := len(src.requests)
	src.mu.Unlock()
	if asked > 5 { // the metadata, the two chunks sent, one waiting and one being read
		t.Fatalf("the source was asked %d times", asked)
	}
}

// The target refuses a chunk (its disk is full): failed, aborted, the rpc code kept.
func TestTransferTargetRefusesAChunk(t *testing.T) {
	dst := &targetNode{}
	dst.onChunk = func(int) error { return &rpc.Error{Code: "no_space", Message: "no space left on device"} }
	d, _, _, _, mux := transferDevices(t, &sourceNode{data: []byte("hello")}, dst)
	end := waitTransfer(t, d, mux, started(t, postTransfer(t, mux, transferBody)).ID)
	if end.State != proto.TransferFailed || end.Code != "no_space" || !strings.Contains(end.Error, "no space left") {
		t.Fatalf("end %+v", end)
	}
	if _, _, aborted := dst.state(); !aborted {
		t.Fatal("not aborted")
	}
}

// Refusals before anything starts: bad bodies, unknown machines, a missing source file, a file over the limit, old
// nodes on either side (one that ignores dest has its upload aborted), and devices that may not start one.
func TestTransferRefusals(t *testing.T) {
	_, _, _, _, mux := transferDevices(t, &sourceNode{data: []byte("x")}, &targetNode{})
	for body, code := range map[string]int{
		`{`: http.StatusBadRequest,
		`{"from":{"machine":"a"},"to":{"machine":"b"}}`:                            http.StatusBadRequest,
		`{"from":{"path":"x"},"to":{"machine":"b"}}`:                               http.StatusBadRequest,
		`{"from":{"machine":"a","path":"x"},"to":{"machine":"b","dest":"dir"}}`:    http.StatusBadRequest,
		`{"from":{"machine":"zz","path":"x"},"to":{"machine":"b"}}`:                http.StatusNotFound,
		`{"from":{"machine":"a","path":"x"},"to":{"machine":"zz","dest":"inbox"}}`: http.StatusNotFound,
	} {
		if rec := postTransfer(t, mux, body); rec.Code != code {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, code)
		}
	}

	// to.dest may be left out: the inbox is the only one.
	if rec := postTransfer(t, mux, `{"from":{"machine":"a","path":"x"},"to":{"machine":"b"}}`); rec.Code != http.StatusAccepted {
		t.Fatalf("no dest: %d %s", rec.Code, rec.Body)
	}

	// The source's node predates file.chunk.
	_, _, _, _, mux = transferDevices(t, &sourceNode{old: true}, &targetNode{})
	if rec := postTransfer(t, mux, transferBody); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"node_outdated"`) || !strings.Contains(rec.Body.String(), `"machine":"a"`) {
		t.Fatalf("old source: %d %s", rec.Code, rec.Body)
	}

	// The source cannot read it: its error, nothing opened on the target.
	dst := &targetNode{}
	_, _, _, _, mux = transferDevices(t, &sourceNode{missing: true}, dst)
	if rec := postTransfer(t, mux, transferBody); rec.Code == http.StatusAccepted || len(dst.begins) != 0 {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body)
	}

	// Over the inbox's limit: refused before the target hears of it.
	dst = &targetNode{}
	_, _, _, _, mux = transferDevices(t, &sourceNode{size: proto.MaxInboxUploadBytes + 1}, dst)
	if rec := postTransfer(t, mux, transferBody); rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"too_large"`) || len(dst.begins) != 0 {
		t.Fatalf("too large: %d %s", rec.Code, rec.Body)
	}

	// Old targets: one that says so, one that ignores dest (aborted, nothing sent).
	for _, old := range []*targetNode{{noDest: true}, {noEcho: true}} {
		_, _, _, _, mux := transferDevices(t, &sourceNode{data: []byte("x")}, old)
		rec := postTransfer(t, mux, transferBody)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"node_outdated"`) || !strings.Contains(rec.Body.String(), `"machine":"b"`) {
			t.Fatalf("old target: %d %s", rec.Code, rec.Body)
		}
		if _, data, aborted := old.state(); len(data) != 0 || old.noEcho && !aborted {
			t.Fatalf("old target: %d bytes, aborted %v", len(data), aborted)
		}
	}

	// Only full devices start or cancel one: the role the hub passes on is checked again.
	_, _, _, _, mux = transferDevices(t, &sourceNode{data: []byte("x")}, &targetNode{})
	for _, role := range []string{"answer", "extension", "link"} {
		for _, req := range []*http.Request{
			httptest.NewRequest("POST", "/api/v1/files/transfer", strings.NewReader(transferBody)),
			httptest.NewRequest("DELETE", "/api/v1/files/transfer/x", nil),
		} {
			req.Header.Set(assistantwire.RoleHeader, role)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: %d", role, req.Method, rec.Code)
			}
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/files/transfer/nope", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown_transfer") {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body)
	}
}

// The apps hear of a transfer through the hub (ws.broadcast file_transfer): at the start and the end, and (throttled)
// in between.
func TestTransferEvents(t *testing.T) {
	d, _, _, _, mux := transferDevices(t, &sourceNode{data: []byte("hello")}, &targetNode{})
	tr := started(t, postTransfer(t, mux, transferBody))
	waitTransfer(t, d, mux, tr.ID)
	var states []string
	for _, raw := range d.opts.Hub.(*fakeHost).broadcastsOf(proto.FileTransferEvent) {
		var ev struct {
			Transfer proto.FileTransfer `json:"transfer"`
		}
		_ = json.Unmarshal(raw, &ev)
		if ev.Transfer.ID != tr.ID {
			t.Fatalf("event %s", raw)
		}
		states = append(states, ev.Transfer.State)
	}
	if len(states) < 2 || states[0] != proto.TransferRunning || states[len(states)-1] != proto.TransferDone {
		t.Fatalf("states %v", states)
	}
}

// At most maxRunningTransfers run at once; a finished transfer is forgotten an hour after it ended.
func TestTransferLimitAndForgetting(t *testing.T) {
	release := make(chan struct{})
	src := &sourceNode{data: []byte("hello")}
	src.onChunk = func(int64) error { <-release; return nil }
	d, _, _, _, mux := transferDevices(t, src, &targetNode{})
	var ids []string
	for range maxRunningTransfers {
		ids = append(ids, started(t, postTransfer(t, mux, transferBody)).ID)
	}
	if rec := postTransfer(t, mux, transferBody); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"code":"busy"`) {
		t.Fatalf("over the limit: %d %s", rec.Code, rec.Body)
	}
	close(release)
	for _, id := range ids {
		waitTransfer(t, d, mux, id)
	}
	d.pruneTransfers(time.Now().Add(transferKeep + time.Minute))
	if _, ok := d.getTransfer(ids[0]); ok {
		t.Fatal("not forgotten")
	}
}

// Starts reserve capacity before the target's begin RPC: a burst cannot all pass runningTransfers while none is
// listed yet (#3599). Once those transfers end, their slots can be used again.
func TestTransferConcurrentStartsReserveSlots(t *testing.T) {
	src := &sourceNode{data: []byte("hello")}
	dst := &targetNode{}
	arrived := make(chan struct{})
	release := make(chan struct{})
	dst.onBegin = func() {
		select {
		case arrived <- struct{}{}:
		case <-release:
		}
		<-release
	}
	d := newTestDevices(t)
	_, _ = fakeMachine(t, d, "a", "Laptop", src.handle)
	switchOn(t, d, "a")
	const starts = maxRunningTransfers + 4
	bodies := make([]string, starts)
	for i := range starts {
		id := fmt.Sprintf("b%d", i)
		_, _ = fakeMachine(t, d, id, id, dst.handle)
		switchOn(t, d, id)
		bodies[i] = fmt.Sprintf(`{"from":{"machine":"a","path":"Movies/movie.mov","cwd":"/home/me"},"to":{"machine":%q,"dest":"inbox"}}`, id)
	}
	mux := d.Handler(http.NotFoundHandler())
	results := make(chan *httptest.ResponseRecorder, starts)
	var wg sync.WaitGroup
	var unblock sync.Once
	defer func() {
		unblock.Do(func() { close(release) })
		wg.Wait()
	}()
	for _, body := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- postTransfer(t, mux, body)
		}()
	}
	var early []*httptest.ResponseRecorder
	begins := 0
	for {
		if (begins == maxRunningTransfers && len(early) == starts-maxRunningTransfers) || begins == starts {
			break
		}
		select {
		case <-arrived:
			begins++
		case rec := <-results:
			early = append(early, rec)
		case <-time.After(10 * time.Second):
			t.Fatal("the starts did not reach the target or the limit")
		}
	}
	if begins != maxRunningTransfers || len(early) != starts-maxRunningTransfers {
		t.Fatalf("target begins %d, early answers %d, want %d and %d", begins, len(early), maxRunningTransfers, starts-maxRunningTransfers)
	}
	for _, rec := range early {
		if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"code":"busy"`) {
			t.Fatalf("over the limit: %d %s", rec.Code, rec.Body)
		}
	}

	unblock.Do(func() { close(release) })
	var ids []string
	for range maxRunningTransfers {
		rec := <-results
		ids = append(ids, started(t, rec).ID)
	}
	wg.Wait()
	for _, id := range ids {
		if end := waitTransfer(t, d, mux, id); end.State != proto.TransferDone {
			t.Fatalf("end %+v", end)
		}
	}
	// Every slot was released by runTransfer, not held by a completed or refused start.
	last := started(t, postTransfer(t, mux, bodies[0]))
	if end := waitTransfer(t, d, mux, last.ID); end.State != proto.TransferDone {
		t.Fatalf("after the slots were reused: %+v", end)
	}
}
