package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/link"
)

func presenceMsg(active, notifies bool, ttlMS int64) link.Message {
	return link.Message{Type: "presence", Active: active, Notifies: notifies, TTLMS: ttlMS}
}

// notifying is a summary listing each card as its own agent's, notifying; more: agents past the cap.
func notifying(more int, cards ...string) link.Attention {
	a := link.Attention{NeedsYou: len(cards) + more, More: more, Items: []link.AttentionItem{}}
	for i, ix := range cards {
		a.Items = append(a.Items, link.AttentionItem{MachineID: "lap", SessionID: fmt.Sprintf("s-%d", i), Kind: "permission",
			Notify: true, InteractionID: ix, Route: fmt.Sprintf("/s/lap/s-%d", i)})
	}
	return a
}

// sentDesktops is how many devices.desktops d sent, and the last one.
func sentDesktops(d *Devices) (int, devicewire.Desktops) {
	f := d.host()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.desktops) == 0 {
		return 0, devicewire.Desktops{}
	}
	return len(f.desktops), f.desktops[len(f.desktops)-1]
}

// heldLink is a link machine's Persona Link connected to d with the hub's handover cfg, asking for the summary, and the
// summary it took (d has one before it connects, so it holds it).
func heldLink(t *testing.T, d *Devices, cfg devicewire.Config) (*testLinkClient, *mgrLink) {
	t.Helper()
	if d.attention.Load() == nil {
		d.SetAttention(notifying(0))
	}
	c, l := attentionLinkWith(t, d, cfg, link.FeatureAttention)
	c.next(t, "attention")
	return c, l
}

func laptopCfg() devicewire.Config {
	return devicewire.Config{HubID: "hub-test", HubName: "test", Machines: []json.RawMessage{linkEntry("Laptop")}}
}

// isDesktop: m is a desktop now.
func isDesktop(m *Machine) bool { return desktopOf(m, time.Now()) != nil }

// desktopOf is m as a desktop at now (nil: none).
func desktopOf(m *Machine, now time.Time) *desktopNow {
	p, _ := m.desktop(now)
	return p
}

// hasPresence: what m's Persona Link said was taken.
func hasPresence(m *Machine) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.presence != nil
}

// Active mode: a presence message makes the machine a desktop until its TTL runs out, every one renews it and is
// reported (the hub needs each renewal), an inactive one ends it, and so does the link going away.
func TestLinkPresenceIsReported(t *testing.T) {
	d := newTestDevices(t)
	c, l := heldLink(t, d, laptopCfg())
	m := l.m
	if isDesktop(m) {
		t.Fatal("a desktop before its Persona Link said so")
	}

	before := time.Now()
	c.send(t, presenceMsg(true, true, 90_000))
	eventually(t, 5*time.Second, "the presence", func() bool { return isDesktop(m) })
	first := desktopOf(m, time.Now())
	if !first.Notifies || first.Since.Before(before) || first.until.Before(before.Add(90*time.Second)) ||
		first.until.After(time.Now().Add(90*time.Second)) || first.TTLMS > 90_000 || first.TTLMS < 80_000 {
		t.Fatalf("desktop %+v", first)
	}
	eventually(t, 5*time.Second, "the presence reported", func() bool {
		_, last := sentDesktops(d)
		return len(last.Desktops) == 1 && last.Desktops[0].MachineID == "lap" && last.Desktops[0].Since.Equal(first.Since) &&
			last.Desktops[0].TTLMS > 80_000
	})
	if desktopOf(m, first.until) != nil {
		t.Fatal("still a desktop once its TTL ran out")
	}
	// What is said is the TTL left: a report later (a handshake) does not fill it up again.
	if later := desktopOf(m, time.Now().Add(30*time.Second)); later.TTLMS > 60_000 {
		t.Fatalf("30 s later the TTL left is %d ms", later.TTLMS)
	}

	n, _ := sentDesktops(d)
	c.send(t, presenceMsg(true, true, 120_000)) // the app's repeat
	eventually(t, 5*time.Second, "the renewed presence", func() bool {
		p := desktopOf(m, time.Now())
		return p != nil && p.until.After(first.until)
	})
	if again := desktopOf(m, time.Now()); !again.Since.Equal(first.Since) {
		t.Fatalf("a repeat moved since: %v → %v", first.Since, again.Since)
	}
	eventually(t, 5*time.Second, "the repeat reported (the hub would let it lapse)", func() bool {
		k, last := sentDesktops(d)
		return k > n && len(last.Desktops) == 1 && last.Desktops[0].TTLMS > 110_000
	})

	c.send(t, presenceMsg(false, true, 0))
	eventually(t, 5*time.Second, "the end of the presence", func() bool { return !isDesktop(m) })
	eventually(t, 5*time.Second, "its end reported", func() bool { _, last := sentDesktops(d); return len(last.Desktops) == 0 })

	c.send(t, presenceMsg(true, true, 0)) // no ttl_ms: the default
	eventually(t, 5*time.Second, "the presence again", func() bool { return isDesktop(m) })
	if p := desktopOf(m, time.Now()); p.Since.Equal(first.Since) || p.until.After(time.Now().Add(link.PresenceTTL)) {
		t.Fatalf("after an inactive one %+v", p)
	}
	_ = c.conn.CloseNow()
	eventually(t, 5*time.Second, "the link's end", func() bool { return !isDesktop(m) })
	// The next link starts with nothing: what the last one said ended with it.
	c, l = dialTestLinkHello(t, d, link.Message{Type: "hello", Machine: "lap", Version: "test", Address: testHubAddress,
		Features: []string{link.FeatureAttention}})
	c.next(t, "attention")
	if l.m != m || isDesktop(m) {
		t.Fatalf("a new link took the last one's presence: %+v", desktopOf(m, time.Now()))
	}
}

