package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

func TestDraftPROwnershipReadsAuthenticatedAuthor(t *testing.T) {
	for _, tc := range []struct {
		name, author, viewer, state string
		draft, lookupFails, own     bool
	}{
		{"own", "contributor", "contributor", "open", true, false, true},
		{"case insensitive", "Contributor", "contributor", "open", true, false, true},
		{"other author", "someone-else", "contributor", "open", true, false, false},
		{"missing author", "", "contributor", "open", true, false, false},
		{"missing viewer", "contributor", "", "open", true, false, false},
		{"identity unavailable", "contributor", "", "open", true, true, false},
		{"ready", "contributor", "contributor", "open", false, false, false},
		{"closed", "contributor", "contributor", "closed", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identityReads := 0
			forge := githubForge{via: func(_ context.Context, path string, out any) (bool, error) {
				body := ""
				switch path {
				case "/repos/example/app/pulls/12":
					body = fmt.Sprintf(`{"state":%q,"draft":%t,"user":{"login":%q},"head":{"sha":"head-a"}}`, tc.state, tc.draft, tc.author)
				case "/user":
					identityReads++
					if tc.lookupFails {
						return true, errors.New("identity unavailable")
					}
					body = fmt.Sprintf(`{"login":%q}`, tc.viewer)
				case "/repos/example/app/pulls/12/reviews?per_page=100":
					body = `[]`
				case "/repos/example/app/commits/head-a/status":
					body = `{"total_count":0}`
				case "/repos/example/app/commits/head-a/check-runs?per_page=100&filter=latest":
					body = `{"check_runs":[]}`
				default:
					t.Fatalf("unexpected forge read: %s", path)
				}
				return true, json.Unmarshal([]byte(body), out)
			}}
			got, err := forge.PRStatus(t.Context(), "example/app", 12)
			if err != nil || got.Own != tc.own || got.Draft != tc.draft || got.State != tc.state {
				t.Fatalf("ownership/facts = %+v, %v; want own=%v", got, err, tc.own)
			}
			wantReads := 0
			if tc.draft && tc.state == "open" && tc.author != "" {
				wantReads = 1
			}
			if identityReads != wantReads {
				t.Fatalf("identity reads = %d; want %d", identityReads, wantReads)
			}
		})
	}
}

func pendingDraftPR(s *Service, id, url string) (superviseSignal, bool) {
	s.supervise.mu.Lock()
	defer s.supervise.mu.Unlock()
	g, ok := s.supervise.pending[(superviseSignal{Todo: id, Kind: sigDraftPR, PRURL: url}).id()]
	return g, ok
}

func TestDraftPRSignalsSupervisedTasks(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		draft, own  bool
		head        string
		parked      bool
		want        bool
	}{
		{"own draft", "open", true, true, "head-a", false, true},
		{"parked own draft", "open", true, true, "head-a", true, true},
		{"other or unknown author", "open", true, false, "head-a", true, false},
		{"ready", "open", false, true, "head-a", true, false},
		{"closed", "closed", true, true, "head-a", false, false},
		{"merged", "merged", true, true, "head-a", false, false},
		{"missing head", "open", true, true, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, hub, task := newPRConflictTask(t, nil)
			if tc.parked {
				if _, err := reviewErr(s, task.ID, mergePark(task.PRURL)); err != nil {
					t.Fatal(err)
				}
			}
			s.deploy.forge = &statusForge{status: TodoPRStatus{State: tc.state, Draft: tc.draft, Own: tc.own, HeadOID: tc.head}}
			s.sweepPRStatuses(t.Context(), time.Now())
			g, ok := pendingDraftPR(s, task.ID, task.PRURL)
			if ok != tc.want {
				t.Fatalf("draft signal = %+v, %v; want %v", g, ok, tc.want)
			}
			if tc.want && (!g.urgent() || g.level() != supervisionTriage) {
				t.Fatalf("draft signal is not actionable and urgent: %+v", g)
			}
			got := todoNow(s, task.ID)
			if tc.draft && triageWaitingForMerge(listedTodo{Todo: got}, "unread", time.Now()) {
				t.Fatal("a draft entered Waiting for merge")
			}
			if tc.want && (got.DoneAt != nil || tc.parked && got.Supervision.State != reviewParked) {
				t.Fatal("draft detection accepted the task or changed its parking")
			}
			sendPendingPRConflicts(s)
			notices := hub.notices()
			if !tc.want {
				if len(notices) != 0 {
					t.Fatalf("non-own-draft notified the supervisor: %q", notices)
				}
				return
			}
			if len(notices) != 1 || !strings.Contains(notices[0], "signal=draft_pr") ||
				!strings.Contains(notices[0], task.PRURL) || !strings.Contains(notices[0], "head-a") {
				t.Fatalf("supervisor did not receive the own-draft facts: %q", notices)
			}
		})
	}
}

