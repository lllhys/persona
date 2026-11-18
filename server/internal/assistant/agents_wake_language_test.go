package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Inspect the session.send payload, not just a directly rendered template.
func TestWakeUsesOwnerLanguageAtDelivery(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"en", "Persona is waking you"}, {"ja", "Personaからの再開確認"}, {"zh-Hans", "Persona 叫醒你"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			s, f := workService(t)
			f.hello.Locale = tc.locale
			f.triage = `{"phase":"blocked","on":"external","line":"Waiting for the build","wait":{"what":"sample build","then":"read the result","check_minutes":5}}`
			f.setItems(proto.Item{Kind: proto.KindText, Role: "assistant", Text: "I will check after the sample build finishes."})
			f.setSessions("box", stopped("s1", "Waiting for the sample build."))
			triage(t, s, "s1")
			s.workTick(context.Background(), time.Now().Add(6*time.Minute))
			got := f.sentToAgents()
			if len(got) != 1 || !strings.Contains(got[0], tc.want) {
				t.Fatalf("session.send language %s: %q", tc.locale, got)
			}
			text := strings.TrimPrefix(got[0], "s1: ")
			in := prompts.ParseInjected(text)
			if in == nil || in.State != "wake" || in.Title != "sample build" {
				t.Fatalf("delivered wake metadata: %+v", in)
			}
		})
	}
}

func TestGoldenLocalizedAgentWake(t *testing.T) {
	for _, tc := range []struct{ lang, suffix string }{{"English", "en"}, {"日本語", "ja"}} {
		for _, variant := range []struct {
			name string
			wake agentWake
		}{
			{"timer", agentWake{Attempt: 2, Wait: "sample build", Then: "read the result"}},
			{"agent", agentWake{Attempt: 1, Wait: "sample worker", Then: "review", Agent: "Sample worker", AgentLine: "The report is ready."}},
			{"approved", agentWake{Attempt: 1, Wait: "checks", Then: "publish sample", Approved: "publish sample"}},
			{"task_approved", agentWake{Attempt: 1, Wait: "checks", Then: "publish sample", Approved: "publish sample", ApprovedByTask: true}},
		} {
			variant.wake.Language = tc.lang
			golden(t, "agent_wake."+variant.name+"."+tc.suffix, renderWake(variant.wake))
		}
	}
}
