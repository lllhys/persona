package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// The attending routes through the manager's HTTP (docs/assistant/meeting-minutes.md "Attending a meeting"). All
// names and texts are placeholders.

const (
	meetingRec   = "33333333-3333-4333-8333-333333333333"
	meetingOther = "44444444-4444-4444-8444-444444444444"
)

// meetingFixture is an assistant with a knowledge base and the main chat c-main on server, served over HTTP.
type meetingFixture struct {
	t     *testing.T
	s     *Service
	f     *fakeHub
	srv   *httptest.Server
	model *meetingModelFake
}

func newMeetingFixture(t *testing.T) *meetingFixture {
	t.Helper()
	f := newFakeHub().withMachine()
	// Background answers may log after a test returns: they log nowhere.
	s := kbServiceOn(t, f, newKBStore(), t.TempDir(), func(string, ...any) {})
	setSettings(t, s, func(v *Settings) { v.Main = "c-main" })
	model := &meetingModelFake{reply: `{"question":"","answer":"We kept the cap.","sources":[]}`}
	s.meetingModel = model
	srv := httptest.NewServer(s)
	m := &meetingFixture{t: t, s: s, f: f, srv: srv, model: model}
	t.Cleanup(func() {
		m.idle()
		srv.Close()
	})
	return m
}

// idle waits until no question is being answered (they write into the test's directory).
func (m *meetingFixture) idle() {
	m.t.Helper()
	waitFor(m.t, "no question being answered", func() bool {
		m.s.asks.mu.Lock()
		defer m.s.asks.mu.Unlock()
		return len(m.s.asks.running) == 0
	})
}

func (m *meetingFixture) engine() *kb.Engine { return kbEng(m.t, m.s) }

// do sends body (a string as it is, anything else as JSON) with role (none: full) and wants status; the decoded body.
func (m *meetingFixture) do(method, path, role string, body any, want int) map[string]any {
	m.t.Helper()
	var data []byte
	switch b := body.(type) {
	case nil:
	case string:
		data = []byte(b)
	default:
		data, _ = json.Marshal(b)
	}
	req, _ := http.NewRequest(method, m.srv.URL+path, bytes.NewReader(data))
	if role != "" {
		req.Header.Set(RoleHeader, role)
	}
	req.Header.Set("Accept-Language", "ja")
	resp, err := m.srv.Client().Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		m.t.Fatalf("%s %s: %d %s, want %d", method, path, resp.StatusCode, raw, want)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		m.t.Fatalf("%s %s: %s", method, path, raw)
	}
	return out
}

// upload PUTs recording id: a phone meeting started an hour ago, with fields over the defaults.
func (m *meetingFixture) upload(id string, revision int, fields map[string]any) map[string]any {
	m.t.Helper()
	body := map[string]any{"revision": revision, "started_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "device": "phone",
		"kind": "meeting", "language": "en-US", "segments": []any{}, "complete": false}
	for k, v := range fields {
		body[k] = v
	}
	return m.do("PUT", "/api/v1/recordings/"+id, "", body, http.StatusOK)
}

func livePath(id string) string { return "/api/v1/recordings/" + id + "/live" }

func wantCode(t *testing.T, body map[string]any, code string) {
	t.Helper()
	if body["code"] != code {
		t.Fatalf("code %v, want %s: %v", body["code"], code, body)
	}
}

// meetingModelFake answers every meeting.answer call with reply (or err); with a gate each call first waits for one
// value on it (or its context's end).
type meetingModelFake struct {
	mu    sync.Mutex
	calls []modelCall
	reply string
	err   error
	gate  chan struct{}
}

func (f *meetingModelFake) JSONMax(ctx context.Context, system, user string, _ int, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, modelCall{system, user})
	reply, err, gate := f.reply, f.err, f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(reply), out)
}

