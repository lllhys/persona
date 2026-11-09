package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// reportLinkDesktop is the manager's devices.state with machine "lap" (named Laptop), then its devices.desktops with
// that computer's presence (nil: none), through the bus handler as the manager's message comes.
func reportLinkDesktop(t *testing.T, h *Hub, desktop *devicewire.Desktop, derived bool) {
	t.Helper()
	h.applyDeviceStates(devicewire.States{Machines: []devicewire.State{{
		Machine: proto.Machine{ID: "lap", Name: "Laptop", Kind: kindLink}, Derived: derived}}})
	list := devicewire.Desktops{Desktops: []devicewire.Desktop{}}
	if desktop != nil {
		list.Desktops = append(list.Desktops, *desktop)
	}
	raw, _ := json.Marshal(list)
	busEvents[devicewire.EventDesktops](h, &busConn{ident: db.BusIdentity{ID: devicesOwner}}, raw)
}

// activeUntil is "lap" active for ttl from when the hub takes it.
func activeUntil(since time.Time, ttl time.Duration) *devicewire.Desktop {
	return &devicewire.Desktop{MachineID: "lap", Since: since, TTLMS: ttl.Milliseconds(), Notifies: true}
}

// sendDesktops is manager owner's devices.desktops as the bus handler takes it.
func sendDesktops(h *Hub, owner string, ds ...devicewire.Desktop) {
	raw, _ := json.Marshal(devicewire.Desktops{Desktops: append([]devicewire.Desktop{}, ds...)})
	busEvents[devicewire.EventDesktops](h, &busConn{ident: db.BusIdentity{ID: owner}}, raw)
}

// linkUntil is when the hub takes "lap" to lapse (zero: not a Persona Link desktop).
func linkUntil(h *Hub) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, d := range h.linkDesktops.list {
		if d.machine == "lap" {
			return d.until
		}
	}
	return time.Time{}
}

// noEventOfType fails when c has an event of type typ queued.
func noEventOfType(t *testing.T, c *client, typ string) {
	t.Helper()
	for {
		select {
		case b := <-c.out:
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && m["type"] == typ {
				t.Fatalf("unexpected %s event: %s", typ, b)
			}
		default:
			return
		}
	}
}

func linkEntries(ev map[string]any) []map[string]any {
	list, _ := ev["desktops"].([]any)
	var out []map[string]any
	for _, d := range list {
		out = append(out, d.(map[string]any))
	}
	return out
}

// A computer whose Persona Link says the user is at it is listed, with the wire's fields, to the clients that asked
// for desktops_link only; a later deadline alone says nothing new; notifications that do not show, or a mesh
// member's computer, do not count; and it goes when its manager no longer reports it.
func TestLinkDesktopListedToClientsThatAsk(t *testing.T) {
	h := newTestHub(t)
	old := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull}
	asks := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull,
		urlFeatures: map[string]bool{featureDesktopsLink: true}}
	h.mu.Lock()
	h.clients[old], h.clients[asks] = struct{}{}, struct{}{}
	h.mu.Unlock()

	since := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	reportLinkDesktop(t, h, activeUntil(since, time.Minute), false)
	got := linkEntries(nextOfType(t, asks, "desktops"))
	want := map[string]any{"kind": "link", "machine_id": "lap", "device": "", "name": "Laptop", "since": "2026-11-09T12:00:00Z"}
	if len(got) != 1 || len(got[0]) != len(want) {
		t.Fatalf("desktops %v", got)
	}
	for k, v := range want {
		if got[0][k] != v {
			t.Fatalf("desktops %v: %s is %v, want %v", got, k, got[0][k], v)
		}
	}
	noEventOfType(t, old, "desktops")
	if !h.linkDesktopOnline(devicesOwner) || h.desktopOnline() {
		t.Fatal("a Persona Link desktop is not the desktop app")
	}
	if l := h.desktops(false); len(l) != 0 {
		t.Fatalf("the list older clients get %v", l)
	}

	reportLinkDesktop(t, h, activeUntil(since, 2*time.Minute), false) // the app's repeat
	noEventOfType(t, asks, "desktops")

	reportLinkDesktop(t, h, &devicewire.Desktop{MachineID: "lap", Since: since, TTLMS: time.Minute.Milliseconds()}, false)
	if got := linkEntries(nextOfType(t, asks, "desktops")); len(got) != 0 || h.linkDesktopOnline(devicesOwner) {
		t.Fatalf("notifications that do not show: %v", got)
	}
	// lap is this hub's own computer, not derived, here: only its manager may say it is a desktop.
	raw, _ := json.Marshal(devicewire.Desktops{Desktops: []devicewire.Desktop{*activeUntil(since, time.Minute)}})
	busEvents[devicewire.EventDesktops](h, &busConn{ident: db.BusIdentity{ID: "m-other"}}, raw)
	noEventOfType(t, asks, "desktops")
	if h.linkDesktopOnline(devicesOwner) {
		t.Fatal("another manager's word made this one's computer a desktop")
	}

	reportLinkDesktop(t, h, activeUntil(since, time.Minute), true)
	noEventOfType(t, asks, "desktops")
	if h.linkDesktopOnline(devicesOwner) {
		t.Fatal("a mesh member's computer counts")
	}

	reportLinkDesktop(t, h, activeUntil(since, time.Minute), false)
	nextOfType(t, asks, "desktops")
	reportLinkDesktop(t, h, nil, false)
	if got := linkEntries(nextOfType(t, asks, "desktops")); len(got) != 0 || h.linkDesktopOnline(devicesOwner) {
		t.Fatalf("after the manager stopped reporting it: %v", got)
	}
	noEventOfType(t, old, "desktops")
}