// A new link that takes over while the old one is still open (a command-line Link, an app restarting) starts with no
// presence: the old link's is not the new one's.
func TestNewLinkDropsTheOldLinksPresence(t *testing.T) {
	d := newTestDevices(t)
	c, l := heldLink(t, d, laptopCfg())
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence", func() bool { return isDesktop(l.m) })
	c2, _ := dialTestLinkHello(t, d, link.Message{Type: "hello", Machine: "lap", Version: "test", Address: testHubAddress,
		Features: []string{link.FeatureAttention}})
	c2.next(t, "attention") // sent once the new link is the machine's
	if l2 := l.m.currentLink(); l2 == nil || l2 == l {
		t.Fatal("the new link did not take over")
	}
	if hasPresence(l.m) || isDesktop(l.m) {
		t.Fatal("the new link took the old link's presence")
	}
}

// What a Persona Link may ask for is capped: no presence keeps phones quiet for long without being said again; and one
// that says its notifications do not show is kept as it is said (the hub does not count it).
func TestLinkPresenceTTLIsCapped(t *testing.T) {
	d := newTestDevices(t)
	c, l := heldLink(t, d, laptopCfg())
	m := l.m
	for _, ttl := range []int64{1 << 62, -5} {
		c.send(t, presenceMsg(false, false, 0))
		eventually(t, 5*time.Second, "no presence", func() bool { return !isDesktop(m) })
		c.send(t, presenceMsg(true, true, ttl))
		eventually(t, 5*time.Second, "the presence", func() bool { return isDesktop(m) })
		limit := devicewire.DesktopTTLMax
		if ttl < 0 {
			limit = link.PresenceTTL
		}
		if p := desktopOf(m, time.Now()); p.until.After(time.Now().Add(limit)) || p.TTLMS > limit.Milliseconds() {
			t.Fatalf("ttl_ms %d: %+v until %v", ttl, p.Desktop, p.until)
		}
	}
	c.send(t, presenceMsg(true, false, 60_000))
	eventually(t, 5*time.Second, "notifications off", func() bool { p := desktopOf(m, time.Now()); return p != nil && !p.Notifies })
}

