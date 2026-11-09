package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const continuationSegment = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func continuationFixture(t *testing.T, path, thread, parent string) []byte {
	t.Helper()
	payload := map[string]any{"id": thread, "cwd": "/project"}
	if parent != "" {
		payload["session_id"], payload["parent_thread_id"] = parent, parent
		payload["source"] = map[string]any{"subagent": map[string]string{"other": "guardian"}}
	}
	header, err := json.Marshal(map[string]any{"type": "session_meta", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	body := append(header, '\n')
	body = append(body, []byte(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u-current","content":[{"type":"text","text":"current segment"}]}}}`+"\n")...)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCodexContinuationHasOneIdentityAndCurrentTranscript(t *testing.T) {
	n := remoteTestNode(t)
	now := time.Now()
	writeRollout(t, n.home, remoteRaw, "/project", now, now.Add(-time.Hour))
	home := canonicalCodexHome(filepath.Join(n.home, ".codex"), n.home) // the default home, as the node spells it
	original := rolloutPath(home, remoteRaw)
	old, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSuffix(original, ".jsonl") + "_" + continuationSegment + ".jsonl"
	body := continuationFixture(t, path, remoteRaw, "")
	expireRollouts(home, true)
	if got := rollouts(home); len(got) != 1 || got[0].id != remoteRaw || got[0].path != path {
		t.Fatalf("continuation inventory: %+v", got)
	}
	n.configureCodex(proto.CodexConfig{Homes: []string{home}})
	rows := n.codexSessions(map[string]bool{})
	if len(rows) != 1 || rows[0].SessionID != remoteRaw {
		t.Fatalf("duplicate sessions: %+v", rows)
	}
	// A process holding the original must not move reads back to the old segment.
	n.registerCodex(home, remoteRaw, original)
	if got := n.transcriptPath(remoteRaw); got != path {
		t.Fatalf("current transcript = %q, want %q", got, path)
	}
	for _, file := range []string{original, path} {
		if err := os.Chtimes(file, now, now); err != nil {
			t.Fatal(err)
		}
	}
	n.registerCodex(home, remoteRaw, original)
	if got := n.transcriptPath(remoteRaw); got != path {
		t.Fatal("equal filesystem timestamps changed the selected continuation")
	}
	out, err := request(t, n, "session.items", map[string]any{"session_id": remoteRaw, "limit": 80})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if !strings.Contains(string(encoded), "current segment") {
		t.Fatalf("current items missing: %s", encoded)
	}
	if got, _ := os.ReadFile(original); string(got) != string(old) {
		t.Fatal("original rollout was modified")
	}
	if got, _ := os.ReadFile(path); string(got) != string(body) {
		t.Fatal("continuation rollout was modified")
	}
}

// A node whose home is behind a link (a linked /home; macOS's /var for test homes) configures Codex when it starts,
// before ~/.codex exists: once Codex is used, the default home's sessions keep their raw thread IDs.
func TestCodexDefaultHomeMadeLaterBehindALink(t *testing.T) {
	n := remoteTestNode(t)
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(n.home, link); err != nil {
		t.Fatal(err)
	}
	n.home = link
	n.codices = newCodexRegistry()
	n.codices.scan = func() []codexProcess { return nil }
	n.configureCodex(proto.CodexConfig{}) // at start: no ~/.codex yet
	now := time.Now()
	writeRollout(t, link, remoteRaw, "/project", now, now)
	n.configureCodex(proto.CodexConfig{})
	rows := n.codexSessions(map[string]bool{})
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.SessionID)
	}
	if len(ids) != 1 || ids[0] != remoteRaw {
		t.Fatalf("the default home's sessions: %q, want [%q]", ids, remoteRaw)
	}
}

func TestCodexContinuationWaitsForHeaderAndKeepsRealSubagents(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".codex")
	now := time.Now()
	dir := filepath.Join(home, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-time-"+remoteRaw+"_"+continuationSegment+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := rollouts(home); len(got) != 0 {
		t.Fatalf("incomplete continuation advertised: %+v", got)
	}
	continuationFixture(t, path, remoteRaw, "")
	expireRollouts(home, false)
	if got := rollouts(home); len(got) != 1 || got[0].id != remoteRaw {
		t.Fatalf("completed metadata was not retried: %+v", got)
	}
	child := filepath.Join(dir, "rollout-child-"+remoteRaw+"_"+remoteOther+".jsonl")
	continuationFixture(t, child, remoteOther, remoteRaw)
	expireRollouts(home, false)
	got := rollouts(home)
	if len(got) != 2 {
		t.Fatalf("real subagent collapsed: %+v", got)
	}
	for _, r := range got {
		if r.id == remoteOther && (!r.sub || r.root != remoteRaw) {
			t.Fatalf("subagent metadata lost: %+v", r)
		}
	}
}

func TestOldCodexContinuationLookup(t *testing.T) {
	for _, archived := range []bool{false, true} {
		n := remoteTestNode(t)
		base := t.TempDir()
		old := time.Now().Add(-2 * codexWindow)
		writeRollout(t, base, remoteRaw, "/project", old, old)
		home := canonicalCodexHome(filepath.Join(base, ".codex"), n.home) // as the node spells it
		original := rolloutPath(home, remoteRaw)
		path := strings.TrimSuffix(original, ".jsonl") + "_" + continuationSegment + ".jsonl"
		if archived {
			path = filepath.Join(home, "archived_sessions", filepath.Base(path))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Rename(original, path); err != nil {
			t.Fatal(err)
		}
		if archived {
			// A child with a matching filename prefix must not hide the archived parent named by metadata.
			active := filepath.Join(filepath.Dir(original), "rollout-active-"+remoteRaw+"_"+remoteOther+".jsonl")
			continuationFixture(t, active, remoteOther, remoteRaw)
		}
		expireRollouts(home, true)
		n.configureCodex(proto.CodexConfig{Homes: []string{home}})
		id := codexSourceID(home, remoteRaw, n.codexInventory().LegacyHome)
		if got := n.oldCodexTranscript(id); got != path {
			t.Fatalf("old continuation lookup: %q, want %q", got, path)
		}
		if got := rolloutPath(home, remoteRaw); got != path {
			t.Fatalf("raw continuation lookup: %q, want %q", got, path)
		}
	}
}