func (f *meetingModelFake) called() []modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// The upload says the role in effect and what was wrong with its attendance fields, never failing for them.
func TestMeetingUploadSaysTheRole(t *testing.T) {
	m := newMeetingFixture(t)
	if out := m.upload(meetingRec, 1, map[string]any{"attendance": "participant"}); out["attendance"] != "participant" || out["attendance_problem"] != nil {
		t.Fatalf("participant: %v", out)
	}
	if out := m.upload(meetingRec, 2, map[string]any{"attendance": "chair"}); out["attendance"] != "off" || out["attendance_problem"] != "unknown_role" ||
		out["status"] != "stored" {
		t.Fatalf("unknown role: %v", out)
	}
	if out := m.upload(meetingOther, 1, map[string]any{"kind": "note", "attendance": "listener"}); out["attendance"] != "off" || out["attendance_problem"] != "not_meeting" {
		t.Fatalf("a note: %v", out)
	}
	if out := m.upload(meetingRec, 3, map[string]any{"attendance": "listener", "attending_session": map[string]any{"machine_id": "server"}}); out["attendance"] != "off" ||
		out["attendance_problem"] != "bad_session" {
		t.Fatalf("half a session: %v", out)
	}
	if out := m.upload(meetingRec, 4, nil); out["attendance"] != "off" || out["attendance_problem"] != nil {
		t.Fatalf("no fields: %v", out)
	}
}

// Live sentences: appended by position, a replay stores nothing twice, a gap says where to send from, and the
// refusals come in the contract's order with its codes.
func TestMeetingLiveHTTP(t *testing.T) {
	m := newMeetingFixture(t)
	m.do("POST", livePath(meetingRec), "", `{"from":0,"segments":[]}`, http.StatusNotFound)
	wantCode(t, m.do("POST", livePath(meetingRec), "", `{"from":0,"segments":[]}`, http.StatusNotFound), codeRecordingNotFound)
	wantCode(t, m.do("GET", livePath(meetingRec), "", nil, http.StatusNotFound), codeRecordingNotFound)
	m.upload(meetingRec, 1, map[string]any{"attendance": "participant"})

	two := `{"from":0,"segments":[{"start":1,"end":3,"text":"Let's keep the cap."},{"start":-2,"end":-5,"text":"Bo has the test."}]}`
	out := m.do("POST", livePath(meetingRec), "", two, http.StatusOK)
	session, _ := out["attending_session"].(map[string]any)
	if out["next"] != float64(2) || out["attendance"] != "participant" || out["fed"] != float64(0) || out["problem"] != nil ||
		session["machine_id"] != "server" || session["session_id"] != "c-main" || session["main"] != true || out["asks"] != nil {
		t.Fatalf("append: %v", out)
	}
	if out := m.do("POST", livePath(meetingRec), "", two, http.StatusOK); out["next"] != float64(2) {
		t.Fatalf("replay: %v", out)
	}
	l, _, _ := m.engine().LiveRead(meetingRec)
	if len(l.Segments) != 2 || l.Segments[1].Start != 0 || l.Segments[1].End != 0 {
		t.Fatalf("stored %+v", l.Segments)
	}
	gap := m.do("POST", livePath(meetingRec), "", `{"from":5,"segments":[{"start":9,"end":10,"text":"x"}]}`, http.StatusConflict)
	if wantCode(t, gap, codeLiveGap); gap["next"] != float64(2) || gap["error"] == "" {
		t.Fatalf("gap: %v", gap)
	}
	if out := m.do("POST", livePath(meetingRec), "", `{"from":2.0,"segments":[]}`, http.StatusOK); out["next"] != float64(2) {
		t.Fatalf("only asking for next: %v", out)
	}
	for name, body := range map[string]string{
		"no from":        `{"segments":[]}`,
		"negative from":  `{"from":-1,"segments":[]}`,
		"fractional":     `{"from":1.5,"segments":[]}`,
		"string from":    `{"from":"2","segments":[]}`,
		"a text time":    `{"from":2,"segments":[{"start":"1","end":2,"text":"x"}]}`,
		"not json":       `{"from":`,
		"segments shape": `{"from":2,"segments":{}}`,
	} {
		if out := m.do("POST", livePath(meetingRec), "", body, http.StatusBadRequest); out["code"] != "bad_request" {
			t.Errorf("%s: %v", name, out)
		}
	}
	many := make([]map[string]any, kb.MaxLiveRequest+1)
	for i := range many {
		many[i] = map[string]any{"start": i, "end": i, "text": "x"}
	}
	m.do("POST", livePath(meetingRec), "", map[string]any{"from": 2, "segments": many}, http.StatusBadRequest)
	wantCode(t, m.do("POST", livePath(meetingRec), "", strings.Repeat(" ", maxLiveBody+1), http.StatusRequestEntityTooLarge), "too_large")
	wantCode(t, m.do("POST", livePath(meetingRec), "answer", two, http.StatusForbidden), "forbidden")
	wantCode(t, m.do("GET", livePath(meetingRec), "extension", nil, http.StatusForbidden), "forbidden")

	got := m.do("GET", livePath(meetingRec), "", nil, http.StatusOK)
	if asks, ok := got["asks"].([]any); !ok || len(asks) != 0 || got["next"] != float64(2) || got["attending_session"] == nil {
		t.Fatalf("GET: %v", got)
	}

	// Another session named: it is fed, not the main chat.
	m.upload(meetingRec, 2, map[string]any{"attendance": "listener", "attending_session": map[string]any{"machine_id": "laptop", "session_id": "agent-1"}})
	session, _ = m.do("GET", livePath(meetingRec), "", nil, http.StatusOK)["attending_session"].(map[string]any)
	if session["machine_id"] != "laptop" || session["session_id"] != "agent-1" || session["main"] != nil {
		t.Fatalf("named session: %v", session)
	}

	// Off: nothing appended; GET is only the role, the counts and the questions.
	m.upload(meetingRec, 3, nil)
	wantCode(t, m.do("POST", livePath(meetingRec), "", `{"from":2,"segments":[]}`, http.StatusConflict), codeNotAttending)
	off := m.do("GET", livePath(meetingRec), "", nil, http.StatusOK)
	if len(off) != 4 || off["attendance"] != "off" || off["next"] != float64(2) || off["fed"] != float64(0) || off["asks"] == nil {
		t.Fatalf("off: %v", off)
	}
	// A bad body is 400 before 409 not_attending, in the contract's order.
	wantCode(t, m.do("POST", livePath(meetingRec), "", `{"from":-1}`, http.StatusBadRequest), "bad_request")

	// Deleted: 410 for both methods.
	m.do("DELETE", "/api/v1/recordings/"+meetingRec, "", nil, http.StatusOK)
	wantCode(t, m.do("POST", livePath(meetingRec), "", `{"from":0}`, http.StatusGone), codeRecordingDeleted)
	wantCode(t, m.do("GET", livePath(meetingRec), "", nil, http.StatusGone), codeRecordingDeleted)
}

