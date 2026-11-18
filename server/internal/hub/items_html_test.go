package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/transcript"
)

// htmlToolRows returns made-up HTML snippets exactly as Claude wrote them.
func htmlToolRows(count int) []string {
	rows := make([]string, count)
	for i := range rows {
		rows[i] = `{"text":"` + strings.Repeat("<b>&amp;</b> ", 22) + `<p></p>"}`
	}
	return rows
}

// htmlToolInput is 61,011 bytes, over the input budget, so ParseLine must take the structural clipping hop.
func htmlToolInput() string {
	return `{"items":[` + strings.Join(htmlToolRows(200), ",") + `]}`
}

// clippedHTMLToolInput is the independently assembled 24,527-byte result: 80 unchanged rows and the object marker.
func clippedHTMLToolInput() string {
	return `{"items":[` + strings.Join(htmlToolRows(80), ",") + `],"zz_persona_clipped":{"note":"Omitted 120 array items and 0 object fields.\n…（已截断，共 61011 字节）"}}`
}

// A clipped tool input of HTML reaches the apps as the exact same 24,527-byte value through every hop: the transcript's
// clip, the node's rpc reply and push, the hub's page, full item, push and subscribe backlog. None writes < > & as six
// bytes each (#2907).
func TestItemsKeepHTMLCharactersOnTheWire(t *testing.T) {
	in := htmlToolInput()
	want := clippedHTMLToolInput()
	if len(in) != 61_011 || len(want) != 24_527 {
		t.Fatalf("HTML fixture sizes are %d and %d, want 61,011 and 24,527", len(in), len(want))
	}
	line := `{"type":"assistant","uuid":"a1","timestamp":"2026-11-10T12:00:00Z","message":{"id":"m","role":"assistant",` +
		`"content":[{"type":"tool_use","id":"t1","name":"mcp__x__y","input":` + in + `}]}}`
	items := transcript.ParseLine(1, []byte(line), &transcript.Meta{})
	if len(items) != 1 {
		t.Fatalf("ParseLine gave %d items, want 1", len(items))
	}
	checkToolInput(t, "ParseLine", items[0].ToolInput, want)

	c := &client{out: make(chan []byte, 1), subs: map[[2]string]bool{{"m1", "s1"}: true}}
	c.canonicalContracts.Store(true)
	h := &Hub{clients: map[*client]struct{}{c: {}}}
	m, node := itemsWire(t, h, func(method string) any {
		if method == "session.item" {
			return items[0]
		}
		return map[string]any{"items": items, "has_more": false}
	})

	req := httptest.NewRequest("GET", "/api/v1/sessions/m1/s1/items", nil)
	req.SetPathValue("sid", "s1")
	w := httptest.NewRecorder()
	h.sessionItems(w, req, m)
	checkToolInput(t, "page", firstToolInput(t, "page", w.Body.Bytes()), want)

	req = httptest.NewRequest("GET", "/api/v1/sessions/m1/s1/items/i1", nil)
	req.SetPathValue("sid", "s1")
	req.SetPathValue("iid", "i1")
	w = httptest.NewRecorder()
	h.sessionItemFull(w, req, m)
	var full struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &full); err != nil {
		t.Fatalf("full item: %v: %.200s", err, w.Body.Bytes())
	}
	checkToolInput(t, "full item", full.ToolInput, want)

	if err := node.Notify("session.items", map[string]any{"session_id": "s1", "items": items}); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-c.out:
		checkToolInput(t, "push", firstToolInput(t, "push", b), want)
	case <-time.After(10 * time.Second):
		t.Fatal("no push reached the client")
	}

	// The catch-up a subscribe starts with: how an app first gets a session's recent items.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.sendBacklog(ctx, c, m, "s1", 0)
	select {
	case b := <-c.out:
		checkToolInput(t, "backlog", firstToolInput(t, "backlog", b), want)
	default:
		t.Fatal("no backlog reached the client")
	}
}

// checkToolInput requires the exact expected clipped JSON and literal HTML characters on every hop.
func checkToolInput(t *testing.T, hop string, got json.RawMessage, want string) {
	t.Helper()
	var value any
	if err := json.Unmarshal(got, &value); err != nil {
		t.Fatalf("%s: tool_input is not JSON: %v", hop, err)
	}
	if string(got) != want {
		t.Errorf("%s: tool_input is %d bytes and differs from the exact %d-byte clipped value:\n got %.200s\nwant %.200s", hop, len(got), len(want), got, want)
	}
	if !strings.Contains(string(got), "<b>&amp;</b>") {
		t.Errorf("%s: tool_input lost the literal HTML snippet", hop)
	}
	for _, sequence := range []string{"u003c", "u003e", "u0026"} {
		if strings.Contains(string(got), string(rune(92))+sequence) {
			t.Errorf("%s: HTML was escaped as %s: %.80s", hop, sequence, got)
		}
	}
}

// firstToolInput is the first item's tool_input in a page or push.
func firstToolInput(t *testing.T, hop string, body []byte) json.RawMessage {
	t.Helper()
	var v struct {
		Items []struct {
			ToolInput json.RawMessage `json:"tool_input"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &v); err != nil || len(v.Items) == 0 {
		t.Fatalf("%s: no items (%v): %.200s", hop, err, body)
	}
	return v.Items[0].ToolInput
}

// itemsWire is a machine of h wired to a node over real rpc lines: the node answers each request with reply(method),
// and what it notifies reaches the machine's onEvent.
func itemsWire(t *testing.T, h *Hub, reply func(method string) any) (*Machine, *rpc.Peer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hp, np := rpc.New("h", h2nW), rpc.New("n", n2hW)
	m := newMachine(h, MachineConfig{ID: "m1"})
	m.peer = hp
	hp.OnEvent = m.onEvent
	np.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "session.items" && method != "session.item" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return reply(method), nil
	}
	done := make(chan struct{}, 2)
	go func() { defer func() { done <- struct{}{} }(); _ = hp.Serve(ctx, n2hR) }()
	go func() { defer func() { done <- struct{}{} }(); _ = np.Serve(ctx, h2nR) }()
	t.Cleanup(func() {
		cancel()
		for _, c := range []io.Closer{h2nR, h2nW, n2hR, n2hW} {
			_ = c.Close()
		}
		for range 2 {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("rpc cleanup timed out")
			}
		}
	})
	return m, np
}
