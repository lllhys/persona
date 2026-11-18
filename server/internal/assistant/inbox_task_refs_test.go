package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// GitHub items link to the one open task naming their number within their repository (#6507): bare #N in a task of
// that repository, owner/repo#N, a PR the task's agents named before the current one, and an issue through the PR that
// says it closes it. Nothing outside the repository (#6424) and nothing ambiguous.

// refsHub is a service whose Inbox is ready, with the twin off: only the deterministic pass links.
func refsHub(t *testing.T) *Service {
	t.Helper()
	s, f := newQuietService(t)
	in := newFakeInbox()
	f.inbox, in.svc = in, s
	if err := s.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// githubItem is an open item of example/project's number n ("pull" or "issues").
func githubItem(id, kind string, n string) *InboxItem {
	return &InboxItem{ID: id, Source: "github", Kind: "author", Title: "A comment", Last: "c-" + id, UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "example/project#" + n, URL: "https://github.com/example/project/" + kind + "/" + n,
			Tags: []connector.Tag{{Key: "repo", Value: "example/project"}}}}
}

func setNotes(s *Service, id, notes string) {
	s.todos.mu.Lock()
	s.todos.todos[id].Notes = notes
	s.todos.mu.Unlock()
}

func putItems(s *Service, items ...*InboxItem) {
	s.inbox.mu.Lock()
	for _, it := range items {
		s.inbox.items[it.ID] = it
	}
	s.inbox.mu.Unlock()
}

func linkedTo(s *Service, it *InboxItem) string {
	s.inbox.mu.Lock()
	defer s.inbox.mu.Unlock()
	if it.Agent == nil {
		return ""
	}
	return it.Agent.TodoID
}

func TestInboxTaskRefsBareNumberInTheRepository(t *testing.T) {
	for _, c := range []struct {
		name, notes string
		want        bool
	}{
		{"bare number in a task naming the repository", "Follow example/project's PRs until merged: #4914, #4891.", true},
		{"owner/repo#N", "Follow example/project#4914 until merged.", true},
		{"bare number in a task of another repository", "Follow other/thing's PRs: #4914.", false},
		{"bare number with no repository", "Look at #4914 again.", false},
		{"a URL fragment is no number", "See https://example.com/page#4914 for the log.", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := refsHub(t)
			task := supervisedTodo(t, s, "Follow up PRs")
			setNotes(s, task.ID, c.notes)
			it := githubItem("github:a", "pull", "4914")
			putItems(s, it)
			s.handPass(context.Background())
			if got := linkedTo(s, it) == task.ID; got != c.want {
				t.Fatalf("linked=%v, want %v (link %q)", got, c.want, linkedTo(s, it))
			}
			s.handPass(context.Background()) // the repository scope (#6424) keeps what it linked
			if got := linkedTo(s, it) == task.ID; got != c.want {
				t.Fatalf("second pass: linked=%v, want %v", got, c.want)
			}
		})
	}
}

func TestInboxTaskRefsAmbiguousBareNumber(t *testing.T) {
	s := refsHub(t)
	a := supervisedTodo(t, s, "Follow up PRs")
	b := supervisedTodo(t, s, "Review the fix")
	setNotes(s, a.ID, "example/project: #4914")
	setNotes(s, b.ID, "https://github.com/example/project/pull/4914")
	it := githubItem("github:a", "pull", "4914")
	putItems(s, it)
	s.handPass(context.Background())
	if got := linkedTo(s, it); got != "" {
		t.Fatalf("two tasks name it, linked to %q", got)
	}
}

func TestInboxTaskRefsEarlierPRsOfTheTask(t *testing.T) {
	ctx := context.Background()
	s := refsHub(t)
	task := supervisedTodo(t, s, "Ship the change")
	s.setTaskStage(ctx, task.ID, "review", "https://github.com/example/project/pull/5813")
	s.setTaskStage(ctx, task.ID, "review", "https://github.com/example/project/pull/6441")
	if got := todoNow(s, task.ID); got.PRURL != "https://github.com/example/project/pull/6441" || len(got.PRs) != 1 {
		t.Fatalf("PR %q, earlier %v", got.PRURL, got.PRs)
	}
	early, late := githubItem("github:early", "pull", "5813"), githubItem("github:late", "pull", "6441")
	putItems(s, early, late)
	s.handPass(ctx)
	if linkedTo(s, early) != task.ID || linkedTo(s, late) != task.ID {
		t.Fatalf("early %q, late %q, want both %s", linkedTo(s, early), linkedTo(s, late), task.ID)
	}
	// The earlier PRs are kept with the stage: a manager started again still knows them.
	s.reports.mu.Lock()
	kept := s.reports.stages[task.ID].PRs
	s.reports.mu.Unlock()
	if len(kept) != 1 || kept[0] != "https://github.com/example/project/pull/5813" {
		t.Fatalf("kept %v", kept)
	}
}

func TestInboxTaskRefsIssueThroughItsClosingPR(t *testing.T) {
	for _, c := range []struct {
		name, prText string
		want         bool
	}{
		{"Closes #N", "Fixes the idle dot.\n\nCloses #4865\nRefs #4657", true},
		{"fixes owner/repo#N", "fixes example/project#4865", true},
		{"another repository's issue", "Closes other/thing#4865", false},
		{"only mentions it", "Related to #4865", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := refsHub(t)
			task := supervisedTodo(t, s, "Follow up PRs")
			setNotes(s, task.ID, "example/project: #4891")
			pr := githubItem("github:pr", "pull", "4891")
			pr.Context = c.prText
			now := time.Now()
			pr.DoneAt = &now // a done item still says what it closes
			issue := githubItem("github:issue", "issues", "4865")
			putItems(s, pr, issue)
			s.handPass(context.Background())
			if got := linkedTo(s, issue) == task.ID; got != c.want {
				t.Fatalf("linked=%v, want %v (link %q)", got, c.want, linkedTo(s, issue))
			}
		})
	}
}

func TestInboxTaskRefsKeepKindOffGitHub(t *testing.T) {
	refs := inboxTaskRefs("https://git.example.com/example/project/pull/7 https://github.com/Example/Project/issues/7")
	if len(refs) != 2 || refs[0] != "git.example.com/example/project/pull/7" || refs[1] != "github.com/example/project#7" {
		t.Fatalf("refs %v", refs)
	}
	if got := inboxBareRefs("#12, (#13) a#14 x/y#15 https://h/p#16 &#17;"); len(got) != 2 || got[0] != 12 || got[1] != 13 {
		t.Fatalf("bare %v", got)
	}
}
