package assistant

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestAdversarialInboxGroupStateWakeAgentStopRecapDoesNotResumeStoppedAgent(t *testing.T) {
	ctx := context.Background()
	h := inboxGroupStateTestHub(t, true)
	h.inbox.handing.Lock()
	t.Cleanup(h.inbox.handing.Unlock)
	it := inboxGroupStateTestItem(h, "event-adversarial-agent-stop", true)
	h.inboxAgentStarted(ctx, "session-1", "server", "agent-session", "synthetic Agent task")

	report := inboxGroupStateReport(it, false, connector.DoneReplied)
	rest, changed := h.applyGroupStates(ctx, inboxGroupStateTestSource(), []connector.InboxThing{report})
	requireInboxGroupStateChange(t, rest, changed, it)
	h.deliverPending(ctx)

	h.f.setSessions("server",
		proto.Session{SessionID: "session-1", Status: proto.StatusIdle},
		proto.Session{MachineID: "server", SessionID: "agent-session", Status: proto.StatusIdle,
			LastText: "Synthetic Agent result", UpdatedAt: time.Now().UTC()})
	h.inboxAgentTurnSettled(ctx, "server", "agent-session")
	if got := inboxAgentStoppedCount(it); got != 1 {
		t.Fatalf("Agent stops = %d, want one", got)
	}
	h.deliverPending(ctx)
	if state, _, _ := handlingNow(h, it); state != HandlingLooking {
		t.Fatalf("handling after Agent stop = %q, want looking before the recap", state)
	}

	if _, _, err := h.suggest(ctx, "session-1", suggestIn{ProcessingTurn: inboxTestProcessingTurn(h, "session-1"), Recap: "Synthetic recap after the Agent stopped"}); err != nil {
		t.Fatal(err)
	}
	state, wake, back := handlingNow(h, it)
	if state == HandlingAgent {
		t.Fatalf("recap-only response to the Agent-stop notice resumed the stopped Agent: handling %q, wake %v, back %q", state, wake, back)
	}
}

