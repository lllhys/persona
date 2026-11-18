package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// A todo's chat thread (todo_thread.go, #5279): bound, rebound, unbound; one per todo and one todo per thread; the
// apps get it with the todo and are told when it changes.

func threadOf(t *testing.T, e *todoEnv, id string) *TodoThread {
	t.Helper()
	got := e.get(id)
	if got == nil {
		t.Fatalf("todo %s is not listed", id)
	}
	return got.Thread
}

func todosEvents(e *todoEnv) int {
	todosTold(e.t, e.s)
	n := 0
	for _, ev := range strings.Fields(e.f.eventTypes()) {
		if ev == "todos" {
			n++
		}
	}
	return n
}

func TestTodoThreadBinding(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		a, b := e.create("发版").ID, e.create("别的事").ID
		first := TodoThread{Channel: "feishu", ChatID: "oc_test", ThreadID: "omt_test_1", Title: "Release 1.3"}
		if threadOf(t, e, a) != nil {
			t.Fatal("a new todo has a thread")
		}

		// Bind: the todo comes back with it (trimmed), updated_at moves, the apps are told.
		before, events := e.get(a).UpdatedAt, todosEvents(e)
		time.Sleep(2 * time.Millisecond)
		var res struct{ Todo Todo }
		w := e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":" Feishu ","chat_id":"oc_test","thread_id":"omt_test_1 ","title":" Release 1.3 "}`)
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Todo.Thread == nil || *res.Todo.Thread != first {
			t.Fatalf("bind answered %s (%v)", w.Body, err)
		}
		if got := e.get(a); got.Thread == nil || *got.Thread != first || !got.UpdatedAt.After(before) {
			t.Fatalf("after bind: %+v (updated_at was %v)", got, before)
		}
		if todosEvents(e) != events+1 {
			t.Fatalf("the apps were told %d times, want %d", todosEvents(e), events+1)
		}
		if database != nil {
			if id, err := database.TodoOfThread(context.Background(), "feishu", "oc_test", "omt_test_1"); err != nil || id != a {
				t.Fatalf("stored: %q, %v", id, err)
			}
		}

		// Exactly the same binding again writes nothing: updated_at stays, the apps are not told.
		same, events := e.get(a).UpdatedAt, todosEvents(e)
		time.Sleep(2 * time.Millisecond)
		w = e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"Release 1.3"}`)
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Todo.Thread == nil || *res.Todo.Thread != first {
			t.Fatalf("the same binding answered %s (%v)", w.Body, err)
		}
		if got := e.get(a); !got.UpdatedAt.Equal(same) || todosEvents(e) != events {
			t.Fatalf("the same binding changed things: updated_at %v (was %v), %d events (was %d)", got.UpdatedAt, same, todosEvents(e), events)
		}

		// The same thread with another title only changes its title; another todo cannot take it (409) and both stay as they were.
		e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"Release 1.3.1"}`)
		if got := threadOf(t, e, a); got == nil || got.Title != "Release 1.3.1" {
			t.Fatalf("retitled: %+v", got)
		}
		bBefore := e.get(b).UpdatedAt
		if w := e.do("PUT", "/api/v1/todos/"+b+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusConflict {
			t.Fatalf("a thread of another todo: %d %s", w.Code, w.Body)
		}
		if threadOf(t, e, b) != nil || !e.get(b).UpdatedAt.Equal(bBefore) || threadOf(t, e, a).ThreadID != "omt_test_1" {
			t.Fatalf("after the refused bind: a=%+v b=%+v", threadOf(t, e, a), e.get(b))
		}

		// Rebind: the first thread is free for the other todo, and a todo has one thread.
		e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_2"}`)
		if got := threadOf(t, e, a); got == nil || got.ThreadID != "omt_test_2" || got.Title != "" {
			t.Fatalf("rebound: %+v", got)
		}
		e.must("PUT", "/api/v1/todos/"+b+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)

		// Unbind: the thread goes, updated_at moves, the apps are told; again, nothing changes.
		events, before = todosEvents(e), e.get(a).UpdatedAt
		time.Sleep(2 * time.Millisecond)
		w = e.must("DELETE", "/api/v1/todos/"+a+"/thread", "")
		var unbound struct{ Todo Todo }
		if err := json.Unmarshal(w.Body.Bytes(), &unbound); err != nil || unbound.Todo.Thread != nil || unbound.Todo.ID != a {
			t.Fatalf("unbind answered %s (%v)", w.Body, err)
		}
		if threadOf(t, e, a) != nil || !e.get(a).UpdatedAt.After(before) || todosEvents(e) != events+1 {
			t.Fatalf("after unbind: %+v, %d events (was %d)", e.get(a), todosEvents(e), events)
		}
		after, events := e.get(a).UpdatedAt, todosEvents(e)
		e.must("DELETE", "/api/v1/todos/"+a+"/thread", "")
		if !e.get(a).UpdatedAt.Equal(after) || todosEvents(e) != events {
			t.Fatalf("unbinding a todo with no thread changed it: %+v, %d events (was %d)", e.get(a), todosEvents(e), events)
		}

		// Editing the todo leaves its thread alone.
		e.must("PATCH", "/api/v1/todos/"+b, `{"notes":"n"}`)
		if got := threadOf(t, e, b); got == nil || got.ThreadID != "omt_test_1" {
			t.Fatalf("after PATCH: %+v", got)
		}

		// Deleting the todo frees its thread: nothing finds it, another todo can bind it.
		e.must("DELETE", "/api/v1/todos/"+b, "")
		if database != nil {
			if id, err := database.TodoOfThread(context.Background(), "feishu", "oc_test", "omt_test_1"); err == nil {
				t.Fatalf("the deleted todo's thread still finds %q", id)
			}
		}
		e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)
	})
}

