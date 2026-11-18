package devices

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// diagNode is a node that answers machine.diagnose and machine.cleanup with what the test sets, and keeps the params
// it was asked with.
type diagNode struct {
	mu     sync.Mutex
	calls  []string
	params []json.RawMessage
	answer any
	fail   error
}

func diagMachine(t *testing.T, h *Devices, id string, caps []string) (*Machine, *diagNode) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	n := &diagNode{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		switch method {
		case proto.MethodMachineDiagnose, proto.MethodMachineCleanup:
			n.calls = append(n.calls, method)
			n.params = append(n.params, params)
			if n.fail != nil {
				return nil, n.fail
			}
			return n.answer, nil
		case "node.configure":
			return map[string]any{}, nil
		}
		return nil, &rpc.Error{Message: "unknown method " + method}
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online = hubSide, true
	m.info = proto.NodeInfo{OS: "linux", Version: "test", Protocol: nodeproto.Version, Capabilities: caps}
	h.addEntry(m)
	return m, n
}

func TestDiagnosisPassesTheNodesAnswerThrough(t *testing.T) {
	h := newTestDevices(t)
	_, n := diagMachine(t, h, "box", nodeproto.Announced)
	cpu := 12.5
	tools := []proto.DiagnosedTool{{ID: "docker", Version: "27.3.1"}}
	n.answer = proto.Diagnosis{
		At:        1794000000,
		Processes: &proto.ProcessReport{MemTotal: 8 << 30, Count: 3, Groups: []proto.ProcessGroup{{Name: "claude", Kind: proto.ProcessKindAgent, Count: 2, RSS: 1 << 30, CPU: &cpu}}},
		Disk:      &proto.DiskReport{Volumes: []proto.DiskVolume{{Mount: "/", Used: 1, Total: 2}}, Folders: []proto.DiskFolder{{Path: "~/code", Bytes: 9}}, Caches: []proto.DiskCache{{ID: "go-build", Path: "~/.cache/go-build", Bytes: 5}}},
		Tools:     &tools,
	}
	mux := h.Handler(http.NotFoundHandler())

	code, out := clipCall(t, mux, "GET", "/api/v1/machines/box/diagnosis?sections=processes,disk,tools,disk", "", "full")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
	if out["machine_id"] != "box" || out["at"] != float64(1794000000) {
		t.Fatalf("answer = %v", out)
	}
	groups := out["processes"].(map[string]any)["groups"].([]any)
	if g := groups[0].(map[string]any); g["name"] != "claude" || g["kind"] != "agent" || g["cpu"] != 12.5 {
		t.Fatalf("group = %v", g)
	}
	if c := out["disk"].(map[string]any)["caches"].([]any)[0].(map[string]any); c["id"] != "go-build" || c["in_use"] != false {
		t.Fatalf("cache = %v", c)
	}
	if tl := out["tools"].([]any)[0].(map[string]any); tl["id"] != "docker" || tl["version"] != "27.3.1" {
		t.Fatalf("tool = %v", tl)
	}
	var p proto.DiagnoseParams
	_ = json.Unmarshal(n.params[0], &p)
	if !slices.Equal(p.Sections, []string{"processes", "disk", "tools"}) {
		t.Fatalf("node asked for %v", p.Sections)
	}
	// No sections: all of them, left to the node.
	if code, _ := clipCall(t, mux, "GET", "/api/v1/machines/box/diagnosis", "", ""); code != http.StatusOK {
		t.Fatalf("all sections: %d", code)
	}
	p = proto.DiagnoseParams{}
	_ = json.Unmarshal(n.params[1], &p)
	if len(p.Sections) != 0 {
		t.Fatalf("node asked for %v", p.Sections)
	}
}

func TestCleanupPassesTheNodesAnswerThrough(t *testing.T) {
	h := newTestDevices(t)
	_, n := diagMachine(t, h, "box", nodeproto.Announced)
	n.answer = proto.CleanupResult{FreedBytes: 1234, AvailableBefore: 1, AvailableAfter: 1235, Items: []proto.CleanupItem{
		{ID: "go-build", Bytes: 1200, Status: proto.CleanupCleaned},
		{ID: "npm-cache", Bytes: 50, Status: proto.CleanupInUse},
	}}
	mux := h.Handler(http.NotFoundHandler())
	code, out := clipCall(t, mux, "POST", "/api/v1/machines/box/cleanup", `{"ids":["go-build","npm-cache"]}`, "full")
	if code != http.StatusOK || out["freed_bytes"] != float64(1234) || len(out["items"].([]any)) != 2 {
		t.Fatalf("%d %v", code, out)
	}
	var p proto.CleanupParams
	_ = json.Unmarshal(n.params[0], &p)
	if !slices.Equal(p.IDs, []string{"go-build", "npm-cache"}) {
		t.Fatalf("node asked for %v", p.IDs)
	}
}

