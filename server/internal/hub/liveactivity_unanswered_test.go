package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

var (
	fleetWorking = FleetState{Phase: "working", Working: 1, Items: []FleetItem{{ID: "m/a", Title: "upgrade", Kind: "working"}}}
	fleetAsking  = FleetState{Phase: "needsYou", NeedsYou: 1, Items: []FleetItem{{ID: "m/b", Title: "todo", Kind: "permission", Interaction: "ix1"}}}
)

// fleetRun is localFleetLoop's hub with a loop that can be restarted, stopped when the test ends (a failed one too).
type fleetRun struct {
	h    *Hub
	sink *fleetPushSink
	stop func()
}

func newFleetRun(t *testing.T) *fleetRun {
	t.Helper()
	r := &fleetRun{}
	r.h, r.sink, r.stop = localFleetLoop(t)
	t.Cleanup(func() { r.stop() })
	return r
}

// restart replaces the hub's Live Activity manager with a new one over the same store, as a hub restart or a
// blue-green switch does: what each device shows, the round and who was started in it are gone.
func (r *fleetRun) restart() {
	r.stop()
	r.h.live = newLiveActivities(r.h)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.h.live.run(ctx) }()
	synctest.Wait()
	r.stop = func() { cancel(); <-done }
}

// elapse moves the bubble's clock on by d and lets the loop settle.
func elapse(d time.Duration) {
	time.Sleep(d) // sleep-ok: the fake clock of a synctest bubble, no wall time
	synctest.Wait()
}

func fleetStarts(sink *fleetPushSink) int { return sink.count("phone", "liveactivity", "start") }

// A push-started activity whose update token the phone reported follows the fleet as before: the agent it lists
// ending and the card it shows being answered end it, across a hub restart too, and start nothing more.
func TestFleetPushStartedActivityFollowsFleet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shown   FleetState
		restart bool
	}{
		{"agent ends", fleetWorking, false},
		{"card answered", fleetAsking, false},
		{"agent ends across a restart", fleetWorking, true},
		{"card answered across a restart", fleetAsking, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newFleetRun(t)
				h, sink := r.h, r.sink
				h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
				reportLive(t, h, tc.shown, FleetStats{})
				elapse(fleetCoalesce)
				if got := fleetStarts(sink); got != 1 {
					t.Fatalf("starts: %d, want 1", got)
				}
				// The app, woken by the start, reports the new activity's token.
				h.live.update("phone", func(v *db.LiveActivity) {
					v.ActivityID, v.PushToken, v.StartedAt = "started", "phone-started", time.Now()
				})
				synctest.Wait()
				if tc.restart {
					r.restart()
				}
				reportLive(t, h, FleetState{}, FleetStats{})
				elapse(fleetEndAfter + 30*time.Second)
				if got := sink.count("phone", "liveactivity", "end"); got != 1 {
					t.Fatalf("the activity was not ended: %d end pushes", got)
				}
				if got := fleetStarts(sink); got != 1 {
					t.Fatalf("starts after the end: %d, want 1", got)
				}
			})
		})
	}
}

