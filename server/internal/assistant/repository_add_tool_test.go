package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func repositoryAddCall(ctx context.Context, s *Service, name, args string) proto.ToolResult {
	return s.Tool(ctx, ToolCall{Name: name, MachineID: "server", SessionID: "c1", Arguments: json.RawMessage(args)})
}

func answerRepositoryCard(t *testing.T, s *Service, answer proto.Resolution, inspect func(proto.Interaction)) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		card, ok := awaitCard(s)
		if !ok {
			t.Error("no repository confirmation card")
			return
		}
		inspect(card)
		raw, _ := json.Marshal(answer)
		if _, err := s.Action(context.Background(), Action{Op: "answer", InteractionID: card.ID, Answer: raw}); err != nil {
			t.Error(err)
		}
	}()
	return done
}

func TestRepositoryAddToolConfirmationAndSharedStore(t *testing.T) {
	s, checks := repositoryService(t, ruleStore{&memStore{}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	allow := true
	done := answerRepositoryCard(t, s, proto.Resolution{Allow: &allow}, func(card proto.Interaction) {
		if card.ToolName != "repository_add" || card.Kind != proto.IxPermission || card.SessionID != "c1" || !strings.Contains(card.Summary, "gitlab.example.com/example/subgroup/project") {
			t.Errorf("unexpected card: %+v", card)
		}
		doc, _ := s.repositoryDocument(ctx)
		if len(doc.Repositories) != 0 || *checks != 0 {
			t.Error("repository saved or checked before approval")
		}
	})
	out := repositoryAddCall(ctx, s, "repository_add", repoExample)
	<-done
	var binding RepositoryBinding
	if out.IsError || json.Unmarshal([]byte(out.Content[0].Text), &binding) != nil || binding.ID == "" || binding.VerifiedAt.IsZero() || *checks != 1 {
		t.Fatalf("add: %+v, checks %d", out, *checks)
	}
	if w := placeDo(s, "", "GET", repoBase+"/"+binding.ID, ""); w.Code != 200 || !strings.Contains(w.Body.String(), binding.ID) {
		t.Fatalf("MCP save not visible to Settings: %d %s", w.Code, w.Body)
	}
	if out := repositoryAddCall(ctx, s, "repository_add", repoExample); !out.IsError || !strings.Contains(out.Content[0].Text, "repository_duplicate") || len(s.pendingQuestions()) != 0 || *checks != 1 {
		t.Fatalf("duplicate was not refused before confirmation: %+v", out)
	}
}

func TestRepositoryAddToolRefusesInvalidAndUnapprovedChanges(t *testing.T) {
	s, checks := repositoryService(t, ruleStore{&memStore{}})
	for _, args := range []string{`null`, `{"url":"https://token@code.example.com/example/project"}`, `{"url":"https://github.com/example/project","manager_id":"other"}`, `{"url":null}`, repoExample + `{}`} {
		if out := repositoryAddCall(context.Background(), s, "repository_add", args); !out.IsError || len(s.pendingQuestions()) != 0 {
			t.Fatalf("accepted invalid input: %s: %+v", args, out)
		}
	}
	allow := true
	for _, answer := range []proto.Resolution{{Allow: &allow, AnsweredBy: "assistant"}, {Dismiss: true}, {Allow: new(bool)}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		done := answerRepositoryCard(t, s, answer, func(proto.Interaction) {})
		out := repositoryAddCall(ctx, s, "repository_add", repoExample)
		<-done
		cancel()
		if out.IsError || *checks != 0 {
			t.Fatalf("unapproved mutation: %+v, checks %d", out, *checks)
		}
	}
	doc, _ := s.repositoryDocument(context.Background())
	if len(doc.Repositories) != 0 {
		t.Fatal("unapproved repository was saved")
	}
}
