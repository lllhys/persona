package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The done receipt lists the brief's criteria as they stood when the agent stopped, read from the supervision
// ledger's per-criterion state (the one the agent reported is met with its evidence, the other still unchecked), and the
// conclusion card lists each with its result once the review accepted the work, then what was done beyond the brief.
func TestReceiptAndConclusionListTheBriefsCriteria(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, false)
	ctx := context.Background()
	agentTurn(s, f, proto.StatusWorking, "", 0)
	if err := s.recordCriterionReports(ctx, todo, "box", "s1", []criterionReport{{Number: 1, State: "met", Evidence: "pull/12 merged"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitsForReview(t, s, f, todo.ID, "PR #12 已合并 https://code.example.com/a/b/pull/12")
	var receipt receiptBody
	waitFor(t, "the done receipt", func() bool {
		items, _, err := s.feed(ctx, "conv1", time.Time{})
		if err != nil {
			return false
		}
		for _, it := range items {
			if it.Kind == feedReceipt && it.Ref == todo.ID {
				_ = json.Unmarshal(it.Body, &receipt)
			}
		}
		return receipt.State == "done"
	})
	if len(receipt.Criteria) != 2 || receipt.Criteria[0].Criterion != "PR 合并到 master" || receipt.Criteria[0].State != criterionMet ||
		receipt.Criteria[0].Evidence != "pull/12 merged" || receipt.Criteria[1].State != criterionUnchecked || receipt.CriteriaMore != 0 {
		t.Fatalf("receipt = %+v", receipt)
	}
	before := len(posts())
	reviewTask(t, s, todo.ID, map[string]any{"verdict": "accepted", "note": "合并了",
		"checks": []map[string]any{
			{"criterion": "PR 合并到 master", "met": true, "evidence": "已合并"},
			{"criterion": "浅色 / 深色截图贴进 PR", "met": true, "evidence": "两种截图已核对"},
		},
		"also_done": []string{"补了回归测试"}})
	waitFor(t, "the conclusion card", func() bool { return len(cardsWith(posts()[before:], "结论：修登录崩溃")) == 1 })
	card := cardsWith(posts()[before:], "结论：修登录崩溃")[0]
	for _, want := range []string{"对照当初的要求", "✅ PR 合并到 master", "✅ 浅色 / 深色截图贴进 PR", "另外做了：补了回归测试", "✅ 做完了"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	// A decision's card is about what to choose: it lists no result.
	a := askOf(s, todo.ID)
	if a == nil {
		t.Fatal("no ask")
	}
	decide := *a
	decide.Outcome = outcomeDecide
	if raw, _ := json.Marshal(s.conclusionCard(ctx, todoNow(s, todo.ID), decide)); strings.Contains(string(raw), "对照当初的要求") {
		t.Errorf("a decision's card lists the result:\n%s", raw)
	}
}

// A result as a card lists it: every state in words and a mark, a long list counted past the first eight.
func TestBriefResultMarkdown(t *testing.T) {
	r := &BriefResult{AlsoDone: []string{"removed a dead flag", "updated the doc"}, Criteria: []BriefCriterion{
		{Criterion: "PR merged", State: criterionMet}, {Criterion: "a test covers it", State: criterionNotMet},
		{Criterion: "screenshots", State: criterionDropped, Change: "no UI change after all"}, {Criterion: "changelog", State: criterionUnchecked}}}
	want := "**Against what was asked**\n✅ PR merged\n❌ a test covers it (not met)\n➖ screenshots (dropped: no UI change after all)\n" +
		"▫️ changelog (not checked)\nAlso done: removed a dead flag; updated the doc"
	if got := briefResultMarkdown(i18n.En, r); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	for range 7 {
		r.Criteria = append(r.Criteria, BriefCriterion{Criterion: "more", State: criterionMet})
	}
	if got := briefResultMarkdown(i18n.En, r); !strings.Contains(got, "… and 3 more") || strings.Count(got, "\n") != 10 {
		t.Fatalf("got:\n%s", got)
	}
	if briefResultMarkdown(i18n.En, nil) != "" || briefResultMarkdown(i18n.En, &BriefResult{}) != "" {
		t.Fatal("an empty result says something")
	}
}
