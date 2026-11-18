package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestLarkSummaryShowsMessageContextNotInternalFields(t *testing.T) {
	calls := []larkCall{
		{Method: "POST", Path: "/conversations/oc_1/messages", Body: `{"text":"I will check the build","clientId":"internal-client","expectedSenderId":"internal-sender"}`},
		{Method: "POST", Path: "/messages/om_1/replies", Body: `{"text":"I will check it too","replyMode":"thread"}`},
	}
	contexts := []larkCardContext{
		{OK: true, Chat: "Release Room"},
		{OK: true, Chat: "Release Room", ReplySender: "Example User", ReplyText: "Can we ship on Friday?", InTopic: true},
	}
	s := larkSummaryWithContext(olGate(""), calls, contexts, i18n.En)
	for _, want := range []string{"Send a message to “Release Room”", "I will check the build", "Reply to Example User", "Can we ship on Friday?", "(in a topic)", "I will check it too"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	for _, unwanted := range []string{"clientId", "expectedSenderId", "replyMode", "/conversations/oc_1/messages", "/messages/om_1/replies", "internal-client", "internal-sender"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("summary exposes %q:\n%s", unwanted, s)
		}
	}
}

func TestLarkSummaryMetadataFailureKeepsOldSummary(t *testing.T) {
	calls := []larkCall{{Method: "POST", Path: "/messages/om_1/replies", Body: `{"text":"hello","clientId":"internal"}`}}
	s := larkSummaryWithContext(olGate(""), calls, []larkCardContext{{}}, i18n.En)
	for _, want := range []string{"POST /messages/om_1/replies", `"text": "hello"`, `"clientId": "internal"`} {
		if !strings.Contains(s, want) {
			t.Errorf("fallback lacks %q:\n%s", want, s)
		}
	}
}

func TestLarkCardContextFromReadOnlyCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "lark-cli")
	script := `#!/bin/sh
case "$*" in
  *"/chats/oc_1"*) printf '%s\n' '{"chat":{"name":"Release Room"}}' ;;
  *"/messages/om_1"*) printf '%s\n' '{"message":{"chat_id":"oc_1","thread_id":"omt_1","sender":{"name":"Example User"},"body":{"content":"{\"text\":\"Can we ship on Friday?\"}"}}}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	n := &Node{}
	cfg := proto.AssistantLark{CLI: &proto.LarkCLI{Bin: bin}}
	calls := []larkCall{{Method: "POST", Path: "/messages/om_1/replies", Body: `{"text":"yes"}`}}
	got := n.larkCardContexts(context.Background(), cfg, calls)[0]
	if !got.OK || got.Chat != "Release Room" || got.ReplySender != "Example User" || got.ReplyText != "Can we ship on Friday?" || !got.InTopic {
		t.Fatalf("unexpected context: %+v", got)
	}
	summary, ok := larkMessageSummary(larkCall{Method: "POST", Path: "/messages/om_1/replies", Body: `{"text":"yes"}`}, got, i18n.En)
	if !ok || !strings.Contains(summary, "Release Room") || !strings.Contains(summary, "Can we ship on Friday?") {
		t.Fatalf("summary: %q", summary)
	}
}

func TestLarkCardContextFailureFallsBack(t *testing.T) {
	n := &Node{}
	cfg := proto.AssistantLark{CLI: &proto.LarkCLI{Bin: filepath.Join(t.TempDir(), "missing-lark-cli")}}
	calls := []larkCall{{Method: "POST", Path: "/conversations/oc_1/messages", Body: `{"text":"hello"}`}}
	got := n.larkCardContexts(context.Background(), cfg, calls)[0]
	if got.OK {
		t.Fatalf("missing CLI unexpectedly produced metadata: %+v", got)
	}
}
