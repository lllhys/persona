package hub

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apns"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devicewire"
)

// The ids these tests add (cleared first: the test database outlives a run).
var addTestIDs = []string{"onb-laptop", "onb-laptop-2", "onb-laptop-3", "onb-own", "linux", "onb-a", "onb-b", "onb-c", "onb-d", "onb-cli", "onb-dev",
	"onb-keep", "onb-away", "onb-none", "onb-wait", "onb-plain", "onb-carrier", "onb-move", "onb-kept"}

// addHub is a running hub (its manager has the entries) with TLS (a Persona Link can pair), one confirmed link
// computer onb-own, and a signed-in phone's token.
func addHub(t *testing.T) (*Hub, *db.DB, string) {
	t.Helper()
	database := testDB(t)
	ctx := context.Background()
	for _, id := range addTestIDs {
		if err := database.ForgetMachineTrust(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := database.RevokeMachineCredentials(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	h, _ := trustHub(t, MachineConfig{ID: "onb-own", Name: "Own", Kind: kindLink})
	h.devices.entries, h.devices.started = slices.Clone(h.cfg.Machines), true
	h.trust.pending = map[string]time.Time{} // only this test's
	h.cfg.PublicURL = "https://hub.example.com"
	_, phone := call(t, h.routes(), "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"小林的手机","kind":"phone"}`)
	return h, database, phone["token"].(string)
}

type computerCode struct {
	Token     string            `json:"token"`
	ExpiresAt time.Time         `json:"expires_at"`
	Machine   string            `json:"machine"`
	Name      string            `json:"name"`
	Trust     string            `json:"trust"`
	Added     bool              `json:"added"`
	PairURL   string            `json:"pair_url"`
	Command   string            `json:"command"`
	Install   map[string]string `json:"install"`
	QR        json.RawMessage   `json:"qr"`
	Manager   string            `json:"manager"`
}

func askCode(t *testing.T, h *Hub, tok, body string, status int, code string) computerCode {
	t.Helper()
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tok, body, "198.51.100.1:1", true)
	requireCode(t, w, status, code)
	var c computerCode
	if status == 200 {
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("a pairing code must not be cached")
		}
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func hubJSONIDs(t *testing.T, h *Hub) []string {
	t.Helper()
	disk, err := h.diskConfig()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range disk.Machines {
		ids = append(ids, m.ID)
	}
	return ids
}

// A signed-in phone adds a computer by its (Chinese) name: a link entry in hub.json and the manager's, pending, with
// its Persona Link's code, the command and (when the hub knows its site) the Linux line. The same name again before
// it connects is the same computer; a name a confirmed one has gets another id.
func TestDeviceAddsAComputer(t *testing.T) {
	h, database, tok := addHub(t)
	c := askCode(t, h, tok, `{"add":{"name":"小林的 Onb Laptop","os":"mac","icon":"laptop"}}`, 200, "")
	if c.Machine != "onb-laptop" || c.Name != "小林的 Onb Laptop" || c.Trust != "pending" || !c.Added || c.Manager != devicesOwner {
		t.Fatalf("reply %+v", c)
	}
	u, err := url.Parse(c.PairURL)
	if err != nil || u.Scheme != "persona" || u.Host != "link" || u.Query().Get("machine") != "onb-laptop" || u.Query().Get("token") != c.Token {
		t.Fatalf("pair_url %q", c.PairURL)
	}
	if c.Command != "persona-link pair '"+c.PairURL+"'" || c.Install != nil || len(c.QR) != 0 {
		t.Fatalf("command %q install %v qr %s: no site, no install line; no app QR code", c.Command, c.Install, c.QR)
	}
	if !h.machinePending("onb-laptop") || !slices.Contains(hubJSONIDs(t, h), "onb-laptop") || h.cfgEntry("onb-laptop") == nil {
		t.Fatal("not pending, or not in hub.json and the manager's entries")
	}
	if mc := h.cfgEntry("onb-laptop"); mc.Kind != kindLink || mc.Icon != "laptop" {
		t.Fatalf("entry %+v", mc)
	}
	pt, err := database.PairTokenByHash(context.Background(), hashToken(c.Token))
	if err != nil || pt == nil || pt.MachineID != "onb-laptop" {
		t.Fatalf("pair token %+v %v", pt, err)
	}

	// The same name before it connected: that computer's code again. The hub knows where it was published now.
	h.build.UpdateURL = "https://site.example.com/dl"
	again := askCode(t, h, tok, `{"add":{"name":"小林的 Onb Laptop","os":"linux"}}`, 200, "")
	if again.Machine != "onb-laptop" || again.Added || again.Trust != "pending" || again.Token == c.Token {
		t.Fatalf("again %+v", again)
	}
	if want := "curl -fsSL https://site.example.com/install.sh | sh -s -- --node --pair '" + again.PairURL + "'"; again.Install["linux"] != want {
		t.Fatalf("install %q", again.Install["linux"])
	}
	// {"machine"} for a pending one: its code again too.
	if m := askCode(t, h, tok, `{"machine":"onb-laptop"}`, 200, ""); m.Machine != "onb-laptop" || m.Trust != "pending" || m.Added {
		t.Fatalf("machine %+v", m)
	}
	// Another name with the same letters: a second computer.
	if c2 := askCode(t, h, tok, `{"add":{"name":"Onb Laptop"}}`, 200, ""); c2.Machine != "onb-laptop-2" || !c2.Added {
		t.Fatalf("second %+v", c2)
	}
	// No letters in the name: the system's.
	if c3 := askCode(t, h, tok, `{"add":{"name":"小林的电脑","os":"linux"}}`, 200, ""); c3.Machine != "linux" {
		t.Fatalf("chinese name %+v", c3)
	}
	// Each code is in the audit.
	audit, err := database.MachineAudits(context.Background(), "onb-laptop")
	n := 0
	for _, a := range audit {
		if a.Action == "code" {
			n++
		}
	}
	if err != nil || n < 3 {
		t.Fatalf("audit codes %d %v", n, err)
	}
}

// What a device may not do: a computer confirmed already (its code would let another computer be it), one there is
// not, a bad request; three computers waiting is the most.
func TestDeviceAddRefusals(t *testing.T) {
	h, _, tok := addHub(t)
	askCode(t, h, tok, `{"machine":"onb-own"}`, 409, "not_pending")
	askCode(t, h, tok, `{"machine":"onb-nowhere"}`, 404, "not_found")
	askCode(t, h, tok, `{"add":{"name":""}}`, 400, "invalid_name")
	askCode(t, h, tok, `{"add":{"name":"x","os":"beos"}}`, 400, "invalid_os")
	askCode(t, h, tok, `{"add":{"name":"x","id":"Bad ID"}}`, 400, "invalid_id")
	askCode(t, h, tok, `{"add":{"name":"x","id":"onb-own"}}`, 409, "exists")
	askCode(t, h, tok, `{"add":{"name":"x"},"machine":"onb-own"}`, 400, "")
	askCode(t, h, tok, `{"add":{"name":"x"},"web":"https://hub.example.com/"}`, 400, "")
	if strings.Contains(strings.Join(hubJSONIDs(t, h), ","), "x") {
		t.Fatal("a refused request added a computer")
	}
	for _, name := range []string{"onb a", "onb b", "onb c"} {
		askCode(t, h, tok, `{"add":{"name":"`+name+`"}}`, 200, "")
	}
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tok, `{"add":{"name":"onb d"}}`, "198.51.100.1:1", true)
	requireCode(t, w, 429, "too_many_pending")
	if !strings.Contains(w.Body.String(), "已经加了 3 台电脑还没处理完") {
		t.Fatalf("too many: %s", w.Body.String())
	}
	if h.cfgEntry("onb-d") != nil {
		t.Fatal("the fourth was added")
	}
	// Confirming one makes room; the same name again is that computer's code, which does not count.
	if _, err := h.confirmMachine(context.Background(), "onb-a", "test"); err != nil {
		t.Fatal(err)
	}
	askCode(t, h, tok, `{"add":{"name":"onb b"}}`, 200, "")
	askCode(t, h, tok, `{"add":{"name":"onb d"}}`, 200, "")
}

// Who may ask: not the watch, a computer's link, a pairing code's stand-in; the master token only over loopback. A hub
// without TLS says so with the action that fixes it; a member hub sends the device to the center.
func TestComputerCodeCallers(t *testing.T) {
	h, _, tok := addHub(t)
	_, watch := call(t, h.routes(), "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"kind":"watch"}`)
	_, linkTok := call(t, h.routes(), "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"kind":"link","machine":"onb-own"}`)
	pair := newPairCode(t, h).Token
	body := `{"add":{"name":"onb a"}}`
	for _, who := range []string{watch["token"].(string), linkTok["token"].(string), pair} {
		requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", who, body, "198.51.100.1:1", true), 403, "forbidden")
	}
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, body, "198.51.100.1:1", true), 403, "")
	if h.cfgEntry("onb-a") != nil {
		t.Fatal("a refused caller added a computer")
	}

	h.cfg.PublicURL = "http://hub.example.com:18660"
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tok, body, "198.51.100.1:1", true)
	requireCode(t, w, 409, "tls_required")
	var refusal map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal["action"] != "enable_tls" || refusal["error"] != "hub 还没打开安全连接，电脑接不进来" {
		t.Fatalf("tls refusal %v", refusal)
	}
	h.cfg.PublicURL = "https://hub.example.com"

	h.mesh.mu.Lock()
	h.mesh.running, h.mesh.group = true, meshGroup{Center: "another-hub", Term: 1}
	h.mesh.mu.Unlock()
	askCode(t, h, tok, body, 409, "mesh_member")
	h.mesh.mu.Lock()
	h.mesh.running = false
	h.mesh.mu.Unlock()
	if h.cfgEntry("onb-a") != nil {
		t.Fatal("added on a member")
	}
}

// The master token over loopback (`hub pair --machine <id> --add`) adds a confirmed computer under that id, and
// makes a code for any link computer, confirmed ones too. Without --add a computer hub.json does not have is a
// 400 the CLI turns into "add --add".
func TestMasterAddsAConfirmedComputer(t *testing.T) {
	h, _, _ := addHub(t)
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"add":{"id":"onb-cli","name":"书房的电脑"}}`, "127.0.0.1:1", false)
	requireCode(t, w, 200, "")
	var c computerCode
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	if c.Machine != "onb-cli" || c.Trust != "confirmed" || !c.Added || h.machinePending("onb-cli") || h.cfgEntry("onb-cli").Name != "书房的电脑" {
		t.Fatalf("master add %+v", c)
	}
	// Again: its code, nothing added.
	w = securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"add":{"id":"onb-cli"}}`, "127.0.0.1:1", false)
	requireCode(t, w, 200, "")
	c = computerCode{}
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	if c.Added || c.Machine != "onb-cli" {
		t.Fatalf("master again %+v", c)
	}
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"machine":"onb-own"}`, "127.0.0.1:1", false), 200, "")
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"machine":"onb-nowhere"}`, "127.0.0.1:1", false), 400, "unknown_machine")
}

