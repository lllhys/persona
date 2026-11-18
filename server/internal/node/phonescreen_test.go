package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// phoneWorld is a Mac with Persona Input whose --preflight says `preflight`, and a fake helper behind phoneStart that
// answers each request with `answer` and records what it got.
type phoneWorld struct {
	node    *Node
	mu      sync.Mutex
	got     []map[string]any
	answer  func(req map[string]any) map[string]any
	eofs    atomic.Int32 // sessions whose socket the node closed
	starts  atomic.Int32
	granted bool
	signed  map[string]bool
	closers map[string][]io.Closer // what screenLogins.attach bound to each pair
	// records: the helper's restore file here; restores: how many times --phone-screen-restore ran.
	records  string
	restores atomic.Int32
}

func newPhoneWorld(t *testing.T, preflight string) *phoneWorld {
	t.Helper()
	w := &phoneWorld{granted: true, signed: map[string]bool{"P": true, "Q": true}, closers: map[string][]io.Closer{}}
	w.answer = func(req map[string]any) map[string]any {
		switch req["op"] {
		case "start":
			return map[string]any{"ok": true, "display": map[string]any{"id": 7, "x": 1920, "y": 0, "width": 540, "height": 1021, "scale": 2}}
		case "windows":
			return map[string]any{"ok": true, "windows": []any{map[string]any{"pid": 10, "window": 3, "app": "TextEdit", "title": "A"}}}
		case "place":
			return map[string]any{"ok": false, "error": "window_gone", "message": "gone"}
		}
		return map[string]any{"ok": true}
	}
	bin := t.TempDir()
	app := fakeInputBundle(t, t.TempDir(), "com.example.persona.link.input", "v1")
	swap(t, &screenCopyDir, func() string { return bin })
	swap(t, &inputHelperSource, func() string { return app })
	swap(t, &inputHelpers, &inputResolver{})
	swap(t, &screenSigner, func(string) (helperSigner, error) { return nil, nil })
	swap(t, &inputPermissions, &inputPermissionCache{ttl: time.Minute})
	swap(t, &inputPreflight, func(context.Context, string) string { return preflight })
	w.records = filepath.Join(t.TempDir(), "phone-screen.json")
	swap(t, &phoneStart, func(context.Context, string) (net.Conn, bool, error) {
		w.starts.Add(1)
		node, helper := net.Pipe()
		go w.serve(helper)
		return node, w.granted, nil
	})
	w.node = &Node{logger: log.New(io.Discard, "", 0)}
	w.node.phones = &phoneScreens{logf: t.Logf,
		signed:       func(pair string) bool { w.mu.Lock(); defer w.mu.Unlock(); return w.signed[pair] },
		restoreFile:  func() string { return w.records },
		restoreRun:   func(context.Context, string) error { w.restores.Add(1); return nil },
		restoreAfter: 10 * time.Millisecond,
		attach: func(pair string, c io.Closer) bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			if !w.signed[pair] {
				return false
			}
			w.closers[pair] = append(w.closers[pair], c)
			return true
		}}
	return w
}

// serve is the helper's side: a JSON line in, a JSON line out, until the node closes the socket.
func (w *phoneWorld) serve(c net.Conn) {
	defer c.Close()
	rd := bufio.NewReader(c)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			w.eofs.Add(1)
			return
		}
		var req map[string]any
		_ = json.Unmarshal(line, &req)
		w.mu.Lock()
		w.got = append(w.got, req)
		answer := w.answer
		w.mu.Unlock()
		out, _ := json.Marshal(answer(req))
		if _, err := c.Write(append(out, '\n')); err != nil {
			return
		}
	}
}

// endStream is the pair's RFB stream closing: screenLogins closes what was attached to it.
func (w *phoneWorld) endStream(pair string) {
	w.mu.Lock()
	cs := w.closers[pair]
	delete(w.closers, pair)
	w.signed[pair] = false
	w.mu.Unlock()
	for _, c := range cs {
		_ = c.Close()
	}
}

func (w *phoneWorld) ask(t *testing.T, params map[string]any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := w.node.screenPhone(context.Background(), raw)
	if err != nil {
		return nil, err
	}
	m, _ := out.(map[string]any)
	return m, nil
}

