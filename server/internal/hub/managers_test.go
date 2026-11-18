package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// managersHub is newBusHub with no install codes and no default picked.
func managersHub(t *testing.T) (*Hub, *httptest.Server, *db.DB) {
	t.Helper()
	h, srv, store := newBusHub(t)
	clear := func() {
		list, _ := store.PendingManagerInstalls(context.Background())
		for _, c := range list {
			_, _ = store.DropManagerInstallCodes(context.Background(), c.ManagerID)
		}
		_ = store.PutSetting(context.Background(), managerDefaultKey, nil)
	}
	clear()
	t.Cleanup(clear)
	return h, srv, store
}

// managersDo sends an app's request with the master token (loopback) and decodes the answer into out.
func managersDo(t *testing.T, h *Hub, srv *httptest.Server, method, path string, body, out any) int {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	if path != bus.JoinPath {
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if l, ok := out.(*managersReply); ok {
			*l = managersReply{} // json reuses the slice's elements, keeping fields the answer leaves out
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

type managersReply struct {
	Managers []managerInfo `json:"managers"`
	Default  string        `json:"default"`
}

func (r managersReply) find(id string) (managerInfo, bool) {
	for _, m := range r.Managers {
		if m.ID == id {
			return m, true
		}
	}
	return managerInfo{}, false
}

// The assistant the hub starts is the first manager and the default; a new one is made with a name, waits as
// installing, trades its code once for a token, connects, and the roster keeps where it ran after it drops.
func TestManagersRosterAndInstall(t *testing.T) {
	h, srv, _ := managersHub(t)
	assistantTok := busToken(t, h, assistantwire.AssistantID, "助手")
	_ = dialRawBus(t, srv, assistantTok)

	var list managersReply
	if code := managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list); code != 200 {
		t.Fatalf("list: %d", code)
	}
	if list.Default != assistantwire.AssistantID || len(list.Managers) != 1 || !list.Managers[0].Default || !list.Managers[0].Builtin {
		t.Fatalf("first roster: %+v", list)
	}

	var made struct {
		Manager   managerInfo `json:"manager"`
		Code      string      `json:"code"`
		ExpiresAt time.Time   `json:"expires_at"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": " "}, nil); code != 400 {
		t.Fatalf("empty name: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "工作"}, &made); code != 201 {
		t.Fatalf("make: %d", code)
	}
	id := made.Manager.ID
	if !installCodeShape.MatchString(made.Code) || time.Until(made.ExpiresAt) > installCodeTTL+time.Minute || time.Until(made.ExpiresAt) < 9*time.Minute {
		t.Fatalf("code %q until %v", made.Code, made.ExpiresAt)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if m, ok := list.find(id); !ok || m.Installing == nil || m.Online || m.Name != "工作" {
		t.Fatalf("installing: %+v", list)
	}

	var joined struct {
		ID, Name, Token string
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": "mi_" + "0123456789abcdef0123456789abcdef"}, nil); code != 401 {
		t.Fatalf("unknown code: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, &joined); code != 200 {
		t.Fatalf("join: %d", code)
	}
	if joined.ID != id || joined.Name != "工作" || joined.Token == "" {
		t.Fatalf("joined: %+v", joined)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != 401 {
		t.Fatalf("a code works once, got %d", code)
	}

	r := dialRawBus(t, srv, joined.Token)
	if err := r.call("manager.hello", map[string]any{"id": id, "host": "box", "os": "linux", "arch": "amd64", "deploy": "service", "version": "v1"}, nil); err != nil {
		t.Fatal(err)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	m, _ := list.find(id)
	if !m.Online || m.Installing != nil || m.Host != "box" || m.Deploy != "service" || m.Default || m.Builtin {
		t.Fatalf("connected: %+v", m)
	}

	_ = r.ws.Close(websocket.StatusNormalClosure, "bye")
	deadline := time.Now().Add(5 * time.Second)
	for {
		managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
		if m, _ = list.find(id); !m.Online || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Online || m.LastSeen == nil || m.Host != "box" {
		t.Fatalf("offline: %+v", m)
	}
}

// The built-in assistant is kept out of the switcher while it is not used (#9055): hidden only while the choice is
// on, it is not the default and it owns no machine; a machine that falls back to it shows it again by itself.
func TestManagersHideTheBuiltinWhileUnused(t *testing.T) {
	h, srv, store := managersHub(t)
	t.Cleanup(func() { _ = store.PutSetting(context.Background(), managerBuiltinHideKey, nil) })
	busToken(t, h, assistantwire.AssistantID, "助手")
	busToken(t, h, "m-abc123", "家")
	builtin := func() managerInfo {
		t.Helper()
		var list managersReply
		managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
		m, ok := list.find(assistantwire.AssistantID)
		if !ok || !m.Builtin {
			t.Fatalf("no built-in manager: %+v", list)
		}
		return m
	}
	hide := func(id string, on bool) int {
		t.Helper()
		return managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+id, map[string]any{"hide": on}, nil)
	}
	machine := func(on bool) { // a hub.json computer no manager was given: the carrier's, the built-in one's
		t.Helper()
		d := &h.devices
		d.mu.Lock()
		defer d.mu.Unlock()
		list := &h.cfg.Machines
		if d.started {
			list = &d.entries
		}
		if on {
			*list = append(*list, MachineConfig{ID: "box9055", Kind: "local"})
		} else {
			*list = slices.DeleteFunc(*list, func(mc MachineConfig) bool { return mc.ID == "box9055" })
		}
	}

	if code := hide(assistantwire.AssistantID, true); code != 409 {
		t.Fatalf("hiding the default: %d", code)
	}
	if code := hide("m-abc123", true); code != 400 {
		t.Fatalf("hiding another manager: %d", code)
	}
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/m-abc123", map[string]any{"default": true}, nil)
	if m := builtin(); m.Hide || m.Hidden {
		t.Fatalf("not asked yet: %+v", m)
	}
	if code := hide(assistantwire.AssistantID, true); code != 200 {
		t.Fatalf("hide: %d", code)
	}
	if m := builtin(); !m.Hide || !m.Hidden {
		t.Fatalf("hidden: %+v", m)
	}

	// A machine falls back to it: shown, the choice kept; hiding is refused while it has one.
	machine(true)
	if m := builtin(); !m.Hide || m.Hidden {
		t.Fatalf("with a machine: %+v", m)
	}
	if code := hide(assistantwire.AssistantID, true); code != 409 {
		t.Fatalf("hiding it with a machine: %d", code)
	}
	machine(false)
	if m := builtin(); !m.Hidden {
		t.Fatalf("the machine gone: %+v", m)
	}

	// Something in its inbox needs the user: shown until it is handled (the apps would mark it waiting otherwise).
	r := dialRawBus(t, srv, busToken(t, h, assistantwire.AssistantID, "助手"))
	if err := r.call(assistantwire.MethodHello, map[string]any{"id": assistantwire.AssistantID}, nil); err != nil {
		t.Fatal(err)
	}
	say := func(n int) {
		t.Helper()
		if err := r.peer.Notify(assistantwire.EventInboxNeedsYou, assistantwire.InboxNeedsYou{Count: n}); err != nil {
			t.Fatal(err)
		}
	}
	say(2)
	eventually(t, 5*time.Second, "shown while its inbox needs the user", func() bool { m := builtin(); return m.Hide && !m.Hidden })
	say(0)
	eventually(t, 5*time.Second, "hidden again once nothing waits", func() bool { return builtin().Hidden })

	// Made the default: shown; asking for both at once is refused.
	if code := managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"default": true, "hide": true}, nil); code != 409 {
		t.Fatalf("default and hide at once: %d", code)
	}
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"default": true}, nil)
	if m := builtin(); !m.Hide || m.Hidden {
		t.Fatalf("the default: %+v", m)
	}

	// Shown again on request, whatever it is.
	if code := hide(assistantwire.AssistantID, false); code != 200 {
		t.Fatalf("show: %d", code)
	}
	if m := builtin(); m.Hide || m.Hidden {
		t.Fatalf("shown: %+v", m)
	}
}

// Rename, make default, revoke: the default and the hub's own cannot be removed; a revoked token is hung up and
// opens nothing.
func TestManagersPatchAndRevoke(t *testing.T) {
	h, srv, store := managersHub(t)
	busToken(t, h, assistantwire.AssistantID, "助手")
	tok := busToken(t, h, "m-abc123", "家")
	r := dialRawBus(t, srv, tok)
	if err := r.call("manager.hello", map[string]any{"id": "m-abc123"}, nil); err != nil {
		t.Fatal(err)
	}

	if code := managersDo(t, h, srv, "PATCH", "/api/v1/managers/m-abc123", map[string]any{"name": "家里", "default": true}, nil); code != 200 {
		t.Fatalf("patch: %d", code)
	}
	var list managersReply
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if list.Default != "m-abc123" || list.Managers[0].ID != "m-abc123" || list.Managers[0].Name != "家里" {
		t.Fatalf("after patch: %+v", list)
	}
	// The hub making the assistant a new token on start keeps the name the user gave it.
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"name": "工作助手"}, nil)
	busToken(t, h, assistantwire.AssistantID, "助手")
	if b, _ := store.BusIdentityByID(context.Background(), assistantwire.AssistantID); b == nil || b.Name != "工作助手" {
		t.Fatalf("assistant renamed: %+v", b)
	}

	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 409 {
		t.Fatalf("delete the default: %d", code)
	}
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+assistantwire.AssistantID, nil, nil); code != 409 {
		t.Fatalf("delete the builtin: %d", code)
	}
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"default": true}, nil)
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	r.closedWith(t, bus.CloseRevoked)
	if _, ok, _ := h.busIdentity(context.Background(), tok); ok {
		t.Fatal("a revoked token still names its manager")
	}
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 404 {
		t.Fatalf("delete again: %d", code)
	}

	// A pending install is called off: its code no longer trades.
	var made struct {
		Manager managerInfo `json:"manager"`
		Code    string      `json:"code"`
	}
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "临时"}, &made)
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+made.Manager.ID, nil, nil); code != 200 {
		t.Fatalf("call off: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != 401 {
		t.Fatalf("a called-off code traded: %d", code)
	}
}

// A removed manager's pending cards are withdrawn: the apps hear they are over and no longer get them; another
// manager's stay.
func TestManagersRevokeWithdrawsItsCards(t *testing.T) {
	h, srv, _ := managersHub(t)
	busToken(t, h, assistantwire.AssistantID, "助手")
	r := dialRawBus(t, srv, busToken(t, h, "m-abc123", "家"))
	if err := r.call("manager.hello", map[string]any{"id": "m-abc123"}, nil); err != nil {
		t.Fatal(err)
	}
	// Its card is on its own machine (a card on another manager's is not taken: ownership.go cardAllowed).
	h.cfg.Machines = []MachineConfig{{ID: "mb", Name: "mb", Kind: "local"}}
	if _, err := h.moveMachine(context.Background(), "mb", "m-abc123"); err != nil {
		t.Fatal(err)
	}
	if err := r.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: proto.Interaction{
		ID: "ix-gone", MachineID: "mb", SessionID: "s1", Kind: "question", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the interaction", func() bool { return h.interactionOwner("ix-gone") == "m-abc123" })
	h.takeFleetInteractions([]proto.Interaction{{ID: "ix-keep", MachineID: "ma", SessionID: "s2", Kind: "question", CreatedAt: time.Now()}})
	app := wsApp(t, h)

	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if _, ok := h.pendingInteraction("ix-gone"); ok {
		t.Fatal("the removed manager's card is still pending")
	}
	if h.interactionOwner("ix-keep") != assistantwire.AssistantID {
		t.Fatal("the assistant's card went with the removed manager")
	}
	deadline := time.After(5 * time.Second)
	for heard := false; !heard; {
		select {
		case raw := <-app.out:
			var m struct{ Type, ID string }
			_ = json.Unmarshal(raw, &m)
			if m.Type == "interaction_resolved" && m.ID == "ix-keep" {
				t.Fatalf("the assistant's card was withdrawn: %s", raw)
			}
			heard = m.Type == "interaction_resolved" && m.ID == "ix-gone"
		case <-deadline:
			t.Fatal("the apps never heard the removed manager's card is over")
		}
	}
}

// Only the apps that asked for "managers" hear that the roster changed (a manager connected).
func TestManagersEventIsOptIn(t *testing.T) {
	h, srv, _ := managersHub(t)
	plain := wsApp(t, h)
	asked := &client{device: "test-device-2", out: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[asked] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), asked, []byte(`{"type":"client_hello","features":["managers"]}`))
	<-asked.out // client_hello_ack
	r := dialRawBus(t, srv, busToken(t, h, "m-abc123", "家"))
	if err := r.call("manager.hello", map[string]any{"id": "m-abc123"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-asked.out:
		if string(raw) != `{"type":"managers"}` {
			t.Fatalf("got %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app that asked heard nothing")
	}
	select {
	case raw := <-plain.out:
		t.Fatalf("an app that did not ask got %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

// A join whose token cannot be saved does not use up its code: once the store works again the same code joins.
func TestManagersInstallCodeSurvivesAFailedSave(t *testing.T) {
	h, srv, _ := managersHub(t)
	var made struct {
		Manager managerInfo `json:"manager"`
		Code    string      `json:"code"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "工作"}, &made); code != 201 {
		t.Fatalf("make: %d", code)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	allow := func() {
		_, _ = conn.Exec(ctx, `DROP TRIGGER IF EXISTS test_refuse_bus_identity ON bus_identities`)
		_, _ = conn.Exec(ctx, `DROP FUNCTION IF EXISTS test_refuse_bus_identity()`)
	}
	t.Cleanup(allow)
	for _, q := range []string{
		`CREATE OR REPLACE FUNCTION test_refuse_bus_identity() RETURNS trigger LANGUAGE plpgsql
			AS $$ BEGIN RAISE EXCEPTION 'bus_identities write refused by the test'; END $$`,
		`CREATE TRIGGER test_refuse_bus_identity BEFORE INSERT OR UPDATE ON bus_identities
			FOR EACH ROW EXECUTE FUNCTION test_refuse_bus_identity()`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("join while the token cannot be saved: %d", code)
	}
	allow()

	var joined struct {
		ID, Name, Token string
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, &joined); code != 200 {
		t.Fatalf("the same code after the store recovered: %d", code)
	}
	if joined.ID != made.Manager.ID || joined.Name != "工作" || joined.Token == "" {
		t.Fatalf("joined: %+v", joined)
	}
	_ = dialRawBus(t, srv, joined.Token)
}

// A manager made and not installed stays in the roster after its code expires, as installing with the expired time
// (the apps say Code expired): the expired code no longer joins, …/code gives it a new one that does, and a pending one can
// be called off. Expired more than db.ManagerInstallKept ago, it leaves the roster and is cleaned up.
func TestManagersExpiredInstallIsRenewed(t *testing.T) {
	h, srv, _ := managersHub(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	expire := func(id string, ago time.Duration) {
		t.Helper()
		if _, err := conn.Exec(ctx, `UPDATE manager_install_codes SET expires_at = now() - make_interval(secs => $2)
			WHERE manager_id = $1`, id, ago.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	type madeReply struct {
		Manager managerInfo `json:"manager"`
		Code    string      `json:"code"`
	}
	var made madeReply
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "工作"}, &made); code != 201 {
		t.Fatalf("make: %d", code)
	}
	id := made.Manager.ID
	expire(id, time.Minute)

	var list managersReply
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	m, ok := list.find(id)
	if !ok || m.Installing == nil || !m.Installing.Before(time.Now()) || m.Name != "工作" || m.Online {
		t.Fatalf("expired, not installed: %+v (%v)", m, ok)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != 401 {
		t.Fatalf("the expired code joined: %d", code)
	}
	if code := managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+id, map[string]any{"name": "x"}, nil); code != 409 {
		t.Fatalf("rename while installing: %d", code)
	}

	// A new code for it: installing again until the new expiry; the new code joins.
	var renewed struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers/"+id+"/code", nil, &renewed); code != 200 || !installCodeShape.MatchString(renewed.Code) {
		t.Fatalf("a new code for the expired install: %d %+v", code, renewed)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if m, _ := list.find(id); m.Installing == nil || !m.Installing.After(time.Now()) {
		t.Fatalf("after the new code: %+v", m)
	}
	var joined struct{ ID, Name, Token string }
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": renewed.Code}, &joined); code != 200 || joined.ID != id || joined.Name != "工作" {
		t.Fatalf("join with the new code: %d %+v", code, joined)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if m, _ := list.find(id); m.Installing != nil {
		t.Fatalf("still installing after the join: %+v", m)
	}

	// Another, expired and called off: gone, its code with it.
	var other madeReply
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "临时"}, &other)
	expire(other.Manager.ID, time.Hour)
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+other.Manager.ID, nil, nil); code != 200 {
		t.Fatalf("call off an expired install: %d", code)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if _, ok := list.find(other.Manager.ID); ok {
		t.Fatalf("called off, still listed: %+v", list)
	}

	// A third, expired longer than it is kept: not in the roster, no new code, and its rows go with the next code made.
	var old madeReply
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "旧的"}, &old)
	expire(old.Manager.ID, db.ManagerInstallKept+time.Hour)
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if _, ok := list.find(old.Manager.ID); ok {
		t.Fatalf("kept past %s: %+v", db.ManagerInstallKept, list)
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers/"+old.Manager.ID+"/code", nil, nil); code != 404 {
		t.Fatalf("a new code for one dropped: %d", code)
	}
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "再一个"}, nil)
	var left int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM manager_install_codes WHERE manager_id = $1`, old.Manager.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("its codes left behind: %d %v", left, err)
	}
}

// A manager names config.own in its hello when it reads its own manager.json. One that doesn't (a build from before)
// is said once in the log, however often it connects, and the roster marks it needs_update while it is online.
func TestManagerWithoutConfigOwnNeedsUpdate(t *testing.T) {
	h, srv, _ := managersHub(t)
	var mu sync.Mutex
	var logs bytes.Buffer
	h.logger = log.New(lockedWriter{&mu, &logs}, "", 0)
	tok := busToken(t, h, assistantwire.AssistantID, "助手")
	hello := func(caps []string) {
		r := dialRawBus(t, srv, tok)
		params := map[string]any{"id": assistantwire.AssistantID, "host": "box", "version": "v"}
		if caps != nil {
			params["capabilities"] = caps
		}
		if err := r.call(assistantwire.MethodHello, params, nil); err != nil {
			t.Fatal(err)
		}
	}
	builtin := func() managerInfo {
		var list managersReply
		if code := managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list); code != 200 {
			t.Fatalf("list: %d", code)
		}
		m, _ := list.find(assistantwire.AssistantID)
		return m
	}
	// The hub's hello-era sections go to a built-in manager it did not start, raw, and not to its own child.
	if err := os.WriteFile(filepath.Join(h.cfg.dir, "hub.json"), []byte(`{"token":"t","kb":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for deploy, want := range map[string]string{"child": "", "external": `{"kb":{"enabled":false}}`} {
		r := dialRawBus(t, srv, tok)
		var res struct {
			LegacyConfig json.RawMessage `json:"legacy_config"`
		}
		params := map[string]any{"id": assistantwire.AssistantID, "deploy": deploy, "capabilities": []string{assistantwire.CapabilityConfigOwn}}
		if err := r.call(assistantwire.MethodHello, params, &res); err != nil || string(res.LegacyConfig) != want {
			t.Fatalf("%s: legacy %s %v", deploy, res.LegacyConfig, err)
		}
	}
	hello(nil)
	if m := builtin(); !m.Online || !m.NeedsUpdate {
		t.Fatalf("an older manager: %+v", m)
	}
	hello(nil)
	mu.Lock()
	said := strings.Count(logs.String(), "is from before manager.json")
	mu.Unlock()
	if said != 1 {
		t.Fatalf("said %d times:\n%s", said, logs.String())
	}
	hello([]string{assistantwire.CapabilityConfigOwn, " ", "other"})
	if m := builtin(); !m.Online || m.NeedsUpdate {
		t.Fatalf("a manager with config.own: %+v", m)
	}
	for _, p := range h.busRoster() {
		if p.ID == assistantwire.AssistantID && !slices.Equal(p.Capabilities, []string{assistantwire.CapabilityConfigOwn, "other"}) {
			t.Fatalf("capabilities %q", p.Capabilities)
		}
	}
}

