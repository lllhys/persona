package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func phoneWriteHub(t *testing.T) *Hub {
	t.Helper()
	h := newTestHub(t)
	h.store = setupTestDB(t)
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "known-phone", Tools: []proto.Tool{{Name: "phone_reminders_create", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	return h
}

func startPhoneWriteTestLoop(t *testing.T, h *Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := h.startPhoneWrites(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.phoneWrites.done:
		case <-time.After(5 * time.Second):
			t.Error("phone write delivery loop did not stop")
		}
	})
}

func enqueuePhoneWriteTest(t *testing.T, h *Hub, title string) db.PhoneWrite {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"title": title, "queue_if_offline": true})
	result := h.callPhone(context.Background(), "phone_reminders_create", args)
	var row db.PhoneWrite
	if result.IsError || json.Unmarshal([]byte(textOf(result)), &row) != nil || row.ID == "" || row.State != "queued" {
		t.Fatalf("enqueue = %+v, row = %+v", result, row)
	}
	return row
}

func registerQueuePhone(t *testing.T, h *Hub, names ...string) *client {
	t.Helper()
	done := make(chan struct{})
	phone := &client{hub: h, device: "credential-phone", role: roleFull, out: make(chan []byte, 16), done: done, cancel: sync.OnceFunc(func() { close(done) })}
	h.mu.Lock()
	h.clients[phone] = struct{}{}
	h.mu.Unlock()
	var tools []proto.Tool
	for _, name := range names {
		tools = append(tools, proto.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)})
	}
	msg, _ := json.Marshal(map[string]any{"type": "register_phone", "device_id": "queue-phone", "tools": tools})
	h.onClientMessage(context.Background(), phone, msg)
	return phone
}

type queuedPhoneCall struct {
	Type   string          `json:"type"`
	CallID string          `json:"call_id"`
	Name   string          `json:"name"`
	Args   json.RawMessage `json:"args"`
}

func nextQueuedPhoneCall(t *testing.T, phone *client) queuedPhoneCall {
	t.Helper()
	select {
	case data := <-phone.out:
		var call queuedPhoneCall
		if err := json.Unmarshal(data, &call); err != nil || call.Type != "tool_call" || call.CallID == "" {
			t.Fatalf("phone frame = %s (%v)", data, err)
		}
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("queued call did not reach phone")
		return queuedPhoneCall{}
	}
}

func answerQueuedPhoneCall(h *Hub, phone *client, call queuedPhoneCall, text string, isError bool) {
	msg, _ := json.Marshal(map[string]any{"type": "tool_result", "call_id": call.CallID, "content": []proto.Content{{Type: "text", Text: text}}, "is_error": isError})
	h.onClientMessage(context.Background(), phone, msg)
}

