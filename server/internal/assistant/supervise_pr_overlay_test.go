package assistant

import (
	"context"
	"testing"
	"time"
)

func TestRemovingFalsePRDropsQueuedConflict(t *testing.T) {
	for _, replacement := range []string{"", "https://github.com/example/app/pull/13"} {
		t.Run(replacement, func(t *testing.T) {
			s, f, task := newPRConflictTask(t, nil)
			ctx := context.Background()
			status := TodoPRStatus{URL: task.PRURL, State: "open", HeadOID: "false-pr-head", Conflicts: true, CheckedAt: time.Now()}
			if err := s.applyPRStatus(ctx, task, status); err != nil {
				t.Fatal(err)
			}
			signal, ok := pendingPRConflict(s, task.ID)
			if !ok {
				t.Fatal("no conflict queued before the false PR was removed")
			}
			if err := s.forgetTaskPR(ctx, task.ID, task.PRURL, replacement); err != nil {
				t.Fatal(err)
			}
			if _, holds := s.digestEntry(ctx, signal, ""); holds {
				t.Fatal("a confirmed issue still produced an actionable PR conflict")
			}
			if s.deliverDigest(ctx, s.settings(ctx), "conv1", []superviseSignal{signal}, "", time.Now(), time.Time{}) ||
				len(f.notices()) != 0 {
				t.Fatal("a conflict for the removed issue reached the supervisor")
			}
		})
	}
}
