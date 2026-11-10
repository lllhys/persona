package kb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func editEngine(t *testing.T) *Engine {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: s}
}
func editApply(t *testing.T, e *Engine, kind, id string, u Update) {
	t.Helper()
	if _, err := e.Apply(kind, id, u); err != nil {
		t.Fatal(err)
	}
}

type editModel struct{ run func(string, any) error }

func (m editModel) JSONMax(_ context.Context, _, in string, _ int, out any) error {
	return m.run(in, out)
}

func TestRemoveRelationshipsBothWays(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat", People: []string{"person"}, Projects: []string{"project"}})
	_ = e.Store.PutPerson(&Person{ID: "person", Chats: []string{"chat"}, Projects: []string{"project"}})
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Aliases: []string{"alias"}, People: []string{"person"}, Chats: []string{"chat"}})
	editApply(t, e, KindProject, "project", Update{People: []string{"person"}, RemovePeople: []string{"person"}, RemoveChats: []string{"chat"}, RemoveAliases: []string{"alias"}})
	p, _ := e.Store.Project("project")
	c, _ := e.Store.Chat("chat")
	person, _ := e.Store.Person("person")
	if len(p.People)+len(p.Chats)+len(p.Aliases)+len(c.Projects)+len(person.Projects) != 0 {
		t.Fatalf("links remain: %+v %+v %+v", p, c, person)
	}
	p.People = []string{"person"}
	c.Projects = []string{"project"}
	_ = e.Store.PutProject(p)
	_ = e.Store.PutChat(c)
	if len(p.People)+len(c.Projects) != 0 {
		t.Fatal("pipeline caller retained stale removed links")
	}
	p, _ = e.Store.Project("project")
	c, _ = e.Store.Chat("chat")
	if len(p.People)+len(c.Projects) != 0 {
		t.Fatal("pipeline restored locked removal")
	}
	editApply(t, e, KindPerson, "person", Update{RemoveChats: []string{"chat"}})
	person, _ = e.Store.Person("person")
	c, _ = e.Store.Chat("chat")
	if len(person.Chats)+len(c.People) != 0 {
		t.Fatal("person/chat links remain")
	}
}

