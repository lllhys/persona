package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// pending_since is set on the way into pending_review, kept by every write while the review waits, and gone in every
// other state. A review that waited before the hub kept the time gets none from a later write.
func TestPendingSinceFollowsTheReviewState(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	t1 := t0.Add(20 * time.Minute)
	for _, c := range []struct {
		name          string
		before, after string
		since         *time.Time
		want          *time.Time
	}{
		{"judged finished", "", reviewPending, nil, &t1},
		{"finished after a rework", reviewRework, reviewPending, nil, &t1},
		{"finished again after it was accepted and reopened", reviewAccepted, reviewPending, nil, &t1},
		{"another write while the review waits", reviewPending, reviewPending, &t0, &t0},
		{"a review that waited with no time recorded", reviewPending, reviewPending, nil, nil},
		{"accepted", reviewPending, reviewAccepted, &t0, nil},
		{"sent back", reviewPending, reviewRework, &t0, nil},
		{"for the owner", reviewPending, reviewNeedsUser, &t0, nil},
		{"its agent works again", reviewPending, "", &t0, nil},
		{"never waited", "", "", nil, nil},
	} {
		got := pendingSince(c.before, c.after, c.since, t1)
		if (got == nil) != (c.want == nil) || got != nil && !got.Equal(*c.want) {
			t.Errorf("%s: pending_since = %v, want %v", c.name, got, c.want)
		}
	}
}

// A task judged finished carries when its review began, on the ledger and on the wire. Handing the review to another
// supervisor does not restart the clock; its agent working again ends the wait, the next finish starts a new one, and
// the review ends it.
func TestSupervisedTaskCarriesWhenItsReviewBegan(t *testing.T) {
	s, f, todo := sideReviewFixture(t)
	ctx := context.Background()
	began := todo.Supervision.PendingSince
	if began == nil || time.Since(*began) > time.Minute || began.After(time.Now()) {
		t.Fatalf("pending_since = %v, want the moment the task was judged finished", began)
	}
	var wire struct {
		Supervision struct {
			State        string     `json:"state"`
			PendingSince *time.Time `json:"pending_since"`
		} `json:"supervision"`
	}
	raw, _ := json.Marshal(todo)
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Supervision.State != reviewPending ||
		wire.Supervision.PendingSince == nil || !wire.Supervision.PendingSince.Equal(*began) {
		t.Fatalf("the apps' todo: %s (%v)", raw, err)
	}
	// stored and loaded as it is
	if back := supervisionFromRow(todo.Supervision.row(todo.ID)); back.PendingSince == nil || !back.PendingSince.Equal(*began) {
		t.Fatalf("after a reload: %v, want %v", back.PendingSince, began)
	}

	// handed to the main conversation's follow-up: a ledger write that leaves the review waiting since the same moment
	s.superviseWatch(ctx, time.Now().Add(time.Hour))
	handed := todoNow(s, todo.ID).Supervision
	if handed.Supervisor != "conv1" || handed.State != reviewPending || handed.PendingSince == nil || !handed.PendingSince.Equal(*began) {
		t.Fatalf("after the handover: %+v, want conv1's review waiting since %v", handed, began)
	}
	if !handed.UpdatedAt.After(*began) {
		t.Fatalf("the handover did not write the ledger: updated %v, pending since %v", handed.UpdatedAt, began)
	}

	agentTurn(s, f, proto.StatusWorking, "", 0)
	if v := todoNow(s, todo.ID).Supervision; v.State == reviewPending || v.PendingSince != nil {
		t.Fatalf("its agent works again: state %q, pending_since %v", v.State, v.PendingSince)
	}
	if raw, _ := json.Marshal(todoNow(s, todo.ID).Supervision); json.Valid(raw) && jsonHas(raw, "pending_since") {
		t.Fatalf("pending_since on the wire outside pending_review: %s", raw)
	}

	agentTurn(s, f, proto.StatusDone, "补了测试，PR https://github.com/o/r/pull/7 已合并", 0)
	waitFor(t, "the second wait for review", func() bool { return todoNow(s, todo.ID).Supervision.State == reviewPending })
	again := todoNow(s, todo.ID).Supervision.PendingSince
	if again == nil || !again.After(*began) {
		t.Fatalf("the second review waits since %v, want later than the first (%v)", again, began)
	}

	if _, err := runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": "PR 已合并",
		"checks": []map[string]any{{"criterion": "PR 合并到 main", "met": true, "evidence": "pull/7"}}}); err != nil {
		t.Fatal(err)
	}
	if v := todoNow(s, todo.ID).Supervision; v.State != reviewAccepted || v.PendingSince != nil {
		t.Fatalf("reviewed: state %q, pending_since %v", v.State, v.PendingSince)
	}
}

func jsonHas(raw []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}