// A push-start the phone never answered (#7641: no update token came back, so the hub can neither update nor end
// that activity) holds the next one: none in the same round after a restart, none in the next round. A token report,
// the app connecting, or fleetMaxAge lifts it, and the store keeps both the hold and the lift across restarts.
func TestFleetUnansweredStartHoldsTheNext(t *testing.T) {
	// unanswered leaves a start nobody answered and, a restart and a round later, a request waiting.
	unanswered := func(t *testing.T) (r *fleetRun, started time.Time) {
		r = newFleetRun(t)
		h, sink := r.h, r.sink
		h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
		reportLive(t, h, fleetWorking, FleetStats{})
		elapse(fleetCoalesce)
		started = time.Now()
		if got := fleetStarts(sink); got != 1 {
			t.Fatalf("starts: %d, want 1", got)
		}
		// Same round, past the rolling cooldown, on a hub that restarted (or the other slot).
		elapse(fleetRefresh + time.Minute)
		r.restart()
		elapse(fleetCoalesce)
		if got := fleetStarts(sink); got != 1 {
			t.Fatalf("restart in the same round: %d starts, want 1", got)
		}
		// The agent ends; later a request arrives: a new round.
		reportLive(t, h, FleetState{}, FleetStats{})
		elapse(fleetRefresh)
		reportLive(t, h, fleetAsking, FleetStats{})
		elapse(fleetCoalesce)
		if got := fleetStarts(sink); got != 1 {
			t.Fatalf("next round: %d starts, want 1", got)
		}
		if n := sink.count("phone", "liveactivity", "update") + sink.count("phone", "liveactivity", "end"); n != 0 {
			t.Fatalf("pushes to an activity without a token: %d", n)
		}
		return r, started
	}

	t.Run("token report", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, _ := unanswered(t)
			h, sink := r.h, r.sink
			h.live.update("phone", func(v *db.LiveActivity) {
				v.ActivityID, v.PushToken, v.StartedAt = "started", "phone-started", time.Now()
			})
			elapse(20 * time.Second) // the next tick refreshes an activity the hub has pushed nothing to
			if got := sink.count("phone", "liveactivity", "update"); got != 1 {
				t.Fatalf("the reported activity got %d updates, want 1", got)
			}
			reportLive(t, h, FleetState{}, FleetStats{}) // answered: the activity ends
			elapse(fleetEndAfter + 30*time.Second)
			if got := sink.count("phone", "liveactivity", "end"); got != 1 {
				t.Fatalf("ends: %d, want 1", got)
			}
			r.restart()
			reportLive(t, h, fleetWorking, FleetStats{})
			elapse(fleetCoalesce)
			if got := fleetStarts(sink); got != 2 {
				t.Fatalf("after the phone reported a token: %d starts, want 2", got)
			}
		})
	})

	t.Run("app connects", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, _ := unanswered(t)
			h, sink := r.h, r.sink
			reportLive(t, h, FleetState{}, FleetStats{})
			elapse(fleetCoalesce)
			app := &client{hub: h, device: "credential-ios", role: roleFull, out: make(chan []byte, 64), clean: true}
			addTestClient(h, app)
			h.onClientMessage(context.Background(), app, []byte(`{"type":"register_phone","device_id":"phone","tools":[]}`))
			synctest.Wait()
			h.dropClient(app)
			synctest.Wait()
			r.restart()
			reportLive(t, h, fleetWorking, FleetStats{})
			elapse(fleetCoalesce)
			if got := fleetStarts(sink); got != 2 {
				t.Fatalf("after the app connected: %d starts, want 2", got)
			}
		})
	})

	t.Run("fleetMaxAge", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, started := unanswered(t)
			sink := r.sink
			elapse(time.Until(started.Add(fleetMaxAge - time.Minute)))
			if got := fleetStarts(sink); got != 1 {
				t.Fatalf("before fleetMaxAge: %d starts, want 1", got)
			}
			r.restart()
			elapse(2 * time.Minute)
			if got := fleetStarts(sink); got != 2 {
				t.Fatalf("after fleetMaxAge: %d starts, want 2", got)
			}
			sink.mu.Lock()
			last := sink.got[len(sink.got)-1]
			sink.mu.Unlock()
			if last.event != "start" || last.state.NeedsYou != 1 {
				t.Fatalf("the start after the hold: %+v, want the request still waiting", last)
			}
		})
	})
}