// Only a computer that gets the summary as it is counts (attentionFor's rule): its Persona Link asked for it, it is not
// waiting to be confirmed, not a work computer. While the hub cannot say which computers wait (the summary freezes, so
// no new card reaches it) it does not count; a work computer is known all the same.
func TestPresenceCountsOnlyWhereTheSummaryGoes(t *testing.T) {
	d := newTestDevices(t)
	d.SetAttention(notifying(0))
	c, l := attentionLink(t, d) // no attention in its hello
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence taken", func() bool { return hasPresence(l.m) })
	if isDesktop(l.m) {
		t.Fatal("a desktop whose Persona Link takes no summary")
	}

	d = newTestDevices(t)
	cfg := laptopCfg()
	cfg.Pending = []string{"lap"}
	c, l = heldLink(t, d, cfg)
	m := l.m
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence taken", func() bool { return hasPresence(m) })
	if isDesktop(m) {
		t.Fatal("a desktop waiting to be confirmed")
	}
	cfg.Pending = nil // confirmed: the summary as it is goes out
	d.Configure(cfg)
	c.next(t, "attention")
	eventually(t, 5*time.Second, "a desktop once confirmed", func() bool { return isDesktop(m) })
	cfg.PendingUnknown = true
	d.Configure(cfg)
	if isDesktop(m) {
		t.Fatal("a desktop while the hub cannot say: its summary is frozen, a new card would reach neither")
	}
	cfg.PendingUnknown = false
	d.Configure(cfg)
	if !isDesktop(m) {
		t.Fatal("not a desktop once the hub can say again")
	}
	cfg.Pending = []string{"lap"}
	d.Configure(cfg)
	if isDesktop(m) {
		t.Fatal("still a desktop once waiting to be confirmed again")
	}

	d = newTestDevices(t)
	work, _ := json.Marshal(MachineConfig{ID: "lap", Name: "Laptop", Kind: kindLink, Trust: "work"})
	c, l = heldLink(t, d, devicewire.Config{HubID: "hub-test", HubName: "test", Machines: []json.RawMessage{work}, PendingUnknown: true})
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence taken", func() bool { return hasPresence(l.m) })
	if isDesktop(l.m) {
		t.Fatal("a work computer counts while the hub cannot say")
	}
}

// A Persona Link desktop must hold the summary as it is now: none yet, no desktop; once it took it, a desktop.
func TestLinkDesktopHoldsTheSummary(t *testing.T) {
	d := newTestDevices(t)
	c, l := attentionLinkWith(t, d, laptopCfg(), link.FeatureAttention) // the manager has no summary yet
	c.next(t, "endpoints")
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence taken", func() bool { return hasPresence(l.m) })
	if isDesktop(l.m) {
		t.Fatal("a desktop that holds no summary")
	}
	d.SetAttention(notifying(0, "ix-1"))
	c.next(t, "attention")
	eventually(t, 5*time.Second, "a desktop once it holds it", func() bool { return isDesktop(l.m) })
}

// What a link holds: nothing until it took a summary, then the summary and its cards; a newer summary not delivered
// yet still counts; a failure ends it until one is taken again. Confirmations count only for cards of the summary it
// holds or the one on its way (the app may answer before the send returns), once each, and go when a summary no longer
// lists their cards.
func TestSummaryHeld(t *testing.T) {
	var h summaryHeld
	a1, a2 := notifying(0, "ix-1", "ix-2"), notifying(1, "ix-2", "ix-3")
	a1.Rev, a2.Rev = "r1", "r2"
	if h.holds() {
		t.Fatal("held before any summary was taken")
	}
	h.confirm("ix-1")
	if len(h.rang) != 0 {
		t.Fatal("a confirmation before any summary counts")
	}
	h.done(&a1, true)
	if !h.holds() {
		t.Fatal("not held once taken")
	}
	h.confirm("ix-1")
	h.confirm("ix-1")
	h.confirm("ix-9") // no card of the summary it holds
	if !slices.Equal(h.rang, []string{"ix-1"}) {
		t.Fatalf("rang %v", h.rang)
	}
	h.done(&a2, false)
	if h.holds() {
		t.Fatal("held after a failed delivery")
	}
	h.done(&a2, true)
	if !h.holds() || len(h.rang) != 0 {
		t.Fatalf("after r2 was taken: held %v, rang %v (ix-1 is no longer listed)", h.holds(), h.rang)
	}
	a3 := notifying(0, "ix-3", "ix-4")
	a3.Rev = "r3"
	h.begin(&a3)
	h.confirm("ix-4") // the app was quicker than the send's return
	h.done(&a3, true)
	if !slices.Equal(h.rang, []string{"ix-4"}) {
		t.Fatalf("a confirmation of the summary on its way: rang %v", h.rang)
	}
	h.confirm("ix-1") // in neither: no longer on its way
	if !slices.Equal(h.rang, []string{"ix-4"}) {
		t.Fatalf("a confirmation of a card no summary lists now: rang %v", h.rang)
	}
}