func TestRemoveProjectsFromEveryEntityKind(t *testing.T) {
	for _, kind := range []string{KindChat, KindPerson, KindDoc, KindMeeting, KindEvent} {
		t.Run(kind, func(t *testing.T) {
			e := editEngine(t)
			id := "entity"
			_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", People: []string{id}, Chats: []string{id}, Items: []string{kind + "/" + id}})
			switch kind {
			case KindChat:
				_ = e.Store.PutChat(&Chat{ID: id, Projects: []string{"project"}})
			case KindPerson:
				_ = e.Store.PutPerson(&Person{ID: id, Projects: []string{"project"}})
			default:
				_ = e.Store.PutItem(&Item{Kind: kind, ID: id, Projects: []string{"project"}})
			}
			editApply(t, e, kind, id, Update{RemoveProjects: []string{"project"}})
			d, err := e.Get(kind, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			var refs struct{ Projects []string }
			_ = json.Unmarshal(d.Meta, &refs)
			if len(refs.Projects) != 0 {
				t.Fatal("forward link remains")
			}
			p, _ := e.Store.Project("project")
			switch kind {
			case KindChat:
				if len(p.Chats) != 0 {
					t.Fatal(p.Chats)
				}
			case KindPerson:
				if len(p.People) != 0 {
					t.Fatal(p.People)
				}
			default:
				if len(p.Items) != 0 {
					t.Fatal(p.Items)
				}
			}
		})
	}
}

func TestPinnedSurvivesPipelineForEveryKind(t *testing.T) {
	for _, kind := range []string{KindChat, KindPerson, KindProject, KindDoc, KindMeeting, KindEvent} {
		t.Run(kind, func(t *testing.T) {
			e := editEngine(t)
			id := "entity"
			fact := "Confirmed fact"
			switch kind {
			case KindChat:
				_ = e.Store.PutChat(&Chat{ID: id})
			case KindPerson:
				_ = e.Store.PutPerson(&Person{ID: id})
			case KindProject:
				_ = e.Store.PutProject(&Project{Slug: id, Name: "Entity"})
			default:
				_ = e.Store.PutItem(&Item{Kind: kind, ID: id})
			}
			editApply(t, e, kind, id, Update{Pinned: &fact})
			switch kind {
			case KindChat:
				p, _ := e.Store.Chat(id)
				p.Pinned = "model"
				_ = e.Store.PutChat(p)
			case KindPerson:
				p, _ := e.Store.Person(id)
				p.Pinned = "model"
				_ = e.Store.PutPerson(p)
			case KindProject:
				p, _ := e.Store.Project(id)
				p.Pinned = "model"
				_ = e.Store.PutProject(p)
			default:
				p, _ := e.Store.Item(kind, id)
				p.Pinned = "model"
				_ = e.Store.PutItem(p)
			}
			d, _ := e.Get(kind, id, 0)
			var tag Tagging
			_ = json.Unmarshal(d.Meta, &tag)
			if tag.Pinned != fact {
				t.Fatal(tag.Pinned)
			}
			empty := ""
			editApply(t, e, kind, id, Update{Pinned: &empty})
			d, _ = e.Get(kind, id, 0)
			var cleared Tagging
			_ = json.Unmarshal(d.Meta, &cleared)
			if cleared.Pinned != "" {
				t.Fatal("explicit pinned clear failed")
			}
		})
	}
}

func TestCompressionPreservesConcurrentHandEdits(t *testing.T) {
	e := editEngine(t)
	now := time.Now().UTC()
	_ = e.Store.PutPerson(&Person{ID: "person", Name: "Example"})
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat", Kind: "dm", People: []string{"person"}, Created: now.Add(-time.Hour), LastAt: now, Pending: 2})
	c, _ := e.Store.Chat("chat")
	_ = e.Store.AppendMessages(c, []Line{{At: now, Sender: "Example", Text: "Synthetic message"}})
	entered, release := make(chan struct{}), make(chan struct{})
	e.Model = editModel{run: func(_ string, out any) error {
		close(entered)
		<-release
		return json.Unmarshal([]byte(`{"summary":"model summary","people":[{"name":"Example","note":"model note"}]}`), out)
	}}
	done := make(chan error, 1)
	go func() { done <- e.Compress(context.Background(), c) }()
	<-entered
	editApply(t, e, KindPerson, "person", Update{Note: "Confirmed role"})
	fact := "Pinned context"
	editApply(t, e, KindChat, "chat", Update{Name: "Manual chat", Summary: "Manual summary", Pinned: &fact})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	person, _ := e.Store.Person("person")
	c, _ = e.Store.Chat("chat")
	if person.Note != "Confirmed role" || c.Name != "Manual chat" || c.Pinned != fact || strings.TrimSpace(e.Store.Summary(KindChat, "chat")) != "Manual summary" {
		t.Fatalf("hand edit overwritten: %+v %+v", person, c)
	}
}

func TestProjectRewritePreservesConcurrentHandEdits(t *testing.T) {
	e := editEngine(t)
	fact := "Confirmed launch scope"
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Kind: "short", Status: "active", Dirty: true})
	editApply(t, e, KindProject, "project", Update{Pinned: &fact})
	p, _ := e.Store.Project("project")
	entered, release := make(chan struct{}), make(chan struct{})
	e.Model = editModel{run: func(in string, out any) error {
		if !strings.Contains(in, fact) {
			t.Error("pinned missing from project input")
		}
		close(entered)
		<-release
		return json.Unmarshal([]byte(`{"summary":"model summary","goal":"model goal","kind":"short","status":"done"}`), out)
	}}
	done := make(chan error, 1)
	go func() { done <- e.refreshProject(context.Background(), p) }()
	<-entered
	editApply(t, e, KindProject, "project", Update{Name: "Manual project", Kind: "long", Status: "paused", Goal: "Manual goal", Summary: "Manual summary"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p, _ = e.Store.Project("project")
	if p.Name != "Manual project" || p.Kind != "long" || p.Status != "paused" || p.Goal != "Manual goal" || p.Pinned != fact || strings.TrimSpace(e.Store.Summary(KindProject, "project")) != "Manual summary" {
		t.Fatalf("hand edit overwritten: %+v", p)
	}
}

func TestLockFalseDoesNotUnlockExistingFields(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "person"})
	no := false
	editApply(t, e, KindPerson, "person", Update{Note: "Unconfirmed", Lock: &no})
	p, _ := e.Store.Person("person")
	if p.fieldLocked("note") {
		t.Fatal("lock=false added lock")
	}
	editApply(t, e, KindPerson, "person", Update{Note: "Confirmed"})
	editApply(t, e, KindPerson, "person", Update{Note: "Edited", Lock: &no})
	p, _ = e.Store.Person("person")
	p.Note = "Model"
	_ = e.Store.PutPerson(p)
	p, _ = e.Store.Person("person")
	if p.Note != "Edited" {
		t.Fatal("existing lock was removed")
	}
}

