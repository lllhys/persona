package link

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestParsePresenceLine(t *testing.T) {
	for line, want := range map[string]Presence{
		`{"presence":{"active":true,"notifies":true}}`:  {Active: true, Notifies: true},
		`{"presence":{"active":false,"notifies":true}}`: {Notifies: true},
		`{"presence":{}}`: {},
	} {
		if got, ok := ParsePresenceLine([]byte(line)); !ok || got != want {
			t.Errorf("%s: %+v %v, want %+v", line, got, ok, want)
		}
	}
	for _, line := range []string{``, `{}`, `{"presence":null}`, `{"other":{"active":true}}`} {
		if p, ok := ParsePresenceLine([]byte(line)); ok {
			t.Errorf("%q read as presence %+v", line, p)
		}
	}
}

// What the manager is told: nothing before the app said anything, what is left of the TTL while an active line is
// fresh, inactive once it is PresenceTTL old (the app stopped saying it).
func TestPresenceBoxLatest(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if _, ok := (*PresenceBox)(nil).Latest(t0); ok {
		t.Fatal("a nil box (no app) has a presence")
	}
	b := NewPresenceBox()
	if _, ok := b.Latest(t0); ok {
		t.Fatal("a presence before the app said one")
	}
	b.Set(Presence{Active: true, Notifies: true}, t0)
	for after, want := range map[time.Duration]PresenceInfo{
		0:                                {Active: true, Notifies: true, TTLMS: 120_000},
		30 * time.Second:                 {Active: true, Notifies: true, TTLMS: 90_000},
		PresenceTTL - time.Millisecond:   {Active: true, Notifies: true, TTLMS: 1},
		PresenceTTL - time.Millisecond/2: {Notifies: true},
		PresenceTTL:                      {Notifies: true},
		PresenceTTL + 10*time.Minute:     {Notifies: true},
	} {
		if got, ok := b.Latest(t0.Add(after)); !ok || got != want {
			t.Errorf("%s after: %+v, want %+v", after, got, want)
		}
	}
	b.Set(Presence{Notifies: true}, t0)
	if got, _ := b.Latest(t0.Add(time.Second)); got != (PresenceInfo{Notifies: true}) {
		t.Errorf("inactive: %+v", got)
	}
}

// Active mode: the latest presence goes as the link comes up, again for every line the app writes (the same one
// included: it renews the TTL), and again on the next link.
func TestLinkSendsPresence(t *testing.T) {
	type sent struct {
		link int32
		m    Message
	}
	box := NewPresenceBox()
	box.Set(Presence{Active: true, Notifies: true}, time.Now().Add(-time.Minute))
	got := make(chan sent, 64)
	var links atomic.Int32
	drop := make(chan struct{})
	srv, _ := fakeHubHello(t, func(c *websocket.Conn, _ Message) {
		n := links.Add(1)
		if n == 1 {
			go func() {
				<-drop
				c.CloseNow()
			}()
		}
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m Message
			if json.Unmarshal(b, &m) == nil && m.Type == "presence" {
				got <- sent{n, m}
			}
		}
	})
	runAgainst(t, srv, "jd_x", Options{Presence: box})
	wait := func(what string, link int32, ok func(Message) bool) {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case s := <-got:
				if s.link == link && ok(s.m) {
					return
				}
			case <-timeout:
				t.Fatalf("no presence on link %d: %s", link, what)
			}
		}
	}
	// Said a minute ago: half the TTL is left.
	wait("as the link comes up", 1, func(m Message) bool {
		return m.Active && m.Notifies && m.TTLMS > 0 && m.TTLMS <= PresenceTTL.Milliseconds()-time.Minute.Milliseconds()
	})
	box.Set(Presence{Active: true, Notifies: true}, time.Now())
	wait("the same line again, with the whole TTL", 1, func(m Message) bool {
		return m.Active && m.TTLMS > PresenceTTL.Milliseconds()-time.Minute.Milliseconds()
	})
	box.Set(Presence{Notifies: true}, time.Now())
	wait("a change", 1, func(m Message) bool { return !m.Active && m.Notifies && m.TTLMS == 0 })
	close(drop)
	wait("on the next link", 2, func(m Message) bool { return !m.Active && m.Notifies })
}

func TestParseRangLine(t *testing.T) {
	if c, ok := ParseRangLine([]byte(`{"rang":{"interaction_id":"ix-1","conn":3}}`)); !ok || c != (Confirm{"ix-1", 3}) {
		t.Fatalf("%+v %v", c, ok)
	}
	for _, line := range []string{``, `{}`, `{"rang":null}`, `{"rang":{}}`, `{"rang":{"interaction_id":"","conn":1}}`,
		`{"rang":{"interaction_id":"ix-1"}}`, `{"rang":{"interaction_id":"ix-1","conn":0}}`, `{"presence":{"active":true}}`} {
		if c, ok := ParseRangLine([]byte(line)); ok {
			t.Errorf("%q read as a confirmation %+v", line, c)
		}
	}
}

