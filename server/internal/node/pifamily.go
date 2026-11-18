package node

import "time"

// The agents Persona reads as pi (pi.go): pi itself, and omp (Oh My Pi), a pi fork whose extension API, session files and
// conversation model are pi's (docs/integrations/omp.md). A pane and its keys are the host's; the conversation and the
// control (send, interrupt, cards, live text) are the Persona extension in that same process. Both start in herdr and
// resume there, with their own command.

const (
	piAgent  = "pi"
	ompAgent = "omp"
)

// piFamily says whether agent is one Persona reads as pi.
func piFamily(agent string) bool { return agent == piAgent || agent == ompAgent }

// piFamilyNoExtension is what a running agent of the family says without Persona's extension: its terminal only, and
// how to install the extension.
func piFamilyNoExtension(agent string) string {
	if agent == ompAgent {
		return "这个 omp 没有装 Persona 的扩展：手机上只能看终端。装上（omp install <Persona>/integrations/omp）再重开 omp，就能看对话、发消息"
	}
	return piNoExtension
}

// piFamilyCode is the code a refused send to a running agent of the family carries without its extension (pi's own
// code is pi_no_extension, unchanged).
func piFamilyCode(agent string) string { return agent + "_no_extension" }

// familyNote is an omp pane in this pass and the session it reports, empty while it is listed by its fallback id.
type familyNote struct {
	pane    string
	session string
}

// familySeenPane is what the node remembers of an omp pane: when it was first seen (zero: it was there when the node
// started) and the last session it reported.
type familySeenPane struct {
	seen    time.Time
	session string
}

// noteFamilyPanes remembers each omp pane, whether it is listed by its fallback id (omp-w1-p2) or by its session, and
// forgets the panes gone. Claude's own first-seen map is paneSeen (panes_bind.go); this one is omp's, so a stopped
// session's file can be told from the one a live omp is writing (ompdisk.go), also when a pane's session id goes
// missing again after it was reported. A pane that reports another session than before was first seen now: what its
// directory had before is not its file.
func (n *Node) noteFamilyPanes(notes []familyNote) {
	n.mu.Lock()
	defer n.mu.Unlock()
	first := n.familySeen == nil
	if first {
		n.familySeen = map[string]familySeenPane{}
	}
	now, here := time.Now(), make(map[string]bool, len(notes))
	for _, a := range notes {
		here[a.pane] = true
		e, ok := n.familySeen[a.pane]
		switch {
		case !ok && first:
			e = familySeenPane{session: a.session}
		case !ok:
			e = familySeenPane{seen: now, session: a.session}
		case a.session != "" && e.session != "" && a.session != e.session:
			e = familySeenPane{seen: now, session: a.session}
		case a.session != "":
			e.session = a.session
		}
		n.familySeen[a.pane] = e
	}
	for p := range n.familySeen {
		if !here[p] {
			delete(n.familySeen, p)
		}
	}
}

// familyPane is a live omp pane the node knows only by its fallback id: where it runs, when the pane was first seen
// (zero: it was there before this node started) and the session it last reported, if it ever did.
type familyPane struct {
	cwd     string
	seen    time.Time
	session string
}

// ompFallbackPanes are those panes now. A pi pane is never one: pi writes its sessions under its own agent directory,
// not omp's.
func (n *Node) ompFallbackPanes() []familyPane {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []familyPane
	for id, a := range n.agents {
		if a.PaneID == "" || a.Agent != ompAgent || validID(id) {
			continue // a real session: its row is the session's, not the pane's
		}
		e := n.familySeen[a.PaneID]
		out = append(out, familyPane{cwd: a.Cwd, seen: e.seen, session: e.session})
	}
	return out
}

// piFamilyIDGrace is how long a start of the family waits for the extension to report the new session's id after the
// host has reported its pane: without one the pane's fallback id (omp-w1-p2) is returned instead.
var piFamilyIDGrace = 8 * time.Second

// paneAlias is a fallback row id (omp-w1-p2) and the pane it is the row of.
type paneAlias struct{ id, pane string }

// resolveAlias turns a fallback row id a client opened (or sent to) into the session its pane turned out to run.
func (n *Node) resolveAlias(id string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if a, ok := n.idAlias[id]; ok {
		return a.id
	}
	return id
}