// The statuses offer phone_screen only from a helper whose --preflight says it has it, and read the same cached
// --preflight as screen_input (one launch a minute, not two).
func TestPhoneScreenStatusComesFromTheSamePreflight(t *testing.T) {
	var launches atomic.Int32
	newPhoneWorld(t, "")
	swap(t, &inputPreflight, func(context.Context, string) string {
		launches.Add(1)
		return "post_event: granted\nphone_screen: 1 missing\n"
	})
	ctx := context.Background()
	st := phoneScreenStatus(ctx)
	if st == nil || st.Version != 1 || st.Permission != "missing" {
		t.Fatalf("status %+v", st)
	}
	if in := screenInputStatus(ctx); in == nil || in.Permission != "granted" {
		t.Fatalf("screen_input %+v", in)
	}
	if n := launches.Load(); n != 1 {
		t.Fatalf("%d preflights, want 1", n)
	}

	newPhoneWorld(t, "post_event: granted\n")
	if st := phoneScreenStatus(ctx); st != nil {
		t.Fatalf("a helper without Phone Screen offered it: %+v", st)
	}
	swap(t, &inputHelperSource, func() string { return "" })
	swap(t, &inputHelpers, &inputResolver{})
	if st := phoneScreenStatus(ctx); st != nil {
		t.Fatalf("no helper, status %+v", st)
	}
}

// A signed-in pair starts the session; its requests reach the helper in order and the answers come back without
// `ok`; a helper's refusal is an rpc error with its code; another pair is busy; the RFB stream ending ends the session.
func TestPhoneScreenSessionFollowsItsPair(t *testing.T) {
	w := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")

	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "windows"}); rpcCode(err) != "no_session" {
		t.Fatalf("windows before start: %v", err)
	}
	out, err := w.ask(t, map[string]any{"pair": "P", "op": "start", "width": 402, "height": 760})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := out["display"].(map[string]any); d["id"] != float64(7) {
		t.Fatalf("start answered %v", out)
	}
	if _, has := out["ok"]; has {
		t.Fatalf("ok passed on: %v", out)
	}
	// Starting again (the page came back) reuses the session.
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "start", "width": 402, "height": 760}); err != nil {
		t.Fatal(err)
	}
	if n := w.starts.Load(); n != 1 {
		t.Fatalf("%d helpers started, want 1", n)
	}
	if _, err := w.ask(t, map[string]any{"pair": "Q", "op": "start", "width": 402, "height": 760}); rpcCode(err) != "busy" {
		t.Fatalf("another pair: %v", err)
	}
	if out, err := w.ask(t, map[string]any{"pair": "P", "op": "windows"}); err != nil || len(out["windows"].([]any)) != 1 {
		t.Fatalf("windows: %v %v", out, err)
	}
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "place", "pid": 10, "window": 3}); rpcCode(err) != "window_gone" {
		t.Fatalf("place answered %v", err)
	}
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "place", "pid": 10}); rpcCode(err) != "bad_request" {
		t.Fatalf("place without a window: %v", err)
	}
	w.mu.Lock()
	ops := []any{}
	for _, r := range w.got {
		ops = append(ops, r["op"])
	}
	place := w.got[len(w.got)-1]
	w.mu.Unlock()
	if len(ops) != 4 || ops[0] != "start" || ops[3] != "place" || place["pid"] != float64(10) || place["window"] != float64(3) {
		t.Fatalf("the helper got %v (last %v)", ops, place)
	}

	w.endStream("P")
	eventually(t, "the helper's socket closed", func() bool { return w.eofs.Load() == 1 })
	if s := w.node.phones.session("P"); s != nil {
		t.Fatal("the session outlived its stream")
	}
	// Free again: the other pair may start now.
	if _, err := w.ask(t, map[string]any{"pair": "Q", "op": "start", "width": 390, "height": 700}); err != nil {
		t.Fatal(err)
	}
}

// stop asks the helper to put everything back and ends the session; a stop with none is fine.
func TestPhoneScreenStopEndsTheSession(t *testing.T) {
	w := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "stop"}); err != nil {
		t.Fatalf("stop with no session: %v", err)
	}
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "start", "width": 402, "height": 760}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "stop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the helper's socket closed", func() bool { return w.eofs.Load() == 1 })
	w.mu.Lock()
	last := w.got[len(w.got)-1]["op"]
	w.mu.Unlock()
	if last != "stop" || w.node.phones.session("P") != nil {
		t.Fatalf("last request %v, session %v", last, w.node.phones.session("P"))
	}
}