func phoneWriteState(t *testing.T, h *Hub, id, state string) db.PhoneWrite {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var last *db.PhoneWrite
	for {
		rows, err := h.store.PhoneWrites(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ID == id {
				last = &row
				if row.State == state {
					return row
				}
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("write %s did not become %s: %+v", id, state, last)
		}
	}
}

func TestPhoneWriteQueueOptInAndManagement(t *testing.T) {
	h := phoneWriteHub(t)
	for _, args := range []string{`{"title":"default"}`, `{"title":"false","queue_if_offline":false}`} {
		result := h.callPhone(context.Background(), "phone_reminders_create", json.RawMessage(args))
		if !result.IsError || !strings.Contains(textOf(result), "not connected") {
			t.Fatalf("default offline call = %+v", result)
		}
	}
	for _, args := range []string{`{"queue_if_offline":true}`, `{"queue_if_offline":"true"}`} {
		if result := h.callPhone(context.Background(), "phone_device_info", json.RawMessage(args)); !result.IsError {
			t.Fatalf("read accepted queue option: %+v", result)
		}
	}
	first := enqueuePhoneWriteTest(t, h, "queued reminder")
	duplicate := h.callPhone(context.Background(), "phone_reminders_create", json.RawMessage(` { "queue_if_offline": true, "title": "queued reminder" } `))
	var same db.PhoneWrite
	if duplicate.IsError || json.Unmarshal([]byte(textOf(duplicate)), &same) != nil || same.ID != first.ID {
		t.Fatalf("duplicate = %+v, want %s", duplicate, first.ID)
	}
	if strings.Contains(string(first.Args), "queue_if_offline") {
		t.Fatalf("hub option persisted as phone argument: %s", first.Args)
	}
	listed := h.callPhone(context.Background(), "phone_write_queue_list", json.RawMessage(`{}`))
	var list struct {
		Items        []db.PhoneWrite `json:"items"`
		PendingCount int             `json:"pending_count"`
	}
	if listed.IsError || json.Unmarshal([]byte(textOf(listed)), &list) != nil || len(list.Items) != 1 || list.PendingCount != 1 {
		t.Fatalf("list = %+v", listed)
	}
	cancelled := h.callPhone(context.Background(), "phone_write_queue_cancel", json.RawMessage(`{"id":"`+first.ID+`"}`))
	var row db.PhoneWrite
	if cancelled.IsError || json.Unmarshal([]byte(textOf(cancelled)), &row) != nil || row.State != "cancelled" {
		t.Fatalf("cancel = %+v", cancelled)
	}
	if result := h.callPhone(context.Background(), "phone_write_queue_cancel", json.RawMessage(`{"id":"missing"}`)); !result.IsError {
		t.Fatalf("missing cancel = %+v", result)
	}
	startPhoneWriteTestLoop(t, h)
	phone := registerQueuePhone(t, h, "phone_reminders_create")
	// A new record acts as a barrier: a cancelled earlier record must never precede it.
	marker, err := h.store.EnqueuePhoneWrite(context.Background(), "after-cancel", "phone_reminders_create", json.RawMessage(`{"title":"marker"}`))
	if err != nil {
		t.Fatal(err)
	}
	h.wakePhoneWrites()
	call := nextQueuedPhoneCall(t, phone)
	if !strings.Contains(string(call.Args), "marker") {
		t.Fatalf("cancelled call was delivered: %s", call.Args)
	}
	answerQueuedPhoneCall(h, phone, call, "saved", false)
	phoneWriteState(t, h, marker.ID, "succeeded")
}

func TestPhoneWriteQueueDeliversInOrderAndRecordsResults(t *testing.T) {
	h := phoneWriteHub(t)
	rows := []db.PhoneWrite{
		enqueuePhoneWriteTest(t, h, "first"),
		enqueuePhoneWriteTest(t, h, "second"),
		enqueuePhoneWriteTest(t, h, "third"),
	}
	startPhoneWriteTestLoop(t, h)
	phone := registerQueuePhone(t, h, "phone_reminders_create")
	for i, want := range []struct{ title, text, state string }{
		{"first", "saved", "succeeded"},
		{"second", "The user declined on the phone.", "declined"},
		{"third", "Permission denied: reminders", "failed"},
	} {
		call := nextQueuedPhoneCall(t, phone)
		var args map[string]any
		if json.Unmarshal(call.Args, &args) != nil || args["title"] != want.title || call.Name != "phone_reminders_create" {
			t.Fatalf("call %d = %+v", i, call)
		}
		phoneWriteState(t, h, rows[i].ID, "delivering")
		for range 8 {
			h.wakePhoneWrites()
		}
		for j := i + 1; j < len(rows); j++ {
			phoneWriteState(t, h, rows[j].ID, "queued")
		}
		if result := h.callPhone(context.Background(), "phone_write_queue_cancel", json.RawMessage(`{"id":"`+rows[i].ID+`"}`)); !result.IsError {
			t.Fatal("in-flight call could be cancelled")
		}
		select {
		case data := <-phone.out:
			t.Fatalf("another call sent before confirmation: %s", data)
		default:
		}
		answerQueuedPhoneCall(h, phone, call, want.text, i != 0)
		row := phoneWriteState(t, h, rows[i].ID, want.state)
		var result proto.ToolResult
		if json.Unmarshal(row.Result, &result) != nil || textOf(result) != want.text || result.IsError != (i != 0) {
			t.Fatalf("saved result = %s", row.Result)
		}
	}
}

func TestPhoneWriteQueueUncertainIsNeverRetried(t *testing.T) {
	for _, reason := range []string{"timeout", "disconnect"} {
		t.Run(reason, func(t *testing.T) {
			oldWait := phoneConfirmWait
			phoneConfirmWait = 100 * time.Millisecond
			t.Cleanup(func() { phoneConfirmWait = oldWait })
			h := phoneWriteHub(t)
			row := enqueuePhoneWriteTest(t, h, "do not repeat")
			startPhoneWriteTestLoop(t, h)
			phone := registerQueuePhone(t, h, "phone_reminders_create")
			first := nextQueuedPhoneCall(t, phone)
			if reason == "disconnect" {
				h.mu.Lock()
				delete(h.clients, phone)
				h.mu.Unlock()
				phone.cancel()
			}
			phoneWriteState(t, h, row.ID, "uncertain")
			// A late answer cannot turn uncertainty into success or enqueue a retry.
			answerQueuedPhoneCall(h, phone, first, "saved late", false)
			if reason == "disconnect" {
				phone = registerQueuePhone(t, h, "phone_reminders_create")
			}
			marker, err := h.store.EnqueuePhoneWrite(context.Background(), "after-uncertain", "phone_reminders_create", json.RawMessage(`{"title":"next"}`))
			if err != nil {
				t.Fatal(err)
			}
			h.wakePhoneWrites()
			next := nextQueuedPhoneCall(t, phone)
			if !strings.Contains(string(next.Args), "next") {
				t.Fatalf("uncertain call was replayed: %s", next.Args)
			}
			answerQueuedPhoneCall(h, phone, next, "saved", false)
			phoneWriteState(t, h, marker.ID, "succeeded")
			phoneWriteState(t, h, row.ID, "uncertain")
		})
	}
}

func TestPhoneWriteQueueStartupRecoversWithoutReplay(t *testing.T) {
	h := phoneWriteHub(t)
	inflight := enqueuePhoneWriteTest(t, h, "already sent")
	pending := enqueuePhoneWriteTest(t, h, "still queued")
	if ok, err := h.store.ClaimPhoneWrite(context.Background(), inflight.ID); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	// A second Hub has no memory of the first one's queue or calls.
	reloaded := newTestHub(t)
	reloaded.store = h.store
	startPhoneWriteTestLoop(t, reloaded)
	phoneWriteState(t, reloaded, inflight.ID, "uncertain")
	phone := registerQueuePhone(t, reloaded, "phone_reminders_create")
	call := nextQueuedPhoneCall(t, phone)
	if !strings.Contains(string(call.Args), "still queued") {
		t.Fatalf("startup replayed in-flight write: %s", call.Args)
	}
	answerQueuedPhoneCall(reloaded, phone, call, "saved", false)
	phoneWriteState(t, reloaded, pending.ID, "succeeded")
}

func TestPhoneWriteQueueOnlineCallsStripHubOption(t *testing.T) {
	h := phoneWriteHub(t)
	phone := registerQueuePhone(t, h, "phone_reminders_create")
	for _, option := range []bool{false, true} {
		result := make(chan proto.ToolResult, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go func() {
			result <- h.callPhone(ctx, "phone_reminders_create", json.RawMessage(fmt.Sprintf(`{"title":"online","queue_if_offline":%t}`, option)))
		}()
		call := nextQueuedPhoneCall(t, phone)
		var args map[string]any
		if json.Unmarshal(call.Args, &args) != nil || len(args) != 1 || args["title"] != "online" {
			t.Fatalf("forwarded args = %s", call.Args)
		}
		answerQueuedPhoneCall(h, phone, call, "saved online", false)
		select {
		case got := <-result:
			if got.IsError || textOf(got) != "saved online" {
				t.Fatalf("online result = %+v", got)
			}
		case <-ctx.Done():
			t.Fatal("online result was not returned")
		}
	}
	rows, err := h.store.PhoneWrites(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("online calls were queued: %+v (%v)", rows, err)
	}
}

func TestPhoneWriteQueueAPIRequiresFullAuth(t *testing.T) {
	h := phoneWriteHub(t)
	row := enqueuePhoneWriteTest(t, h, "API reminder")
	for _, role := range []string{roleFull, roleAnswer} {
		token := "jd_queue_" + role
		if err := h.store.CreateDeviceToken(context.Background(), db.DeviceToken{ID: "queue-" + role, Name: "test phone", Role: role}, hashToken(token)); err != nil {
			t.Fatal(err)
		}
	}
	handler := h.routes()
	for _, token := range []string{"", "jd_queue_answer"} {
		for _, req := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/phone/write-queue"},
			{http.MethodPost, "/api/v1/phone/write-queue/" + row.ID + "/cancel"},
		} {
			status, _ := call(t, handler, req.method, req.path, token, "")
			if status != http.StatusUnauthorized && status != http.StatusForbidden {
				t.Fatalf("restricted token %q: %s %s = %d", token, req.method, req.path, status)
			}
		}
	}
	status, list := call(t, handler, http.MethodGet, "/api/v1/phone/write-queue?id="+row.ID, "jd_queue_full", "")
	if status != http.StatusOK || list["pending_count"] != float64(1) || len(list["items"].([]any)) != 1 {
		t.Fatalf("API list = %d %+v", status, list)
	}
	status, cancelled := call(t, handler, http.MethodPost, "/api/v1/phone/write-queue/"+row.ID+"/cancel", "jd_queue_full", "")
	if status != http.StatusOK || cancelled["state"] != "cancelled" {
		t.Fatalf("API cancel = %d %+v", status, cancelled)
	}
	status, _ = call(t, handler, http.MethodPost, "/api/v1/phone/write-queue/missing/cancel", "jd_queue_full", "")
	if status != http.StatusNotFound {
		t.Fatalf("missing API cancel = %d", status)
	}
	inflight, err := h.store.EnqueuePhoneWrite(context.Background(), "api-inflight", "phone_reminders_create", json.RawMessage(`{"title":"in flight"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.store.ClaimPhoneWrite(context.Background(), inflight.ID); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	status, _ = call(t, handler, http.MethodPost, "/api/v1/phone/write-queue/"+inflight.ID+"/cancel", "jd_queue_full", "")
	if status != http.StatusConflict {
		t.Fatalf("in-flight API cancel = %d", status)
	}
}

// A blocked queue scan must not hold the registration path or the hub's global lock.
type stalledPhoneWriteScan struct {
	Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *stalledPhoneWriteScan) QueuedPhoneWrites(ctx context.Context) ([]db.PhoneWrite, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.Store.QueuedPhoneWrites(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPhoneWriteQueueScanDoesNotBlockRegistration(t *testing.T) {
	h := phoneWriteHub(t)
	enqueuePhoneWriteTest(t, h, "queued")
	stalled := &stalledPhoneWriteScan{Store: h.store, started: make(chan struct{}), release: make(chan struct{})}
	h.store = stalled
	startPhoneWriteTestLoop(t, h)
	select {
	case <-stalled.started:
	case <-time.After(5 * time.Second):
		t.Fatal("queue scan did not start")
	}
	registered := make(chan struct{})
	go func() {
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "registration", Tools: []proto.Tool{{Name: "phone_reminders_create"}}})
		close(registered)
	}()
	select {
	case <-registered:
	case <-time.After(time.Second):
		close(stalled.release)
		t.Fatal("queue scan blocked tool registration")
	}
	if !lockWithin(t, h, time.Second) {
		close(stalled.release)
		t.Fatal("queue scan held h.mu")
	}
	close(stalled.release)
}

func TestPhoneWriteQueueSchemaOnlyDecoratesConfirmedWrites(t *testing.T) {
	h := newTestHub(t)
	const schema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`
	var tools []proto.Tool
	for name := range phoneConfirmedTools {
		tools = append(tools, proto.Tool{Name: name, Description: "Write on the phone.", InputSchema: json.RawMessage(schema)})
	}
	tools = append(tools, proto.Tool{Name: "phone_device_info", Description: "Read from the phone.", InputSchema: json.RawMessage(schema)})
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "schema-phone", Tools: tools})
	for _, tool := range h.phoneToolList() {
		var parsed struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &parsed); err != nil {
			t.Fatal(err)
		}
		_, hasQueue := parsed.Properties["queue_if_offline"]
		if hasQueue != phoneConfirmedTools[tool.Name] {
			t.Fatalf("queue option on %s = %v", tool.Name, hasQueue)
		}
		if len(parsed.Required) != 1 || parsed.Required[0] != "title" {
			t.Fatalf("queue flag changed required fields: %s", tool.InputSchema)
		}
		if hasQueue && !strings.Contains(tool.Description, "queue_if_offline") {
			t.Fatalf("model lacks opt-in guidance: %s", tool.Description)
		}
	}
	for _, tool := range tools {
		if string(tool.InputSchema) != schema {
			t.Fatalf("registration schema mutated: %s", tool.InputSchema)
		}
	}
}

