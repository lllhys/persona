package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

func TestRepositoryToolsSearchPaginationAndIsolation(t *testing.T) {
	s, calls := repositoryService(t, ruleStore{&memStore{}})
	for n := 0; n < 4; n++ {
		body := fmt.Sprintf(`{"url":"https://github.com/example/project-%d","name":"Project %d","aliases":["service"],"purpose":"Example research"}`, n, n)
		if w := placeDo(s, "", "POST", repoBase, body); w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	var first, second repositoryPage
	out, err := s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(`{"query":"service","limit":2}`))
	if err != nil || json.Unmarshal([]byte(out), &first) != nil || len(first.Repositories) != 2 || first.NextCursor == "" {
		t.Fatalf("%s %v", out, err)
	}
	out, err = s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(`{"query":"service","limit":2,"cursor":"`+first.NextCursor+`"}`))
	if err != nil || json.Unmarshal([]byte(out), &second) != nil || len(second.Repositories) != 2 || second.NextCursor != "" || first.Repositories[0].ID == second.Repositories[0].ID {
		t.Fatalf("%s %v", out, err)
	}
	for _, query := range []string{"Project 0", "project-0", "research", "SERVICE"} {
		out, err = s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(`{"query":"`+query+`"}`))
		if err != nil || !strings.Contains(out, "project-0") {
			t.Fatalf("query %s: %s %v", query, out, err)
		}
	}
	id := first.Repositories[0].ID
	out, err = s.toolRepositories(context.Background(), "repository_get", json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil || !strings.Contains(out, `"verified_at"`) || !strings.Contains(out, `"purpose"`) {
		t.Fatalf("details: %s %v", out, err)
	}
	other, _ := repositoryService(t, ruleStore{&memStore{}})
	other.id = "work"
	if _, err := other.toolRepositories(context.Background(), "repository_get", json.RawMessage(`{"id":"`+id+`"}`)); err == nil {
		t.Fatal("cross-assistant detail lookup")
	}
	for _, bad := range []string{`null`, `[]`, `{"query":null}`, `{"limit":null}`, `{"manager_id":"other"}`, `{"limit":0}`, `{"limit":51}`, `{"limit":1.5}`, `{"query":"` + strings.Repeat("x", 201) + `"}`, `{"cursor":"invalid"}`, `{"query":"changed","cursor":"` + first.NextCursor + `"}`} {
		if _, err := s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if *calls != 4 {
		t.Fatalf("reads re-probed %d times", *calls)
	}
	// Removed bindings never leak through list, search or details.
	if _, err := s.unbindRepository(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.toolRepositories(context.Background(), "repository_get", json.RawMessage(`{"id":"`+id+`"}`)); err == nil {
		t.Fatal("unbound detail")
	}
	if _, err := s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(`{"cursor":"`+first.NextCursor+`","query":"service"}`)); err == nil {
		t.Fatal("stale cursor must require restarting the list")
	}
}

func TestRepositoryRetrievalGuidanceContract(t *testing.T) {
	// These scenarios pin available guidance and tool behavior, NOT model choice.
	// No live model, launcher, private service or mandatory lookup preflight runs.
	list, get, plan := prompts.Tool("repositories_list"), prompts.Tool("repository_get"), prompts.Tool("task_plan")
	for _, scenario := range []struct {
		name, text string
		phrases    []string
	}{
		{"ambiguous repository", list.Description, []string{"resolve repository identity", "repository_get"}},
		{"explicit URL or path", plan.Description, []string{"explicit repository URL or path", "takes precedence"}},
		{"non-repository task", plan.Description, []string{"Non-repository tasks need no lookup", "not a mandatory dispatch preflight"}},
		{"reference not permission", get.Description, []string{"not authorization", "never infer", "machine", "checkout"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, phrase := range scenario.phrases {
				if !strings.Contains(scenario.text, phrase) {
					t.Errorf("guidance missing %q", phrase)
				}
			}
		})
	}
	for _, tool := range []string{"repositories_list", "repository_get"} {
		var schema map[string]any
		_ = json.Unmarshal(prompts.Tool(tool).InputSchema, &schema)
		properties := schema["properties"].(map[string]any)
		if _, exists := properties["manager_id"]; exists || schema["additionalProperties"] != false {
			t.Fatalf("scope-bearing schema: %s", tool)
		}
	}
}