// After a handshake the hub may have lost what it had: the full report carries the machines first, then the desktops
// again, the same list included; an ordinary report carries them only when they changed. On a new connection a report
// does not wait for the full devices.state (the hub drops entries of computers it does not yet have as this manager's):
// the full one after the state is the list as it is once the state went out.
func TestDesktopsGoOutOnHandshake(t *testing.T) {
	d := newTestDevices(t)
	c, l := heldLink(t, d, laptopCfg())
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence reported", func() bool {
		_, last := sentDesktops(d)
		return len(last.Desktops) == 1
	})
	settle := func() { d.desktopsReported(context.Background(), d.requestDesktops(false)) }
	settle()
	n, _ := sentDesktops(d)
	d.report(false)
	settle()
	if again, _ := sentDesktops(d); again != n {
		t.Fatalf("an ordinary report resent unchanged desktops (%d → %d)", n, again)
	}
	f := d.host()
	f.mu.Lock()
	from := len(f.notified)
	f.mu.Unlock()
	release := f.hold(devicewire.EventState)
	defer release()
	d.Hello(json.RawMessage(`{}`)) // a new connection to the hub
	reported := make(chan struct{})
	go func() { d.report(true); close(reported) }()
	eventually(t, 5*time.Second, "the machines on their way", f.held)
	// A real change while the hub has not taken the machines: it goes out at once, and what goes out after the state
	// has it too.
	c.send(t, presenceMsg(true, false, 30_000))
	eventually(t, 5*time.Second, "the renewal reported before the state", func() bool {
		_, last := sentDesktops(d)
		return len(last.Desktops) == 1 && !last.Desktops[0].Notifies
	})
	release()
	<-reported
	settle()
	f.mu.Lock()
	order := slices.Clone(f.notified[from:])
	f.mu.Unlock()
	i := slices.Index(order, devicewire.EventState)
	if i < 0 || !slices.Contains(order[i+1:], devicewire.EventDesktops) {
		t.Fatalf("a handshake's events %v: no devices.desktops after its devices.state", order)
	}
	_, last := sentDesktops(d)
	if len(last.Desktops) != 1 || last.Desktops[0].MachineID != l.m.cfg.ID || last.Desktops[0].Notifies {
		t.Fatalf("the desktops after the state: %+v; want the list as it is then (notifications off)", last)
	}
}

// While the manager stops nothing goes out (the next manager says it), and a request is dealt with at once: a push
// waiting for the report does not wait out its deadline.
func TestStoppingSendsNoDesktops(t *testing.T) {
	d := newTestDevices(t)
	d.stopping.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !d.desktopsReported(ctx, d.requestDesktops(true)) {
		t.Fatal("a request waited while the manager stops")
	}
	if n, _ := sentDesktops(d); n != 0 {
		t.Fatalf("%d devices.desktops sent while the manager stops", n)
	}
}

// A Persona Link entry that another hub.json entry absorbed (the same computer) is reported by that one's id: the hub
// knows the computer only by it.
func TestAbsorbedLinkPresenceUsesTheComputersID(t *testing.T) {
	d := newTestDevices(t)
	cfg := devicewire.Config{HubID: "hub-test", HubName: "test", Machines: []json.RawMessage{linkEntryOf("box"), linkEntry("Laptop")}}
	c, l := heldLink(t, d, cfg)
	f := &d.fleet
	f.mu.Lock()
	f.init()
	f.absorbed[l.m] = d.cfgMachine("box")
	f.mu.Unlock()
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the presence reported", func() bool {
		_, last := sentDesktops(d)
		return len(last.Desktops) == 1 && last.Desktops[0].MachineID == "box"
	})
}

func rangMsg(card string) link.Message { return link.Message{Type: "rang", InteractionID: card} }

// activeDesktop is a laptop whose Persona Link holds the summary s (its cards) and says the user is at it.
func activeDesktop(t *testing.T, d *Devices, s link.Attention) (*testLinkClient, *mgrLink) {
	t.Helper()
	d.SetAttention(s)
	c, l := heldLink(t, d, laptopCfg())
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the desktop", func() bool { return isDesktop(l.m) })
	return c, l
}