// A Persona Link desktop lapses at its deadline even when no manager says anything more: the mute ends when it is
// asked, and the clients hear it.
func TestLinkDesktopLapses(t *testing.T) {
	h := newTestHub(t)
	asks := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull,
		urlFeatures: map[string]bool{featureDesktopsLink: true}}
	h.mu.Lock()
	h.clients[asks] = struct{}{}
	h.mu.Unlock()
	reportLinkDesktop(t, h, activeUntil(time.Now(), 300*time.Millisecond), false)
	if got := linkEntries(nextOfType(t, asks, "desktops")); len(got) != 1 || !h.linkDesktopOnline(devicesOwner) {
		t.Fatalf("desktops %v", got)
	}
	for {
		select {
		case b := <-asks.out:
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && m["type"] == "desktops" {
				if got := linkEntries(m); len(got) != 0 {
					t.Fatalf("after it lapsed: %v", got)
				}
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the clients never heard it lapsed")
		}
	}
}

// Whether a Persona Link desktop mutes is read when an alert goes out, not left to the timer: one past its deadline
// does not count even before the timer took it out.
func TestLinkDesktopCountsOnlyUntilItsTime(t *testing.T) {
	h := newTestHub(t)
	h.mu.Lock()
	h.linkDesktops.list = []linkDesktop{{owner: devicesOwner, machine: "lap", name: "Laptop", since: time.Now().Add(-time.Minute),
		until: time.Now().Add(-time.Second)}}
	h.mu.Unlock()
	if h.linkDesktopOnline(devicesOwner) {
		t.Fatal("a lapsed Persona Link desktop mutes the phone")
	}
	if l := h.desktops(true); len(l) != 0 {
		t.Fatalf("a lapsed Persona Link desktop is listed: %v", l)
	}
}

// A client that asks for desktops_link in its client_hello (after the list it got on connecting) gets the list again,
// with the Persona Link desktops.
func TestClientHelloAskingForLinkDesktopsGetsThem(t *testing.T) {
	h := newTestHub(t)
	reportLinkDesktop(t, h, activeUntil(time.Now(), time.Minute), false)
	c := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.sendDesktops(c)
	if got := linkEntries(nextOfType(t, c, "desktops")); len(got) != 0 {
		t.Fatalf("before asking %v", got)
	}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["desktops_link"]}`))
	if got := linkEntries(nextOfType(t, c, "desktops")); len(got) != 1 || got[0]["kind"] != "link" {
		t.Fatalf("after asking %v", got)
	}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["desktops_link"]}`))
	noEventOfType(t, c, "desktops")
	// Asking no more: the list it has holds a Persona Link desktop it would not hear end.
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":[]}`))
	if got := linkEntries(nextOfType(t, c, "desktops")); len(got) != 0 {
		t.Fatalf("after no longer asking %v", got)
	}
}

