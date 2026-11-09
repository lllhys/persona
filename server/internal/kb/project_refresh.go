package kb

import (
	"context"
	"sort"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/prompts"
)

// projectRetry is a project whose rewrite failed: how many times in a row, and when it may be tried again. Kept in the
// engine's memory, not in the project's file: PutProject would bump its Updated time and tell the clients about a
// change that is not one, and after a restart one more try is cheap.
type projectRetry struct {
	failures int
	at       time.Time
}

type projectDigest struct {
	Summary    string     `json:"summary"`
	OneLine    string     `json:"one_line"`
	Goal       string     `json:"goal"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Importance string     `json:"importance"`
	Relevance  modelScore `json:"relevance"`
	Tags       []string   `json:"tags"`
	Keywords   []string   `json:"keywords"`
}

// RefreshProjects rewrites the summaries of projects with news, at most max per run. A project whose rewrite failed
// waits like a chat whose compression failed (retryWait), so a model that keeps failing (2026-11-08: a model name the
// endpoint did not serve, 404 every two minutes) does not cost a call and a log line every round. Only the knowledge
// base loop calls it.
func (e *Engine) RefreshProjects(ctx context.Context, max int) int {
	if e.Model == nil {
		return 0
	}
	now := time.Now()
	var dirty []*Project
	live := map[string]bool{}
	for _, p := range e.Store.Projects() {
		if p.Dirty && p.Status != "ignored" && p.Live() {
			live[p.Slug] = true
			if now.Before(e.projectRetries[p.Slug].at) {
				continue
			}
			dirty = append(dirty, p)
		}
	}
	for slug := range e.projectRetries { // done, merged or gone in the meantime
		if !live[slug] {
			delete(e.projectRetries, slug)
		}
	}
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].Relevance > dirty[j].Relevance })
	n := 0
	for _, p := range dirty {
		if n >= max || ctx.Err() != nil {
			break
		}
		if err := e.refreshProject(ctx, p); err != nil {
			if ai.IsPaused(err) || ctx.Err() != nil { // the connection is held or the run ended: the rest wait (still dirty)
				break
			}
			if e.projectRetries == nil {
				e.projectRetries = map[string]projectRetry{}
			}
			r := e.projectRetries[p.Slug]
			r.failures++
			wait := retryWait(r.failures)
			r.at = time.Now().Add(wait)
			e.projectRetries[p.Slug] = r
			e.logf("project %s: %v (failed %d time(s) in a row, next try in %s)", p.Slug, err, r.failures, wait)
			continue
		}
		delete(e.projectRetries, p.Slug)
		n++
	}
	return n
}

func (e *Engine) refreshProject(ctx context.Context, p *Project) error {
	current, ok := e.Store.Project(p.Slug)
	if !ok {
		return nil
	}
	p = current
	in := e.projectInput(p)
	var d projectDigest
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := e.modelFor("kb.project").JSONMax(cctx, prompts.RenderKBProject(prompts.KBOwner{Owner: e.Owner}), prompts.RenderKBProjectInput(in), 8000, &d); err != nil {
		return err
	}
	return e.commitProject(p, in, d)
}

func (e *Engine) projectInput(p *Project) prompts.KBProjectInput {
	in := prompts.KBProjectInput{Pinned: p.Pinned, Name: p.Name, Kind: p.Kind, Aliases: p.Aliases, Tags: e.TopTags(40),
		Summary: e.Store.Summary(KindProject, p.Slug), Log: e.Store.Log(p.Slug, 40)}
	for i, cid := range p.Chats {
		if i >= 8 {
			break
		}
		sum := e.Store.Summary(KindChat, cid)
		if r := []rune(sum); len(r) > 1500 {
			sum = string(r[:1500]) + "…"
		}
		in.Chats = append(in.Chats, prompts.KBProjectChat{ID: cid, Summary: sum})
	}
	in.Moments = e.projectMomentsForPrompt(p, 5)
	return in
}