func TestPhoneWriteQueueRequiresRegisteredProvider(t *testing.T) {
	h := phoneWriteHub(t)
	args := json.RawMessage(`{"queue_if_offline":true,"title":"calendar entry"}`)
	result := h.callPhone(context.Background(), "phone_calendar_create_event", args)
	if !result.IsError || !strings.Contains(textOf(result), "registered provider") {
		t.Fatalf("unregistered write = %+v", result)
	}
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "calendar-phone", CredentialID: "calendar-credential", Tools: []proto.Tool{{Name: "phone_calendar_create_event"}}})
	h.revoked("calendar-credential")
	result = h.callPhone(context.Background(), "phone_calendar_create_event", args)
	if !result.IsError || !strings.Contains(textOf(result), "registered provider") {
		t.Fatalf("revoked provider write = %+v", result)
	}
	rows, err := h.store.PhoneWrites(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("unregistered writes persisted: %+v, %v", rows, err)
	}
}

func TestPhoneWriteQueueWaitsForEligibleProvider(t *testing.T) {
	h := phoneWriteHub(t)
	ctx := context.Background()
	calendar, err := h.store.EnqueuePhoneWrite(ctx, "calendar-first", "phone_calendar_create_event", json.RawMessage(`{"title":"calendar"}`))
	if err != nil {
		t.Fatal(err)
	}
	reminder := enqueuePhoneWriteTest(t, h, "reminder")
	startPhoneWriteTestLoop(t, h)
	phone := registerQueuePhone(t, h, "phone_reminders_create")
	call := nextQueuedPhoneCall(t, phone)
	if call.Name != "phone_reminders_create" {
		t.Fatalf("phone received an unsupported tool: %+v", call)
	}
	phoneWriteState(t, h, calendar.ID, "queued")
	answerQueuedPhoneCall(h, phone, call, "saved reminder", false)
	phoneWriteState(t, h, reminder.ID, "succeeded")
	calendarPhone := registerQueuePhone(t, h, "phone_calendar_create_event")
	call = nextQueuedPhoneCall(t, calendarPhone)
	if call.Name != "phone_calendar_create_event" {
		t.Fatalf("eligible phone received wrong tool: %+v", call)
	}
	answerQueuedPhoneCall(h, calendarPhone, call, "saved event", false)
	phoneWriteState(t, h, calendar.ID, "succeeded")
}