// Nothing starts without a signed-in stream, a helper that has the mode, and Accessibility; a start the helper refuses
// (no display) ends the session so nothing is left half done.
func TestPhoneScreenRefusals(t *testing.T) {
	start := map[string]any{"pair": "P", "op": "start", "width": 402, "height": 760}

	w := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	w.signed["P"] = false
	if _, err := w.ask(t, start); rpcCode(err) != "not_signed_in" {
		t.Fatalf("not signed in: %v", err)
	}
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "start"}); rpcCode(err) != "bad_request" {
		t.Fatalf("no size: %v", err)
	}

	w = newPhoneWorld(t, "post_event: granted\n")
	if _, err := w.ask(t, start); rpcCode(err) != "unavailable" {
		t.Fatalf("an old helper: %v", err)
	}
	w = newPhoneWorld(t, "post_event: granted\nphone_screen: 1 missing\n")
	if _, err := w.ask(t, start); rpcCode(err) != "permission" || w.starts.Load() != 0 {
		t.Fatalf("no Accessibility: %v, %d started", err, w.starts.Load())
	}
	w = newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	w.granted = false // the cached preflight was stale: the helper itself says missing
	if _, err := w.ask(t, start); rpcCode(err) != "permission" {
		t.Fatalf("helper says missing: %v", err)
	}

	w = newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	w.answer = func(map[string]any) map[string]any {
		return map[string]any{"ok": false, "error": "display", "message": "macOS did not bring a virtual display online"}
	}
	if _, err := w.ask(t, start); rpcCode(err) != "display" {
		t.Fatalf("no display: %v", err)
	}
	eventually(t, "the session ended", func() bool { return w.eofs.Load() == 1 })

	off := &Node{logger: log.New(io.Discard, "", 0)} // not a Mac
	raw, _ := json.Marshal(start)
	if _, err := off.screenPhone(context.Background(), raw); rpcCode(err) != "unavailable" {
		t.Fatalf("off a Mac: %v", err)
	}
	if _, err := w.node.screenPhone(context.Background(), []byte(`{"op":"start"}`)); rpcCode(err) != "bad_request" {
		t.Fatalf("no pair: %v", err)
	}
}

// What a helper that died left (#9500: a terminal's text size must not stay raised) is put back when the node starts
// and after a session ends, by --phone-screen-restore: only with records on disk, only with the helper allowed, and
// never while a session runs (that session puts them back as it starts).
func TestPhoneScreenLeftoversArePutBack(t *testing.T) {
	ctx := context.Background()
	w := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	w.node.phones.restoreLeftovers(ctx)
	if n := w.restores.Load(); n != 0 {
		t.Fatalf("ran %d times with nothing to put back", n)
	}
	if err := os.WriteFile(w.records, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w.node.phones.restoreLeftovers(ctx)
	if n := w.restores.Load(); n != 1 {
		t.Fatalf("ran %d times with records left", n)
	}

	// A session running: it handles them itself.
	if _, err := w.ask(t, map[string]any{"pair": "P", "op": "start", "width": 402, "height": 760}); err != nil {
		t.Fatal(err)
	}
	w.node.phones.restoreLeftovers(ctx)
	if n := w.restores.Load(); n != 1 {
		t.Fatalf("ran beside a live session (%d)", n)
	}
	// The session ends (its helper died, say) with the records still there: put back a moment later.
	w.endStream("P")
	eventually(t, "the leftovers put back after the session", func() bool { return w.restores.Load() == 2 })

	// Without Accessibility nothing runs (it couldn't move a window).
	w2 := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 missing\n")
	if err := os.WriteFile(w2.records, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w2.node.phones.restoreLeftovers(ctx)
	if n := w2.restores.Load(); n != 0 {
		t.Fatalf("ran without the permission (%d)", n)
	}
}

// `open -W` fails when the helper finished before it could wait (#9500's follow-up): with the records gone that is a
// done restore, never logged as a failure; with records still there the failure is logged.
func TestPhoneScreenRestoreLogsNoFalseFailure(t *testing.T) {
	ctx := context.Background()
	w := newPhoneWorld(t, "post_event: granted\nphone_screen: 1 granted\n")
	var mu sync.Mutex
	var lines []string
	w.node.phones.logf = func(f string, a ...any) { mu.Lock(); lines = append(lines, fmt.Sprintf(f, a...)); mu.Unlock() }
	w.node.phones.restoreRun = func(context.Context, string) error {
		_ = os.Remove(w.records) // the helper put everything back and exited before open could wait
		return errors.New("open: exit status 1: Unable to block on application")
	}
	if err := os.WriteFile(w.records, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w.node.phones.restoreLeftovers(ctx)
	w.node.phones.restoreRun = func(context.Context, string) error { return errors.New("open: no such app") }
	if err := os.WriteFile(w.records, []byte(`{"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w.node.phones.restoreLeftovers(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 || strings.Contains(lines[0], "Unable") || !strings.Contains(lines[0], "put back") ||
		!strings.Contains(lines[1], "no such app") {
		t.Fatalf("logged %q", lines)
	}
}