func TestDraftPRQueuedSignalRechecksCurrentFacts(t *testing.T) {
	for _, changed := range []string{"ready", "other author", "closed", "merged", "new head", "new task PR", "new stage PR", "unlinked", "completed", "no status", "stale", "future"} {
		t.Run(changed, func(t *testing.T) {
			s, hub, task := newPRConflictTask(t, nil)
			status := TodoPRStatus{URL: task.PRURL, State: "open", Draft: true, Own: true, HeadOID: "head-a", CheckedAt: time.Now()}
			if err := s.applyPRStatus(t.Context(), task, status); err != nil {
				t.Fatal(err)
			}
			g, ok := pendingDraftPR(s, task.ID, task.PRURL)
			if !ok {
				t.Fatal("no draft queued before the facts changed")
			}
			switch changed {
			case "new task PR":
				s.setTaskStage(t.Context(), task.ID, "review", "https://github.com/example/app/pull/13")
				// The old PR remains followed; remove the task's old facts to model a replaced observation.
				s.todos.mu.Lock()
				s.todos.todos[task.ID].OtherPRs = nil
				s.todos.mu.Unlock()
			case "new stage PR":
				s.reports.mu.Lock()
				stage := s.reports.stages[task.ID]
				stage.PR, stage.PRs = "https://github.com/example/app/pull/13", nil
				s.reports.stages[task.ID] = stage
				s.reports.mu.Unlock()
			case "unlinked":
				s.todos.mu.Lock()
				row := s.todos.todos[task.ID]
				row.PRURL, row.PRs, row.OtherPRs = "https://github.com/example/app/pull/13", nil, []TodoPRStatus{status}
				s.todos.mu.Unlock()
			default:
				s.todos.mu.Lock()
				row := s.todos.todos[task.ID]
				switch changed {
				case "ready":
					row.PRStatus.Draft = false
				case "other author":
					row.PRStatus.Own = false
				case "closed", "merged":
					row.PRStatus.State = changed
				case "new head":
					row.PRStatus.HeadOID = "head-b"
				case "completed":
					now := time.Now()
					row.DoneAt = &now
				case "no status":
					row.PRStatus = nil
				case "stale":
					row.PRStatus.CheckedAt = time.Now().Add(-triagePRStatusFreshFor - time.Second)
				case "future":
					row.PRStatus.CheckedAt = time.Now().Add(time.Minute)
				}
				s.todos.mu.Unlock()
			}
			if _, holds := s.digestEntry(t.Context(), g, ""); holds {
				t.Fatal("the stale queued draft still holds")
			}
			if s.deliverDigest(t.Context(), s.settings(t.Context()), "conv1", []superviseSignal{g}, "", time.Now(), time.Time{}) ||
				len(hub.notices()) != 0 {
				t.Fatalf("a stale queued draft was delivered: %q", hub.notices())
			}
		})
	}
}

func TestDraftPRBusySupervisorRevalidatesBeforeDelivery(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing relay %v", existing), func(t *testing.T) {
			s, hub, task := newPRConflictTask(t, nil)
			ctx := t.Context()
			supervisor, ok := s.session(ctx, "server", "conv1")
			if !ok {
				t.Fatal("missing supervisor")
			}
			supervisor.Status = "working"
			hub.setSessions("server", supervisor)
			want := 0
			if existing {
				s.signal(superviseSignal{Todo: task.ID, Kind: sigStalled, Key: "earlier"})
				sendPendingPRConflicts(s)
				want = 1
			}
			status := TodoPRStatus{URL: task.PRURL, State: "open", Draft: true, Own: true, HeadOID: "head-a", CheckedAt: time.Now()}
			if err := s.applyPRStatus(ctx, task, status); err != nil {
				t.Fatal(err)
			}
			sendPendingPRConflicts(s)
			if _, pending := pendingDraftPR(s, task.ID, task.PRURL); !pending || s.queuedFor("conv1") != want || len(hub.notices()) != 0 {
				t.Fatal("draft was not left pending outside the rendered relay queue")
			}
			for _, msg := range queuedRelays(s, "conv1") {
				if strings.Contains(msg.Text, "signal=draft_pr") {
					t.Fatal("draft was folded into an existing rendered relay")
				}
			}
			status.Draft = false
			if err := s.applyPRStatus(ctx, task, status); err != nil {
				t.Fatal(err)
			}
			supervisor.Status = "idle"
			hub.setSessions("server", supervisor)
			sessions, err := s.sessions(ctx, "server")
			if err != nil {
				t.Fatal(err)
			}
			s.drainRelays("server", sessions)
			if existing {
				waitFor(t, "the unrelated relay to drain", func() bool { return len(hub.notices()) == want })
			}
			sendPendingPRConflicts(s)
			if len(hub.notices()) != want {
				t.Fatalf("a ready PR's draft signal escaped: %q", hub.notices())
			}
		})
	}
}