// What came of a start decides its hold (#7641 review). Accepted, or no answer at all (APNs may have taken it): the
// next start waits for the phone or fleetMaxAge. Refused by APNs or the relay (any status but 200, 410 included): there
// is no card to hold for, so the next round starts again once the rolling cooldown is over, as before #7641.
func TestFleetStartOutcomeDecidesTheHold(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		err      error
		wantHold time.Duration
	}{
		{"accepted", http.StatusOK, `{"ok":true}`, nil, fleetMaxAge},
		{"no answer", 0, "", errors.New("connection reset after the request was written"), fleetMaxAge},
		{"relay refused", http.StatusUnprocessableEntity, `{"error":"apns 400","reason":"BadDeviceToken"}`, nil, fleetRefresh},
		{"APNs unavailable", http.StatusBadGateway, `{"error":"apns 503","reason":"ServiceUnavailable"}`, nil, fleetRefresh},
		{"relay rejects the payload", http.StatusBadRequest, `{"error":"bad aps key"}`, nil, fleetRefresh},
		{"token gone", http.StatusGone, `{"reason":"ExpiredToken"}`, nil, fleetRefresh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h, sink, stop := localFleetLoopWith(t, func(h *Hub) { h.push.retryWaits = nil })
				defer stop()
				store := h.store.(*fleetStartStore)
				sink.mu.Lock()
				sink.answer = func(call fleetPushCall) (int, string, error) {
					if call.event != "start" {
						return http.StatusOK, `{"ok":true}`, nil
					}
					return tc.status, tc.body, tc.err
				}
				sink.mu.Unlock()
				h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
				reportLive(t, h, fleetWorking, FleetStats{})
				elapse(fleetCoalesce)
				if got := fleetStarts(sink); got != 1 {
					t.Fatalf("starts: %d, want 1", got)
				}
				store.mu.Lock()
				hold := store.due["phone"].Sub(store.claimed["phone"])
				store.mu.Unlock()
				if hold != tc.wantHold {
					t.Fatalf("next start held for %v after the start, want %v", hold, tc.wantHold)
				}
				// The round is the same past the cooldown: one start per round either way.
				elapse(fleetRefresh + time.Minute)
				if got := fleetStarts(sink); got != 1 {
					t.Fatalf("same round past the cooldown: %d starts, want 1", got)
				}
				// The agent ends; a request arrives: the next round, 10 minutes after the start's cooldown ran out.
				if tc.status == http.StatusGone { // the phone hands the hub its new push-to-start token
					h.live.update("phone", func(v *db.LiveActivity) { v.PushToStartToken = "phone-start-2" })
				}
				reportLive(t, h, FleetState{}, FleetStats{})
				elapse(fleetEndAfter + 30*time.Second)
				reportLive(t, h, fleetAsking, FleetStats{})
				elapse(fleetCoalesce)
				want := 1
				if tc.wantHold == fleetRefresh {
					want = 2
				}
				if got := fleetStarts(sink); got != want {
					t.Fatalf("next round: %d starts, want %d", got, want)
				}
			})
		})
	}
}