// LinkRang counts only the app's confirmations: false at once without a desktop; a card the summary lists but the app
// did not confirm, or one it confirmed though no summary it holds lists it, rang for nothing; one it confirms while
// LinkRang waits, or confirmed before it began, rang; and then the hub has been told of the desktop.
func TestLinkRang(t *testing.T) {
	d := newTestDevices(t)
	start := time.Now()
	if d.LinkRang(context.Background(), "ix-1", 5*time.Second) || time.Since(start) > time.Second {
		t.Fatal("rang (or waited) with no desktop")
	}
	c, _ := activeDesktop(t, d, notifying(1, "ix-1", "ix-2", "ix-3"))
	if d.LinkRang(context.Background(), "ix-1", 100*time.Millisecond) {
		t.Fatal("a card the app never confirmed rang")
	}
	c.send(t, rangMsg("ix-old")) // in More only: the app has nothing of it to show
	if d.LinkRang(context.Background(), "ix-old", 100*time.Millisecond) {
		t.Fatal("a confirmation of a card no summary the link took lists counts")
	}
	c.send(t, rangMsg("ix-2"))
	eventually(t, 5*time.Second, "the confirmation taken", func() bool {
		return d.LinkRang(context.Background(), "ix-2", 10*time.Millisecond)
	})
	n, _ := sentDesktops(d)
	if n == 0 {
		t.Fatal("LinkRang said yes before the hub was told of the desktop")
	}
	got := make(chan bool, 1)
	go func() { got <- d.LinkRang(context.Background(), "ix-3", 5*time.Second) }()
	select {
	case ok := <-got:
		t.Fatalf("LinkRang ended with %v before the app confirmed the card", ok)
	case <-time.After(50 * time.Millisecond):
	}
	c.send(t, rangMsg("ix-3"))
	if !<-got {
		t.Fatal("a confirmation that came while LinkRang waited")
	}
}

// A desktop that stops counting for a while (its presence lapses, a send fails) does not end a card's wait: a
// confirmation once it counts again, in time, still covers the card.
func TestLinkRangOutlivesAGap(t *testing.T) {
	d := newTestDevices(t)
	c, l := activeDesktop(t, d, notifying(0, "ix-1", "ix-2"))
	got := make(chan bool, 1)
	go func() { got <- d.LinkRang(context.Background(), "ix-1", 10*time.Second) }()
	select {
	case ok := <-got:
		t.Fatalf("LinkRang ended with %v before the gap", ok)
	case <-time.After(50 * time.Millisecond):
	}
	c.send(t, presenceMsg(false, true, 0))
	eventually(t, 5*time.Second, "no desktop", func() bool { return !isDesktop(l.m) })
	c.send(t, rangMsg("ix-2")) // wakes the wait while there is no desktop
	eventually(t, 5*time.Second, "the confirmation during the gap taken", func() bool {
		l.heldMu.Lock()
		defer l.heldMu.Unlock()
		return slices.Contains(l.held.rang, "ix-2")
	})
	select {
	case ok := <-got:
		t.Fatalf("LinkRang ended with %v during the gap", ok)
	case <-time.After(50 * time.Millisecond):
	}
	c.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the desktop back", func() bool { return isDesktop(l.m) })
	c.send(t, rangMsg("ix-1"))
	if !<-got {
		t.Fatal("a gap in the desktop ended the wait")
	}
}

