package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/prompts"
)

// cannedJSON is a jsonModel that answers every call with the same JSON (or error) and keeps what it was asked.
type cannedJSON struct {
	answer       string
	err          error
	calls        int
	system, user string
}

func (m *cannedJSON) JSON(_ context.Context, system, user string, out any) error {
	m.calls++
	m.system, m.user = system, user
	if m.err != nil {
		return m.err
	}
	return json.Unmarshal([]byte(m.answer), out)
}

func doneReply(text string) prompts.TaskDoneReply {
	return prompts.TaskDoneReply{Machine: "desk", Ask: "Fix the login retry", Text: text}
}

func TestTaskDoneWithoutModel(t *testing.T) {
	merged := doneReply("Merged https://github.com/example/app/pull/7 into main.")
	for _, tc := range []struct {
		name        string
		replies     []prompts.TaskDoneReply
		blocked     bool
		ok          bool
		verdict     string
		reason      string
		wantReplies int
	}{
		{"side conversation without evidence", []prompts.TaskDoneReply{merged}, true, true, verdictNotDone, verdictWords[verdictNotDone], 1},
		{"no reply", nil, false, true, verdictUnsure, "没读到 Agent 的回复", 0}, // i18n-ignore: the verdict's stored reason
		{"cut off by the model service", []prompts.TaskDoneReply{merged, doneReply("API Error: 529 overloaded")}, false, true, verdictNotDone, interruptedSummary, 2},
		{"left to the model", []prompts.TaskDoneReply{merged}, false, false, "", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := taskDoneWithoutModel(tc.replies, tc.blocked)
			if ok != tc.ok || c.Verdict != tc.verdict || c.Reason != tc.reason || c.Replies != tc.wantReplies {
				t.Fatalf("got %+v ok=%v, want verdict %q reason %q replies %d ok=%v", c, ok, tc.verdict, tc.reason, tc.wantReplies, tc.ok)
			}
			if len(tc.replies) > 0 && !c.Signals.MergedPR {
				t.Fatalf("signals = %+v: a merged PR link is a hint whatever the verdict", c.Signals)
			}
		})
	}
}

func TestJudgeTaskDoneWithVerdicts(t *testing.T) {
	deployed := []prompts.TaskDoneReply{doneReply("Deployed to production and checked the login flow: retry works.")}
	for _, tc := range []struct {
		name     string
		delivery bool
		answer   string
		verdict  string
		close    bool
		state    string // the delivery's state; "" when delivery is not judged
		gate     string
		unblock  string
	}{
		{"done, any case and spacing", false, `{"verdict":" DONE ","reason":"finished"}`, verdictDone, true, "", "", ""},
		{"a made-up verdict is unsure", false, `{"verdict":"finished","reason":"looks finished"}`, verdictUnsure, false, "", "", ""},
		{"waiting stays open", false, `{"verdict":"waiting","reason":"waiting for CI"}`, verdictWaiting, false, "", "", ""},
		{"delivery off ignores the delivery fields", false, `{"verdict":"done","delivery":"pr","gate":"merge","unblock":"user"}`, verdictDone, true, "", "", ""},
		{"done but held at a PR is undelivered", true, `{"verdict":"done","delivery":"pr","gate":"merge approval","unblock":"user"}`, verdictUndelivered, false, deliveryPR, "merge approval", unblockUser},
		{"done and live closes", true, `{"verdict":"done","delivery":"live","gate":"none","unblock":"user"}`, verdictDone, true, deliveryLive, "", ""},
		{"not done keeps its verdict while delivery is judged", true, `{"verdict":"not_done","delivery":"pr"}`, verdictNotDone, false, deliveryPR, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &cannedJSON{answer: tc.answer}
			c, err := judgeTaskDoneWith(context.Background(), m, taskDoneInput{Owner: "Alex", Delivery: tc.delivery, Title: "Fix the login retry", Replies: deployed})
			if err != nil {
				t.Fatal(err)
			}
			if m.calls != 1 {
				t.Fatalf("model calls = %d, want 1", m.calls)
			}
			if c.Verdict != tc.verdict || c.WouldClose != tc.close {
				t.Fatalf("verdict %q close %v, want %q %v", c.Verdict, c.WouldClose, tc.verdict, tc.close)
			}
			if tc.state == "" {
				if c.Delivery != nil {
					t.Fatalf("delivery = %+v, want none while it is not judged", c.Delivery)
				}
				return
			}
			if c.Delivery == nil || c.Delivery.State != tc.state || c.Delivery.Gate != tc.gate || c.Delivery.Unblock != tc.unblock || c.Delivery.At.IsZero() {
				t.Fatalf("delivery = %+v, want state %q gate %q unblock %q with a time", c.Delivery, tc.state, tc.gate, tc.unblock)
			}
		})
	}
}

func TestJudgeTaskDoneWithReasonIsOneShortLine(t *testing.T) {
	long := "The retry works now.\n\n" + strings.Repeat("Checked again on the staging hub. ", 4)
	m := &cannedJSON{answer: `{"verdict":"done","reason":` + judgeJSONText(t, long) + `}`}
	c, err := judgeTaskDoneWith(context.Background(), m, taskDoneInput{Title: "Fix the login retry", Replies: []prompts.TaskDoneReply{doneReply("Done.")}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(c.Reason, "\n\r") || utf8.RuneCountInString(c.Reason) != 41 || !strings.HasSuffix(c.Reason, "…") {
		t.Fatalf("reason = %q (%d runes), want one line of 40 runes and an ellipsis", c.Reason, utf8.RuneCountInString(c.Reason))
	}
}

func TestJudgeTaskDoneWithModelError(t *testing.T) {
	m := &cannedJSON{err: errors.New("no model")}
	replies := []prompts.TaskDoneReply{doneReply("Merged https://github.com/example/app/pull/7 into main.")}
	c, err := judgeTaskDoneWith(context.Background(), m, taskDoneInput{Title: "Fix the login retry", Replies: replies})
	if err == nil {
		t.Fatal("a model error must come back: the todo is left as it is")
	}
	if c.Verdict != "" || c.WouldClose || c.Replies != 1 || !c.Signals.MergedPR {
		t.Fatalf("check = %+v, want no verdict with the replies counted and the hints found", c)
	}
}

func TestJudgeTaskDoneWithAsksWhatItWasGiven(t *testing.T) {
	notes := "Retry the token refresh once before signing the user out. " + strings.Repeat("x", 4000) + " Keep the old error text."
	m := &cannedJSON{answer: `{"verdict":"done","reason":"ok"}`}
	in := taskDoneInput{Owner: "Alex", Delivery: true, Title: "Fix the login retry", Notes: notes, Criteria: []string{"PR merged", "deployed and verified"},
		Replies: []prompts.TaskDoneReply{doneReply("Deployed and verified.")}}
	if _, err := judgeTaskDoneWith(context.Background(), m, in); err != nil {
		t.Fatal(err)
	}
	if want := prompts.RenderTaskDone(prompts.TaskDone{Owner: "Alex", Delivery: true}); m.system != want {
		t.Fatalf("system prompt differs from the task_done template for this owner and delivery")
	}
	for _, want := range []string{"Fix the login retry", "Retry the token refresh once", "Keep the old error text.", "\n…\n", "PR merged", "deployed and verified", "Deployed and verified."} {
		if !strings.Contains(m.user, want) {
			t.Fatalf("user text lacks %q", want)
		}
	}
	if strings.Contains(m.user, strings.Repeat("x", 4000)) {
		t.Fatal("long notes go clipped in the middle")
	}
}

func judgeJSONText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