// A refusal lifts the hold of the start it answers, not of the next one (#7641 review). Start A's refusal comes only
// after the phone answered and start B was claimed, sent and accepted: B's hold stays in the store and the round
// after starts nothing. Before the fix A's refusal put B back on the cooldown and the next round put a second card
// beside B's.
func TestFleetLateRefusalLeavesTheNextStartHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const slow = 30 * time.Minute // how long start A's answer takes: past the phone's answer and start B
		h, sink, stop := localFleetLoopWith(t, func(h *Hub) { h.push.retryWaits = nil })
		defer stop()
		store := h.store.(*fleetStartStore)
		var starts atomic.Int32 // the phone's: the tablet's activity ends and starts with the rounds too
		sink.mu.Lock()
		sink.answer = func(call fleetPushCall) (int, string, error) {
			if call.event != "start" || call.device != "phone" || starts.Add(1) > 1 {
				return http.StatusOK, `{"ok":true}`, nil
			}
			time.Sleep(slow) // sleep-ok: the fake clock of a synctest bubble, no wall time
			return http.StatusUnprocessableEntity, `{"error":"apns 400","reason":"BadDeviceToken"}`, nil
		}
		sink.mu.Unlock()
		round := func(st FleetState) {
			t.Helper()
			reportLive(t, h, FleetState{}, FleetStats{})
			elapse(fleetEndAfter + 30*time.Second)
			reportLive(t, h, st, FleetStats{})
			elapse(fleetCoalesce)
		}
		held := func() (claimed time.Time, hold time.Duration) {
			store.mu.Lock()
			defer store.mu.Unlock()
			return store.claimed["phone"], store.due["phone"].Sub(store.claimed["phone"])
		}

		// Start A goes out; its answer is on its way for half an hour.
		h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
		reportLive(t, h, fleetWorking, FleetStats{})
		elapse(fleetCoalesce)
		sent := time.Now()
		claimedA, hold := held()
		if got := fleetStarts(sink); got != 1 || hold != fleetMaxAge {
			t.Fatalf("start A: %d starts held for %v, want 1 held for %v", got, hold, fleetMaxAge)
		}
		// The phone answers (its app connects and leaves): A's hold is lifted.
		reportLive(t, h, FleetState{}, FleetStats{})
		elapse(fleetCoalesce)
		app := &client{hub: h, device: "credential-ios", role: roleFull, out: make(chan []byte, 64), clean: true}
		addTestClient(h, app)
		h.onClientMessage(context.Background(), app, []byte(`{"type":"register_phone","device_id":"phone","tools":[]}`))
		synctest.Wait()
		h.dropClient(app)
		synctest.Wait()
		if _, hold = held(); hold != fleetRefresh {
			t.Fatalf("after the phone answered: next start %v after A, want %v", hold, fleetRefresh)
		}
		// Past A's cooldown a new round claims and sends start B, which APNs accepts: held.
		elapse(fleetRefresh)
		round(fleetAsking)
		claimedB, hold := held()
		if got := fleetStarts(sink); got != 2 || !claimedB.After(claimedA) || hold != fleetMaxAge {
			t.Fatalf("start B: %d starts, claimed %v after A, held for %v; want 2, later, %v", got, claimedB.Sub(claimedA), hold, fleetMaxAge)
		}
		// Past B's cooldown the next round asks the store and is told of B's hold.
		elapse(fleetRefresh)
		round(fleetWorking)
		if got := fleetStarts(sink); got != 2 {
			t.Fatalf("round after B: %d starts, want 2", got)
		}
		if waited := time.Since(sent); waited >= slow {
			t.Fatalf("A's answer was due %v ago: the test must reach here before it", waited-slow)
		}
		// A's refusal arrives now.
		elapse(time.Until(sent.Add(slow)) + time.Second)
		if got := starts.Load(); got != 2 {
			t.Fatalf("the relay was asked for %d starts, want 2", got)
		}
		if at, hold := held(); !at.Equal(claimedB) || hold != fleetMaxAge {
			t.Fatalf("A's late refusal left B's next start %v after it, want %v", hold, fleetMaxAge)
		}
		// The round after still starts nothing: B's card may be on the phone.
		round(fleetAsking)
		if got := fleetStarts(sink); got != 2 {
			t.Fatalf("round after A's late refusal: %d starts, want 2", got)
		}
		// B's own answer still lifts it.
		h.live.phoneAnswered(context.Background(), "phone")
		if _, hold = held(); hold != fleetRefresh {
			t.Fatalf("after the phone answered B: next start %v after it, want %v", hold, fleetRefresh)
		}
		round(fleetWorking)
		if got := fleetStarts(sink); got != 3 {
			t.Fatalf("round after the phone answered B: %d starts, want 3", got)
		}
	})
}

