package proto

import (
	"encoding/json"
	"testing"
)

func TestPlainAllowWithEditedText(t *testing.T) {
	yes := true
	no := false
	for _, tc := range []struct {
		name string
		r    Resolution
		want bool
	}{
		{"allow and text", Resolution{Allow: &yes, EditedText: "edited"}, true},
		{"no text", Resolution{Allow: &yes}, false},
		{"deny with text", Resolution{Allow: &no, EditedText: "edited"}, false},
		{"message", Resolution{Allow: &yes, EditedText: "edited", Message: "why"}, false},
		{"always", Resolution{Allow: &yes, EditedText: "edited", Always: true}, false},
		{"dismiss", Resolution{Allow: &yes, EditedText: "edited", Dismiss: true}, false},
		{"cancel", Resolution{Allow: &yes, EditedText: "edited", Cancel: true}, false},
		{"answers", Resolution{Allow: &yes, EditedText: "edited", Answers: map[string]string{"q": "a"}}, false},
		{"values", Resolution{Allow: &yes, EditedText: "edited", Values: map[string]string{"f": "v"}}, false},
		{"answered by", Resolution{Allow: &yes, EditedText: "edited", AnsweredBy: AnsweredByAssistant}, false},
		{"reason", Resolution{Allow: &yes, EditedText: "edited", Reason: "why"}, false},
		{"remembered", Resolution{Allow: &yes, EditedText: "edited", Remembered: true}, false},
		{"action", Resolution{Allow: &yes, EditedText: "edited", Action: "go"}, false},
		{"content", Resolution{Allow: &yes, EditedText: "edited", Content: map[string]any{"x": 1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.PlainAllowWithEditedText(); got != tc.want {
				t.Fatalf("PlainAllowWithEditedText(%+v) = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
	card := Interaction{EditableText: "original"}
	cardJSON, _ := json.Marshal(card)
	if string(cardJSON) != `{"id":"","machine_id":"","session_id":"","kind":"","created_at":"0001-01-01T00:00:00Z","expires_at":"0001-01-01T00:00:00Z","editable_text":"original"}` {
		t.Fatalf("card JSON: %s", cardJSON)
	}
	answerJSON, _ := json.Marshal(Resolution{Allow: &yes, EditedText: "edited"})
	if string(answerJSON) != `{"allow":true,"edited_text":"edited"}` {
		t.Fatalf("answer JSON: %s", answerJSON)
	}
}