// LinkRang checks the link again once the report went out: a link replaced meanwhile by one without the confirmation
// does not count, and the wait goes on until the new link confirms the card.
func TestLinkRangChecksTheLinkAfterTheReport(t *testing.T) {
	d := newTestDevices(t)
	c, l := activeDesktop(t, d, notifying(0, "ix-1"))
	c.send(t, rangMsg("ix-1"))
	eventually(t, 5*time.Second, "the confirmation taken", func() bool {
		return d.LinkRang(context.Background(), "ix-1", 10*time.Millisecond)
	})
	release := d.host().hold(devicewire.EventDesktops)
	defer release()
	stuck := d.requestDesktops(true) // the reporter is now stuck in the hub's hands
	got := make(chan bool, 1)
	go func() { got <- d.LinkRang(context.Background(), "ix-1", 10*time.Second) }()
	// LinkRang asks for a report only once it found ix-1 on the first link.
	eventually(t, 5*time.Second, "LinkRang waiting for the report", func() bool { return d.desktopsRep.asked.Load() > stuck })

	c2, _ := dialTestLinkHello(t, d, link.Message{Type: "hello", Machine: "lap", Version: "test", Address: testHubAddress,
		Features: []string{link.FeatureAttention}})
	c2.next(t, "attention")
	c2.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the new link's desktop", func() bool {
		p, nl := l.m.desktop(time.Now())
		return p != nil && nl != l
	})
	release()
	select {
	case ok := <-got:
		t.Fatalf("LinkRang said %v once the report went out, before the new link confirmed the card", ok)
	case <-time.After(300 * time.Millisecond):
	}
	c2.send(t, rangMsg("ix-1"))
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("the new link's confirmation did not count")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LinkRang did not end on the new link's confirmation")
	}
}

// LinkRang's check after the report takes the same link only. LinkRang found the card confirmed on the laptop's link;
// before the report it waits for takes its list, the laptop stops counting and the box confirms the card. That report
// lists the box, but LinkRang found the box only after the list was taken: it must not answer on the box's confirmation
// with that report, and waits for one taken after it found the box.
func TestLinkRangChecksTheSameLinkAfterTheReport(t *testing.T) {
	d := newTestDevices(t)
	d.SetAttention(notifying(0, "ix-1"))
	d.Configure(devicewire.Config{HubID: "hub-test", HubName: "test", Machines: []json.RawMessage{linkEntry("Laptop"), linkEntryOf("box")}})
	join := func(id string) (*testLinkClient, *mgrLink) {
		c, l := dialTestLinkHello(t, d, link.Message{Type: "hello", Machine: id, Version: "test", Address: testHubAddress,
			Features: []string{link.FeatureAttention}})
		c.next(t, "attention")
		c.send(t, presenceMsg(true, true, 60_000))
		eventually(t, 5*time.Second, id+" a desktop", func() bool { return isDesktop(l.m) })
		return c, l
	}
	lap, lapLink := join("lap")
	box, boxLink := join("box")
	lap.send(t, rangMsg("ix-1"))
	eventually(t, 5*time.Second, "the laptop's confirmation taken", func() bool {
		return d.LinkRang(context.Background(), "ix-1", 10*time.Millisecond)
	})
	f := d.host()
	release := f.hold(devicewire.EventDesktops)
	defer release()
	stuck := d.requestDesktops(true)
	eventually(t, 5*time.Second, "a report held at the hub", f.held)
	base, _ := sentDesktops(d)

	got := make(chan bool, 1)
	go func() { got <- d.LinkRang(context.Background(), "ix-1", 10*time.Second) }()
	// LinkRang asks for a report only once it found ix-1 on the laptop's link.
	eventually(t, 5*time.Second, "LinkRang waiting for the report", func() bool { return d.desktopsRep.asked.Load() > stuck })
	lap.send(t, presenceMsg(false, true, 0))
	box.send(t, rangMsg("ix-1"))
	eventually(t, 5*time.Second, "the laptop no desktop", func() bool { return !isDesktop(lapLink.m) })
	eventually(t, 5*time.Second, "the box's confirmation taken", func() bool {
		boxLink.heldMu.Lock()
		defer boxLink.heldMu.Unlock()
		return slices.Contains(boxLink.held.rang, "ix-1")
	})

	f.passOne(t, devicewire.EventDesktops) // the report held before LinkRang asked
	eventually(t, 5*time.Second, "the report LinkRang waits for held, its list taken", func() bool {
		n, _ := sentDesktops(d)
		return n == base+1 && f.held()
	})
	before := desktopOf(boxLink.m, time.Now()).until
	box.send(t, presenceMsg(true, true, 60_000)) // a renewal: a report taken now differs from the one held
	eventually(t, 5*time.Second, "the box's renewal taken", func() bool {
		p := desktopOf(boxLink.m, time.Now())
		return p != nil && p.until.After(before)
	})
	f.passOne(t, devicewire.EventDesktops) // the report LinkRang asked for
	select {
	case ok := <-got:
		t.Fatalf("LinkRang said %v on the box's confirmation without a report taken after it found the box", ok)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("the box's confirmation did not count once its report went out")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LinkRang did not end once the box's report went out")
	}
}

