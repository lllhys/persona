package node

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

func TestCodexQuestionOptionsEncodeAsArray(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			options []proto.QuestionOption
		}{
			{name: "free text"},
			{name: "empty", options: []proto.QuestionOption{}},
			{name: "choices", options: []proto.QuestionOption{{Label: "Blue", Description: "Cool"}}},
		} {
			name := "sync/" + tc.name
			if async {
				name = "async/" + tc.name
			}
			t.Run(name, func(t *testing.T) {
				card := askCard(cxID, codexAgent{}, "Question", transcript.CodexAsk{
					CallID: "call_options", Async: async,
					Questions: []transcript.CodexAskQuestion{{Question: "Which color?", Options: tc.options}},
				})
				encoded, err := json.Marshal(card)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Questions []struct {
						Options json.RawMessage `json:"options"`
					} `json:"questions"`
				}
				if err := json.Unmarshal(encoded, &wire); err != nil {
					t.Fatal(err)
				}
				if len(wire.Questions) != 1 {
					t.Fatalf("questions: %s", encoded)
				}
				if len(tc.options) == 0 {
					if string(wire.Questions[0].Options) != "[]" {
						t.Fatalf("free-text options must be an array, got %s", wire.Questions[0].Options)
					}
				} else if !reflect.DeepEqual(card.Questions[0].Options, tc.options) {
					t.Fatalf("choices changed: %+v", card.Questions[0].Options)
				}
			})
		}
	}
}
