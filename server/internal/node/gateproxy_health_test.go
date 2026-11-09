package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

// sendAllowed is the kind of rule that stops confirming sends in a conversation (docs/assistant/assistant.md "Lark: skill supplied by the Lark extension", Gate).
var sendAllowed = proto.LarkRule{Method: "POST", Path: "/conversations/*/messages", Action: "allow"}

// spaceRig is a node on a fake hub, with the proxied example gate (the given rules first) and a phone that says yes to
// every card. processIn starts an assistant process the way the node does, so that the address it gets at the gate
// proxy is the one a real process would.
type spaceRig struct {
	t     *testing.T
	n     *Node
	api   *fakeAPI
	hub   *scriptHub
	state string // the state directory (.persona) a conversation's directory is in
}

func newSpaceRig(t *testing.T, user ...proto.LarkRule) *spaceRig {
	t.Helper()
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	api := newFakeAPI(t)
	hub := newScriptHub(t, func(context.Context, proto.Interaction) (any, error) {
		return proto.Resolution{Allow: boolp(true)}, nil
	}, notifyOK)
	n := hub.n
	n.mu.Lock()
	n.dir = t.TempDir()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL, user...)}}, time.Now()
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	return &spaceRig{t: t, n: n, api: api, hub: hub, state: filepath.Join(t.TempDir(), ".persona")}
}

// skillIn puts the skill of every gate the node has in cwd, where assistantLarkEnv looks for its script.
func (r *spaceRig) skillIn(cwd string) {
	r.t.Helper()
	r.n.mu.Lock()
	gates := append([]proto.CommandGate(nil), r.n.larkCfg.Gates...)
	r.n.mu.Unlock()
	for _, g := range gates {
		script := filepath.Join(cwd, ".claude", "skills", filepath.FromSlash(g.Script))
		if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(script, []byte("#!/bin/bash\n"), 0o755); err != nil {
			r.t.Fatal(err)
		}
	}
}

// addGate gives the node another command gate, as a second extension would.
func (r *spaceRig) addGate(g proto.CommandGate) {
	r.n.mu.Lock()
	defer r.n.mu.Unlock()
	r.n.larkCfg.Gates = append(r.n.larkCfg.Gates, g)
}

// processIn starts an assistant process for session in cwd, which has the gates' skills; base is its own address at
// the example gate's proxy.
func (r *spaceRig) processIn(cwd, session string) (base string) {
	r.t.Helper()
	r.skillIn(cwd)
	env, release := r.n.assistantLarkEnv(cwd, session)
	r.t.Cleanup(release)
	base, ok := envOf(env, "EXAMPLE_BASE")
	if !ok {
		r.t.Fatalf("no address at the gate proxy in the process's environment: %v", env)
	}
	return base
}

// processNamed is processIn for a process whose conversation has no id yet (a new Codex conversation): name gives it
// one later. env is what the process got, with the address of each gate.
func (r *spaceRig) processNamed(cwd string) (env []string, name func(id string)) {
	r.t.Helper()
	r.skillIn(cwd)
	env, release, name := r.n.assistantLarkEnvNamed(cwd, "")
	r.t.Cleanup(release)
	return env, name
}

// open is openIn for a process outside the health space, for the tests that are not about the space.
func (p *gateProxy) open(command, session string) (base string, release func(), err error) {
	return p.openIn(command, session, false)
}

// cards is how many cards the phone was asked.
func (r *spaceRig) cards() int {
	r.hub.mu.Lock()
	defer r.hub.mu.Unlock()
	return len(r.hub.created)
}