// Full: the live log refuses more with 413 live_full, and problem says so from then on.
func TestMeetingLiveFull(t *testing.T) {
	m := newMeetingFixture(t)
	m.upload(meetingRec, 1, map[string]any{"attendance": "listener"})
	big := strings.Repeat("a", 4000)
	segs := make([]map[string]any, kb.MaxLiveRequest)
	for i := range segs {
		segs[i] = map[string]any{"start": i, "end": i, "text": big}
	}
	// 4 MiB of live text in all: 1048 sentences of 4000 bytes fit (4 192 000 bytes), one more does not.
	from := 0
	for range 5 {
		from = int(m.do("POST", livePath(meetingRec), "", map[string]any{"from": from, "segments": segs}, http.StatusOK)["next"].(float64))
	}
	from = int(m.do("POST", livePath(meetingRec), "", map[string]any{"from": from, "segments": segs[:48]}, http.StatusOK)["next"].(float64))
	wantCode(t, m.do("POST", livePath(meetingRec), "", map[string]any{"from": from, "segments": segs[:1]}, http.StatusRequestEntityTooLarge), codeLiveFull)
	if got := m.do("GET", livePath(meetingRec), "", nil, http.StatusOK); got["problem"] != codeLiveFull || got["next"] != float64(from) {
		t.Fatalf("after full: %v", got)
	}
}

// A read-only knowledge base: GET answers, POST is 403 kb_read_only, but only after 404 and 400's turn has not
// come: the recording is checked first.
func TestMeetingLiveReadOnly(t *testing.T) {
	m := newMeetingFixture(t)
	m.upload(meetingRec, 1, map[string]any{"attendance": "participant"})
	m.do("POST", livePath(meetingRec), "", `{"from":0,"segments":[{"start":1,"end":2,"text":"Hello"}]}`, http.StatusOK)
	m.f.mu.Lock()
	m.f.hello.KB = &KBSettings{ReadOnly: true}
	m.f.mu.Unlock()
	wantCode(t, m.do("POST", livePath(meetingOther), "", `{"from":0}`, http.StatusNotFound), codeRecordingNotFound)
	wantCode(t, m.do("POST", livePath(meetingRec), "", `{"from":-1}`, http.StatusForbidden), "kb_read_only")
	if got := m.do("GET", livePath(meetingRec), "", nil, http.StatusOK); got["next"] != float64(1) {
		t.Fatalf("GET on a read-only knowledge base: %v", got)
	}
}

