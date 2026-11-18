package node

import (
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A chat whose history is the App's database has no turn_context to say what it runs with: its row's model and effort
// are the App's own, from its database's model and reasoning_effort (#6501). A schema without them lists as before.
func TestCodexAppChatModelAndEffortFromTheAppsDatabase(t *testing.T) {
	for _, c := range []struct {
		name, schema, set string
		model, effort     string
		efforts           []string
	}{
		{"with the columns", codexAppSchema, `ALTER TABLE threads ADD COLUMN model TEXT; ALTER TABLE threads ADD COLUMN reasoning_effort TEXT;` +
			`UPDATE threads SET model = 'gpt-x', reasoning_effort = 'ultra';`, "gpt-x", "ultra", []string{"low", "ultra"}},
		{"an older schema", codexAppSchema, "", "", "", codexEffortLevels},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, _ := startFakeCodexBus(t, appChat)
			project := t.TempDir()
			writeCodexApp(t, home, project, c.schema+codexAppRow(appChat, "", project, "Alpha chat", time.Now(), 0)+c.set)
			n := remoteTestNode(t)
			n.configureCodex(proto.CodexConfig{Homes: []string{home}})
			withCodexAppModels(t, canonicalCodexHome(home, n.home), map[string]codexModelEfforts{"gpt-x": {levels: []string{"low", "ultra"}}})
			var chat proto.Session
			for _, s := range n.codexSessions(map[string]bool{}) {
				if s.SourceThreadID == appChat {
					chat = s
				}
			}
			if chat.SessionID == "" || chat.Title != "Alpha chat" {
				t.Fatalf("not listed: %+v", chat)
			}
			if chat.Model != c.model || chat.Effort != c.effort || !slices.Equal(chat.Efforts, c.efforts) {
				t.Fatalf("model %q effort %q efforts %v, want %q %q %v", chat.Model, chat.Effort, chat.Efforts, c.model, c.effort, c.efforts)
			}
		})
	}
}

// What the chat's rollout ran with last, and the phone's choice after it, come before the database's.
func TestCodexAppChatEffortOrder(t *testing.T) {
	n, id, _, _ := appEffortChat(t) // its last turn: gpt-x at high
	src, _ := n.codexSource(id)
	codexAppDBs.Lock()
	db := &codexAppDB{at: time.Now(), byID: map[string]codexAppThread{appChat: {ID: appChat, Model: "gpt-db", Effort: "low"}}}
	codexAppDBs.byHome[src.home] = db
	codexAppDBs.Unlock()
	t.Cleanup(func() {
		codexAppDBs.Lock()
		delete(codexAppDBs.byHome, src.home)
		codexAppDBs.Unlock()
	})
	s := proto.Session{SessionID: id, Model: "gpt-x", Capabilities: &proto.SessionCapabilities{Send: true}}
	n.codexDesktopEffortRow(&s, src, n.metaOf(id))
	if s.Model != "gpt-x" || s.Effort != "high" {
		t.Fatalf("rollout over database: %q %q", s.Model, s.Effort)
	}
	s = proto.Session{SessionID: id, Capabilities: &proto.SessionCapabilities{Send: true}}
	n.codexDesktopEffortRow(&s, src, quickMeta{})
	if s.Model != "gpt-db" || s.Effort != "low" {
		t.Fatalf("nothing else said: %q %q", s.Model, s.Effort)
	}
}