// check runs command through the gate as the node does for a Bash call of the process's conversation, and says what it
// decided.
func (r *spaceRig) check(cwd, session, command string) string {
	r.t.Helper()
	ix := proto.Interaction{SessionID: session, ExpiresAt: time.Now().Add(time.Minute)}
	d, _, err := r.n.larkCheck(context.Background(), session, cwd, command, ix)
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

// A health conversation's Feishu writes are confirmed on the phone each time (docs/assistant/health.md "Outgoing"), also when a
// rule allows the route and the request reaches the gate proxy on its own instead of through the gated command: it is
// refused, the API is not asked, and no card is made.
func TestGateProxyHealthWriteWithAnAllowRuleNeedsTheCard(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	base := r.processIn(filepath.Join(r.state, identity.SpaceHealth), "h1")
	code, body := send(t, "POST", base+"/api/v1/conversations/7123/messages", `{"text":"hi"}`)
	if code != http.StatusForbidden || !strings.Contains(body, "手机上确认") {
		t.Fatalf("a write no card confirmed, in a health conversation: %d %s", code, body)
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Fatalf("the API got %+v", got)
	}
	if r.cards() != 0 {
		t.Fatalf("%d cards: the proxy does not ask the phone", r.cards())
	}
}

// What the phone confirmed goes through the proxy once, in a health conversation as anywhere: larkCheck asked, the
// grant it left is used up by the one request it was for, and the same call sent again is refused.
func TestGateProxyHealthWriteConfirmedOnThePhoneGoesOnce(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	cwd := filepath.Join(r.state, identity.SpaceHealth)
	base := r.processIn(cwd, "h1")
	if d := r.check(cwd, "h1", `ex POST /conversations/7123/messages '{"text":"hi"}'`); d != "allow" || r.cards() != 1 {
		t.Fatalf("the gated command: %q, %d cards", d, r.cards())
	}
	url := base + "/api/v1/conversations/7123/messages"
	if code, body := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write the phone confirmed: %d %s", code, body)
	}
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	if got := r.api.requests(); len(got) != 1 || got[0].Body != `{"text":"hi"}` {
		t.Fatalf("the API got %+v", got)
	}
}

// A health conversation's reads go as they do anywhere: no card, and the rules' allow is enough.
func TestGateProxyHealthReadsGoWithoutACard(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	base := r.processIn(filepath.Join(r.state, identity.SpaceHealth), "h1")
	for _, method := range []string{"GET", "HEAD"} {
		if code, body := send(t, method, base+"/api/v1/conversations/7123/messages?page=2", ""); code != http.StatusOK {
			t.Fatalf("%s in a health conversation: %d %s", method, code, body)
		}
	}
	got := r.api.requests()
	if len(got) != 2 || got[0].Method != "GET" || got[1].Method != "HEAD" || got[0].Query != "page=2" {
		t.Fatalf("the API got %+v", got)
	}
	if r.cards() != 0 {
		t.Fatalf("%d cards for reads", r.cards())
	}
}

// Outside the health space an allow rule still lets a write through unasked, as before. The proxy is shared: the
// conversations that run side by side each have their own address, and only a health conversation's holds its writes.
func TestGateProxyOutsideHealthAnAllowRuleStillLetsAWriteThrough(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	write := func(base string) int {
		t.Helper()
		code, _ := send(t, "POST", base+"/api/v1/conversations/7123/messages", `{"text":"hi"}`)
		return code
	}
	health := r.processIn(filepath.Join(r.state, identity.SpaceHealth), "h1")
	for name, cwd := range map[string]string{
		"the assistant's own space":     filepath.Join(r.state, identity.AssistantDirName("")),
		"another manager's space":       filepath.Join(r.state, identity.AssistantDirName("m-1a2b")),
		"a directory only named health": filepath.Join(t.TempDir(), identity.SpaceHealth),
	} {
		before := len(r.api.requests())
		if code := write(r.processIn(cwd, "s1")); code != http.StatusOK {
			t.Fatalf("%s: %d", name, code)
		}
		if got := len(r.api.requests()); got != before+1 {
			t.Fatalf("%s: the API got %d requests, want %d", name, got, before+1)
		}
		if code := write(health); code != http.StatusForbidden {
			t.Fatalf("beside %s, the health conversation's own address: %d", name, code)
		}
	}
	if r.cards() != 0 {
		t.Fatalf("%d cards", r.cards())
	}
}

