package node

import (
	"context"

	"github.com/nwverse/persona/server/internal/host"
)

// Every tab Persona opens for an agent is marked with personaTabToken (herdr keeps a pane's metadata tokens until the
// pane goes; a herdr restart drops them). When the agent exits — by itself, by /exit, or stopped by Persona
// (retire.go) — the tab stays with the shell it ran in (#4754, docs/integrations/engines.md "Agents run in a
// terminal"), for the user to read and Resume in, until it has sat unchanged for the grace period: then the node
// closes it (finished_close.go, #10111), on herdr only a tab with this mark. The mark names the session the terminal was
// opened for.

// personaTabToken marks a pane Persona opened for an agent; its value is the session id when the start knew it.
const personaTabToken = "persona_tab"

// markPersonaTab marks pane as one Persona opened for session id (empty when not known yet).
func markPersonaTab(ctx context.Context, launch host.Launcher, pane host.PaneRef, id string) {
	r, ok := launch.(host.MetadataReporter)
	if !ok || pane.ID == "" {
		return
	}
	if id == "" {
		id = "agent"
	}
	_ = r.ReportMetadata(ctx, pane, "persona", map[string]*string{personaTabToken: &id})
}

// namePersonaTab gives a pane Persona marked before its session was known ("agent": a Codex start, whose thread id
// comes once Codex runs) the session id (#10111): the mark then names the session as a Claude start's does, so the
// terminal is the session's kept shell (keptPane) and the grace-period close takes the session's recovery lock. A pane
// with no mark, or one naming another session, is left as it is.
func (n *Node) namePersonaTab(ctx context.Context, pane, id string) {
	if pane == "" || !validID(id) {
		return
	}
	r, ref, err := paneCap[host.MetadataReporter](n, pane)
	if err != nil {
		return
	}
	n.mu.Lock()
	mark, found := "", false
	for _, l := range n.layouts {
		for _, p := range l.panes {
			if p.PaneID == pane {
				mark, found = p.Tokens[personaTabToken], true
			}
		}
	}
	n.mu.Unlock()
	if found && mark != "agent" {
		return
	}
	_ = r.ReportMetadata(ctx, ref, "persona", map[string]*string{personaTabToken: &id})
}
