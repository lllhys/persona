package node

import (
	"errors"
	"os"
	"path/filepath"
)

// The registry remembers routing identities, not whether a conversation is still
// listed. Reconcile moved rollouts before consulting stale Desktop/remote caches.
// Keep archived transcripts addressable by ID without advertising them as agents.
// pass (codexListPass) keeps the stat for the row and each directory's resolved path for the threads beside it.
func (n *Node) listedCodexSource(id string, src codexSource, pass *codexListPass) (codexSource, bool) {
	if src.path != "" {
		if _, err := pass.stat(src.path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				n.logf("codex inventory: stat %s: %v", src.thread, err)
				return src, true
			}
			src.path = rolloutPath(src.home, src.thread)
		}
	} else {
		src.path = rolloutPath(src.home, src.thread)
	}
	if src.path != "" {
		return src, pass.realPath(filepath.Dir(src.path)) != filepath.Join(src.home, "archived_sessions")
	}
	// A live app-server thread may not have written a rollout yet. A remembered
	// identity alone (after deletion or disconnection) is not an offline session.
	_, _, err := n.codexOwner(id)
	return src, err != errCodexNoOwner
}