// `personad hub pair --machine <id> --add --name <name>`: the add request, the link alone on stdout, "Added" on stderr;
// without --add, a computer the hub does not have says to add --add.
func TestPairMachineCLIAdd(t *testing.T) {
	var got map[string]any
	expires := time.Now().Add(pairLifetime)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		if _, ok := got["add"]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "没有", "code": "unknown_machine"})
			return
		}
		p := pairCode{Token: "jp_x", ExpiresAt: expires, URL: "https://hub.example.com", Machine: "study"}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": p.Token, "expires_at": p.ExpiresAt, "url": p.URL, "machine": p.Machine,
			"name": "书房", "trust": "confirmed", "added": true})
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cfg := &Config{Listen: ":" + port, Token: "master-secret"}
	var out, help bytes.Buffer
	if err := cfg.printLinkPair(context.Background(), "study", true, "书房", &out, &help); err != nil {
		t.Fatal(err)
	}
	add, _ := got["add"].(map[string]any)
	if add["id"] != "study" || add["name"] != "书房" {
		t.Fatalf("request %v", got)
	}
	if !strings.HasPrefix(out.String(), "persona://link?") || strings.Count(out.String(), "\n") != 1 || !strings.Contains(help.String(), "已添加：书房") {
		t.Fatalf("out %q help %q", out.String(), help.String())
	}
	err := cfg.printLinkPair(context.Background(), "study", false, "", &out, &help)
	if err == nil || !strings.Contains(err.Error(), "--add") {
		t.Fatalf("without --add: %v", err)
	}

	// A hub that does not know "add" answers an app's code (no machine): not printed as the computer's link.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "jp_app", "expires_at": expires, "url": "https://hub.example.com"})
	}))
	defer old.Close()
	_, oldPort, _ := net.SplitHostPort(old.Listener.Addr().String())
	out.Reset()
	cfg = &Config{Listen: ":" + oldPort, Token: "master-secret"}
	err = cfg.printLinkPair(context.Background(), "study", true, "书房", &out, &help)
	if err == nil || !strings.Contains(err.Error(), "升级") || out.Len() != 0 {
		t.Fatalf("old hub: err %v out %q", err, out.String())
	}
}

