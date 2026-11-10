package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestTriageRequestFormatting(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"**Should I push to beta?**", true},
		{"…? 🙏", true},
		{"…? (y/n)", true},
		{"「要推吗？」", true},
		{"Let me know which option you prefer.", true},
		{"Please open …", true},
		{"请回复 1 或 2", true},
		{"麻烦确认一下是否推送", true},
		{"Open the authorization page.", true},
		{"Visit the sign-in page.", true},
		{"Reply with your choice.", true},
		{"请帮我确认环境", true},
		{"请在这两个方案里选一个", true},
		{"你决定用哪个方案", true},
		{"Confirm to continue.", true},
		{"`Should I continue?` (yes/no) 🙏🏻  ", true},
		{"（要推吗？）", true},
		{"『要推吗？』", true},
		{"【要推吗？】", true},
		{"\"Should I continue?\"", true},
		{"Should I continue? (yes/no) [one choice]", true},
		{"好，收到 worker-a 的结果后我直接在 #123 发评论。目前还没收到结果。", false},
		{"收到，不用再确认了，结果一到我就直接在 #123 发评论。", false},
		{"**I will post after the checks pass.** 🙏", false},
		{"I will wait (for the test results).", false},
		{"", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := explicitUserRequest(tc.text); got != tc.want {
				t.Fatalf("explicitUserRequest=%v, want %v", got, tc.want)
			}
			if tc.text == "" {
				return
			}
			s, f := workService(t)
			out, err := json.Marshal(triageOut{Phase: proto.WorkBlocked, On: proto.WaitUser,
				Questions: []proto.Question{{Question: tc.text}}})
			if err != nil {
				t.Fatal(err)
			}
			f.triage = string(out)
			f.setSessions("box", stopped("s1", tc.text))
			triage(t, s, "s1")
			if got := len(s.workCards()) == 1; got != tc.want {
				t.Fatalf("card=%v, want %v", got, tc.want)
			}
			// Recognizing formatting does not let the model invent a request absent from the reply.
			if tc.want {
				s, f = workService(t)
				f.triage = string(out)
				f.setSessions("box", stopped("s1", "The checks are still running."))
				triage(t, s, "s1")
				if len(s.workCards()) != 0 {
					t.Fatal("accepted a request absent from the reply")
				}
			}
		})
	}
}

func answeredRecord(question, answer string) db.InteractionRecord {
	return db.InteractionRecord{Status: "resolved", Interaction: proto.Interaction{Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: question}}}, Resolution: &proto.Resolution{Answers: map[string]string{question: answer}}}
}

func TestTriageHistoryExcludesNonDecisions(t *testing.T) {
	for _, lang := range i18n.Langs {
		for _, key := range []string{answerSkip, answerNotNow, answerHold, answerWait, answerStop} {
			t.Run(string(lang)+"/"+key, func(t *testing.T) {
				rec := answeredRecord("Continue?", answerLabel(lang, key))
				if got := triageAnswered([]db.InteractionRecord{rec}); len(got) != 0 {
					t.Fatalf("deferral became a decision: %+v", got)
				}
			})
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*db.InteractionRecord)
	}{
		{"structured defer", func(r *db.InteractionRecord) {
			r.Interaction.Questions[0].Options = []proto.QuestionOption{{Label: "Custom pause", Action: proto.QuestionOptionDefer}}
			r.Resolution.Answers["Continue?"] = "Custom pause"
		}},
		{"legacy defer", func(r *db.InteractionRecord) {
			r.Interaction.Questions[0].Options = []proto.QuestionOption{{Label: "先不管，稍后再决定"}}
			r.Resolution.Answers["Continue?"] = "先不管，稍后再决定"
		}},
		{"assistant answer", func(r *db.InteractionRecord) { r.Resolution.AnsweredBy = proto.AnsweredByAssistant }},
		{"permission", func(r *db.InteractionRecord) { r.Interaction.Kind = proto.IxPermission }},
		{"pending", func(r *db.InteractionRecord) { r.Status = "pending" }},
		{"cancelled", func(r *db.InteractionRecord) { r.Status = "cancelled" }},
		{"cancel", func(r *db.InteractionRecord) { r.Resolution.Cancel = true }},
		{"dismiss", func(r *db.InteractionRecord) { r.Resolution.Dismiss = true }},
		{"missing resolution", func(r *db.InteractionRecord) { r.Resolution = nil }},
		{"empty answer", func(r *db.InteractionRecord) { r.Resolution.Answers["Continue?"] = "  " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := answeredRecord("Continue?", "Use beta")
			tc.edit(&rec)
			got := triageAnswered([]db.InteractionRecord{rec, answeredRecord("Which target?", "Use beta")})
			if len(got) != 1 || got[0].Question != "Which target?" || got[0].Answer != "Use beta" {
				t.Fatalf("wrong decision history: %+v", got)
			}
		})
	}
}

func TestTriageHistoryBoundsPairsAndRunes(t *testing.T) {
	// The store orders newest first. Several questions on one card count as separate pairs.
	records := []db.InteractionRecord{answeredRecord("Pause?", answerLabel("en", answerSkip))}
	var questions []string
	for i := range 10 {
		rec := answeredRecord("", "")
		rec.Interaction.Questions = nil
		for j := range 3 {
			q := fmt.Sprintf("question-%02d-%d-", i, j) + strings.Repeat("界", 250)
			questions = append(questions, q)
			rec.Interaction.Questions = append(rec.Interaction.Questions, proto.Question{Question: q})
			rec.Resolution.Answers[q] = strings.Repeat("答", 250)
		}
		records = append(records, rec)
	}
	got := triageAnswered(records)
	if len(got) != 20 {
		t.Fatalf("got %d pairs", len(got))
	}
	for i, pair := range got {
		if pair.Question != string([]rune(questions[i])[:200]) || pair.Answer != strings.Repeat("答", 200) ||
			utf8.RuneCountInString(pair.Question) != 200 || !utf8.ValidString(pair.Question) || !utf8.ValidString(pair.Answer) {
			t.Fatalf("pair %d is out of order or not bounded by runes: %+v", i, pair)
		}
	}
}