func TestPersistedChangesNotifyAfterUnlock(t *testing.T) {
	e := editEngine(t)
	var changes []Change
	e.Changed = func(c Change) {
		if !e.Store.metaMu.TryLock() {
			t.Error("callback holds metadata lock")
		} else {
			e.Store.metaMu.Unlock()
		}
		_ = e.Store.Zone()
		_, _ = e.Store.Chat("chat")
		changes = append(changes, c)
	}
	e.ObserveChanges()
	_ = e.Store.PutChat(&Chat{ID: "chat"})
	_ = e.Store.PutPerson(&Person{ID: "person"})
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project"})
	for _, kind := range []string{KindDoc, KindMeeting, KindEvent} {
		_ = e.Store.PutItem(&Item{Kind: kind, ID: "item"})
	}
	_ = e.Store.PutSummary(KindChat, "chat", "Summary")
	c, _ := e.Store.Chat("chat")
	_ = e.Store.AppendMessages(c, []Line{{At: time.Now(), Sender: "Example", Text: "Synthetic"}})
	_ = e.Store.AppendLog("project", time.Now(), "Example", "Synthetic")
	for _, kind := range []string{KindChat, KindPerson, KindProject, KindDoc, KindMeeting, KindEvent} {
		if !slices.ContainsFunc(changes, func(c Change) bool { return c.Kind == kind && c.What == "metadata" }) {
			t.Fatalf("missing metadata event for %s: %+v", kind, changes)
		}
	}
	for _, what := range []string{"summary", "messages", "log"} {
		if !slices.ContainsFunc(changes, func(c Change) bool { return c.What == what }) {
			t.Fatal("missing event", what)
		}
	}
	(&Engine{Store: e.Store}).ObserveChanges()
	before := len(changes)
	_ = e.Store.PutSummary(KindChat, "chat", "Next")
	if len(changes) != before+1 {
		t.Fatal("listener lost")
	}
}

func TestForgottenEntityCannotBeRestoredByStalePipeline(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "source-chat", Source: "source"})
	stale, _ := e.Store.Chat("source-chat")
	if _, err := e.ForgetSource("source"); err != nil {
		t.Fatal(err)
	}
	stale.OneLine = "Stale model"
	if err := e.Store.PutChat(stale); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Store.Chat(stale.ID); ok {
		t.Fatal("forgotten entity resurrected")
	}
}