// sendFailure tells a start that never reached APNs (never sent, or refused with a status) from one nobody answered.
func TestSendFailureOutcome(t *testing.T) {
	refused := &relayRefusal{status: http.StatusUnprocessableEntity, msg: "apns 400", reason: "BadDeviceToken"}
	for _, tc := range []struct {
		name string
		err  error
		want pushResult
	}{
		{"registration changed", errRegistrationChanged, pushRefused},
		{"request not made", fmt.Errorf("%w: %v", errNotSent, errors.New("bad URL")), pushRefused},
		{"relay status", refused, pushRefused},
		{"relay status after retries", fmt.Errorf("%w (%d tries)", refused, 3), pushRefused},
		{"grant renewal failed", fmt.Errorf("%w; renewing the grant: %v", &relayRefusal{status: http.StatusUnauthorized}, errors.New("register: 500")), pushRefused},
		{"no answer", fmt.Errorf("%w (%d tries)", errors.New("i/o timeout"), 3), pushUnknown},
	} {
		if got := sendFailure(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// startHold is how long after the device's last claimed start the store lets the next one be claimed.
func startHold(store *fleetStartStore, device string) time.Duration {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.due[device].Sub(store.claimed[device])
}

// The app connecting lifts the hold of the device the start was claimed for (#7641 review). The start budget is kept
// by the device's push id; register_phone names it, and an older app that leaves it out is the devices its
// connection's credential registered, or, on a connection without one, the devices registered without one (the
// phones wakeDevices picks for such a registration). Before the fix only a named push id lifted anything, and the id
// the registration falls back to (the credential, "legacy") is no push id: the hold stayed for the 8 hours.
func TestRegisterPhoneLiftsTheHoldOfItsDevice(t *testing.T) {
	for _, tc := range []struct {
		name       string
		credential string            // of the connection
		message    string            // its register_phone
		registered map[string]string // push id -> the credential that registered it
		lifted     map[string]bool
	}{
		{"push id", "credential-ios", `{"type":"register_phone","device_id":"phone","tools":[]}`,
			map[string]string{"phone": "credential-ios", "pad": "credential-ios"}, map[string]bool{"phone": true}},
		{"no device id: the credential's device", "credential-ios", `{"type":"register_phone","tools":[]}`,
			map[string]string{"phone": "credential-ios", "pad": "credential-pad"}, map[string]bool{"phone": true}},
		{"no device id, no credential: the device registered without one", "", `{"type":"register_phone","tools":[]}`,
			map[string]string{"phone": "", "pad": "credential-pad"}, map[string]bool{"phone": true}},
		{"no device id: a credential that registered neither", "credential-other", `{"type":"register_phone","tools":[]}`,
			map[string]string{"phone": "credential-ios", "pad": ""}, map[string]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h, sink, stop := localFleetLoopWith(t, func(h *Hub) {
					h.push.mu.Lock()
					defer h.push.mu.Unlock()
					for id, credential := range tc.registered {
						d := h.push.devices[id]
						d.CredentialID, d.Credential = credential, credential
						h.push.devices[id] = d
					}
				})
				defer stop()
				store := h.store.(*fleetStartStore)
				devices := []string{"phone", "pad"}
				for _, id := range devices {
					h.live.update(id, func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
				}
				reportLive(t, h, fleetWorking, FleetStats{})
				elapse(fleetCoalesce)
				for _, id := range devices {
					if got, hold := sink.count(id, "liveactivity", "start"), startHold(store, id); got != 1 || hold != fleetMaxAge {
						t.Fatalf("%s: %d starts held for %v, want 1 held for %v", id, got, hold, fleetMaxAge)
					}
				}
				reportLive(t, h, FleetState{}, FleetStats{})
				elapse(fleetCoalesce)
				app := &client{hub: h, device: tc.credential, role: roleFull, out: make(chan []byte, 64), clean: true}
				addTestClient(h, app)
				h.onClientMessage(context.Background(), app, []byte(tc.message))
				synctest.Wait()
				h.dropClient(app)
				synctest.Wait()
				for _, id := range devices {
					want := fleetMaxAge
					if tc.lifted[id] {
						want = fleetRefresh
					}
					if hold := startHold(store, id); hold != want {
						t.Errorf("%s after the app connected: next start %v after its last, want %v", id, hold, want)
					}
				}
			})
		})
	}
}

// Two hubs over one store, as the two slots of a blue-green switch are: what lifts a hold on one (the phone's app
// connecting there, a start it sent being refused) lets the other start in its next round (#7641 review). Before the
// fix each kept the deadline the store last told it and did not ask again before then, so a hub that had been told
// of the hold held for the 8 hours whatever the other one lifted.
func TestFleetHoldLiftedOnTheOtherSlot(t *testing.T) {
	// slots are hub a and, over a's store, hub b; neither has an update token for the phone.
	slots := func(t *testing.T, prepareB func(h *Hub)) (a, b *Hub, sinkA, sinkB *fleetPushSink, store *fleetStartStore) {
		t.Helper()
		a, sinkA, stopA := localFleetLoop(t)
		t.Cleanup(stopA)
		store = a.store.(*fleetStartStore)
		b, sinkB, stopB := localFleetLoopWith(t, func(h *Hub) {
			h.store = store
			if prepareB != nil {
				prepareB(h)
			}
		})
		t.Cleanup(stopB)
		for _, h := range []*Hub{a, b} {
			h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
		}
		synctest.Wait()
		return a, b, sinkA, sinkB, store
	}
	round := func(t *testing.T, h *Hub, st FleetState) {
		t.Helper()
		reportLive(t, h, FleetState{}, FleetStats{})
		elapse(fleetEndAfter + 30*time.Second)
		reportLive(t, h, st, FleetStats{})
		elapse(fleetCoalesce)
	}

	t.Run("the phone's app connects there", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, b, sinkA, sinkB, store := slots(t, nil)
			// A's start goes unanswered; past the cooldown its next round asks the store and is told of the hold.
			reportLive(t, a, fleetWorking, FleetStats{})
			elapse(fleetCoalesce)
			elapse(fleetRefresh)
			round(t, a, fleetAsking)
			if got, hold := fleetStarts(sinkA), startHold(store, "phone"); got != 1 || hold != fleetMaxAge {
				t.Fatalf("A, a round after its unanswered start: %d starts held for %v, want 1 held for %v", got, hold, fleetMaxAge)
			}
			// The phone's app connects to B and leaves.
			app := &client{hub: b, device: "credential-ios", role: roleFull, out: make(chan []byte, 64), clean: true}
			addTestClient(b, app)
			b.onClientMessage(context.Background(), app, []byte(`{"type":"register_phone","device_id":"phone","tools":[]}`))
			synctest.Wait()
			b.dropClient(app)
			synctest.Wait()
			if hold := startHold(store, "phone"); hold != fleetRefresh {
				t.Fatalf("after the app connected to B: next start %v after the last, want %v", hold, fleetRefresh)
			}
			// A's next round starts again.
			round(t, a, fleetWorking)
			if got := fleetStarts(sinkA); got != 2 {
				t.Fatalf("A's round after B heard the phone: %d starts, want 2", got)
			}
			if got := fleetStarts(sinkB); got != 0 {
				t.Fatalf("B, whose fleet is empty, started %d", got)
			}
		})
	})

	t.Run("its start is refused there", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const slow = 20 * time.Minute // how long the answer to B's start takes: past A's round and the cooldown
			a, b, sinkA, sinkB, store := slots(t, func(h *Hub) { h.push.retryWaits = nil })
			sinkB.mu.Lock()
			sinkB.answer = func(call fleetPushCall) (int, string, error) {
				if call.event != "start" || call.device != "phone" {
					return http.StatusOK, `{"ok":true}`, nil
				}
				time.Sleep(slow) // sleep-ok: the fake clock of a synctest bubble, no wall time
				return http.StatusUnprocessableEntity, `{"error":"apns 400","reason":"BadDeviceToken"}`, nil
			}
			sinkB.mu.Unlock()
			// B sends a start; while its answer is on its way A's round asks the store and is told of the hold.
			reportLive(t, b, fleetWorking, FleetStats{})
			elapse(fleetCoalesce)
			sent := time.Now()
			reportLive(t, a, fleetAsking, FleetStats{})
			elapse(fleetCoalesce)
			if gotA, gotB, hold := fleetStarts(sinkA), fleetStarts(sinkB), startHold(store, "phone"); gotA != 0 || gotB != 1 || hold != fleetMaxAge {
				t.Fatalf("while B's start is out: A started %d, B %d, held for %v; want 0, 1, %v", gotA, gotB, hold, fleetMaxAge)
			}
			// The refusal reaches B.
			elapse(time.Until(sent.Add(slow)) + time.Second)
			if hold := startHold(store, "phone"); hold != fleetRefresh {
				t.Fatalf("after B's start was refused: next start %v after it, want %v", hold, fleetRefresh)
			}
			// A's next round starts.
			round(t, a, fleetWorking)
			if got := fleetStarts(sinkA); got != 1 {
				t.Fatalf("A's round after B's start was refused: %d starts, want 1", got)
			}
			if got := fleetStarts(sinkB); got != 1 {
				t.Fatalf("B started %d, want its 1", got)
			}
		})
	})
}