// A device asking a pending computer's code again revokes what the earlier code gave: whatever traded it (a forwarded
// code, another computer) is out before the new one comes in. A bad name is refused before anything is written.
func TestComputerCodeAgainRevokesTheEarlier(t *testing.T) {
	h, _, tok := addHub(t)
	c1 := askCode(t, h, tok, `{"add":{"name":"小林的 Onb Laptop"}}`, 200, "")
	code, first := call(t, h.routes(), "POST", "/api/v1/auth/tokens", c1.Token, `{"kind":"link"}`)
	if code != 200 {
		t.Fatalf("redeem 1: %d", code)
	}
	firstTok := first["token"].(string)
	if _, ok := h.authenticate(context.Background(), firstTok); !ok {
		t.Fatal("first link token not accepted")
	}
	// Same name (it has not connected): its code again, and the first credential is gone.
	c2 := askCode(t, h, tok, `{"add":{"name":"小林的 Onb Laptop"}}`, 200, "")
	if c2.Machine != "onb-laptop" || c2.Added {
		t.Fatalf("again %+v", c2)
	}
	if _, ok := h.authenticate(context.Background(), firstTok); ok {
		t.Fatal("the first link token still works after a new code")
	}
	code, second := call(t, h.routes(), "POST", "/api/v1/auth/tokens", c2.Token, `{"kind":"link"}`)
	if code != 200 {
		t.Fatalf("redeem 2: %d", code)
	}
	// {"machine"} again: the second one is revoked too.
	askCode(t, h, tok, `{"machine":"onb-laptop"}`, 200, "")
	if _, ok := h.authenticate(context.Background(), second["token"].(string)); ok {
		t.Fatal("the second link token still works after {machine}")
	}

	before := h.pendingCount()
	askCode(t, h, tok, `{"add":{"name":"`+strings.Repeat("长", 41)+`"}}`, 400, "invalid_name")
	askCode(t, h, tok, `{"add":{"name":"a\tb"}}`, 400, "invalid_name")
	if h.pendingCount() != before {
		t.Fatal("a bad name took a pending slot")
	}
}