func TestPhoneWriteQueueDuplicateAfterReconnectKeepsReceipt(t *testing.T) {
	h := phoneWriteHub(t)
	row := enqueuePhoneWriteTest(t, h, "pending reminder")
	phone := registerQueuePhone(t, h, "phone_reminders_create")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := h.callPhone(ctx, "phone_reminders_create", json.RawMessage(`{"queue_if_offline":true,"title":"pending reminder"}`))
	var duplicate db.PhoneWrite
	if result.IsError || json.Unmarshal([]byte(textOf(result)), &duplicate) != nil || duplicate.ID != row.ID || duplicate.State != "queued" {
		t.Fatalf("duplicate after reconnect = %+v", result)
	}
	select {
	case frame := <-phone.out:
		t.Fatalf("duplicate bypassed the queue: %s", frame)
	default:
	}
	startPhoneWriteTestLoop(t, h)
	call := nextQueuedPhoneCall(t, phone)
	answerQueuedPhoneCall(h, phone, call, "saved", false)
	phoneWriteState(t, h, row.ID, "succeeded")
}

func TestGoldenPhoneWriteQueueTools(t *testing.T) {
	h := newTestHub(t)
	h.store = newMeshStore()
	tools := h.withPhoneWriteTools([]proto.Tool{{
		Name:        "phone_reminders_create",
		Description: "Create a reminder after confirmation on the phone.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`),
	}})
	raw, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "phone_write_queue_tools", string(raw))
}