func TestClearedPinnedIsClearedInPipelineObject(t *testing.T) {
	e := editEngine(t)
	fact := "Confirmed fact"
	_ = e.Store.PutChat(&Chat{ID: "chat"})
	editApply(t, e, KindChat, "chat", Update{Pinned: &fact})
	stale, _ := e.Store.Chat("chat")
	empty := ""
	editApply(t, e, KindChat, "chat", Update{Pinned: &empty})
	if err := e.Store.PutChat(stale); err != nil {
		t.Fatal(err)
	}
	if stale.Pinned != "" {
		t.Fatal("caller retained cleared pinned fact")
	}
}

func TestManualSummaryFailureIsReturned(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat"})
	// A non-empty directory in place of summary.md: the rename over it fails on every platform.
	if err := os.MkdirAll(filepath.Join(e.Store.Path(KindChat, "chat", "summary.md"), "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(KindChat, "chat", Update{Summary: "Synthetic summary"}); err == nil {
		t.Fatal("summary write failure was ignored")
	}
}

func TestCompressInputCarriesCurrentPinnedFacts(t *testing.T) {
	e := editEngine(t)
	now := time.Now().UTC()
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat", Kind: "dm", People: []string{"person"}, Created: now.Add(-time.Hour), LastAt: now, Pending: 1})
	_ = e.Store.PutPerson(&Person{ID: "person", Name: "Example"})
	stale, _ := e.Store.Chat("chat")
	_ = e.Store.AppendMessages(stale, []Line{{At: now, Sender: "Example", Text: "Synthetic"}})
	chatFact, personFact := "Confirmed chat fact", "Confirmed person fact"
	editApply(t, e, KindChat, "chat", Update{Pinned: &chatFact})
	editApply(t, e, KindPerson, "person", Update{Pinned: &personFact})
	e.Model = editModel{run: func(in string, out any) error {
		for _, fact := range []string{chatFact, personFact} {
			if !strings.Contains(in, fact) {
				t.Error("pinned fact missing", fact)
			}
		}
		return json.Unmarshal([]byte(`{}`), out)
	}}
	if err := e.Compress(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRewriteDoesNotRestoreForgottenSource(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "source-chat", Source: "source"})
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Status: "active", Chats: []string{"source-chat"}, Dirty: true})
	p, _ := e.Store.Project("project")
	entered, release := make(chan struct{}), make(chan struct{})
	e.Model = editModel{run: func(_ string, out any) error {
		close(entered)
		<-release
		return json.Unmarshal([]byte(`{"summary":"Stale source context"}`), out)
	}}
	done := make(chan error, 1)
	go func() { done <- e.refreshProject(context.Background(), p) }()
	<-entered
	if _, err := e.ForgetSource("source"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p, _ = e.Store.Project("project")
	if len(p.Chats) != 0 || !p.Dirty || strings.Contains(e.Store.Summary(KindProject, p.Slug), "Stale source") {
		t.Fatal("rewrite restored forgotten context")
	}
}

func TestCompressionDoesNotReviveForgottenChat(t *testing.T) {
	e := editEngine(t)
	now := time.Now().UTC()
	_ = e.Store.PutChat(&Chat{ID: "source-chat", Source: "source", Name: "Chat", Kind: "group", Created: now.Add(-time.Hour), LastAt: now, Pending: 1})
	c, _ := e.Store.Chat("source-chat")
	_ = e.Store.AppendMessages(c, []Line{{At: now, Sender: "Example", Text: "Synthetic"}})
	entered, release := make(chan struct{}), make(chan struct{})
	e.Model = editModel{run: func(_ string, out any) error {
		close(entered)
		<-release
		return json.Unmarshal([]byte(`{"summary":"Stale source summary"}`), out)
	}}
	done := make(chan error, 1)
	go func() { done <- e.Compress(context.Background(), c) }()
	<-entered
	if _, err := e.ForgetSource("source"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.Store.Path(KindChat, c.ID)); !os.IsNotExist(err) {
		t.Fatalf("forgotten directory returned: %v", err)
	}
	if err := e.Store.PutSummary(KindChat, c.ID, "Stale delayed write"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.Store.Path(KindChat, c.ID)); !os.IsNotExist(err) {
		t.Fatalf("summary write recreated forgotten directory: %v", err)
	}
}