// The knowledge base off or not open: 503 first, before anything is read.
func TestMeetingRoutesWithoutKnowledgeBase(t *testing.T) {
	m := newMeetingFixture(t)
	m.f.mu.Lock()
	m.f.hello.KB = &KBSettings{Off: true}
	m.f.mu.Unlock()
	wantCode(t, m.do("POST", livePath(meetingRec), "", strings.Repeat(" ", maxLiveBody+1), http.StatusServiceUnavailable), "kb_disabled")
	wantCode(t, m.do("POST", "/api/v1/recordings/"+meetingRec+"/ask", "", `{}`, http.StatusServiceUnavailable), "kb_disabled")
}

// meeting_attendance_default: off until changed, listener or participant, anything else 400; changed by full
// credentials only.
func TestMeetingAttendanceDefaultSetting(t *testing.T) {
	m := newMeetingFixture(t)
	if got := m.do("GET", "/api/v1/assistant", "", nil, http.StatusOK); got["meeting_attendance_default"] != "off" {
		t.Fatalf("default: %v", got["meeting_attendance_default"])
	}
	for _, bad := range []string{`"chair"`, `""`, `1`, `"Listener"`} {
		m.do("PUT", "/api/v1/assistant", "", `{"meeting_attendance_default":`+bad+`}`, http.StatusBadRequest)
	}
	m.do("PUT", "/api/v1/assistant", "answer", `{"meeting_attendance_default":"listener"}`, http.StatusForbidden)
	if got := m.do("PUT", "/api/v1/assistant", "", `{"meeting_attendance_default":"listener"}`, http.StatusOK); got["meeting_attendance_default"] != "listener" {
		t.Fatalf("PUT: %v", got["meeting_attendance_default"])
	}
	m.do("PUT", "/api/v1/assistant", "", `{"recording_days":30}`, http.StatusOK)
	if got := m.s.meetingAttendanceDefault(context.Background()); got != "listener" {
		t.Fatalf("another setting changed it: %s", got)
	}
}

// persona_settings reads and changes assistant.meeting_attendance_default: the same value asks nothing, another role
// asks on the phone with the roles named in the user's language, anything else is refused.
func TestMeetingAttendanceDefaultSettingsTool(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, newFakeHub().withMachine())
	const key = "assistant.meeting_attendance_default"
	tool := func(args string) (string, bool) {
		r := s.Tool(ctx, ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text, r.IsError
	}
	get := func() string { out, _ := tool(`{"action":"get","key":"` + key + `"}`); return out }
	set := func(value string) (string, bool) {
		return tool(`{"action":"set","key":"` + key + `","value":` + value + `}`)
	}
	if out := get(); out != "off" {
		t.Fatalf("default: %s", out)
	}
	if out, isErr := set(`"off"`); isErr || !strings.Contains(out, "unchanged") || len(s.pendingQuestions()) != 0 {
		t.Fatalf("set to what it is: %s", out)
	}
	if out, isErr := set(`"chair"`); !isErr || !strings.Contains(out, "off, listener or participant") {
		t.Fatalf("an unknown role: %s", out)
	}
	role := func(r string) string { return i18n.T(i18n.Default, "assistant-settings_tool.meeting_role_"+r) }
	card := i18n.T(i18n.Default, "assistant-settings_tool.change_meeting_attendance_default", i18n.Args{"before": role("off"), "after": role("participant")})
	go func() {
		q, ok := awaitCard(s)
		if !ok || q.Summary != card {
			t.Errorf("card %+v, want %q", q, card)
			return
		}
		allow := true
		raw, _ := json.Marshal(proto.Resolution{Allow: &allow})
		if _, err := s.Action(ctx, Action{Op: "answer", InteractionID: q.ID, Answer: raw}); err != nil {
			t.Error(err)
		}
	}()
	if out, _ := set(`"participant"`); !strings.HasPrefix(out, key+" is participant") || get() != "participant" {
		t.Fatalf("approved: %s", out)
	}
}
