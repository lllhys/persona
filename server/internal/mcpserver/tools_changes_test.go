package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLeaderToolsRefreshAfterLateIdentityAndRestart(t *testing.T) {
	old := dialNode
	t.Cleanup(func() { dialNode = old })
	type answer struct {
		body string
		err  error
	}
	answers := make(chan answer)
	polls := make(chan map[string]string, 1)
	var mu sync.Mutex
	current := `{"revision":"before","session":"","tools":[{"name":"report_to_supervisor"}]}`
	dialNode = func(ctx context.Context, req any) ([]byte, error) {
		cursor := req.(map[string]any)["input"].(map[string]string)
		if cursor["revision"] == "" {
			mu.Lock()
			defer mu.Unlock()
			return []byte(current), nil
		}
		select {
		case polls <- cursor:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case res := <-answers:
			return []byte(res.body), res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	in, input := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, out) }()
	t.Cleanup(func() { _ = input.Close(); <-done })
	send := func(line string) {
		t.Helper()
		if _, err := io.WriteString(input, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	poll := func() map[string]string {
		t.Helper()
		select {
		case p := <-polls:
			return p
		case <-time.After(4 * time.Second):
			t.Fatal("tool watch did not reconnect")
			return nil
		}
	}
	reply := func(body string) {
		t.Helper()
		mu.Lock()
		current = body
		mu.Unlock()
		answers <- answer{body: body}
	}
	notifications := func() int { return strings.Count(out.String(), `"method":"notifications/tools/list_changed"`) }

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	waitFor(t, "listChanged capability", func() bool { return strings.Contains(out.String(), `"listChanged":true`) })
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if p := poll(); p["revision"] != "before" || p["session"] != "" {
		t.Fatalf("initial cursor: %v", p)
	}
	if !strings.Contains(out.String(), `"id":2`) || notifications() != 0 {
		t.Fatalf("watch preceded initial list: %s", out.String())
	}
	// Even repeated heartbeats do not exhaust a startup wait window.
	for range 3 {
		answers <- answer{body: `{"revision":"before","session":"","unchanged":true}`}
		poll()
	}
	if notifications() != 0 {
		t.Fatal("unchanged lists notified the client")
	}
	reply(`{"revision":"linked","session":"leader","tools":[{"name":"report_to_supervisor"},{"name":"task_plan"},{"name":"task_review"}]}`)
	if p := poll(); p["revision"] != "linked" || p["session"] != "leader" {
		t.Fatalf("late identity not retained: %v", p)
	}
	waitFor(t, "leader tools changed notification", func() bool { return notifications() == 1 })
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	waitFor(t, "refreshed tool list", func() bool { return strings.Contains(out.String(), `"id":3`) })
	if !strings.Contains(out.String(), `"name":"task_plan"`) || !strings.Contains(out.String(), `"name":"task_review"`) {
		t.Fatalf("missing dispatch tools: %s", out.String())
	}

	// A disconnected manager is not an empty list. Retry, then use its new cursor.
	answers <- answer{err: errors.New("manager restarted")}
	poll()
	if notifications() != 1 {
		t.Fatal("outage removed tools")
	}
	reply(`{"revision":"restarted","session":"leader","tools":[{"name":"task_review"},{"name":"task_plan"},{"name":"report_to_supervisor"}]}`)
	poll()
	if notifications() != 1 {
		t.Fatal("ordering or process revision alone refreshed tools")
	}
	// A temporary downgrade must not permanently end the watch or clear tools.
	answers <- answer{body: `{"tools":[{"name":"report_to_supervisor"}]}`}
	poll()
	if notifications() != 1 {
		t.Fatal("rollback removed the last successful list")
	}
	reply(`{"revision":"closed","session":"leader","tools":[{"name":"report_to_supervisor"}]}`)
	poll()
	waitFor(t, "closed leader tools removed", func() bool { return notifications() == 2 })
}

func TestLeaderOldNodeKeepsOneShotDiscovery(t *testing.T) {
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[{"name":"report_to_supervisor"}]}`), nil })
	result, err := handle(context.Background(), "test", listRequest())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "report_to_supervisor") {
		t.Fatalf("old node list: %s", b)
	}
}