// A link passes on the app's confirmations for itself, as rang messages, and says which link it is in its status
// (Conn). A confirmation for an earlier link is not passed on, also when it reaches the queue only once the next link
// is up (it was stuck in the pipe).
func TestLinkSendsConfirmations(t *testing.T) {
	type sent struct {
		link int32
		card string
	}
	confirms := NewConfirms()
	got := make(chan sent, 16)
	var links atomic.Int32
	drop := make(chan struct{})
	srv, _ := fakeHubHello(t, func(c *websocket.Conn, _ Message) {
		n := links.Add(1)
		if n == 1 {
			go func() {
				<-drop
				c.CloseNow()
			}()
		}
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m Message
			if json.Unmarshal(b, &m) == nil && m.Type == "rang" {
				got <- sent{n, m.InteractionID}
			}
		}
	})
	status := runAgainst(t, srv, "jd_x", Options{Confirms: confirms})
	// pushUntil pushes c every 50 ms (the link's sender may not run yet) until a rang comes, and checks it.
	pushUntil := func(what string, c Confirm, link int32) {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for tick := time.NewTicker(50 * time.Millisecond); ; {
			confirms.Push(c)
			select {
			case s := <-got:
				tick.Stop()
				if s != (sent{link, c.Card}) {
					t.Fatalf("%s: got %+v, want %s on link %d", what, s, c.Card, link)
				}
				return
			case <-tick.C:
			case <-timeout:
				t.Fatalf("no rang: %s", what)
			}
		}
	}
	first := waitFor(t, status, func(s Status) bool { return s.State == "connected" })
	if first.Conn == 0 {
		t.Fatal("a connected status names no link")
	}
	pushUntil("a card rung for on this link", Confirm{"ix-1", first.Conn}, 1)
	close(drop)
	second := waitFor(t, status, func(s Status) bool { return s.State == "connected" && s.Conn != first.Conn })
	if second.Conn <= first.Conn {
		t.Fatalf("the next link is %d after %d", second.Conn, first.Conn)
	}
	for drained := false; !drained; { // repeats of ix-1 the first link sent before it went
		select {
		case <-got:
		default:
			drained = true
		}
	}
	confirms.Push(Confirm{"ix-stale", first.Conn}) // the earlier link's, out of the pipe only now
	pushUntil("on the next link", Confirm{"ix-2", second.Conn}, 2)
}

// A restarted persona-link numbers its links above the earlier run's: a confirmation the app wrote for the earlier
// run's link matches none of the new one's.
func TestLinkNumbersOutliveARestart(t *testing.T) {
	srv, _ := fakeHubHello(t, func(c *websocket.Conn, _ Message) {
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	})
	connected := func(s Status) bool { return s.State == "connected" }
	first := waitFor(t, runAgainst(t, srv, "jd_x", Options{}), connected)
	// Each run seeds its counter from Unix milliseconds, then increments it for the first link.
	// Start the next run only once its seed is past the earlier link's number.
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for uint64(time.Now().UnixMilli()) <= first.Conn {
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatal("clock did not advance past the earlier link's number")
		}
	}
	second := waitFor(t, runAgainst(t, srv, "jd_x", Options{}), connected)
	if second.Conn <= first.Conn {
		t.Fatalf("a later run's first link is %d, the earlier run's %d", second.Conn, first.Conn)
	}
}

// A run never numbers two links the same, also across a switch to passive mode and back: a confirmation still queued
// for the earlier active mode's link does not pass for the new one's.
func TestLinkNumbersOutliveAModeSwitch(t *testing.T) {
	srv, _ := fakeHubHello(t, func(c *websocket.Conn, _ Message) {
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	})
	path := filepath.Join(t.TempDir(), "link.json")
	status := runAgainst(t, srv, "jd_x", Options{Path: path})
	first := waitFor(t, status, func(s Status) bool { return s.State == "connected" })
	if _, err := SetMode(path, ModePassive, "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, status, func(s Status) bool { return s.Mode == ModePassive })
	if _, err := SetMode(path, ModeActive, ""); err != nil {
		t.Fatal(err)
	}
	again := waitFor(t, status, func(s Status) bool { return s.Mode == ModeActive && s.State == "connected" })
	if again.Conn <= first.Conn {
		t.Fatalf("the link after the switch back is %d, the one before it %d", again.Conn, first.Conn)
	}
}