// "Phone Muted" with only a Persona Link desktop holds back the "Needs you" cards on the WebSocket, the one alert
// Persona Link shows; finished turns, reply reminders and other alerts still come.
func TestWSNotifyMuteWhileLinkDesktop(t *testing.T) {
	h := newTestHub(t)
	phone := &client{device: "test-device", out: make(chan []byte, 16)}
	h.mu.Lock()
	h.clients[phone] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), phone, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	if err := h.push.setPrefs(phone.device, NotifyPrefs{Input: true, Done: DoneAlways, MuteWhileDesktop: true}); err != nil {
		t.Fatal(err)
	}
	kinds := func() []string {
		var out []string
		for {
			select {
			case raw := <-phone.out:
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["type"] == "notify" {
					out = append(out, m["kind"].(string))
				}
			default:
				return out
			}
		}
	}
	ix := proto.Interaction{ID: "ix1", MachineID: "m", SessionID: "s", Kind: proto.IxQuestion, Summary: "Pick one"}
	everything := func() {
		h.notifyInteraction(assistantwire.AssistantID, ix, "box", true)
		h.notifyDone(assistantwire.AssistantID, "done", "m", "box", "s", "Done", "", pushWants{Kind: "done", Took: time.Hour, Long: time.Minute})
		h.notifyReply(assistantwire.AssistantID, "m", "box", "s", "Still waiting", "")
		alert := notifyPayload(notifyRequest{Title: "t", Level: levelActive}, "", "")
		h.notifyWSFrom("", kindAlert, func(i18n.Lang) map[string]any { return alert }, pushWants{Kind: "input"}.fn(), quietNone)
	}
	kinds()
	everything()
	all := kinds()
	if len(all) != 4 || all[0] != "interaction" {
		t.Fatalf("no desktop: %v", all)
	}

	reportLinkDesktop(t, h, activeUntil(time.Now(), time.Minute), false)
	everything()
	if got := kinds(); len(got) != 3 || got[0] == "interaction" || got[1] == "interaction" || got[2] == "interaction" {
		t.Fatalf("with a Persona Link desktop: %v, want all but the card", got)
	}
	// A card the manager does not say Persona Link rings for (another card of the same agent, one past the summary's
	// cap): the phone still gets it.
	h.notifyInteraction(assistantwire.AssistantID, ix, "box", false)
	if got := kinds(); len(got) != 1 || got[0] != "interaction" {
		t.Fatalf("a card Persona Link does not ring for, with a Persona Link desktop: %v", got)
	}
	// Another manager's card: that Persona Link shows only its own manager's summary.
	h.notifyInteraction("m-other", ix, "box", true)
	if got := kinds(); len(got) != 1 || got[0] != "interaction" {
		t.Fatalf("another manager's card with a Persona Link desktop: %v", got)
	}

	if err := h.push.setPrefs(phone.device, NotifyPrefs{Input: true, Done: DoneAlways}); err != nil {
		t.Fatal(err)
	}
	everything()
	if got := kinds(); len(got) != 4 {
		t.Fatalf("Phone Muted off: %v", got)
	}
}

// The same for APNs: a card is held back from a muted phone while a Persona Link desktop is there, a finished turn is
// not; and with no desktop the card goes.
func TestPushMuteWhileLinkDesktop(t *testing.T) {
	h, f := testLive(t)
	h.push.desktopOnline, h.push.desktopOpen = h.desktopOnline, h.desktopOpen
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true, Done: DoneAlways, MuteWhileDesktop: true}); err != nil {
		t.Fatal(err)
	}
	input := pushWants{Kind: "input"}
	card := func(id string, covered bool) {
		h.notifyInteraction(assistantwire.AssistantID, proto.Interaction{ID: id, MachineID: "m1", SessionID: "s1", Kind: proto.IxPermission, Summary: "ls"},
			"Laptop", covered)
	}
	// Straight to the hub's list: testLive's store keeps no machine entries, which a devices.state would write.
	setLinkDesktops := func(list []linkDesktop) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.setLinkDesktopsLocked(list, time.Now())
	}
	setLinkDesktops([]linkDesktop{{owner: assistantwire.AssistantID, machine: "lap", name: "Laptop", since: time.Now(),
		until: time.Now().Add(time.Minute)}})
	card("ix1", true)
	h.push.notifyDone("m1", "Laptop", "s1", "Done", "", input.fn())
	if got := pushedTo(t, f, 0, 1); !same(got, "devtok") {
		t.Fatalf("with a Persona Link desktop: %v, want the finished turn only", got)
	}
	card("ix2", false) // not one Persona Link rings for
	if got := pushedTo(t, f, 1, 1); !same(got, "devtok") {
		t.Fatalf("a card Persona Link does not ring for went to %v", got)
	}
	setLinkDesktops(nil)
	card("ix3", true)
	if got := pushedTo(t, f, 2, 1); !same(got, "devtok") {
		t.Fatalf("with no desktop the card went to %v", got)
	}
}

