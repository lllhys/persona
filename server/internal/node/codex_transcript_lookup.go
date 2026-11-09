package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// oldCodexTranscript resolves an opaque identity after its rollout has aged out
// of discovery and the node has restarted. This is a read lookup, not inventory:
// do not register the source, which would advertise old or archived conversations.
func (n *Node) oldCodexTranscript(id string) string {
	if len(id) != len("c0de-")+32 || !validID(id) {
		return ""
	}
	cfg := n.codexInventory()
	for _, configured := range cfg.Homes {
		home := canonicalCodexHome(configured, n.home)
		if home == "" || home == cfg.LegacyHome {
			continue
		}
		for _, pattern := range []string{
			filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"),
			filepath.Join(home, "archived_sessions", "rollout-*.jsonl"),
		} {
			paths, err := filepath.Glob(pattern)
			if err != nil {
				n.logf("codex transcript lookup: %v", err)
				continue
			}
			var matches []string
			for _, path := range paths {
				thread := rolloutID(path)
				if thread == "" {
					continue
				}
				// Configured home aliases are canonicalized, but a nested symlink
				// must not make an identity for this home read another home's file.
				real, err := filepath.EvalSymlinks(path)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						n.logf("codex transcript lookup: %v", err)
					}
					continue // A concurrently moved or unavailable transcript is not readable.
				}
				if rolloutID(real) != thread {
					continue
				}
				rel, err := filepath.Rel(home, real)
				if err != nil {
					n.logf("codex transcript lookup: %v", err)
					continue
				}
				if !(strings.HasPrefix(rel, "sessions"+string(filepath.Separator)) ||
					strings.HasPrefix(rel, "archived_sessions"+string(filepath.Separator))) {
					continue
				}
				st, err := os.Stat(real)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						n.logf("codex transcript lookup: %v", err)
					}
					continue
				}
				if st.Mode().IsRegular() && codexSourceID(home, rolloutThreadID(home, real), cfg.LegacyHome) == id {
					matches = append(matches, real)
				}
			}
			if len(matches) > 0 {
				return newestFirst(matches)[0]
			}
		}
	}
	return ""
}
