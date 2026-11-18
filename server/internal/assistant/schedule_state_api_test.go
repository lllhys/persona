package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const taskStateBody = `"title":"Daily","prompt":"Example","repeat":"daily","time":"09:00","enabled":true`

func TestTaskStateAPIUsesOnlyStateText(t *testing.T) {
	s, _, store := scheduleStorageFixture(t)
	// Echoed response metadata, including malformed and oversized values,
	// cannot set a note on POST or overwrite a newer note on PUT.
	oversized, err := json.Marshal(TaskStateNote{Text: strings.Repeat("x", taskStateMaxBytes+1), Revision: 999})
	if err != nil {
		t.Fatal(err)
	}
	w := scheduleRequest(s, "POST", "tasks", `{`+taskStateBody+`,"state_note":`+string(oversized)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("new task: %d %s", w.Code, w.Body)
	}
	var created struct{ Task Task }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Task.ID == "" || created.Task.StateNote != nil {
		t.Fatalf("new task retained echoed note or could not be read: %s (%v)", w.Body, err)
	}
	if w := scheduleRequest(s, "POST", "tasks", `{`+taskStateBody+`,"state_note":`+string(oversized)+`,"state_text":"initial","state_revision":0}`); w.Code != http.StatusOK {
		t.Fatalf("validated POST note: %d %s", w.Code, w.Body)
	} else {
		var note struct{ Task Task }
		if err := json.Unmarshal(w.Body.Bytes(), &note); err != nil || note.Task.StateNote == nil ||
			note.Task.StateNote.Text != "initial" || note.Task.StateNote.Revision != 1 || note.Task.StateNote.UpdatedAt.IsZero() {
			t.Fatalf("validated POST note did not use Hub metadata: %s (%v)", w.Body, err)
		}
	}

	if w = scheduleRequest(s, "PUT", "tasks/daily", `{`+taskStateBody+`,"state_text":"newer"}`); w.Code != http.StatusOK {
		t.Fatalf("note edit: %d %s", w.Code, w.Body)
	}
	task, _ := taskByID(s, "daily")
	for _, body := range []string{
		`{` + taskStateBody + `,"state_note":` + string(oversized) + `}`,
		`{` + taskStateBody + `,"STATE_NOTE":{"updated_at":"not a time"}}`,
	} {
		if w = scheduleRequest(s, "PUT", "tasks/daily", body); w.Code != http.StatusOK {
			t.Fatalf("echoing a task response: %d %s", w.Code, w.Body)
		}
		if after, _ := taskByID(s, "daily"); !reflect.DeepEqual(after.StateNote, task.StateNote) {
			t.Fatalf("echoed note changed state: %+v", after.StateNote)
		}
	}
	assertScheduleStored(t, s, store)
}

func TestTaskStateAPIConflictingRevision(t *testing.T) {
	s, _, store := scheduleStorageFixture(t)
	put := func(body string) *httptest.ResponseRecorder {
		return scheduleRequest(s, "PUT", "tasks/daily", `{`+taskStateBody+`,`+body+`}`)
	}
	if w := put(`"state_text":"initial","state_revision":0`); w.Code != http.StatusOK {
		t.Fatalf("state edit: %d %s", w.Code, w.Body)
	}
	if w := put(`"state_text":"stale","state_revision":0`); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), "task_state_changed") {
		t.Fatalf("stale state edit: %d %s", w.Code, w.Body)
	}
	for _, revision := range []string{`null`, `"1"`, `1.5`, `-1`} {
		if w := put(`"state_text":"invalid","state_revision":` + revision); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid revision %s: %d %s", revision, w.Code, w.Body)
		}
	}
	if task, _ := taskByID(s, "daily"); task.StateNote == nil || task.StateNote.Text != "initial" || task.StateNote.Revision != 1 {
		t.Fatalf("stale or invalid update changed state: %+v", task.StateNote)
	}
	if w := put(`"state_text":"","state_revision":1`); w.Code != http.StatusOK {
		t.Fatalf("clear note: %d %s", w.Code, w.Body)
	}
	if task, _ := taskByID(s, "daily"); task.StateNote == nil || task.StateNote.Text != "" || task.StateNote.Revision != 2 {
		t.Fatalf("clearing did not advance revision: %+v", task.StateNote)
	}
	assertScheduleStored(t, s, store)
}
