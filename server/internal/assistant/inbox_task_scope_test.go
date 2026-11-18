package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
)

// githubIssueItem is a Needs you comment on issue example/project#12, as the GitHub connector files it.
func githubIssueItem(id string) *InboxItem {
	return &InboxItem{ID: id, Source: "github", Kind: KindMention, Tier: TierAction, Title: "Panes stay idle through a running turn",
		Sender: "reporter", Preview: "Still shows a grey dot while the agent works",
		Context: "reporter: Still shows a grey dot while the agent works", Last: "12", TriagedAt: "12", UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "example/project#12", URL: "https://github.com/example/project/issues/12",
			Tags: []connector.Tag{{Key: "repo", Value: "example/project", Name: "example/project"}}}}
}

// judgeTodo makes an open task with notes for the judge.
func judgeTodo(t *testing.T, h *inboxHub, title, notes string) Todo {
	t.Helper()
	todo, err := h.createTodo(context.Background(), title, "", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	h.todos.mu.Lock()
	h.todos.todos[todo.ID].Notes = notes
	h.todos.mu.Unlock()
	return todo
}

// A GitHub item is only offered the tasks that name its repository (#6424). With none, the judge is not asked and
// the item stays unlinked, whatever a model would have said; with one, only that one is offered.
func TestInboxJudgeGitHubItemNeedsItsRepository(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(map[bool]string{false: "no task names it", true: "one task names it"}[named], func(t *testing.T) {
			ctx := context.Background()
			h := agentHub(t)
			other := judgeTodo(t, h, "Session coverage wrap-up", "Fix the coverage reports from the chat group: AI coding sessions")
			var mine Todo
			if named {
				mine = judgeTodo(t, h, "Fix idle status dots", "Bug in https://github.com/example/project/issues/9: the node's status")
			}
			h.setConfig(func(cfg *assistantwire.InboxConfig) { cfg.Agent.Enabled = true })
			reply := `{"task_id":"` + other.ID + `","evidence":"AI coding sessions"}`
			if named {
				reply = `{"task_id":"` + mine.ID + `","evidence":"Fix idle status dots"}`
			}
			model, calls := captureModel(t, reply)
			h.in.setModel(model)
			it := githubIssueItem("github:issue12")
			h.inbox.mu.Lock()
			h.inbox.items[it.ID] = it
			h.inbox.mu.Unlock()
			h.handPass(ctx)
			got := calls()
			h.inbox.mu.Lock()
			linked := ""
			if it.Agent != nil {
				linked = it.Agent.TodoID
			}
			h.inbox.mu.Unlock()
			if !named {
				if len(got) != 0 || linked != "" {
					t.Fatalf("judge calls=%d linked=%q: a task that never names the repository was offered", len(got), linked)
				}
				return
			}
			c := onlyCall(t, got)
			if !strings.Contains(c.User, mine.ID) || strings.Contains(c.User, other.ID) {
				t.Fatalf("offered tasks wrong:\n%s", c.User)
			}
			if linked != mine.ID {
				t.Fatalf("linked=%q, want %q", linked, mine.ID)
			}
		})
	}
}

// The judge's answer counts only with evidence copied from the task it names.
func TestInboxJudgeNeedsEvidence(t *testing.T) {
	ctx := context.Background()
	task := prompts.InboxTask{ID: "todo_1", Title: "Fix the login crash", Notes: "Crash after sign-in on the release build;  reply to the group"}
	for _, c := range []struct {
		name, evidence string
		linked         bool
	}{
		{"none", "", false},
		{"too short", "Fix", false},
		{"paraphrased", "fix a crash at login", false},
		{"from another place", "login crash fixed today", false},
		{"title", "the login crash", true},
		{"notes, white space and case folded", "release build; reply", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := agentHub(t)
			h.in.setModel(fakeModel(t, `{"task_id":"todo_1","evidence":"`+c.evidence+`"}`))
			it := actionItem("lark:f1:12", time.Now())
			it.Agent = &InboxAgent{State: AgentQueued}
			h.inbox.mu.Lock()
			h.inbox.items[it.ID] = it
			h.inbox.mu.Unlock()
			if got := h.linkTask(ctx, it, []prompts.InboxTask{task}); got != c.linked || (it.Agent.TodoID == "todo_1") != c.linked {
				t.Fatalf("linked=%v (%q), want %v", got, it.Agent.TodoID, c.linked)
			}
		})
	}
}

