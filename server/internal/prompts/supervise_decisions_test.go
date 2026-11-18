package prompts

import (
	"strings"
	"testing"
)

func TestSuperviseDecisionInstructionsAppearOnce(t *testing.T) {
	policy := RenderSupervisionInstructions()
	for _, instruction := range []string{"- `recheck`:", "- `undecidable`:", "- `owner_cannot_answer`:", "`todo_decision_add`"} {
		if got := strings.Count(policy, instruction); got != 1 {
			t.Errorf("static policy: %q occurs %d times, want 1", instruction, got)
		}
	}
	if !strings.Contains(policy, "Never take an irreversible/externally visible step on the owner's behalf") {
		t.Fatal("static decision policy lost the owner's approval boundary")
	}
	for _, role := range []struct {
		name string
		data Supervise
	}{
		{"main", Supervise{}},
		{"side", Supervise{Side: true}},
		{"leader", Supervise{Leader: true}},
		{"background", Supervise{Leader: true, Background: true}},
	} {
		t.Run(role.name, func(t *testing.T) {
			for _, decisions := range []bool{false, true} {
				data := role.data
				data.Decisions = decisions
				data.Entries = []SuperviseEntry{{ID: "todo_example", Title: "Example task", Kind: "recheck"}}
				text := RenderSupervise(data)
				// i18n-ignore: old handling instructions must not return in event payloads.
				for _, instruction := range []string{
					"- 决定交回你核对的：", "- 用户那边没有能回答的东西的：",
					"- 用户回「你来定」的：", "- 用户回「这不是我要的」的：", "- 用户回「先讲清背景」的：",
				} {
					if got := strings.Count(text, instruction); got != 0 {
						t.Errorf("Decisions=%v: event repeats %q %d times", decisions, instruction, got)
					}
				}
				if got := strings.Count(text, "todo_decision_add"); got != 0 {
					t.Errorf("Decisions=%v: event repeats decision recording %d times", decisions, got)
				}
				// i18n-ignore: approval handling also belongs only in the static fragment.
				approval := "标着「不会默认执行」的（不可撤回，或别人看得到）你不能替用户走这一步"
				if got := strings.Count(text, approval); got != 0 {
					t.Errorf("Decisions=%v: event repeats approval handling %d times", decisions, got)
				}
			}
		})
	}
}
