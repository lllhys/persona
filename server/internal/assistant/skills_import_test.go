package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/rpc"
)

type skillImportHub struct {
	Hub
	calls []DeviceCall
	err   error
}

func (h *skillImportHub) DeviceCall(ctx context.Context, p DeviceCall) (json.RawMessage, error) {
	if p.Method != "skills.import" {
		return h.Hub.DeviceCall(ctx, p)
	}
	h.calls = append(h.calls, p)
	if h.err != nil {
		return nil, h.err
	}
	return json.RawMessage(`{"name":"example-skill","reference":"opaque"}`), nil
}

func skillImportService(t *testing.T) (*Service, *skillImportHub) {
	t.Helper()
	f := newFakeHub().withMachine()
	f.hello.Assistant.Machines = []string{"server"}
	f.devices = []Device{{ID: "server", Home: "/home/example", Name: "Example computer"}}
	s := newTestService(t, f)
	h := &skillImportHub{Hub: f}
	setHub(s, h)
	return s, h
}

func importHTTP(s *Service, body, role string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", strings.NewReader(body))
	r.Header.Set(RoleHeader, role)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestSkillImportAuthorizedCurrentTargetAndStrictInput(t *testing.T) {
	s, h := skillImportService(t)
	w := importHTTP(s, `{"op":"preview","archive":"UEs="}`, "")
	if w.Code != http.StatusOK || len(h.calls) != 1 {
		t.Fatalf("preview: %d %s, calls %d", w.Code, w.Body, len(h.calls))
	}
	var out struct {
		Environment string `json:"environment"`
		Machine     string `json:"machine"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Environment == "" || out.Machine != "Example computer" || h.calls[0].Device != "server" {
		t.Fatalf("route or target preview: %s %+v", w.Body, h.calls)
	}
	var p map[string]any
	_ = json.Unmarshal(h.calls[0].Params, &p)
	if p["dir"] != s.assistantRel() || p["scope"] == "" || p["cwd"] != nil || p["path"] != nil {
		t.Fatalf("target not server-derived: %+v", p)
	}
	for _, body := range []string{
		`{"op":"preview","path":"/arbitrary"}`, `{"op":"preview","machine_id":"other"}`,
		`{"op":"preview","cwd":"/arbitrary"}`, `{"op":"preview","dir":"/arbitrary"}`,
		`{"op":"preview","scope":"another"}`, `{"op":"preview"} {}`, `{"op":"install","reference":"opaque"}`,
	} {
		w := importHTTP(s, body, "")
		if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
			t.Fatalf("unsafe input: %d %s", w.Code, w.Body)
		}
	}
	if len(h.calls) != 1 {
		t.Fatal("unsafe browser target reached a node")
	}
	for _, role := range []string{"answer", "read", "extension"} {
		if w := importHTTP(s, `{"op":"preview"}`, role); w.Code != http.StatusForbidden {
			t.Fatalf("role %q: %d %s", role, w.Code, w.Body)
		}
	}
	if len(h.calls) != 1 {
		t.Fatal("unauthorized caller reached a node")
	}
}

type changeWhileReading struct {
	once func()
	io.Reader
}

func (r *changeWhileReading) Read(p []byte) (int, error) {
	if r.once != nil {
		r.once()
		r.once = nil
	}
	return r.Reader.Read(p)
}

func TestSkillImportEnvironmentChangeAndOffline(t *testing.T) {
	s, h := skillImportService(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", &changeWhileReading{
		once: func() {
			_, err := s.updateSettings(context.Background(), func(v *Settings) error { v.MachineID = "other"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			// Make the changed machine a valid current target, not an invalid-setting fallback.
			f := h.Hub.(*fakeHub)
			f.mu.Lock()
			f.hello.Assistant.Machines = append(f.hello.Assistant.Machines, "other")
			f.devices = append(f.devices, Device{ID: "other", Home: "/home/other"})
			f.mu.Unlock()
		},
		Reader: strings.NewReader(`{"op":"preview","archive":"UEs="}`),
	})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusConflict || len(h.calls) != 0 {
		t.Fatalf("changed during upload: %d %s, calls %d", w.Code, w.Body, len(h.calls))
	}
	s, h = skillImportService(t)
	h.err = apierr.ErrOffline
	w = importHTTP(s, `{"op":"preview","archive":"UEs="}`, "")
	if w.Code < 400 || !strings.Contains(w.Body.String(), "offline") {
		t.Fatalf("offline: %d %s", w.Code, w.Body)
	}
	h.err = errors.New("unknown method skills.import")
	w = importHTTP(s, `{"op":"preview","archive":"UEs="}`, "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "node_outdated") {
		t.Fatalf("old node: %d %s", w.Code, w.Body)
	}
	h.err = &rpc.Error{Code: "unknown_method", Message: "private implementation detail"}
	w = importHTTP(s, `{"op":"preview","archive":"UEs="}`, "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "node_outdated") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("coded old node: %d %s", w.Code, w.Body)
	}
	for code, status := range map[string]int{"skill_conflict": 409, "skill_zip_path": 422, "skill_import_busy": 429, "skill_import_io": 500} {
		h.err = &rpc.Error{Code: code, Message: "private filesystem detail"}
		w = importHTTP(s, `{"op":"preview","archive":"UEs="}`, "")
		if w.Code != status || !strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "private filesystem") {
			t.Fatalf("stable sanitized validation error: %d %s", w.Code, w.Body)
		}
	}
}

func TestSkillImportIsARelayedManagerRoute(t *testing.T) {
	s, _ := skillImportService(t)
	for _, routes := range [][]string{s.Routes(), assistantwire.Routes} {
		found := false
		for _, r := range routes {
			found = found || r == "POST /api/v1/skills/import"
		}
		if !found {
			t.Fatal("missing relayed manager import route")
		}
	}
}