// fleetHoldHookStore runs duringHold before the first start's hold reaches the store, as an answer that arrives while
// that write is on its way and lands first does.
type fleetHoldHookStore struct {
	Store
	duringHold func()
	fired      bool
}

func (s *fleetHoldHookStore) HoldLiveActivityStart(ctx context.Context, device string, asOf time.Time, hold time.Duration) (bool, error) {
	if hold == fleetMaxAge && !s.fired {
		s.fired = true
		s.duringHold()
	}
	return s.Store.HoldLiveActivityStart(ctx, device, asOf, hold)
}

// The hold write can block for the store's timeout after the start was claimed and checked. When the phone's app
// connects or it reports an update token meanwhile, no start goes out, and the hold its answer lifted (and this late
// write put back) is lifted again (#7641 review).
func TestFleetStartRechecksAfterSlowHold(t *testing.T) {
	for _, change := range []string{"register_phone", "update_token"} {
		t.Run(change, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h, sink, stop := localFleetLoop(t)
				stop()
				store := h.store.(*fleetStartStore)
				h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
				reportLive(t, h, fleetWorking, FleetStats{})
				h.store = &fleetHoldHookStore{Store: store, duringHold: func() {
					switch change {
					case "register_phone":
						app := &client{hub: h, device: "credential-ios", role: roleFull, out: make(chan []byte, 64), clean: true}
						addTestClient(h, app)
						h.onClientMessage(context.Background(), app, []byte(`{"type":"register_phone","device_id":"phone","tools":[]}`))
					case "update_token":
						h.live.update("phone", func(v *db.LiveActivity) { v.PushToken, v.StartedAt = "fresh", time.Now() })
					}
				}}
				due := h.live.flush(fleetWorking, true, true)
				synctest.Wait()
				if !h.store.(*fleetHoldHookStore).fired {
					t.Fatal("no start was claimed and held")
				}
				if got := fleetStarts(sink); got != 0 {
					t.Fatalf("%s during the hold write still pushed %d starts", change, got)
				}
				if hold := startHold(store, "phone"); hold != fleetRefresh {
					t.Fatalf("next start %v after the claim, want the cooldown %v", hold, fleetRefresh)
				}
				if !due.Equal(time.Now().Add(fleetRefresh)) {
					t.Fatalf("spent claim lost retry deadline: %s", due)
				}
			})
		})
	}
}