// A confirmation counts on the link it came on: a new link of the computer starts with none.
func TestConfirmationCountsOnItsLinkOnly(t *testing.T) {
	d := newTestDevices(t)
	c, l := activeDesktop(t, d, notifying(0, "ix-1"))
	c.send(t, rangMsg("ix-1"))
	eventually(t, 5*time.Second, "the confirmation taken", func() bool {
		return d.LinkRang(context.Background(), "ix-1", 10*time.Millisecond)
	})
	c2, _ := dialTestLinkHello(t, d, link.Message{Type: "hello", Machine: "lap", Version: "test", Address: testHubAddress,
		Features: []string{link.FeatureAttention}})
	c2.next(t, "attention")
	c2.send(t, presenceMsg(true, true, 60_000))
	eventually(t, 5*time.Second, "the new link's desktop", func() bool {
		p, nl := l.m.desktop(time.Now())
		return p != nil && nl != l
	})
	if d.LinkRang(context.Background(), "ix-1", 100*time.Millisecond) {
		t.Fatal("a confirmation from the earlier link counts on the new one")
	}
}

// A confirmation of a computer that is no desktop now (its Persona Link says the user left, or its notifications do
// not show) does not count.
func TestConfirmationOfALinkThatIsNoDesktop(t *testing.T) {
	d := newTestDevices(t)
	c, l := activeDesktop(t, d, notifying(0, "ix-1"))
	c.send(t, rangMsg("ix-1"))
	eventually(t, 5*time.Second, "the confirmation taken", func() bool {
		return d.LinkRang(context.Background(), "ix-1", 10*time.Millisecond)
	})
	c.send(t, presenceMsg(true, false, 60_000))
	eventually(t, 5*time.Second, "notifications off", func() bool { p := desktopOf(l.m, time.Now()); return p != nil && !p.Notifies })
	if d.LinkRang(context.Background(), "ix-1", 100*time.Millisecond) {
		t.Fatal("a desktop whose notifications do not show counts")
	}
	c.send(t, presenceMsg(false, true, 0))
	eventually(t, 5*time.Second, "no desktop", func() bool { return !isDesktop(l.m) })
	if d.LinkRang(context.Background(), "ix-1", 100*time.Millisecond) {
		t.Fatal("a computer that is no desktop now counts")
	}
}

// LinkRang's deadline covers its wait for the hub to be told of the desktops: with the hub not taking devices.desktops,
// a card says no once its time is up, also while another card's LinkRang still waits (neither waits behind the other),
// and the service's end ends a wait at once.
func TestLinkRangWithinTheDeadlineWhileTheHubIsSlow(t *testing.T) {
	d := newTestDevices(t)
	c, _ := activeDesktop(t, d, notifying(0, "ix-1", "ix-2"))
	c.send(t, rangMsg("ix-1"))
	c.send(t, rangMsg("ix-2"))
	eventually(t, 5*time.Second, "the confirmations taken", func() bool {
		return d.LinkRang(context.Background(), "ix-2", 10*time.Millisecond)
	})
	release := d.host().hold(devicewire.EventDesktops)
	defer release()
	stuck := d.requestDesktops(true) // the reporter is now stuck in the hub's hands
	eventually(t, 5*time.Second, "a report held at the hub", d.host().held)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan bool, 1)
	go func() { first <- d.LinkRang(ctx, "ix-1", time.Minute) }()
	eventually(t, 5*time.Second, "LinkRang waiting for the report", func() bool { return d.desktopsRep.asked.Load() > stuck })

	start := time.Now()
	if d.LinkRang(context.Background(), "ix-2", 300*time.Millisecond) {
		t.Fatal("yes while the hub was never told of the desktop")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a wait of 300 ms took %v: past its deadline, or behind the other card's", took)
	}
	select {
	case <-first:
		t.Fatal("the other card's LinkRang ended before its time")
	default:
	}

	cancel()
	select {
	case ok := <-first:
		if ok {
			t.Fatal("yes once the service ended")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the service's end did not end LinkRang")
	}
}