func TestTodoThreadRequests(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		id := e.create("发版").ID
		// A read-only device cannot write, whatever it sends; an unknown todo is 404.
		for _, c := range []struct{ method, path string }{{"PUT", "/api/v1/todos/" + id + "/thread"}, {"DELETE", "/api/v1/todos/" + id + "/thread"}} {
			if w := e.doAs("answer", c.method, c.path, `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusForbidden {
				t.Fatalf("%s as a read-only role: %d", c.method, w.Code)
			}
		}
		e.must("PUT", "/api/v1/todos/"+id+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)
		if w := e.doAs("answer", "DELETE", "/api/v1/todos/"+id+"/thread", ""); w.Code != http.StatusForbidden || threadOf(t, e, id) == nil {
			t.Fatalf("a read-only role unbound it: %d", w.Code)
		}
		if w := e.do("PUT", "/api/v1/todos/todo_gone/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusNotFound {
			t.Fatalf("bind an unknown todo: %d", w.Code)
		}
		if w := e.do("DELETE", "/api/v1/todos/todo_gone/thread", ""); w.Code != http.StatusNotFound {
			t.Fatalf("unbind an unknown todo: %d", w.Code)
		}

		// Bad fields are 400 and leave the binding as it was.
		long := strings.Repeat("x", threadFieldMax+1)
		for _, body := range []string{`{`, `[]`, `{}`, `{"channel":"feishu","chat_id":"oc_test"}`, `{"channel":"  ","chat_id":"oc_test","thread_id":"t"}`,
			`{"channel":"feishu","chat_id":"","thread_id":"t"}`, `{"channel":"feishu","chat_id":"oc_test","thread_id":" \t"}`,
			`{"channel":"` + long + `","chat_id":"oc_test","thread_id":"t"}`, `{"channel":"feishu","chat_id":"` + long + `","thread_id":"t"}`,
			`{"channel":"feishu","chat_id":"oc_test","thread_id":"` + long + `"}`, `{"channel":1,"chat_id":"oc_test","thread_id":"t"}`} {
			if w := e.do("PUT", "/api/v1/todos/"+id+"/thread", body); w.Code != http.StatusBadRequest {
				t.Fatalf("%.60s: %d %s", body, w.Code, w.Body)
			}
		}
		if got := threadOf(t, e, id); got == nil || got.ThreadID != "omt_test_1" {
			t.Fatalf("after the refused binds: %+v", got)
		}
		// The title is a label: clipped (with an ellipsis, as the decisions' texts are), not refused.
		e.must("PUT", "/api/v1/todos/"+id+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"`+strings.Repeat("标", threadTitleMax+50)+`"}`)
		if got := threadOf(t, e, id); got == nil || len([]rune(got.Title)) != threadTitleMax+1 || !strings.HasSuffix(got.Title, "…") {
			t.Fatalf("long title: %+v", got)
		}
	})
}

