package node

import (
	"context"

	"github.com/nwverse/persona/server/internal/prompts"
)

// A message the user sends while a Claude conversation is still answering is written to the running process, and
// Claude Code takes it up only at the next tool boundary: behind a long foreground command (a download without a
// timeout, a wait loop) the user waits minutes for any reaction. With the assistant's interject_background setting on,
// the node asks Claude Code to move its foreground work to the background right after writing such a message, as
// Ctrl+B does in the terminal, so the model sees the message at once; the work goes on and its completion comes back
// as a task notification (#3278).

// backgroundTasksMinVersion is the first Claude Code whose stream-json control channel takes background_tasks
// (Agent SDK 0.2.139).
var backgroundTasksMinVersion = [3]int{2, 1, 139}

// interjects says whether a message to p now goes in as an interjection: it is Claude on the CLI, busy answering, and
// its init said a Claude Code that takes background_tasks. Called before the message marks it busy.
func (p *assistantProc) interjects() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agent == "claude" && p.runtime == "" && p.busy && versionAtLeast(p.claudeVersion, backgroundTasksMinVersion)
}

// interjectNote goes with the message, after the live snapshot: answer this first.
func interjectNote() map[string]any {
	return map[string]any{"type": "text", "text": prompts.RenderInterject()}
}

// backgroundForeground moves what p runs in the foreground (Bash commands, subagents) to the background. Best effort:
// the message is in already, and a refusal only means it waits for the next tool boundary as before.
func (n *Node) backgroundForeground(p *assistantProc) {
	go func() {
		if err := p.control(context.Background(), map[string]any{"subtype": "background_tasks"}); err != nil {
			n.logf("assistant %s: move foreground work to the background for a message: %v", short(p.id), err)
			return
		}
		n.logger.Printf("assistant %s: a message came while it answered: foreground work moved to the background", short(p.id))
	}()
}