// The other paired devices hear of a code a device made for a computer (not the device itself), and of a pending
// computer's Persona Link trading it: the one to confirm.
func TestComputerCodePushes(t *testing.T) {
	h, _, tok := addHub(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) == nil {
			payload["to"] = strings.TrimPrefix(r.URL.Path, "/3/device/")
			sent <- payload
		}
	}))
	t.Cleanup(srv.Close)
	h.push.direct = apns.NewWithKey(apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key)
	h.push.direct.Base = srv.URL
	phoneID := principalFor(t, h, tok).ID
	h.push.devices["mine"] = Device{DeviceID: "mine", Name: "小林的手机", APNSToken: "self-token", Credential: phoneID}
	h.push.devices["other"] = Device{DeviceID: "other", Name: "小林的 iPad", APNSToken: "other-token"}

	c := askCode(t, h, tok, `{"add":{"name":"小林的 Onb Laptop"}}`, 200, "")
	body := alertTo(t, sent, "other-token")
	if body != "小林的手机 给电脑「小林的 Onb Laptop」出了一个配对码" {
		t.Fatalf("code push %q", body)
	}
	// The computer's Persona Link trades the code: every device hears it wants to join.
	code, _ := call(t, h.routes(), "POST", "/api/v1/auth/tokens", c.Token, `{"kind":"link"}`)
	if code != 200 {
		t.Fatalf("redeem %d", code)
	}
	for range 2 {
		select {
		case p := <-sent:
			if b := p["aps"].(map[string]any)["alert"].(map[string]any)["body"]; b != "新电脑「小林的 Onb Laptop」想加入：打开 App 确认是不是你的" {
				t.Fatalf("joining push %v", b)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("joining push not sent to both")
		}
	}
}

// alertTo waits for the push to the device with that APNs token and is its body; a push to another one fails.
func alertTo(t *testing.T, sent chan map[string]any, token string) string {
	t.Helper()
	select {
	case p := <-sent:
		if p["to"] != token {
			t.Fatalf("pushed to %v, want only %s", p["to"], token)
		}
		return p["aps"].(map[string]any)["alert"].(map[string]any)["body"].(string)
	case <-time.After(5 * time.Second):
		t.Fatal("no push")
	}
	return ""
}

func principalFor(t *testing.T, h *Hub, tok string) principal {
	t.Helper()
	p, ok := h.authenticate(context.Background(), tok)
	if !ok {
		t.Fatal("token not accepted")
	}
	return p
}

// The admin page adds through the same function: confirmed (no trust row), in hub.json and the manager's.
func TestAdminAddUsesTheSameFunction(t *testing.T) {
	h, _, _ := addHub(t)
	mc := MachineConfig{ID: "onb-b", Kind: kindLink, Icon: "weird"}
	if err := h.addMachine(&mc, "admin"); err != nil {
		t.Fatal(err)
	}
	if mc.Name != "onb-b" || mc.Icon != "laptop" || h.cfgEntry("onb-b") == nil || h.machinePending("onb-b") {
		t.Fatalf("added %+v", mc)
	}
	b, _ := os.ReadFile(filepath.Join(h.cfg.dir, "hub.json"))
	if !strings.Contains(string(b), `"onb-b"`) {
		t.Fatal("not in hub.json")
	}
	if err := h.addMachine(&mc, "admin"); err == nil || !strings.Contains(err.Error(), "已经有") {
		t.Fatalf("again: %v", err)
	}
}