// The binding is in the database: an assistant that starts afterwards has it, and a thread a todo holds there is taken
// even for an assistant that did not see the write.
func TestTodoThreadSurvivesRestart(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	a, b := e.create("发版").ID, e.create("别的事").ID
	e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"Release"}`)

	again := &todoEnv{t: t, s: newServiceOn(t, e.f, database), f: e.f}
	if got := threadOf(t, again, a); got == nil || got.Title != "Release" || got.ThreadID != "omt_test_1" {
		t.Fatalf("after a restart: %+v", got)
	}
	if got := threadOf(t, again, b); got != nil {
		t.Fatalf("the other todo has %+v", got)
	}

	// The first assistant's memory is stale (the second one unbinds and another todo binds the thread): the store
	// decides, and the refused bind leaves memory as it was.
	again.must("DELETE", "/api/v1/todos/"+a+"/thread", "")
	again.must("PUT", "/api/v1/todos/"+b+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)
	if w := e.do("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusConflict {
		t.Fatalf("a thread another todo holds in the store: %d %s", w.Code, w.Body)
	}
	if got := threadOf(t, e, a); got == nil || got.ThreadID != "omt_test_1" {
		t.Fatalf("memory after the refused bind: %+v", got)
	}
}

// todo_bind_thread binds the todo the agent works on, and only that one; a thread the todo already has stays unless it
// is the same one.
func TestAgentBindsThread(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		ctx := context.Background()
		e.f.setSessions("box", proto.Session{SessionID: "s1", Title: "迁移 Agent"}, proto.Session{SessionID: "s2", Title: "闲着的 Agent"})
		todoID := e.create("天气 App 发 1.3 版").ID
		other := e.create("别的事").ID
		e.must("PUT", "/api/v1/todos/"+todoID+"/agents/box/s1", "")
		e.must("PUT", "/api/v1/todos/"+other+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_taken"}`)
		tool := func(session, args string) proto.ToolResult {
			return e.s.agentTool(ctx, "box", session, "todo_bind_thread", json.RawMessage(args))
		}
		text := func(res proto.ToolResult) string { return res.Content[0].Text }
		const mine = `"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"`

		// Its own todo, with or without naming it; the apps are told.
		events := todosEvents(e)
		if res := tool("s1", `{`+mine+`,"title":"Release"}`); res.IsError || !strings.Contains(text(res), todoID) {
			t.Fatalf("bind: %+v", res)
		}
		if got := threadOf(t, e, todoID); got == nil || got.ThreadID != "omt_test_1" || got.Title != "Release" || todosEvents(e) != events+1 {
			t.Fatalf("after bind: %+v, %d events (was %d)", got, todosEvents(e), events)
		}
		events, was := todosEvents(e), e.get(todoID).UpdatedAt
		if res := tool("s1", `{`+mine+`,"title":"Release"}`); res.IsError || todosEvents(e) != events || !e.get(todoID).UpdatedAt.Equal(was) {
			t.Fatalf("calling again with the same binding: %+v, %d events (was %d)", res, todosEvents(e), events)
		}
		if res := tool("s1", `{"todo_id":"`+todoID+`",`+mine+`,"title":"Release 1.3"}`); res.IsError {
			t.Fatalf("the same thread again, naming the todo: %+v", res)
		}
		if got := threadOf(t, e, todoID); got == nil || got.Title != "Release 1.3" {
			t.Fatalf("retitled: %+v", got)
		}

		// Another todo's id, another thread for a todo that has one, a thread another todo holds, bad fields: refused,
		// and nothing changes anywhere.
		for _, c := range []struct{ args, want string }{
			{`{"todo_id":"` + other + `",` + mine + `}`, "only bind the todo you work on"},
			{`{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_2"}`, "already bound to another thread"},
			{`{"channel":"feishu","chat_id":"oc_test"}`, "thread_id cannot be empty"},
			{`{"channel":" ","chat_id":"oc_test","thread_id":"t"}`, "channel cannot be empty"},
			{`{"channel":1}`, "arguments"},
		} {
			if res := tool("s1", c.args); !res.IsError || !strings.Contains(text(res), c.want) {
				t.Fatalf("%s: %+v, want %q", c.args, res, c.want)
			}
		}
		if got := threadOf(t, e, todoID); got == nil || got.ThreadID != "omt_test_1" {
			t.Fatalf("own todo after the refusals: %+v", got)
		}
		if got := threadOf(t, e, other); got == nil || got.ThreadID != "omt_taken" {
			t.Fatalf("the other todo after the refusals: %+v", got)
		}
		// The user can change it, and then the agent may bind the one it was given.
		e.must("DELETE", "/api/v1/todos/"+todoID+"/thread", "")
		if res := tool("s1", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_taken"}`); !res.IsError || !strings.Contains(text(res), "already bound to another todo") {
			t.Fatalf("a thread another todo holds: %+v", res)
		}
		if res := tool("s1", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_2"}`); res.IsError {
			t.Fatalf("bind after the user unbound it: %+v", res)
		}

		// An agent on no todo, or a call from no session: nothing bound.
		if res := tool("s2", `{`+mine+`}`); res.IsError || !strings.Contains(text(res), "not working on an open todo") {
			t.Fatalf("no todo: %+v", res)
		}
		if res := tool("", `{`+mine+`}`); !res.IsError || !strings.Contains(text(res), "which agent session") {
			t.Fatalf("no session: %+v", res)
		}
		// Once the todo is done its agent is on no open todo.
		e.must("PATCH", "/api/v1/todos/"+todoID, `{"done":true}`)
		e.must("DELETE", "/api/v1/todos/"+todoID+"/thread", "")
		if res := tool("s1", `{`+mine+`}`); res.IsError || !strings.Contains(text(res), "not working on an open todo") || threadOf(t, e, todoID) != nil {
			t.Fatalf("done todo: %+v", res)
		}
	})
}
