package assistant

import (
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// A Just me turn that stops with background work running has not ended: the turn that work's report wakes keeps the
// mark, also when the report comes long after the stop, so the woken turn's end (the held work's one completion push,
// carrying the private answer) is never pushed. Once that turn is over its tracker entry goes too, and the owner's
// next ordinary turn drops the mark (#3862, docs/assistant/meeting-minutes.md "Just me"). The delayed checks run by
// hand here: their timers are an hour.
func TestJustMeMarkHoldsThroughBackgroundWork(t *testing.T) {
	ft := newFleetTestNoStore(t)
	ft.f.mu.Lock()
	ft.f.hello.Notify = assistantwire.NotifyTimings{DoneDelayMS: 3_600_000, LongTurnMS: 60_000}
	ft.f.mu.Unlock()
	var clock struct {
		sync.RWMutex
		at time.Time
	}
	clock.at = time.Now()
	now := func() time.Time {
		clock.RLock()
		defer clock.RUnlock()
		return clock.at
	}
	advance := func(d time.Duration) {
		clock.Lock()
		clock.at = clock.at.Add(d)
		clock.Unlock()
	}
	// A turn's end is read asynchronously: advancing the fixture must not race with justMe.clock.
	ft.s.justMe.now = now
	const key = "mac/s1"
	check := func() { // the delayed check of the latest end
		t.Helper()
		book := ft.s.turnsBook()
		book.mu.Lock()
		tr := book.turns[key]
		book.mu.Unlock()
		if tr == nil {
			t.Fatal("no turn tracked")
		}
		ft.s.notifyFinishedNow("mac", "s1", tr.gen, ft.s.notifyTimings())
	}
	work := proto.Session{SessionID: "s1", Title: "Main", Cwd: "/code/app", Agent: "claude", Assistant: true, Status: proto.StatusWorking,
		PaneID: "p1", UpdatedAt: now()}
	ft.s.justMeSent("mac", "s1")
	ft.sessions("mac", work)
	stopped := work
	stopped.Status, stopped.LastText = proto.StatusDone, "Looking it up in the background."
	stopped.Background = []proto.BackgroundTask{{ID: "b1", Kind: "agent"}}
	ft.sessions("mac", stopped)
	check() // held: its work runs
	advance(10 * time.Minute)
	woken := work
	woken.WokeByTask = true
	ft.sessions("mac", woken)
	if !ft.s.justMeNow("mac", "s1") {
		t.Fatal("the turn the work's report woke lost the mark")
	}
	finished := woken
	finished.Status, finished.LastText = proto.StatusDone, "PRIVATE ANSWER"
	ft.sessions("mac", finished)
	check()
	if got := ft.pushesOf(assistantwire.NotifyDone, 1, 0); len(got) != 0 {
		t.Fatalf("the Just me work's end was pushed: %+v", got)
	}
	if !ft.s.turnsBook().startOf(key).IsZero() {
		t.Fatal("the Just me turn stays tracked: the next turn would take its start and its hold")
	}
	advance(time.Minute)
	ft.sessions("mac", work) // the owner's next message
	if ft.s.justMeNow("mac", "s1") {
		t.Fatal("the next ordinary turn is still marked")
	}
}