// A rule's deny stays a deny in a health conversation, as outside it: refused for the rule, not for want of a card.
func TestGateProxyHealthDenyStaysDeny(t *testing.T) {
	r := newSpaceRig(t, proto.LarkRule{Method: "DELETE", Path: "/messages/*", Action: "deny"})
	for _, space := range []string{identity.SpaceHealth, identity.AssistantDirName("")} {
		base := r.processIn(filepath.Join(r.state, space), "s1")
		if code, body := send(t, "DELETE", base+"/api/v1/messages/M1", ""); code != http.StatusForbidden || !strings.Contains(body, "不允许") {
			t.Fatalf("%s: a call the rules deny: %d %s", space, code, body)
		}
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Fatalf("the API got %+v", got)
	}
}

// A write no rule allows is confirmed on the phone and goes once in a health conversation, as it did.
func TestGateProxyHealthWriteTheRulesLeaveToThePhoneGoesOnce(t *testing.T) {
	r := newSpaceRig(t)
	cwd := filepath.Join(r.state, identity.SpaceHealth)
	base := r.processIn(cwd, "h1")
	url := base + "/api/v1/notes/N1"
	if code, _ := send(t, "POST", url, `{"text":"n"}`); code != http.StatusForbidden {
		t.Fatalf("a write nobody confirmed: %d", code)
	}
	if d := r.check(cwd, "h1", `ex POST /notes/N1 '{"text":"n"}'`); d != "allow" || r.cards() != 1 {
		t.Fatalf("the gated command: %q, %d cards", d, r.cards())
	}
	if code, _ := send(t, "POST", url, `{"text":"n"}`); code != http.StatusOK {
		t.Fatalf("the write the phone confirmed: %d", code)
	}
	if code, _ := send(t, "POST", url, `{"text":"n"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	if got := r.api.requests(); len(got) != 1 {
		t.Fatalf("the API got %+v", got)
	}
}

// A health conversation's address takes a grant of its own conversation only: not one made for another conversation,
// nor one made for none, and an address that is not named (nameAddr) takes none (outside the health space it takes
// any, TestGateGrantsStayWithTheirConversation): when in doubt, the phone is asked again. Both for a route a rule allows
// and for one no rule matches, which goes by the other branch of the proxy.
func TestGateProxyHealthTakesOnlyItsOwnConversationsCard(t *testing.T) {
	for _, route := range []struct{ name, path, body string }{
		{"a route a rule allows", "/conversations/7123/messages", `{"text":"hi"}`},
		{"a route no rule matches", "/notes/N1", `{"text":"n"}`},
	} {
		t.Run(route.name, func(t *testing.T) {
			r := newSpaceRig(t, sendAllowed)
			cwd := filepath.Join(r.state, identity.SpaceHealth)
			own, nameless := r.processIn(cwd, "h1"), r.processIn(cwd, "")
			write := func(base string) int {
				t.Helper()
				code, _ := send(t, "POST", base+"/api/v1"+route.path, route.body)
				return code
			}
			call := larkCall{Method: "POST", Path: route.path, Body: route.body}
			card := proto.LarkLetThrough{Kind: proto.LarkThroughCard}
			p := r.n.gates()
			p.grant("ex", "h2", call, card) // another conversation's
			p.grant("ex", "", call, card)   // made for none
			for name, base := range map[string]string{"its own conversation's": own, "no conversation's": nameless} {
				if code := write(base); code != http.StatusForbidden {
					t.Fatalf("a process with %s address, with only grants made for others: %d", name, code)
				}
			}
			p.grant("ex", "h1", call, card)
			if code := write(nameless); code != http.StatusForbidden {
				t.Fatalf("a process with no conversation, with h1's grant there: %d", code)
			}
			if code := write(own); code != http.StatusOK {
				t.Fatalf("h1's own grant: %d", code)
			}
			if code := write(own); code != http.StatusForbidden {
				t.Fatalf("h1 again, with only the others' grants left: %d", code)
			}
			if got := r.api.requests(); len(got) != 1 {
				t.Fatalf("the API got %+v", got)
			}
		})
	}
}

// A resident node keeps a conversation's address from one connection to the next (assistantKeeper): the next
// connection's Node still holds a health conversation's writes to the phone, and lets through the one a card confirmed.
func TestKeptHealthConversationStillNeedsTheCardThroughTheNextConnection(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	keeper := newAssistantKeeper()
	r.n.keeper = keeper // before the first use of the proxy, which is the keeper's then
	base := r.processIn(filepath.Join(r.state, identity.SpaceHealth), "h1")
	next := &Node{dir: t.TempDir(), keeper: keeper, larkCfgAt: time.Now(), logger: log.New(io.Discard, "", 0),
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(r.api.URL, sendAllowed)}}}
	next.gates() // the proxy goes by the next connection's config from now on
	url := base + "/api/v1/conversations/7123/messages"
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("a write no card confirmed, after the connection changed: %d", code)
	}
	next.grantGateCalls(next.larkCfg, "h1", []larkCall{{Gate: 0, Method: "POST", Path: "/conversations/7123/messages", Body: `{"text":"hi"}`}},
		proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write the next connection's phone confirmed: %d", code)
	}
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	if got := r.api.requests(); len(got) != 1 {
		t.Fatalf("the API got %+v", got)
	}
}

// Every kind of write is held, not only POST: PUT, PATCH and DELETE that a rule allows are refused without a card too,
// and go once after one.
func TestGateProxyHealthHoldsEveryKindOfWrite(t *testing.T) {
	r := newSpaceRig(t,
		proto.LarkRule{Method: "PUT", Path: "/conversations/*/unread", Action: "allow"},
		proto.LarkRule{Method: "PATCH", Path: "/notes/*", Action: "allow"},
		proto.LarkRule{Method: "DELETE", Path: "/messages/*/reactions", Action: "allow"})
	cwd := filepath.Join(r.state, identity.SpaceHealth)
	base := r.processIn(cwd, "h1")
	for _, w := range []struct{ method, path, body string }{
		{"PUT", "/conversations/7123/unread", `{}`},
		{"PATCH", "/notes/N1", `{"text":"n"}`},
		{"DELETE", "/messages/M1/reactions", ""},
	} {
		url := base + "/api/v1" + w.path
		if code, body := send(t, w.method, url, w.body); code != http.StatusForbidden || !strings.Contains(body, "手机上确认") {
			t.Fatalf("%s %s that a rule allows, no card made: %d %s", w.method, w.path, code, body)
		}
		if got := r.api.requests(); len(got) != 0 {
			t.Fatalf("%s %s: the API got %+v", w.method, w.path, got)
		}
		command := "ex " + w.method + " " + w.path
		if w.body != "" {
			command += " '" + w.body + "'"
		}
		if d := r.check(cwd, "h1", command); d != "allow" {
			t.Fatalf("%s: the gated command: %q", command, d)
		}
		if code, body := send(t, w.method, url, w.body); code != http.StatusOK {
			t.Fatalf("%s %s that the phone confirmed: %d %s", w.method, w.path, code, body)
		}
		if code, _ := send(t, w.method, url, w.body); code != http.StatusForbidden {
			t.Fatalf("%s %s, sent again: %d", w.method, w.path, code)
		}
		if got := r.api.requests(); len(got) != 1 || got[0].Method != w.method {
			t.Fatalf("%s %s: the API got %+v", w.method, w.path, got)
		}
		r.api.mu.Lock()
		r.api.seen = nil
		r.api.mu.Unlock()
	}
}

// A new Codex conversation's process starts before its conversation has an id: its thread gives it (codexConn.start),
// and larkCheck keeps the card's grant under that id. The process's addresses stand for the thread from then on, so what
// the phone confirmed goes once; until then they take nothing.
func TestGateProxyHealthAddressesStandForTheThreadOnceItStarts(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	cwd := filepath.Join(r.state, identity.SpaceHealth)
	env, name := r.processNamed(cwd)
	base, _ := envOf(env, "EXAMPLE_BASE")
	url := base + "/api/v1/conversations/7123/messages"
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("a write before the thread exists: %d", code)
	}
	r.n.modes, r.n.akeys, r.n.metas, r.n.bgScans = map[string]sessionModes{}, map[string]*assistantProc{}, map[string]quickMeta{}, map[string]*bgScan{}
	p, c, f := newFakeCodex(t, r.n, cwd)
	c.nameGates = name
	f.results["thread/start"] = map[string]any{"thread": map[string]any{"id": testThread, "path": ""}}
	if err := c.start(context.Background(), "", cwd, false); err != nil {
		t.Fatal(err)
	}
	r.n.mu.Lock()
	thread := p.id
	r.n.mu.Unlock()
	if thread != testThread {
		t.Fatalf("the process's id is %q, want the thread's", thread)
	}
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("a write the thread has no card for: %d", code)
	}
	if d := r.check(cwd, thread, `ex POST /conversations/7123/messages '{"text":"hi"}'`); d != "allow" || r.cards() != 1 {
		t.Fatalf("the gated command, under the thread's id: %q, %d cards", d, r.cards())
	}
	if code, body := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write the phone confirmed: %d %s", code, body)
	}
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	if got := r.api.requests(); len(got) != 1 {
		t.Fatalf("the API got %+v", got)
	}
	// The thread is a health conversation from then on: after a `cd` (the hook runs elsewhere) a write is still asked
	// about although a rule allows it, and the card offers no "Always allow in this session".
	before := r.cards()
	if d := r.check(t.TempDir(), thread, `ex POST /conversations/7123/messages '{"text":"again"}'`); d != "allow" || r.cards() != before+1 {
		t.Fatalf("a write with the hook outside the health space: %q, %d cards (was %d)", d, r.cards(), before)
	}
	if keys := r.hub.lastCard().AlwaysKeys; len(keys) != 0 {
		t.Fatalf("the card offers Always allow: %q", keys)
	}
}

// A conversation's space is its process's own, not the directory a hook happens to run in: after the model's `cd`, into
// a subdirectory of the health space or out of it, a health conversation's writes are still confirmed each time, and
// the card offers no "Always allow in this session". A conversation outside the health space is asked as before.
func TestLarkCheckHealthConversationIsHealthWhereverTheHookRuns(t *testing.T) {
	r := newSpaceRig(t)
	health := filepath.Join(r.state, identity.SpaceHealth)
	r.processIn(health, "h1")
	r.processIn(filepath.Join(r.state, identity.AssistantDirName("")), "s1")
	write := `ex POST /notes/N1 '{"text":"n"}'`
	for name, hook := range map[string]string{
		"a subdirectory of the health space": filepath.Join(health, "reports"),
		"a directory outside it":             t.TempDir(),
	} {
		for i := 1; i <= 2; i++ {
			before := r.cards()
			if d := r.check(hook, "h1", write); d != "allow" || r.cards() != before+1 {
				t.Fatalf("hook in %s, write %d: %q, %d cards (was %d)", name, i, d, r.cards(), before)
			}
			if keys := r.hub.lastCard().AlwaysKeys; len(keys) != 0 {
				t.Fatalf("hook in %s: the card offers Always allow: %q", name, keys)
			}
		}
	}
	before := r.cards()
	if d := r.check(t.TempDir(), "s1", write); d != "allow" || r.cards() != before+1 || len(r.hub.lastCard().AlwaysKeys) == 0 {
		t.Fatalf("a conversation outside the health space: %q, %d cards (was %d), always keys %q", d, r.cards(), before, r.hub.lastCard().AlwaysKeys)
	}
}

// With a rule that allows sends, a health conversation whose hook runs in another directory is still asked, so the
// card, the grant and the proxy agree: the write goes once after the phone said yes, instead of being refused by the
// proxy for want of a card nobody asked for.
func TestLarkCheckHealthWriteAfterACdGoesOnceAfterTheCard(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	health := filepath.Join(r.state, identity.SpaceHealth)
	base := r.processIn(health, "h1")
	hook := filepath.Join(health, "reports")
	if d := r.check(hook, "h1", `ex POST /conversations/7123/messages '{"text":"hi"}'`); d != "allow" || r.cards() != 1 {
		t.Fatalf("the gated command, hook in a subdirectory: %q, %d cards", d, r.cards())
	}
	url := base + "/api/v1/conversations/7123/messages"
	if code, body := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write the phone confirmed: %d %s", code, body)
	}
	if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
}

// lastCard is the card the phone was asked last.
func (h *scriptHub) lastCard() proto.Interaction {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.created[len(h.created)-1]
}

// fakeCodexHome makes `codex` on the interactive shell's PATH (zsh or bash, as fakeClaudeHome does for claude) a
// stand-in for `codex app-server`: this test binary, run as TestFakeCodexAppServer. It answers the handshake and thread/start with the
// thread id given, and writes the address it was given at the example gate (EXAMPLE_BASE) to the file returned.
func fakeCodexHome(t *testing.T, thread string) (addrFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the launcher is a POSIX shell script") // Git Bash would need the test binary's path in its own form
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec " + shellQuote(exe) + " -test.run='^TestFakeCodexAppServer$'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o600)
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	addrFile = filepath.Join(home, "address")
	t.Setenv("PERSONA_FAKE_CODEX_THREAD", thread)
	t.Setenv("PERSONA_FAKE_CODEX_ADDRESS", addrFile)
	return addrFile
}

// TestFakeCodexAppServer is the program fakeCodexHome puts on PATH, not a test: it answers the app-server's handshake
// and thread/start, and ends when its input does.
func TestFakeCodexAppServer(t *testing.T) {
	thread := os.Getenv("PERSONA_FAKE_CODEX_THREAD")
	if thread == "" {
		return
	}
	_ = os.WriteFile(os.Getenv("PERSONA_FAKE_CODEX_ADDRESS"), []byte(os.Getenv("EXAMPLE_BASE")), 0o600)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.ID) == 0 || m.Method == "" {
			continue // a notification, or the node answering a request of ours
		}
		var result any = map[string]any{}
		if m.Method == "thread/start" {
			result = map[string]any{"thread": map[string]any{"id": thread, "path": ""}}
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		fmt.Println(string(b))
	}
	os.Exit(0)
}

// A new Codex conversation, through the real spawnCodex: its process is started before the conversation has an id, so
// the address it gets is opened with none, and the thread's id (from the app-server's thread/start) names it. What the
// phone confirmed then goes once and sent again is refused. The warm spare, started the same way and handed to a new
// conversation, is the same.
func TestGateProxyHealthNewCodexConversationGoesOnceAfterTheCard(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spare bool
	}{{"a new conversation", false}, {"the warm spare handed to one", true}} {
		t.Run(tc.name, func(t *testing.T) {
			const thread = "019a1930-7e92-7073-9bb8-d2ad3126f329"
			addrFile := fakeCodexHome(t, thread)
			r := newSpaceRig(t, sendAllowed)
			cwd := filepath.Join(r.state, identity.SpaceHealth)
			r.skillIn(cwd)
			r.n.mu.Lock()
			r.n.modes, r.n.akeys, r.n.metas, r.n.bgScans = map[string]sessionModes{}, map[string]*assistantProc{}, map[string]quickMeta{}, map[string]*bgScan{}
			r.n.mu.Unlock()
			p, err := r.n.spawnCodex("", cwd, "codex", false, tc.spare)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(p.stop)
			if tc.spare {
				r.n.mu.Lock()
				r.n.spare = p
				r.n.mu.Unlock()
				if p = r.n.adoptSpare(cwd, "codex"); p == nil {
					t.Fatal("the spare was not handed out")
				}
			}
			r.n.mu.Lock()
			id := p.id
			r.n.mu.Unlock()
			if id != thread {
				t.Fatalf("the conversation's id is %q, want the thread's", id)
			}
			base, err := os.ReadFile(addrFile)
			if err != nil || len(base) == 0 {
				t.Fatalf("the address the process was given: %q %v", base, err)
			}
			url := string(base) + "/api/v1/conversations/7123/messages"
			if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
				t.Fatalf("a write no card confirmed: %d", code)
			}
			if d := r.check(cwd, id, `ex POST /conversations/7123/messages '{"text":"hi"}'`); d != "allow" || r.cards() != 1 {
				t.Fatalf("the gated command, under the thread's id: %q, %d cards", d, r.cards())
			}
			if code, body := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
				t.Fatalf("the write the phone confirmed: %d %s", code, body)
			}
			if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
				t.Fatalf("the confirmed write, sent again: %d", code)
			}
			if got := r.api.requests(); len(got) != 1 {
				t.Fatalf("the API got %+v", got)
			}
		})
	}
}

// A process with more than one gated command has an address for each, and the thread's id names every one: what the
// phone confirmed goes through the address of the gate it was confirmed for, once.
func TestGateProxyHealthNamingNamesEveryAddress(t *testing.T) {
	r := newSpaceRig(t, sendAllowed)
	second := proxiedGate(r.api.URL, sendAllowed)
	second.Command, second.Title, second.Script = "ex2", "示例二", "example2/scripts/ex2.sh"
	second.Env = map[string]string{"EXAMPLE2_BASE": r.api.URL, "EXAMPLE2_TOKEN": "test-token-2"}
	second.BaseEnv, second.Secret, second.AllowedEnv = "EXAMPLE2_BASE", []string{"EXAMPLE2_TOKEN"}, nil
	second.Auth = &proto.GateAuth{Header: "Authorization", Value: "Bearer ${EXAMPLE2_TOKEN}"}
	r.addGate(second)
	cwd := filepath.Join(r.state, identity.SpaceHealth)
	env, name := r.processNamed(cwd)
	const thread = "019a1930-7e92-7073-9bb8-d2ad3126f330"
	name(thread)
	for i, g := range []struct{ command, baseEnv, token string }{{"ex", "EXAMPLE_BASE", "test-token"}, {"ex2", "EXAMPLE2_BASE", "test-token-2"}} {
		base, ok := envOf(env, g.baseEnv)
		if !ok {
			t.Fatalf("no address for %s in %v", g.command, env)
		}
		url := base + "/api/v1/conversations/7123/messages"
		if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
			t.Fatalf("%s: a write no card confirmed: %d", g.command, code)
		}
		if d := r.check(cwd, thread, g.command+` POST /conversations/7123/messages '{"text":"hi"}'`); d != "allow" || r.cards() != i+1 {
			t.Fatalf("%s: the gated command: %q, %d cards", g.command, d, r.cards())
		}
		if code, body := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusOK {
			t.Fatalf("%s: the write the phone confirmed: %d %s", g.command, code, body)
		}
		if code, _ := send(t, "POST", url, `{"text":"hi"}`); code != http.StatusForbidden {
			t.Fatalf("%s: the confirmed write, sent again: %d", g.command, code)
		}
		got := r.api.requests()
		if len(got) != i+1 || got[i].Auth != "Bearer "+g.token {
			t.Fatalf("%s: the API got %+v", g.command, got)
		}
	}
}
