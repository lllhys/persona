package assistant

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func editableAnswerFleet(t *testing.T) (*fleetTest, proto.Interaction, chan proto.Resolution) {
	t.Helper()
	ft := newFleetTest(t)
	ix := proto.Interaction{ID: "ix_edit", MachineID: "mac", SessionID: "agent", Kind: proto.IxPermission, ToolName: "Bash",
		EditableText: "original text", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
	done := make(chan proto.Resolution, 1)
	ft.s.fleet.mu.Lock()
	ft.s.ixMapLocked()[ix.ID] = &pending{ix: ix, done: done}
	ft.s.fleet.mu.Unlock()
	return ft, ix, done
}

func TestEditedTextAnswerValidation(t *testing.T) {
	ft, ix, done := editableAnswerFleet(t)
	yes := true
	for _, tc := range []struct {
		name string
		r    proto.Resolution
	}{
		{name: "not an editable card", r: proto.Resolution{Allow: &yes, EditedText: "changed"}},
		{name: "deny with edited text", r: proto.Resolution{Allow: new(bool), EditedText: "changed"}},
		{name: "always with edited text", r: proto.Resolution{Allow: &yes, EditedText: "changed", Always: true}},
		{name: "deny note with edited text", r: proto.Resolution{Allow: &yes, EditedText: "changed", Message: "why"}},
		{name: "answers with edited text", r: proto.Resolution{Allow: &yes, EditedText: "changed", Answers: map[string]string{"q": "a"}}},
		{name: "blank text", r: proto.Resolution{Allow: &yes, EditedText: "  \n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "not an editable card" {
				ft.s.fleet.interactions[ix.ID].ix.EditableText = ""
				defer func() { ft.s.fleet.interactions[ix.ID].ix.EditableText = "original text" }()
			}
			if err := ft.s.resolve(ix.ID, tc.r); !errors.Is(err, errInvalidEditedAnswer) {
				t.Fatalf("resolve(%+v) = %v, want errInvalidEditedAnswer", tc.r, err)
			}
			if _, ok := ft.s.pendingIx(ix.ID); !ok {
				t.Fatal("an invalid edit consumed the card")
			}
		})
	}

	if err := ft.s.resolve(ix.ID, proto.Resolution{Allow: &yes, EditedText: "edited text"}); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.EditedText != "edited text" {
		t.Fatalf("the hook received %+v", got)
	}
}

func TestEditedTextRouteRejectsInvalidCombination(t *testing.T) {
	ft, ix, _ := editableAnswerFleet(t)
	h := ft.s.fleetHandler(http.NotFoundHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/interactions/"+ix.ID+"/resolve", strings.NewReader(`{"allow":true,"edited_text":"changed","message":"extra"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid edit returned %d %s", w.Code, w.Body)
	}
	if _, ok := ft.s.pendingIx(ix.ID); !ok {
		t.Fatal("the route consumed the invalid edit's card")
	}
}

func TestDenyNoteStillReachesTheHook(t *testing.T) {
	ft, ix, done := editableAnswerFleet(t)
	no := false
	if err := ft.s.resolve(ix.ID, proto.Resolution{Allow: &no, Message: "change the greeting"}); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.Message != "change the greeting" {
		t.Fatalf("deny reason = %q", got.Message)
	}
}