func TestModelsCannotCreatePinnedOrLocks(t *testing.T) {
	e := editEngine(t)
	now := time.Now().UTC()
	_ = e.Store.PutPerson(&Person{ID: "person", Name: "Example"})
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat", Kind: "dm", People: []string{"person"}, Created: now.Add(-time.Hour), LastAt: now, Pending: 1})
	c, _ := e.Store.Chat("chat")
	_ = e.Store.AppendMessages(c, []Line{{At: now, Sender: "Example", Text: "Synthetic"}})
	e.Model = editModel{run: func(_ string, out any) error {
		return json.Unmarshal([]byte(`{"summary":"Summary","pinned":"model fact","locked":true,"locked_fields":["note"],"projects":[{"name":"Synthetic Project","pinned":"model project fact","locked":true,"locked_fields":["goal"]}],"people":[{"name":"Example","note":"Role","pinned":"model person fact","locked":true,"locked_fields":["note"]}]}`), out)
	}}
	if err := e.Compress(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	chat, _ := e.Store.Chat("chat")
	person, _ := e.Store.Person("person")
	project, _ := e.Store.Project("synthetic-project")
	if project == nil {
		t.Fatal("model project was not created")
	}
	for _, tag := range []Tagging{chat.Tagging, person.Tagging, project.Tagging} {
		if tag.Pinned != "" || tag.Locked || len(tag.LockedFields) > 0 {
			t.Fatalf("model injected protected metadata: %+v", tag)
		}
	}
}

func TestReviewForgetAfterProjectSummary(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "source-chat", Source: "source"})
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Status: "active", Chats: []string{"source-chat"}, Dirty: true})
	e.Model = editModel{run: func(_ string, out any) error { return json.Unmarshal([]byte(`{"summary":"rewritten"}`), out) }}
	e.Changed = func(c Change) {
		if c.Kind == KindProject && c.What == "summary" {
			if _, err := e.ForgetSource("source"); err != nil {
				t.Error(err)
			}
		}
	}
	e.ObserveChanges()
	p, _ := e.Store.Project("project")
	if err := e.refreshProject(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p, _ = e.Store.Project("project")
	if len(p.Chats) != 0 {
		t.Fatalf("forgotten source restored: %+v", p.Chats)
	}
}

func TestProjectRewriteDiscardsDigestAfterInputCorrection(t *testing.T) {
	for _, correction := range []string{"goal", "chat_summary"} {
		t.Run(correction, func(t *testing.T) {
			e := editEngine(t)
			_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat"})
			_ = e.Store.PutSummary(KindChat, "chat", "Original chat context")
			_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Goal: "Original goal", Chats: []string{"chat"}, Dirty: true})
			_ = e.Store.PutSummary(KindProject, "project", "Original project context")
			p, _ := e.Store.Project("project")
			entered, release := make(chan struct{}), make(chan struct{})
			e.Model = editModel{run: func(_ string, out any) error {
				close(entered)
				<-release
				return json.Unmarshal([]byte(`{"summary":"Outdated model context","goal":"Outdated goal"}`), out)
			}}
			done := make(chan error, 1)
			go func() { done <- e.refreshProject(context.Background(), p) }()
			<-entered
			if correction == "goal" {
				editApply(t, e, KindProject, "project", Update{Goal: "Corrected goal"})
			} else {
				editApply(t, e, KindChat, "chat", Update{Summary: "Corrected chat context"})
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(e.Store.Summary(KindProject, "project")); got != "Original project context" {
				t.Fatal("old model input was committed after a correction", got)
			}
			p, _ = e.Store.Project("project")
			if !p.Dirty {
				t.Fatal("discarded rewrite cleared dirty flag")
			}
		})
	}
}