// A chat thread item is offered only the tasks that name its thread when some do; when none does, all of them.
func TestInboxJudgeThreadNarrowsCandidates(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(map[bool]string{false: "no task names it", true: "one task names it"}[named], func(t *testing.T) {
			ctx := context.Background()
			h := agentHub(t)
			first := judgeTodo(t, h, "Old investigation", "Look into the missing links")
			notes := "Follow up in the group"
			if named {
				notes = "Reply in thread omt_thread123 once the release is out"
			}
			second := judgeTodo(t, h, "Release follow-up", notes)
			model, calls := captureModel(t, `{"task_id":"`+second.ID+`","evidence":"Release follow-up"}`)
			h.in.setModel(model)
			it := actionItem("larkcli:thread", time.Now())
			it.Connector = &InboxConnector{Thread: "larkcli:chat:oc_chat456:thread:omt_thread123",
				Tags: []connector.Tag{{Key: "chat", Value: "oc_chat456", Name: "Group"}}}
			it.Agent = &InboxAgent{State: AgentQueued}
			h.inbox.mu.Lock()
			h.inbox.items[it.ID] = it
			h.inbox.mu.Unlock()
			if !h.linkTask(ctx, it, h.activeTasks(ctx)) || it.Agent.TodoID != second.ID {
				t.Fatalf("not linked: %+v", it.Agent)
			}
			if c := onlyCall(t, calls()); strings.Contains(c.User, first.ID) == named {
				t.Fatalf("named=%v but the other task offered=%v:\n%s", named, !named, c.User)
			}
		})
	}
}

// A GitHub item linked earlier to an open task that does not name its repository loses the link on the next pass, and
// the cleared link is what the store keeps; a link to a task that names it stays (#6424).
func TestInboxStaleGitHubLinkIsCleared(t *testing.T) {
	ctx := context.Background()
	h, st := newBulkFailHub(t)
	wrong, err := h.createTodo(ctx, "Session coverage wrap-up", "", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	right, err := h.createTodo(ctx, "Fix idle dots in example/project", "", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	stale, kept := githubIssueItem("github:stale"), githubIssueItem("github:kept")
	stale.Agent, kept.Agent = &InboxAgent{State: AgentSkipped, TodoID: wrong.ID}, &InboxAgent{State: AgentSkipped, TodoID: right.ID}
	h.inbox.mu.Lock()
	h.inbox.items[stale.ID], h.inbox.items[kept.ID] = stale, kept
	h.inbox.mu.Unlock()
	h.linkInboxTaskURLs(ctx)
	h.inbox.mu.Lock()
	staleID, keptID := stale.Agent.TodoID, kept.Agent.TodoID
	h.inbox.mu.Unlock()
	if staleID != "" || keptID != right.ID {
		t.Fatalf("stale=%q kept=%q", staleID, keptID)
	}
	if got := storedItem(t, st, stale.ID); got.Agent == nil || got.Agent.TodoID != "" || got.ID != stale.ID {
		t.Fatalf("the cleared link was not stored: %+v", got.Agent)
	}
}

func TestInboxScopeOf(t *testing.T) {
	for _, c := range []struct {
		name string
		it   InboxItem
		repo string
		keys []string
	}{
		{"repo tag", InboxItem{Source: "github", Connector: &InboxConnector{Tags: []connector.Tag{{Key: "repo", Value: "Example/Project"}}}}, "example/project", nil},
		{"thread only", InboxItem{Source: "github", Connector: &InboxConnector{Thread: "example/project#4"}}, "example/project", nil},
		{"thread of another source", InboxItem{Source: "mail", Connector: &InboxConnector{Thread: "example/project#4"}}, "", nil},
		{"chat thread", InboxItem{Connector: &InboxConnector{Thread: "larkcli:chat:oc_chat456:thread:omt_thread123"}}, "", []string{"oc_chat456", "omt_thread123"}},
		{"short ids ignored", InboxItem{Connector: &InboxConnector{Thread: "x:chat:ab"}}, "", nil},
		{"none", InboxItem{}, "", nil},
	} {
		sc := inboxScopeOf(&c.it)
		if sc.repo != c.repo || strings.Join(sc.keys, ",") != strings.Join(c.keys, ",") {
			t.Errorf("%s: %+v", c.name, sc)
		}
	}
	for _, c := range []struct {
		text string
		want bool
	}{
		{"see https://github.com/example/project/pull/3", true},
		{"example/project.", true},
		{"example/project-tools", false},
		{"other-example/project", false},
		{"example/projects", false},
	} {
		if namesRepo(c.text, "example/project") != c.want {
			t.Errorf("namesRepo(%q) = %v", c.text, !c.want)
		}
	}
	if !taskOfRepo(Todo{Agents: []TodoAgent{{Cwd: "/home/u/src/project-fix-12"}}}, "example/project") ||
		taskOfRepo(Todo{Agents: []TodoAgent{{Cwd: "/home/u/src/other"}}}, "example/project") {
		t.Error("taskOfRepo by working directory")
	}
}