// The hub counts a Persona Link desktop's TTL on its own clock from when it takes it, at most twice the presence TTL:
// a manager's clock ahead or behind (since says it) changes nothing, and no TTL keeps the phone quiet longer.
func TestLinkDesktopTimeIsTheHubs(t *testing.T) {
	h := newTestHub(t)
	for _, since := range []time.Time{time.Now().Add(10 * time.Minute), time.Now().Add(-10 * time.Minute)} {
		before := time.Now()
		reportLinkDesktop(t, h, activeUntil(since, time.Minute), false)
		if u := linkUntil(h); u.Before(before.Add(time.Minute)) || u.After(time.Now().Add(time.Minute)) {
			t.Fatalf("a manager whose since is %v: until %v, want a minute from now", time.Until(since).Round(time.Minute), u)
		}
	}
	reportLinkDesktop(t, h, activeUntil(time.Now(), 24*time.Hour), false)
	if u := linkUntil(h); u.After(time.Now().Add(devicewire.DesktopTTLMax)) {
		t.Fatalf("a day's TTL taken as %v", time.Until(u))
	}
	reportLinkDesktop(t, h, &devicewire.Desktop{MachineID: "lap", Since: time.Now(), TTLMS: 0, Notifies: true}, false)
	if !linkUntil(h).IsZero() || h.linkDesktopOnline(devicesOwner) {
		t.Fatal("no TTL left counted")
	}
}

// A desktop that lapsed while a snapshot comes before the timer: the clients that were told of it hear it went,
// whichever comes first; the timer then has nothing more to say. Without sleeping: the lapsed one is put in by hand.
func TestLapsedLinkDesktopIsAnnouncedWhenASnapshotComesFirst(t *testing.T) {
	h := newTestHub(t)
	asks := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull,
		urlFeatures: map[string]bool{featureDesktopsLink: true}}
	h.applyDeviceStates(devicewire.States{Machines: []devicewire.State{
		{Machine: proto.Machine{ID: "lap", Name: "Laptop", Kind: kindLink}},
		{Machine: proto.Machine{ID: "desk", Name: "Desk", Kind: kindLink}},
	}})
	h.mu.Lock()
	h.clients[asks] = struct{}{}
	gone := linkDesktop{owner: devicesOwner, machine: "lap", name: "Laptop", since: time.Now().Add(-time.Hour),
		until: time.Now().Add(-time.Second)}
	h.linkDesktops.list, h.linkDesktops.shown = []linkDesktop{gone}, []linkDesktop{gone} // told, lapsed, timer not run
	h.mu.Unlock()

	sendDesktops(h, devicesOwner, devicewire.Desktop{MachineID: "desk", Since: time.Now(), TTLMS: 60_000, Notifies: true})
	got := linkEntries(nextOfType(t, asks, "desktops"))
	if len(got) != 1 || got[0]["machine_id"] != "desk" {
		t.Fatalf("after the snapshot: %v, want desk only", got)
	}
	h.expireLinkDesktops()
	noEventOfType(t, asks, "desktops")

	// The same with an empty snapshot.
	h.mu.Lock()
	h.linkDesktops.list, h.linkDesktops.shown = []linkDesktop{gone}, []linkDesktop{gone}
	h.mu.Unlock()
	sendDesktops(h, devicesOwner)
	if got := linkEntries(nextOfType(t, asks, "desktops")); len(got) != 0 {
		t.Fatalf("after an empty snapshot: %v", got)
	}
}

// The order the manager says things in: its devices.desktops naming a computer the hub does not have yet (a first
// registration whose devices.state has not come) counts for nothing, and the next one after the devices.state does; a
// hub that restarted has no Persona Link desktops until the manager's handshake brings them (devices.state, then
// devices.desktops); a manager that connects again replaces its own list, and leaves another manager's.
func TestLinkDesktopsFollowTheMachinesOrder(t *testing.T) {
	h := newTestHub(t)
	sendDesktops(h, devicesOwner, *activeUntil(time.Now(), time.Minute))
	if h.linkDesktopOnline(devicesOwner) {
		t.Fatal("a computer the hub does not have counted")
	}
	reportLinkDesktop(t, h, activeUntil(time.Now(), time.Minute), false)
	if !h.linkDesktopOnline(devicesOwner) {
		t.Fatal("not counted once its machine came")
	}

	restarted := newTestHub(t)
	if restarted.linkDesktopOnline(devicesOwner) {
		t.Fatal("a restarted hub mutes before the manager said anything")
	}
	reportLinkDesktop(t, restarted, activeUntil(time.Now(), time.Minute), false)
	if !restarted.linkDesktopOnline(devicesOwner) {
		t.Fatal("not counted after the manager's handshake")
	}

	h.mu.Lock() // another manager's, as its devices.desktops left it
	h.linkDesktops.list = append(h.linkDesktops.list, linkDesktop{owner: "m-other", machine: "other-lap", name: "Other",
		since: time.Now(), until: time.Now().Add(time.Minute)})
	h.mu.Unlock()
	sendDesktops(h, devicesOwner) // the manager connected again: none of its computers is a desktop now
	if h.linkDesktopOnline(devicesOwner) || !h.linkDesktopOnline("m-other") {
		t.Fatal("a manager's new list did not replace its own, or took another's")
	}
}
