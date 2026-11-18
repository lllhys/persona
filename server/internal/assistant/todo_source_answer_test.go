package assistant

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestSourceReportFullTaskAnswer(t *testing.T) {
	forStores(t, func(t *testing.T, e *askEnv, _ *db.DB) {
		e.fullReply = true
		e.ask(ownerAsk("om_task_result", "/ai Check the release notes"))
		id := onlyTodo(e.s).ID
		if err := e.s.linkTodoAgent(t.Context(), id, "box", "agent01", "Check the release notes", "", "claude"); err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		e.f.setItems(
			proto.Item{Role: "user", Kind: proto.KindText, TS: at, Text: "Check the release notes"},
			proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: "I am running the checks."},
			proto.Item{Role: "assistant", Kind: proto.KindToolUse, TS: at, ToolName: "check"},
			proto.Item{Role: "tool", Kind: proto.KindToolResult, TS: at, Text: "Tool log"},
			proto.Item{ID: "final", Role: "assistant", Kind: proto.KindText, TS: at, Text: "Truncated", FullSize: 100},
			proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: "See the [release notes](https://example.com/notes)."},
			proto.Item{ID: "end", Kind: "turn_duration", TS: at},
		)
		final := "The notes are ready.\n\n- Fixed a crash\n- Added an example"
		e.f.answerNode("session.item", func(machine string, args json.RawMessage) (any, error) {
			var req struct {
				SessionID string `json:"session_id"`
				ItemID    string `json:"item_id"`
			}
			_ = json.Unmarshal(args, &req)
			if machine != "box" || req.SessionID != "agent01" || req.ItemID != "final" {
				t.Errorf("unexpected transcript read %s %s", machine, args)
			}
			return proto.Item{ID: "final", Role: "assistant", Kind: proto.KindText, TS: at, Text: final}, nil
		})
		e.closeDone(id)
		want := final + "\n\nSee the [release notes](https://example.com/notes)."
		notice := e.said("source_report_full_notice", i18n.Args{"minutes": 10, "text": want})
		if !slices.ContainsFunc(textsTo(e.f, askHome), func(p map[string]any) bool {
			return p["text"] == notice && p["full_answer"] == true
		}) {
			t.Fatal("the owner did not get the complete answer preview")
		}
		if source := onlyTodo(e.s).Source; !source.ReportFullReply || source.ReportText != want {
			t.Fatalf("planned reply: %+v", source)
		}
		e.clock.advance(sourceReportDelay - time.Second)
		if got := reportsTo(e.f, askSrc, "om_task_result"); len(got) != 0 {
			t.Fatalf("reported during the preview: %v", got)
		}
		e.clock.advance(time.Second)
		if got := reportsTo(e.f, askSrc, "om_task_result"); len(got) != 1 || got[0]["text"] != want || got[0]["full_answer"] != true || got[0]["no_detour"] != true {
			t.Fatalf("the original message did not get the full answer: %v", got)
		}
	})
}

func TestSourceReportMainAnswerWaitsForItsOwnTurn(t *testing.T) {
	forStores(t, func(t *testing.T, e *askEnv, database *db.DB) {
		minutes := 0
		e.fullReply, e.reportDelay = true, &minutes
		if database != nil {
			e.home = "" // cover the private-chat handoff as well as the topic
		}
		e.ask(ownerAsk("om_main_result", "/ai What shipped?"))
		todo := onlyTodo(e.s)
		handoff := todo.Source.Handoff
		if handoff == "" {
			handoff = todo.Source.Anchor
		}
		at := time.Now().UTC()
		prompt := proto.Item{
			Role: "user", Kind: proto.KindText, TS: at, Text: "What shipped?",
			ClientMsgID: channel.ClientMsgID(channel.Feishu, handoff),
		}
		e.f.setItems(prompt)
		e.closeDone(todo.ID)
		e.clock.advance(0)
		if src := onlyTodo(e.s).Source; src.ReportDueAt != "" {
			t.Fatal("planned a status reply while its own turn was still finishing")
		}
		answer := "This release includes:\n\n1. A fix\n2. A guide"
		e.f.setItems(prompt,
			proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: answer},
			proto.Item{ID: "end", Kind: "turn_duration", TS: at},
			proto.Item{Role: "user", Kind: proto.KindText, TS: at, Text: "Separate private request"},
			proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: "Separate private answer"},
			proto.Item{ID: "other_end", Kind: "turn_duration", TS: at},
		)
		e.clock.advance(5 * time.Second)
		if got := reportsTo(e.f, askSrc, "om_main_result"); len(got) != 1 || got[0]["text"] != answer {
			t.Fatalf("expected the handoff's answer, got %v", got)
		}
	})
}

func TestSourceReportDoesNotShareAnUnrelatedTurn(t *testing.T) {
	e := newAskEnv(t, nil)
	minutes := 0
	e.fullReply, e.reportDelay = true, &minutes
	e.ask(ownerAsk("om_safe_result", "/ai What shipped?"))
	todo := onlyTodo(e.s)
	at := time.Now().UTC()
	e.f.setItems(
		proto.Item{Role: "user", Kind: proto.KindText, TS: at, Text: "What shipped?", ClientMsgID: channel.ClientMsgID(channel.Feishu, todo.Source.Handoff)},
		proto.Item{Role: "user", Kind: proto.KindText, TS: at, Text: "Unrelated request"},
		proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: "Unrelated answer"},
		proto.Item{ID: "end", Kind: "turn_duration", TS: at},
	)
	e.closeDone(todo.ID)
	e.clock.advance(0)
	if got := reportsTo(e.f, askSrc, "om_safe_result"); len(got) != 1 || got[0]["text"] != e.said("source_report_no_final") {
		t.Fatalf("expected the safe outcome fallback, got %v", got)
	}
}

func TestSourceReportFullPreviewCanBeCancelled(t *testing.T) {
	e := newAskEnv(t, nil)
	e.fullReply = true
	e.ask(ownerAsk("om_cancellable_result", "/ai What shipped?"))
	todo := onlyTodo(e.s)
	at := time.Now().UTC()
	e.f.setItems(
		proto.Item{Role: "user", Kind: proto.KindText, TS: at, Text: "What shipped?", ClientMsgID: channel.ClientMsgID(channel.Feishu, todo.Source.Handoff)},
		proto.Item{Role: "assistant", Kind: proto.KindText, TS: at, Text: "A guide shipped."},
		proto.Item{ID: "end", Kind: "turn_duration", TS: at},
	)
	e.closeDone(todo.ID)
	e.deliver(topicEventIn("om_cancel_result", askHome, "omt_home", "/noreply"))
	note := lastNote(t, e.f)
	if !strings.Contains(note, e.said("source_noreply_done")) {
		t.Fatalf("cancel reply %q", note)
	}
	e.clock.advance(sourceReportDelay)
	if got := reportsTo(e.f, askSrc, "om_cancellable_result"); len(got) != 0 {
		t.Fatalf("the cancelled reply went out: %v", got)
	}
}