// fleetHoldGateStore holds every HoldLiveActivityStart until release closes.
type fleetHoldGateStore struct {
	Store
	release chan struct{}
}

func (s *fleetHoldGateStore) HoldLiveActivityStart(ctx context.Context, device string, asOf time.Time, hold time.Duration) (bool, error) {
	<-s.release
	return s.Store.HoldLiveActivityStart(ctx, device, asOf, hold)
}

// register_phone lifts the hold off the app's read loop (#6889): a slow store keeps neither the registration nor the
// app's next message waiting, and the lift lands once the store answers.
func TestRegisterPhoneLiftsWithoutHoldingTheReadLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFleetRun(t)
		h, sink := r.h, r.sink
		store := h.store.(*fleetStartStore)
		h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken = "", "" })
		reportLive(t, h, fleetWorking, FleetStats{})
		elapse(fleetCoalesce)
		if got, hold := fleetStarts(sink), startHold(store, "phone"); got != 1 || hold != fleetMaxAge {
			t.Fatalf("%d starts held for %v, want 1 held for %v", got, hold, fleetMaxAge)
		}
		gate := &fleetHoldGateStore{Store: store, release: make(chan struct{})}
		h.store = gate
		app := &client{hub: h, device: "credential-ios", role: roleFull, out: make(chan []byte, 64), clean: true}
		addTestClient(h, app)
		h.onClientMessage(context.Background(), app, []byte(`{"type":"register_phone","device_id":"phone","tools":[]}`))
		h.onClientMessage(context.Background(), app, []byte(`{"type":"ping","id":"behind"}`))
		if msg := waitClientMessage(t, app, "pong"); msg["id"] != "behind" {
			t.Fatalf("pong: %v", msg)
		}
		if hold := startHold(store, "phone"); hold != fleetMaxAge {
			t.Fatalf("lifted before the store answered: %v", hold)
		}
		close(gate.release)
		synctest.Wait()
		if hold := startHold(store, "phone"); hold != fleetRefresh {
			t.Fatalf("after the store answered: next start %v after its last, want %v", hold, fleetRefresh)
		}
		h.dropClient(app)
		synctest.Wait()
	})
}