// inboxGroupStateWakeBlockingSessionHub holds one session lookup outside the fake's lock, so a test can deliver a new
// source reply while an older no-draft sweep is deciding what to do.
type inboxGroupStateWakeBlockingSessionHub struct {
	*fakeHub
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *inboxGroupStateWakeBlockingSessionHub) DeviceCall(ctx context.Context, p DeviceCall) (json.RawMessage, error) {
	if p.Method == DeviceSessionGet {
		h.once.Do(func() { close(h.entered) })
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return h.fakeHub.DeviceCall(ctx, p)
}

func TestAdversarialInboxGroupStateWakeOldSweepDoesNotSettleNewReply(t *testing.T) {
	ctx := context.Background()
	h := inboxGroupStateTestHub(t, true)
	it := inboxGroupStateTestItem(h, "event-adversarial-stale-sweep", true)
	h.f.setSessions("server", proto.Session{SessionID: "session-1", Status: proto.StatusIdle})

	// An earlier self_read wake from done is already delivered and is old enough for the no-draft sweep.
	read := inboxGroupStateReport(it, true, "")
	rest, changed := h.applyGroupStates(ctx, inboxGroupStateTestSource(), []connector.InboxThing{read})
	requireInboxGroupStateChange(t, rest, changed, it)
	h.deliverPending(ctx)
	started := time.Now().Add(-2 * time.Minute)
	h.inbox.mu.Lock()
	it.Agent.StartedAt = &started
	h.inbox.mu.Unlock()

	blocking := &inboxGroupStateWakeBlockingSessionHub{fakeHub: h.f, entered: make(chan struct{}), release: make(chan struct{})}
	h.Service.hub = blocking
	swept := make(chan struct{})
	go func() {
		h.sweepInboxAgents(ctx, InboxAgentConfig{Enabled: true}, time.Now())
		close(swept)
	}()
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the old sweep did not reach its session lookup")
	}

	// The source reply is a newer turn in the same session and the same visible looking state, but with a new Back/start.
	replied := inboxGroupStateReport(it, false, connector.DoneReplied)
	rest, changed = h.applyGroupStates(ctx, inboxGroupStateTestSource(), []connector.InboxThing{replied})
	requireInboxGroupStateChange(t, rest, changed, it)
	close(blocking.release)
	select {
	case <-swept:
	case <-time.After(5 * time.Second):
		t.Fatal("the old sweep did not finish")
	}

	state, wake, back := handlingNow(h, it)
	if state != HandlingLooking || !wake || back != HandlingWaiting {
		t.Fatalf("the old sweep settled the newer reply wake: handling %q, wake %v, back %q", state, wake, back)
	}
	followCheckNoFailure(t, h, it)

	h.deliverPending(ctx)
	if _, _, err := h.suggest(ctx, "session-1", suggestIn{ProcessingTurn: inboxTestProcessingTurn(h, "session-1"), Recap: "Synthetic recap after the stale sweep"}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := handlingNow(h, it); state != HandlingWaiting {
		t.Fatalf("the newer reply turn ended in %q, want waiting", state)
	}
}

// inboxGroupStateWakeBlockingHelloHub holds the first hello asked for once armed, outside the fake's lock. An expired
// sweep asks for it after it has copied the turn (isWorkMachine, the timeout's words) and before it settles, so a test
// can deliver a new source reply there; later hellos pass.
type inboxGroupStateWakeBlockingHelloHub struct {
	*fakeHub
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (h *inboxGroupStateWakeBlockingHelloHub) Hello(ctx context.Context) (Hello, error) {
	if h.armed.CompareAndSwap(true, false) {
		close(h.entered)
		select {
		case <-h.release:
		case <-ctx.Done():
			return Hello{}, ctx.Err()
		}
	}
	return h.fakeHub.Hello(ctx)
}

// The timeout branch of the same race (#8426): the old turn is past agentTimeout and its session still working, so the
// sweep times it out without asking for the session. A newer source reply in the same session and the same looking
// state, with a new Back and start, arrives after the sweep copied the old turn; the sweep must leave it alone.
func TestAdversarialInboxGroupStateWakeOldTimeoutSweepDoesNotSettleNewReply(t *testing.T) {
	ctx := context.Background()
	h := inboxGroupStateTestHub(t, true)
	it := inboxGroupStateTestItem(h, "event-adversarial-stale-timeout-sweep", true)
	h.f.setSessions("server", proto.Session{SessionID: "session-1", Status: proto.StatusWorking})

	// An earlier self_read wake from done is delivered, still working and past the sweep's timeout.
	read := inboxGroupStateReport(it, true, "")
	rest, changed := h.applyGroupStates(ctx, inboxGroupStateTestSource(), []connector.InboxThing{read})
	requireInboxGroupStateChange(t, rest, changed, it)
	h.deliverPending(ctx)
	cfg := InboxAgentConfig{Enabled: true}
	started := time.Now().Add(-agentTimeout(cfg) - time.Minute)
	h.inbox.mu.Lock()
	it.Agent.StartedAt = &started
	oldBack := it.Agent.Back
	h.inbox.mu.Unlock()
	if state, _, _ := handlingNow(h, it); state != HandlingLooking || oldBack == HandlingWaiting {
		t.Fatalf("setup: handling %q, back %q; want an old looking turn not from waiting", state, oldBack)
	}

	blocking := &inboxGroupStateWakeBlockingHelloHub{fakeHub: h.f, entered: make(chan struct{}), release: make(chan struct{})}
	blocking.armed.Store(true)
	h.Service.hub = blocking
	swept := make(chan struct{})
	go func() {
		h.sweepInboxAgents(ctx, cfg, time.Now())
		close(swept)
	}()
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the old sweep did not get past its copy of the turn")
	}

	replied := inboxGroupStateReport(it, false, connector.DoneReplied)
	rest, changed = h.applyGroupStates(ctx, inboxGroupStateTestSource(), []connector.InboxThing{replied})
	requireInboxGroupStateChange(t, rest, changed, it)
	close(blocking.release)
	select {
	case <-swept:
	case <-time.After(5 * time.Second):
		t.Fatal("the old sweep did not finish")
	}

	state, wake, back := handlingNow(h, it)
	if state != HandlingLooking || !wake || back != HandlingWaiting {
		t.Fatalf("the old timeout sweep settled the newer reply wake: handling %q, wake %v, back %q", state, wake, back)
	}
	followCheckNoFailure(t, h, it)

	h.deliverPending(ctx)
	if _, _, err := h.suggest(ctx, "session-1", suggestIn{ProcessingTurn: inboxTestProcessingTurn(h, "session-1"), Recap: "Synthetic recap after the stale timeout sweep"}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := handlingNow(h, it); state != HandlingWaiting {
		t.Fatalf("the newer reply turn ended in %q, want waiting", state)
	}
}