func TestDraftPRRetriesAndDeduplicatesEachHead(t *testing.T) {
	store := &memStore{}
	s, hub, task := newPRConflictTask(t, store)
	supervisor, ok := s.session(t.Context(), "server", "conv1")
	if !ok {
		t.Fatal("missing supervisor")
	}
	supervisor.Status = "working"
	hub.setSessions("server", supervisor)
	for i, head := range []string{"head-a", "head-a", "head-b", "head-a"} {
		status := TodoPRStatus{URL: task.PRURL, State: "open", Draft: true, Own: true, HeadOID: head, CheckedAt: time.Now()}
		if err := s.applyPRStatus(t.Context(), task, status); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			sendPendingPRConflicts(s)
			if len(hub.notices()) != 0 {
				t.Fatal("busy supervisor received a draft")
			}
			supervisor.Status = "idle"
			hub.setSessions("server", supervisor)
		}
		// Clear volatile sent-state: persisted fingerprints must still prevent duplicate delivery.
		s.supervise.mu.Lock()
		clear(s.supervise.sent)
		s.supervise.mu.Unlock()
		sendPendingPRConflicts(s)
		want := 1
		if i >= 2 {
			want = 2
		}
		if got := len(hub.notices()); got != want {
			t.Fatalf("head %s at pass %d: %d notices; want %d", head, i, got, want)
		}
	}
}

func TestDraftPRSignalsEachFollowedOwnDraft(t *testing.T) {
	s, hub, task := newPRConflictTask(t, nil)
	s.setTaskStage(t.Context(), task.ID, "review", "https://github.com/example/app/pull/13")
	s.deploy.forge = &statusForge{status: TodoPRStatus{State: "open", Draft: true, Own: true, HeadOID: "head-a"}}
	s.sweepPRStatuses(t.Context(), time.Now())
	for _, url := range []string{task.PRURL, "https://github.com/example/app/pull/13"} {
		if _, ok := pendingDraftPR(s, task.ID, url); !ok {
			t.Fatalf("no own-draft signal for followed PR %s", url)
		}
	}
	sendPendingPRConflicts(s)
	if notices := hub.notices(); len(notices) != 1 || strings.Count(notices[0], "signal=draft_pr") != 2 {
		t.Fatalf("the task's drafts were not independently delivered: %q", notices)
	}
}

func TestDraftPRFailedPersistenceDoesNotSignal(t *testing.T) {
	store := &memStore{}
	s, _, task := newPRConflictTask(t, store)
	store.mu.Lock()
	store.failPut = errors.New("synthetic stage write failure")
	store.mu.Unlock()
	err := s.applyPRStatus(t.Context(), task, TodoPRStatus{URL: task.PRURL, State: "open", Draft: true, Own: true,
		HeadOID: "head-a", CheckedAt: time.Now()})
	if err == nil {
		t.Fatal("stage persistence unexpectedly succeeded")
	}
	if g, ok := pendingDraftPR(s, task.ID, task.PRURL); ok {
		t.Fatalf("uncommitted facts signalled: %+v", g)
	}
	if got := todoNow(s, task.ID); got.PRStatus != nil {
		t.Fatalf("uncommitted facts published: %+v", got.PRStatus)
	}
}

func TestDraftPRPromptGolden(t *testing.T) {
	policy := prompts.RenderSupervisionInstructions()
	for _, required := range []string{"- `draft_pr`:", "make it ready if appropriate", "record the concrete reason", "grants no new permission"} {
		if !strings.Contains(policy, required) {
			t.Errorf("canonical supervision policy lacks %q", required)
		}
	}
	for _, tc := range []struct{ locale, fact string }{
		{"en", "is still a draft"},
		{"zh-Hans", "仍是草稿"}, // i18n-ignore: localized prompt assertion
		{"ja", "まだドラフト"},    // i18n-ignore: localized prompt assertion
	} {
		t.Run(tc.locale, func(t *testing.T) {
			s, hub, task := newPRConflictTask(t, nil)
			hub.mu.Lock()
			hub.hello.Locale = tc.locale
			hub.mu.Unlock()
			status := TodoPRStatus{URL: task.PRURL, State: "open", Draft: true, Own: true, HeadOID: "head-a", CheckedAt: time.Now()}
			if err := s.applyPRStatus(t.Context(), task, status); err != nil {
				t.Fatal(err)
			}
			g, ok := pendingDraftPR(s, task.ID, task.PRURL)
			if !ok {
				t.Fatal("no own-draft signal")
			}
			e, ok := s.digestEntry(t.Context(), g, "")
			if !ok {
				t.Fatal("draft signal did not render")
			}
			e.ID, e.Title = "todo_example", "Example task"
			got := prompts.RenderSupervise(prompts.Supervise{Entries: []prompts.SuperviseEntry{e}})
			for _, required := range []string{"signal=draft_pr", "https://github.com/example/app/pull/12", "head-a", tc.fact} {
				if !strings.Contains(got, required) {
					t.Errorf("missing draft fact %q: %s", required, got)
				}
			}
			if strings.Contains(got, "make it ready") || strings.Contains(got, "record the concrete reason") {
				t.Fatal("event repeated handling policy instead of facts")
			}
			golden(t, "draft_pr."+tc.locale, got)
		})
	}
}
