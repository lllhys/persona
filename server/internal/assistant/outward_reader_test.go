package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/kb"
)

// The private parts of a person's profile (the user's words about them, the guesses) never reach a conversation that
// writes to other people: the inbox's AI twin, Lark /agent, one an extension opened. kb_get shows them only the facts
// and commitments, kb_search finds only those lines. The user's own conversations, the main one and a channel's
// included, read everything as before.
func TestOutwardConversationsReadNoPrivateProfile(t *testing.T) {
	st := &memStore{}
	s := newKBServiceOn(t, newFakeHub(), st)
	e := kbEng(t, s)
	if err := e.Store.PutPerson(&kb.Person{ID: "p1", Name: "Example Person"}); err != nil {
		t.Fatal(err)
	}
	profile := &kb.PersonProfile{
		Facts:       []kb.ProfileItem{{Text: "leads the sample project", Sources: []string{"chats/dm-a"}}},
		Views:       []kb.ProfileItem{{Text: "too slow to answer", Sources: []string{"user"}}},
		Inferences:  []kb.ProfileItem{{Text: "may be leaving soon", Basis: "asked about other teams", Sources: []string{"chats/dm-a"}}},
		Commitments: []kb.ProfileCommitment{{Text: "send the sample notes", Owes: "user", Sources: []string{"chats/dm-a"}}},
	}
	if _, err := e.ApplyExtension(kb.KindPerson, "p1", kb.Update{Profile: profile}, kb.ExtensionWrite{Extension: "people"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s.originSet(ctx, "mac", "twin", OriginInbox, "item-1", "")
	s.originSet(ctx, "mac", "lark", OriginLarkAgent, "lark", "lark")
	s.originSet(ctx, "mac", "ext", OriginExtension, "", "sample-ext")
	s.originSet(ctx, "mac", "opened", OriginUser, "", "sample-ext") // opened by an extension's token
	s.originSet(ctx, "mac", "chan", OriginChannel, "feishu", "")
	s.originSet(ctx, "mac", "mine", OriginUser, "", "")

	private := []string{"too slow to answer", "may be leaving soon", "asked about other teams", "[用户原话]", "[推断·仅供参考]"}
	shared := []string{"[事实] leads the sample project", "send the sample notes"}
	read := func(self string) (get, search string) {
		t.Helper()
		get, err := runTool(s, self, "kb_get", map[string]any{"kind": "people", "id": "p1"})
		if err != nil {
			t.Fatalf("%s kb_get: %v", self, err)
		}
		search, err = runTool(s, self, "kb_search", map[string]any{"query": "chats/dm-a", "kind": "people", "messages": false, "limit": 2})
		if err != nil {
			t.Fatalf("%s kb_search: %v", self, err)
		}
		return get, search
	}
	for _, self := range []string{"twin", "lark", "ext", "opened"} {
		get, search := read(self)
		for _, p := range private {
			if strings.Contains(get, p) || strings.Contains(search, p) {
				t.Errorf("%s read %q:\n%s\n%s", self, p, get, search)
			}
		}
		for _, w := range shared {
			if !strings.Contains(get, w) || !strings.Contains(search, w) {
				t.Errorf("%s lost %q:\n%s\n%s", self, w, get, search)
			}
		}
		if n := strings.Count(search, "\n"); n != 2 {
			t.Errorf("%s: the skipped lines took up the limit:\n%s", self, search)
		}
	}
	for _, self := range []string{"mine", "chan", "unknown"} {
		get, _ := read(self)
		for _, w := range append(private, shared...) {
			if !strings.Contains(get, w) {
				t.Errorf("%s lost %q:\n%s", self, w, get)
			}
		}
	}

	// A conversation not noted yet while a start is under way on its machine might be one: it reads less.
	done := s.beginAssistantStart("mac")
	if get, _ := read("starting"); strings.Contains(get, "too slow to answer") {
		t.Errorf("a conversation starting read a view:\n%s", get)
	}
	done()
	if get, _ := read("starting"); !strings.Contains(get, "too slow to answer") {
		t.Errorf("the user's conversation lost the view once no start was under way:\n%s", get)
	}

	// Where conversations come from cannot be read: every conversation reads less.
	s.origins.mu.Lock()
	s.origins.loaded = false
	s.origins.mu.Unlock()
	st.mu.Lock()
	st.failSetting = errors.New("down")
	st.mu.Unlock()
	if get, _ := read("mine"); strings.Contains(get, "too slow to answer") || !strings.Contains(get, "leads the sample project") {
		t.Errorf("origins unreadable:\n%s", get)
	}
}

// Whatever the tool, a private line quoted in what it returns is dropped for such a conversation (conversation_read
// of a chat that showed a profile, say), and kept for the others.
func TestOutwardTextDropsQuotedPrivateLines(t *testing.T) {
	quoted := "user: who is it?\nassistant: - [用户原话] “too slow to answer”（出处：user）\n- [事实] leads the sample project"
	if got := outwardText(true, quoted); strings.Contains(got, "too slow") || !strings.Contains(got, "leads the sample project") {
		t.Fatalf("outward: %q", got)
	}
	if got := outwardText(false, quoted); got != quoted {
		t.Fatalf("the user's: %q", got)
	}
}