// What the manager refuses itself never reaches the node: a malformed request, a node too old, an offline computer,
// an answer-only device.
func TestDiagnosisRefusals(t *testing.T) {
	h := newTestDevices(t)
	_, n := diagMachine(t, h, "box", nodeproto.Announced)
	_, oldN := diagMachine(t, h, "old", slices.DeleteFunc(slices.Clone(nodeproto.Announced), func(c string) bool { return c == nodeproto.MachineDiagnose }))
	off, _ := diagMachine(t, h, "off", nodeproto.Announced)
	off.online = false
	mux := h.Handler(http.NotFoundHandler())

	for _, c := range []struct {
		method, path, body, role string
		status                   int
		code                     string
	}{
		{"GET", "/api/v1/machines/box/diagnosis?sections=processes,argv", "", "full", http.StatusBadRequest, "bad_request"},
		{"GET", "/api/v1/machines/box/diagnosis?sections=", "", "full", http.StatusOK, ""},
		{"POST", "/api/v1/machines/box/cleanup", `{"ids":[]}`, "full", http.StatusBadRequest, "bad_request"},
		{"POST", "/api/v1/machines/box/cleanup", `{"ids":["go-build","/home"]}`, "full", http.StatusBadRequest, "bad_request"},
		{"POST", "/api/v1/machines/box/cleanup", `{"paths":["/"]}`, "full", http.StatusBadRequest, "bad_request"},
		{"POST", "/api/v1/machines/box/cleanup", `not json`, "full", http.StatusBadRequest, "bad_request"},
		{"GET", "/api/v1/machines/old/diagnosis", "", "full", http.StatusConflict, proto.CodeNodeOutdated},
		{"POST", "/api/v1/machines/old/cleanup", `{"ids":["go-build"]}`, "full", http.StatusConflict, proto.CodeNodeOutdated},
		{"GET", "/api/v1/machines/off/diagnosis", "", "full", http.StatusServiceUnavailable, devicewire.CodeMachineOffline},
		{"POST", "/api/v1/machines/off/cleanup", `{"ids":["go-build"]}`, "full", http.StatusServiceUnavailable, devicewire.CodeMachineOffline},
		{"GET", "/api/v1/machines/box/diagnosis", "", "answer", http.StatusForbidden, proto.CodeReadOnly},
		{"POST", "/api/v1/machines/box/cleanup", `{"ids":["go-build"]}`, "extension", http.StatusForbidden, proto.CodeReadOnly},
		{"GET", "/api/v1/machines/nowhere/diagnosis", "", "full", http.StatusNotFound, "not_found"},
	} {
		n.answer = proto.Diagnosis{}
		code, out := clipCall(t, mux, c.method, c.path, c.body, c.role)
		if code != c.status || c.code != "" && out["code"] != c.code {
			t.Errorf("%s %s (%s): %d %v, want %d %s", c.method, c.path, c.role, code, out, c.status, c.code)
		}
		if c.code != "" {
			if msg, _ := out["error"].(string); msg == "" {
				t.Errorf("%s %s: no error text", c.method, c.path)
			}
		}
	}
	if len(n.calls) != 1 || len(oldN.calls) != 0 {
		t.Fatalf("the node was asked: %v / %v", n.calls, oldN.calls)
	}
}

// A node that answers unknown method (it announced the capability but predates it, or is a stand-in) reads as too old;
// a coded deadline as 504; anything else as 502 with plain words.
func TestDiagnosisNodeErrors(t *testing.T) {
	h := newTestDevices(t)
	_, n := diagMachine(t, h, "box", nodeproto.Announced)
	mux := h.Handler(http.NotFoundHandler())
	for _, c := range []struct {
		fail   error
		status int
		code   string
	}{
		{&rpc.Error{Message: "unknown method machine.diagnose"}, http.StatusConflict, proto.CodeNodeOutdated},
		{&rpc.Error{Message: context.DeadlineExceeded.Error()}, http.StatusGatewayTimeout, devicewire.CodeDeadline},
		{&rpc.Error{Message: "disk on fire"}, http.StatusBadGateway, "diagnosis_failed"},
	} {
		n.mu.Lock()
		n.fail = c.fail
		n.mu.Unlock()
		code, out := clipCall(t, mux, "GET", "/api/v1/machines/box/diagnosis", "", "full")
		if code != c.status || out["code"] != c.code {
			t.Errorf("%v: %d %v", c.fail, code, out)
		}
		if msg, _ := out["error"].(string); msg == "" || msg == "disk on fire" {
			t.Errorf("%v: error %q", c.fail, msg)
		}
	}
}