// The hub says once as it starts which of the manager's sections hub.json still has, and where they are read now.
func TestHubSaysWhichSectionsAreTheManagers(t *testing.T) {
	h := newTestHub(t)
	var mu sync.Mutex
	var logs bytes.Buffer
	h.logger = log.New(lockedWriter{&mu, &logs}, "", 0)
	if err := os.WriteFile(filepath.Join(h.cfg.dir, "hub.json"),
		[]byte(`{"token":"t","assistant":{"owner":"o","external":false},"kb":{},"lark":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.sayMovedSections()
	mu.Lock()
	got := logs.String()
	mu.Unlock()
	if !strings.Contains(got, "hub.json's lark, kb, assistant.owner are the manager's") ||
		!strings.Contains(got, filepath.Join("managers", "assistant", "manager.json")) {
		t.Fatalf("said %q", got)
	}
	logs.Reset()
	if err := os.WriteFile(filepath.Join(h.cfg.dir, "hub.json"), []byte(`{"token":"t","assistant":{"external":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if h.sayMovedSections(); logs.Len() != 0 {
		t.Fatalf("said %q for a hub.json without them", logs.String())
	}
}

// A manager says how many of its inbox's open items are in "Needs you" (inbox.needs_you): the roster has the number
// while that connection is up, the apps are told to read the roster again when it differs from the one before and
// not when it is the same, and a manager that said none, one that went away and one that connects again have none.
// The numbers that change within the push's delay are one push.
func TestManagersRosterHasInboxNeedsYou(t *testing.T) {
	h, srv, _ := managersHub(t)
	const delay = 100 * time.Millisecond
	h.bus.mu.Lock()
	h.bus.needsYouDelay = delay
	h.bus.mu.Unlock()
	app := &client{device: "test-device-2", out: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[app] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), app, []byte(`{"type":"client_hello","features":["managers"]}`))
	<-app.out // client_hello_ack
	// pushes are the managers pushes the app got until none came for longer than a delayed one takes.
	pushes := func() int {
		t.Helper()
		n := 0
		for {
			select {
			case raw := <-app.out:
				if string(raw) != `{"type":"managers"}` {
					t.Fatalf("got %s", raw)
				}
				n++
			case <-time.After(delay + 300*time.Millisecond):
				return n
			}
		}
	}
	needsYou := func(id string) *int {
		t.Helper()
		var list managersReply
		if code := managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list); code != 200 {
			t.Fatalf("list: %d", code)
		}
		m, ok := list.find(id)
		if !ok {
			t.Fatalf("no %s in %+v", id, list)
		}
		return m.InboxNeedsYou
	}
	is := func(id string, want int) func() bool {
		return func() bool { n := needsYou(id); return n != nil && *n == want }
	}
	connect := func(id, name string) *rawBus {
		t.Helper()
		r := dialRawBus(t, srv, busToken(t, h, id, name))
		if err := r.call(assistantwire.MethodHello, map[string]any{"id": id}, nil); err != nil {
			t.Fatal(err)
		}
		return r
	}
	say := func(r *rawBus, params any) {
		t.Helper()
		if err := r.peer.Notify(assistantwire.EventInboxNeedsYou, params); err != nil {
			t.Fatal(err)
		}
	}
	// heard: the hub took everything r said so far (a connection's events and calls are taken in order).
	heard := func(r *rawBus) {
		t.Helper()
		if err := r.call("ping", map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	work, home := connect("m-abc123", "工作"), connect("m-def456", "家")
	if n := needsYou("m-abc123"); n != nil {
		t.Fatalf("a manager that said none: %d", *n)
	}
	pushes() // the two connected

	say(work, assistantwire.InboxNeedsYou{Count: 3})
	eventually(t, 5*time.Second, "the roster has the count", is("m-abc123", 3))
	if n := needsYou("m-def456"); n != nil {
		t.Fatalf("another manager's count: %d", *n)
	}
	if n := pushes(); n != 1 {
		t.Fatalf("a new count pushed %d times", n)
	}
	// The same number again, and what is no count (negative, not a number, none at all), change nothing and tell the
	// apps nothing.
	say(work, assistantwire.InboxNeedsYou{Count: 3})
	say(work, map[string]any{"count": -1})
	say(work, map[string]any{"count": "many"})
	say(work, map[string]any{"count": nil})
	say(work, map[string]any{})
	heard(work)
	if n := needsYou("m-abc123"); n == nil || *n != 3 {
		t.Fatalf("after the same count and what is no count: %v", n)
	}
	if n := pushes(); n != 0 {
		t.Fatalf("the same count and what is no count pushed %d times", n)
	}
	say(home, assistantwire.InboxNeedsYou{Count: 0}) // none in "Needs you" is a count: 0, not absent
	eventually(t, 5*time.Second, "the roster has the other's count", is("m-def456", 0))
	if n := pushes(); n != 1 {
		t.Fatalf("the other manager's first count pushed %d times", n)
	}
	var raw struct {
		Managers []map[string]any `json:"managers"`
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &raw)
	for _, m := range raw.Managers {
		_, has := m["inbox_needs_you"]
		if want := m["id"] == "m-abc123" || m["id"] == "m-def456"; has != want {
			t.Fatalf("inbox_needs_you present %v: %v", has, m)
		}
	}

	// A round's items triaged one by one: every step is in the roster at once, and the apps hear of them together.
	for n := 4; n <= 12; n++ {
		say(work, assistantwire.InboxNeedsYou{Count: n})
	}
	say(home, assistantwire.InboxNeedsYou{Count: 2})
	heard(work)
	heard(home)
	if a, b := needsYou("m-abc123"), needsYou("m-def456"); a == nil || *a != 12 || b == nil || *b != 2 {
		t.Fatalf("the roster after the steps: %v %v", a, b)
	}
	if n := pushes(); n != 1 {
		t.Fatalf("ten counts within the delay pushed %d times", n)
	}
	// A count that changes after that push gets its own.
	say(work, assistantwire.InboxNeedsYou{Count: 11})
	if n := pushes(); n != 1 {
		t.Fatalf("a count after the push pushed %d times", n)
	}

	// It goes with the connection: gone while the manager is away, and not back until it says it again.
	_ = work.ws.CloseNow()
	eventually(t, 5*time.Second, "a manager away has no count", func() bool { return needsYou("m-abc123") == nil })
	pushes()
	work = connect("m-abc123", "工作")
	if n := needsYou("m-abc123"); n != nil {
		t.Fatalf("a new connection that said none: %d", *n)
	}
	pushes()
	say(work, assistantwire.InboxNeedsYou{Count: 1})
	eventually(t, 5*time.Second, "the new connection's count", is("m-abc123", 1))
	if n := pushes(); n != 1 {
		t.Fatalf("the new connection's count pushed %d times", n)
	}
}