// adminMachinesRequest calls the admin page's computer routes as a signed-in admin (admin_test.go tests the sign-in).
func adminMachinesRequest(h *Hub, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.adminMachineRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

// Deleting a computer on the admin page revokes its Persona Link credentials and unused pairing codes, as rejecting a
// pending one does, and the audit says so.
func TestAdminDeleteRevokesTheComputer(t *testing.T) {
	h, database, _ := addHub(t)
	ctx := context.Background()
	token, _ := linkToken(t, h, "onb-own")
	code, err := h.newPairCode(ctx, "onb-own")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.authenticate(ctx, token); !ok {
		t.Fatal("its credential does not work before the delete")
	}
	if w := adminMachinesRequest(h, "DELETE", "/api/v1/admin/machines/onb-own", ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if slices.Contains(hubJSONIDs(t, h), "onb-own") {
		t.Fatal("still in hub.json")
	}
	if _, ok := h.authenticate(ctx, token); ok {
		t.Fatal("its Persona Link credential still works after the delete")
	}
	if _, ok := h.authenticate(ctx, code.Token); ok {
		t.Fatal("an unused pairing code for it still works after the delete")
	}
	audit, err := database.MachineAudits(ctx, "onb-own")
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit %v %v", audit, err)
	}
	last := audit[len(audit)-1]
	var detail map[string]any
	_ = json.Unmarshal(last.Detail, &detail)
	if n, _ := detail["credentials_revoked"].(float64); last.Action != "remove" || last.Actor != "admin" || n < 1 {
		t.Fatalf("audit %+v %s", last, last.Detail)
	}
}

// Deleting a computer still waiting to be confirmed removes it as rejecting it does: no pending row or place is left
// to expire or to confirm later.
func TestAdminDeleteOfAPendingComputer(t *testing.T) {
	h, database, phone := addHub(t)
	ctx := context.Background()
	if c := askCode(t, h, phone, `{"add":{"name":"onb c"}}`, 200, ""); c.Machine != "onb-c" || c.Trust != "pending" {
		t.Fatalf("device add: %+v", c)
	}
	token, _ := linkToken(t, h, "onb-c")
	if w := adminMachinesRequest(h, "DELETE", "/api/v1/admin/machines/onb-c", ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if h.machinePending("onb-c") || h.cfgEntry("onb-c") != nil || slices.Contains(hubJSONIDs(t, h), "onb-c") {
		t.Fatal("still pending or still there")
	}
	list, err := database.PendingMachines(ctx)
	if err != nil || slices.ContainsFunc(list, func(p db.PendingMachine) bool { return p.ID == "onb-c" }) {
		t.Fatalf("its pending row stays: %v %v", list, err)
	}
	if _, ok := h.authenticate(ctx, token); ok {
		t.Fatal("its Persona Link credential still works after the delete")
	}
	audit, err := database.MachineAudits(ctx, "onb-c")
	if err != nil || len(audit) == 0 || audit[len(audit)-1].Action != "remove" || audit[len(audit)-1].Actor != "admin" {
		t.Fatalf("audit %+v %v", audit, err)
	}
}

// Adding a link computer, on the admin page, with the master token or from a device, revokes the Persona Link
// credentials and pairing codes left under its id by a computer removed before: they would connect as the new one. A
// computer there is keeps its credential when an add of its id is refused or used again, or it is given another
// pairing code.
func TestAddingALinkComputerRevokesWhatWasLeft(t *testing.T) {
	h, _, phone := addHub(t)
	ctx := context.Background()
	left := func(id string) []string {
		t.Helper()
		token, _ := linkToken(t, h, id)
		code, err := h.newPairCode(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return []string{token, code.Token}
	}
	gone := func(what string, tokens []string) {
		t.Helper()
		for _, tok := range tokens {
			if _, ok := h.authenticate(ctx, tok); ok {
				t.Fatalf("%s: what was left under its id still works", what)
			}
		}
	}

	leftovers := left("onb-a")
	if w := adminMachinesRequest(h, "POST", "/api/v1/admin/machines", `{"kind":"link","id":"onb-a"}`); w.Code != http.StatusOK {
		t.Fatalf("admin add: %d %s", w.Code, w.Body.String())
	}
	gone("admin add", leftovers)

	leftovers = left("onb-cli")
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"add":{"id":"onb-cli"}}`, "127.0.0.1:1", false)
	requireCode(t, w, 200, "")
	gone("master add", leftovers)
	var c computerCode
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	if _, ok := h.authenticate(ctx, c.Token); !ok || !c.Added {
		t.Fatalf("the new computer's own code: %+v", c)
	}

	leftovers = left("onb-dev")
	if c := askCode(t, h, phone, `{"add":{"name":"Onb Dev","id":"onb-dev"}}`, 200, ""); !c.Added {
		t.Fatalf("device add: %+v", c)
	}
	gone("device add", leftovers)

	own, _ := linkToken(t, h, "onb-own")
	if w := adminMachinesRequest(h, "POST", "/api/v1/admin/machines", `{"kind":"link","id":"onb-own"}`); w.Code != http.StatusConflict {
		t.Fatalf("admin add of a computer there is: %d %s", w.Code, w.Body.String())
	}
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"add":{"id":"onb-own"}}`, "127.0.0.1:1", false), 200, "")
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"machine":"onb-own"}`, "127.0.0.1:1", false), 200, "")
	if w := adminMachinesRequest(h, "POST", "/api/v1/admin/machines/onb-own/link-pair", ""); w.Code != http.StatusOK {
		t.Fatalf("pairing link: %d %s", w.Code, w.Body.String())
	}
	if _, ok := h.authenticate(ctx, own); !ok {
		t.Fatal("a computer there is lost its credential")
	}
}

func TestMachineSlug(t *testing.T) {
	for _, tc := range []struct{ name, os, want string }{
		{"小林的 MacBook", "mac", "macbook"},
		{"MacBook Pro (2024)", "", "macbook-pro-2024"},
		{"书房", "windows", "windows"},
		{"书房", "", "computer"},
		{"--x__y--", "", "x-y"},
		{strings.Repeat("a", 80), "", strings.Repeat("a", 40)},
	} {
		if got := machineSlug(tc.name, tc.os); got != tc.want || !adminMachineID.MatchString(got) {
			t.Fatalf("%q: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// rosterIdents is a roster in memory (keptIdents) with one manager still being installed.
type rosterIdents struct {
	*keptIdents
	installing string
}

func (s *rosterIdents) PendingManagerInstalls(context.Context) ([]db.ManagerInstallCode, error) {
	return []db.ManagerInstallCode{{ManagerID: s.installing, Name: "Waiting", ExpiresAt: time.Now().Add(time.Hour)}}, nil
}

// owners is which manager carries which of hub.json's machines, as the hub wrote it (machineOwnersKey).
func (s *rosterIdents) owners(t *testing.T) map[string]string {
	t.Helper()
	m := map[string]string{}
	if _, err := s.Setting(context.Background(), machineOwnersKey, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const (
	addKeeper  = "m-0a0a0a" // online, keeps its own list
	addAway    = "m-0f0f0f" // in the roster, not connected
	addWaiting = "m-0c0c0c" // still being installed
)

// managersAddHub is addHub with a roster of its own: addKeeper connected and keeping its own list, addAway never
// connected, addWaiting being installed.
func managersAddHub(t *testing.T) (*Hub, string, *rosterIdents, *fakeKeeper, *httptest.Server) {
	t.Helper()
	h, _, tok := addHub(t)
	ctx := context.Background()
	idents := &rosterIdents{keptIdents: &keptIdents{ids: &memIdents{}, kept: map[string]json.RawMessage{}}, installing: addWaiting}
	h.bus.idents = idents
	if err := h.loadMachineOwners(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.loadMachinesKept(ctx); err != nil {
		t.Fatal(err)
	}
	srv := newHubServer(t, h)
	keeper := connectKeeper(t, h, srv, addKeeper)
	busToken(t, h, addAway, "Away")
	return h, tok, idents, keeper, srv
}

// handed: the hand-over to manager o carries machine mid.
func handed(h *Hub, o, mid string) bool {
	return slices.ContainsFunc(h.devicesConfigFor(o).Machines, func(raw json.RawMessage) bool { return machineEntryID(raw) == mid })
}

// add.manager: the computer is that manager's from the start, never the carrier's first. An online manager that keeps
// its own list has it there; an offline one has it waiting in hub.json, with its owner recorded (a restart keeps it).
// A manager the roster lacks or one still being installed is refused before anything is written. Without the field,
// or naming the carrier, the computer is the carrier's as before. The reply's manager is who has it.
func TestDeviceAddsAComputerToAManager(t *testing.T) {
	h, tok, idents, keeper, _ := managersAddHub(t)
	ctx := context.Background()

	c := askCode(t, h, tok, `{"add":{"name":"Onb Keep","manager":"`+addKeeper+`"}}`, 200, "")
	if c.Machine != "onb-keep" || !c.Added || c.Trust != "pending" || c.Manager != addKeeper || h.entryOwner("onb-keep") != addKeeper {
		t.Fatalf("to a keeper: %+v, owner %s", c, h.entryOwner("onb-keep"))
	}
	if keeper.has("onb-keep") == nil || h.keptBy("onb-keep") != addKeeper || slices.Contains(hubJSONIDs(t, h), "onb-keep") ||
		handed(h, devicesOwner, "onb-keep") || !h.machinePending("onb-keep") {
		t.Fatalf("not in the keeper's own list alone: keeper %s, kept by %q, hub.json %v", keeper.has("onb-keep"), h.keptBy("onb-keep"), hubJSONIDs(t, h))
	}

	c = askCode(t, h, tok, `{"add":{"name":"Onb Away","manager":"`+addAway+`"}}`, 200, "")
	if c.Machine != "onb-away" || !c.Added || c.Manager != addAway || h.entryOwner("onb-away") != addAway {
		t.Fatalf("to an offline manager: %+v", c)
	}
	if !slices.Contains(hubJSONIDs(t, h), "onb-away") || idents.owners(t)["onb-away"] != addAway ||
		!handed(h, addAway, "onb-away") || handed(h, devicesOwner, "onb-away") {
		t.Fatalf("not waiting in hub.json as %s's: hub.json %v, owners %v", addAway, hubJSONIDs(t, h), idents.owners(t))
	}
	disk, err := h.diskConfig()
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTestHub(t)
	restarted.cfg.Machines, restarted.bus.idents = disk.Machines, idents
	if err := restarted.loadMachineOwners(ctx); err != nil {
		t.Fatal(err)
	}
	if restarted.entryOwner("onb-away") != addAway || restarted.entryOwner("onb-own") != devicesOwner {
		t.Fatal("a restart lost who the computer belongs to")
	}

	// Refused before anything is written: no entry, no pending row, no owner reserved.
	pending := h.pendingCount()
	for _, tc := range []struct {
		body, id string
		status   int
		code     string
		text     string
	}{
		{`{"add":{"name":"Onb None","manager":"m-0d0d0d"}}`, "onb-none", 404, "no_manager", "没有这个助手"},
		{`{"add":{"name":"Onb Wait","manager":"` + addWaiting + `"}}`, "onb-wait", 409, "installing", "这个助手还没装好，装好后再添加电脑"},
	} {
		w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tok, tc.body, "198.51.100.1:1", true)
		requireCode(t, w, tc.status, tc.code)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if out["error"] != tc.text {
			t.Fatalf("%s: %v", tc.code, out)
		}
		if h.knownEntry(tc.id) != nil || slices.Contains(hubJSONIDs(t, h), tc.id) || keeper.has(tc.id) != nil || h.machinePending(tc.id) {
			t.Fatalf("%s: the computer was written", tc.code)
		}
		if _, reserved := idents.owners(t)[tc.id]; reserved {
			t.Fatalf("%s: an owner was reserved", tc.code)
		}
		rows, err := h.trustStore().PendingMachines(ctx)
		if err != nil || slices.ContainsFunc(rows, func(p db.PendingMachine) bool { return p.ID == tc.id }) {
			t.Fatalf("%s: a pending row: %v %v", tc.code, rows, err)
		}
	}
	if h.pendingCount() != pending {
		t.Fatal("a refusal took a pending slot")
	}

	// No manager, or the carrier named: the carrier's, as before the field.
	owners := len(idents.owners(t))
	c = askCode(t, h, tok, `{"add":{"name":"Onb Plain"}}`, 200, "")
	if c.Machine != "onb-plain" || !c.Added || c.Manager != devicesOwner || h.entryOwner("onb-plain") != devicesOwner {
		t.Fatalf("without a manager: %+v", c)
	}
	if _, err := h.confirmMachine(ctx, "onb-keep", "test"); err != nil { // room for one more pending
		t.Fatal(err)
	}
	c = askCode(t, h, tok, `{"add":{"name":"Onb Carrier","manager":"`+devicesOwner+`"}}`, 200, "")
	if c.Machine != "onb-carrier" || !c.Added || c.Manager != devicesOwner || h.entryOwner("onb-carrier") != devicesOwner {
		t.Fatalf("the carrier named: %+v", c)
	}
	for _, id := range []string{"onb-plain", "onb-carrier"} {
		if !slices.Contains(hubJSONIDs(t, h), id) || h.cfgEntry(id) == nil || !handed(h, devicesOwner, id) || keeper.has(id) != nil {
			t.Fatalf("%s is not the carrier's hub.json computer", id)
		}
	}
	if len(idents.owners(t)) != owners {
		t.Fatalf("an owner recorded for a carrier's computer: %v", idents.owners(t))
	}
}

// The same name again is the same computer (it never connected): named for another manager, it moves there first,
// and the earlier code is void. Without the field it stays where it is, and the reply says where. One that another
// manager keeps in its own list, while that manager is away, cannot move: refused, nothing changed.
func TestAComputerUsedAgainMovesToItsManager(t *testing.T) {
	h, tok, idents, keeper, _ := managersAddHub(t)
	ctx := context.Background()
	void := func(what, token string) {
		t.Helper()
		if _, ok := h.authenticate(ctx, token); ok {
			t.Fatalf("%s: the earlier code still works", what)
		}
	}

	c1 := askCode(t, h, tok, `{"add":{"name":"Onb Move"}}`, 200, "")
	if c1.Machine != "onb-move" || c1.Manager != devicesOwner {
		t.Fatalf("first: %+v", c1)
	}
	c2 := askCode(t, h, tok, `{"add":{"name":"Onb Move","manager":"`+addAway+`"}}`, 200, "")
	if c2.Machine != "onb-move" || c2.Added || c2.Manager != addAway || h.entryOwner("onb-move") != addAway {
		t.Fatalf("to an offline manager: %+v", c2)
	}
	void("moved to an offline manager", c1.Token)
	if idents.owners(t)["onb-move"] != addAway || handed(h, devicesOwner, "onb-move") || !handed(h, addAway, "onb-move") {
		t.Fatalf("not %s's: owners %v", addAway, idents.owners(t))
	}

	c3 := askCode(t, h, tok, `{"add":{"name":"Onb Move","manager":"`+addKeeper+`"}}`, 200, "")
	if c3.Machine != "onb-move" || c3.Added || c3.Manager != addKeeper || h.entryOwner("onb-move") != addKeeper {
		t.Fatalf("to a keeper: %+v", c3)
	}
	void("moved to a keeper", c2.Token)
	eventually(t, 10*time.Second, "the keeper put it in its own list", func() bool { return keeper.has("onb-move") != nil })

	// No manager: where it is, as before the field.
	c4 := askCode(t, h, tok, `{"add":{"name":"Onb Move"}}`, 200, "")
	if c4.Machine != "onb-move" || c4.Manager != addKeeper || h.entryOwner("onb-move") != addKeeper {
		t.Fatalf("again without a manager: %+v", c4)
	}
	// The carrier named: back to it.
	c5 := askCode(t, h, tok, `{"add":{"name":"Onb Move","manager":"`+devicesOwner+`"}}`, 200, "")
	if c5.Manager != devicesOwner || h.entryOwner("onb-move") != devicesOwner || keeper.has("onb-move") != nil || !handed(h, devicesOwner, "onb-move") {
		t.Fatalf("back to the carrier: %+v, keeper %s", c5, keeper.has("onb-move"))
	}
	if _, reserved := idents.owners(t)["onb-move"]; reserved {
		t.Fatalf("still recorded as another's: %v", idents.owners(t))
	}

	// Kept by a manager that is away: it cannot be moved now.
	kept := askCode(t, h, tok, `{"add":{"name":"Onb Kept","manager":"`+addKeeper+`"}}`, 200, "")
	if kept.Manager != addKeeper || h.keptBy("onb-kept") != addKeeper {
		t.Fatalf("kept: %+v", kept)
	}
	keeper.stop()
	eventually(t, 15*time.Second, "the keeper gone", func() bool { return h.busConnOf(addKeeper) == nil })
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tok, `{"add":{"name":"Onb Kept","manager":"`+addAway+`"}}`, "198.51.100.1:1", true)
	requireCode(t, w, 503, "assistant_offline")
	if h.keptBy("onb-kept") != addKeeper || h.entryOwner("onb-kept") != addKeeper {
		t.Fatal("moved with its keeper away")
	}
	if _, ok := h.authenticate(ctx, kept.Token); !ok {
		t.Fatal("a refused request voided the computer's code")
	}
}

// A manager removed after a computer was added to it: one waiting in hub.json goes back to the carrier; one in its own
// list keeps the manager from being removed until the computers are dropped on purpose, and then goes with it (as any
// computer in a manager's own list: "Revoking"), pending no more: its place among the pending, its pairing code and
// its Persona Link credential go as a rejected one's do.
func TestAComputerAddedToAManagerThatIsRemoved(t *testing.T) {
	h, tok, idents, keeper, srv := managersAddHub(t)
	ctx := context.Background()
	if c := askCode(t, h, tok, `{"add":{"name":"Onb Away","manager":"`+addAway+`"}}`, 200, ""); c.Manager != addAway {
		t.Fatalf("away: %+v", c)
	}
	kept := askCode(t, h, tok, `{"add":{"name":"Onb Keep","manager":"`+addKeeper+`"}}`, 200, "")
	if kept.Manager != addKeeper || h.keptBy("onb-keep") != addKeeper {
		t.Fatalf("keeper: %+v", kept)
	}
	keptLink, _ := linkToken(t, h, "onb-keep") // as if its Persona Link had traded a code
	if idents.owners(t)["onb-keep"] != addKeeper {
		t.Fatalf("no reservation beside the keeper's list: %v", idents.owners(t))
	}

	if code, out := ownershipGet(t, h, srv, "DELETE", "/api/v1/managers/"+addAway, nil); code != 200 {
		t.Fatalf("remove %s: %d %v", addAway, code, out)
	}
	if h.entryOwner("onb-away") != devicesOwner || !handed(h, devicesOwner, "onb-away") || !h.machinePending("onb-away") {
		t.Fatal("not back to the carrier, still waiting to be confirmed")
	}
	if _, reserved := idents.owners(t)["onb-away"]; reserved {
		t.Fatalf("the removed manager is still recorded: %v", idents.owners(t))
	}

	if code, out := ownershipGet(t, h, srv, "DELETE", "/api/v1/managers/"+addKeeper, nil); code != 409 || out["code"] != "keeps_machines" {
		t.Fatalf("remove a keeper of it: %d %v", code, out)
	}
	if h.entryOwner("onb-keep") != addKeeper || keeper.has("onb-keep") == nil {
		t.Fatal("a refused removal let it go")
	}
	if code, out := ownershipGet(t, h, srv, "DELETE", "/api/v1/managers/"+addKeeper+"?drop_machines=1", nil); code != 200 {
		t.Fatalf("remove it with its computers: %d %v", code, out)
	}
	if h.knownEntry("onb-keep") != nil || h.keptBy("onb-keep") != "" || handed(h, devicesOwner, "onb-keep") {
		t.Fatal("dropped with its manager, still known or handed to the carrier")
	}
	if _, reserved := idents.owners(t)["onb-keep"]; reserved {
		t.Fatalf("its reservation stays: %v", idents.owners(t))
	}
	if h.machinePending("onb-keep") || h.pendingCount() != 1 {
		t.Fatalf("it still takes a pending place: %d pending", h.pendingCount())
	}
	rows, err := h.trustStore().PendingMachines(ctx)
	if err != nil || slices.ContainsFunc(rows, func(p db.PendingMachine) bool { return p.ID == "onb-keep" }) {
		t.Fatalf("its pending row stays: %v %v", rows, err)
	}
	for what, token := range map[string]string{"its Persona Link credential": keptLink, "its pairing code": kept.Token} {
		if _, ok := h.authenticate(ctx, token); ok {
			t.Fatalf("%s still works", what)
		}
	}
	if !h.machinePending("onb-away") {
		t.Fatal("the carrier's pending computer was dropped too")
	}
}

// sentPending is the pending list of the last hand-over the hub actually sent manager id.
func sentPending(t *testing.T, h *Hub, id string) []string {
	t.Helper()
	h.devices.mu.Lock()
	b := h.devices.sent[id]
	h.devices.mu.Unlock()
	var cfg devicewire.Config
	if len(b) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			t.Fatal(err)
		}
	}
	return cfg.Pending
}

// A keeper slower to put the computer in its own list than the hand-over's 200 ms wait is still told the computer is
// pending: the hand-over sent while the put was under way had it as the carrier's, and the one after the put has it
// as the keeper's (else the keeper would give an unconfirmed computer its launchers' keys).
func TestASlowKeeperIsToldTheComputerIsPending(t *testing.T) {
	h, tok, _, keeper, _ := managersAddHub(t)
	keeper.mu.Lock()
	keeper.putDelay = 500 * time.Millisecond
	keeper.mu.Unlock()
	c := askCode(t, h, tok, `{"add":{"name":"Onb Keep","manager":"`+addKeeper+`"}}`, 200, "")
	if c.Manager != addKeeper || !h.machinePending("onb-keep") || keeper.has("onb-keep") == nil {
		t.Fatalf("added: %+v", c)
	}
	eventually(t, 5*time.Second, "the keeper sent the computer as pending", func() bool {
		return slices.Contains(sentPending(t, h, addKeeper), "onb-keep")
	})
}
