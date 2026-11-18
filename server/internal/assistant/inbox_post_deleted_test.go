package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestInboxPostDoesNotRepublishDeletedConversation(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "direct post"
		if automatic {
			name = "scheduled completion"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := newSpawnHarness(t)
			id := "run-delete"
			h.f.known[id] = true
			h.f.setSessions("server", h.parent, proto.Session{MachineID: "server", SessionID: id, Assistant: true, Status: proto.StatusIdle, LastText: "private-late-report"})
			posted, err := h.s.postToInbox(ctx, "server", id, inboxPostArgs{Title: "Saved result", Summary: "already saved record", Reason: "owner asked"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			sc := h.s.loadSchedule(ctx)
			sc.mu.Lock()
			sc.tasks = append(sc.tasks, Task{ID: "scheduled-source", Title: "Latest result", Inbox: inboxEvery, RunSessionID: id, RunMachineID: "server"})
			sc.mu.Unlock()
			if err := h.s.deleteConversation(ctx, "server", id); err != nil {
				t.Fatal(err)
			}
			if automatic {
				h.s.inboxPostEvery(ctx, "server", id)
			} else {
				_, err := h.s.postToInbox(ctx, "server", id, inboxPostArgs{Title: "Private later title", Summary: "private-late-report", Reason: "late callback"}, time.Now())
				if convCode(err) != "conversation_deleted" {
					t.Errorf("deleted source accepted another post: %v", err)
				}
			}
			h.s.inbox.mu.Lock()
			saved := h.s.inbox.items[posted.id]
			unchanged := saved != nil && saved.Title == "Saved result" && saved.Summary == "already saved record"
			h.s.inbox.mu.Unlock()
			if !unchanged {
				t.Fatal("deleted transcript replaced an already saved Inbox result")
			}
			if _, err := h.s.postToInbox(ctx, "server", h.parent.SessionID, inboxPostArgs{Title: "Live control", Summary: "control", Reason: "owner asked"}, time.Now()); err != nil {
				t.Fatalf("ordinary conversation blocked: %v", err)
			}
		})
	}
}
