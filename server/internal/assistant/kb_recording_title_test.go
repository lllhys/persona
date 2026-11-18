package assistant

import (
	"net/http"
	"strings"
	"testing"
)

// PUT …/title (#8877): full credentials only; the recording's name from then on, with where it comes from, as the
// upload's answer says it; a later upload that changes the phone's title takes it back; the contract's errors.
func TestRecordingTitleRoute(t *testing.T) {
	m := newMeetingFixture(t)
	p := "/api/v1/recordings/" + meetingRec + "/title"
	m.do("PUT", p, "answer_only", map[string]any{"title": "Export cap"}, http.StatusForbidden)
	m.do("PUT", p, "", strings.Repeat("x", 5000), http.StatusRequestEntityTooLarge)
	wantCode(t, m.do("PUT", p, "", map[string]any{"title": "Export cap"}, http.StatusNotFound), codeRecordingNotFound)
	m.upload(meetingRec, 1, map[string]any{"title": "Design review"})
	m.do("PUT", p, "", `{}`, http.StatusBadRequest)
	m.do("PUT", p, "", `{"title":null}`, http.StatusBadRequest)
	m.do("PUT", p, "", `{"title":3}`, http.StatusBadRequest)
	m.do("PUT", p, "", map[string]any{"title": "two\nlines"}, http.StatusBadRequest)
	m.do("PUT", p, "", map[string]any{"title": strings.Repeat("长", 201)}, http.StatusBadRequest)
	m.do("PUT", "/api/v1/recordings/not-a-uuid/title", "", map[string]any{"title": "x"}, http.StatusBadRequest)

	out := m.do("PUT", p, "", map[string]any{"title": "  Export cap  "}, http.StatusOK)
	if out["name"] != "Export cap" || out["name_source"] != "renamed" {
		t.Fatalf("renamed: %v", out)
	}
	// The phone's next upload with its title as it was keeps the web's name, and says so.
	if up := m.upload(meetingRec, 2, map[string]any{"title": "Design review"}); up["name"] != "Export cap" || up["name_source"] != "renamed" {
		t.Fatalf("upload after the rename: %v", up)
	}
	// Renamed on the phone since: the phone's name wins.
	if up := m.upload(meetingRec, 3, map[string]any{"title": "Release date"}); up["name"] != "Release date" || up["name_source"] != "title" {
		t.Fatalf("the phone's newer name: %v", up)
	}
	// Cleared on the web: back to the phone's title.
	m.do("PUT", p, "", map[string]any{"title": "Export cap"}, http.StatusOK)
	if out := m.do("PUT", p, "", map[string]any{"title": ""}, http.StatusOK); out["name"] != "Release date" || out["name_source"] != "title" {
		t.Fatalf("cleared: %v", out)
	}
	m.do("DELETE", "/api/v1/recordings/"+meetingRec, "", nil, http.StatusOK)
	wantCode(t, m.do("PUT", p, "", map[string]any{"title": "x"}, http.StatusGone), codeRecordingDeleted)
}
