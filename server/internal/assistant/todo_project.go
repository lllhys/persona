package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/kb"
)

// A todo's project (#2581, docs/assistant/todos.md "Project"): the knowledge-base project it belongs to, usually a
// long-term one standing for an area of the user's work. The apps group open todos by it, and a finished task's outcome
// goes to its timeline (outcomeLog). It is kept as the project's slug. A project merged away later still takes the
// outcomes (ownProject follows the merge); the lists and the apps keep showing the old slug until it is set again.

// todoProject is the slug of the live knowledge-base project name names (its slug, name or an alias); "" for "". An
// error when the knowledge base can't be read or has no such project.
func (s *Service) todoProject(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	e, err := s.kbGet(ctx)
	if err != nil {
		return "", fmt.Errorf("project %q can't be checked: %w", name, err)
	}
	p := e.FindProject(name)
	if p == nil || !p.Live() || p.Status == "ignored" {
		return "", fmt.Errorf("no knowledge-base project %q: give the slug or name of an existing one (kb_list projects)", name)
	}
	return p.Slug, nil
}

// setTodoProject puts project (nil: unchanged; "" clears it) into e as the slug it names.
func (s *Service) setTodoProject(ctx context.Context, project *string, e *db.TodoEdit) error {
	if project == nil {
		return nil
	}
	slug, err := s.todoProject(ctx, *project)
	if err != nil {
		return err
	}
	e.Project = &slug
	return nil
}

// ownProject is the live project todo t was put in, nil when it has none or it is gone.
func ownProject(e *kb.Engine, t Todo) *kb.Project {
	if t.Project == "" {
		return nil
	}
	p := e.FindProject(t.Project)
	if p == nil || !p.Live() || p.Status == "ignored" {
		return nil
	}
	return p
}
