package devices

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/link"
)

// Presence through Persona Link (internal/link/presence.go, docs/operations/link.md "Presence"): a computer's menu bar
// app says the user is at it and its notifications show, and which cards' notifications it handed to Notification
// Center. The manager keeps what a link said until the TTL runs out and reports the computers that count in
// devices.desktops; the hub counts them as desktops for "Phone Muted". A card's push is left to such a desktop only
// once its link confirmed that card (LinkRang). Active mode only: a passive connector has no way to send a confirmation
// back in time, so it is no desktop yet.

// presence is what a computer's Persona Link said last: active until until (this manager's clock), its notifications
// showing or not.
type presence struct {
	since, until time.Time // since: when this run of active presence began
	notifies     bool
}

// keepPresenceLocked takes what the Persona Link said at now (nil or inactive: nothing). m.mu is held.
func (m *Machine) keepPresenceLocked(p *link.PresenceInfo, now time.Time) {
	if p == nil || !p.Active {
		m.presence = nil
		return
	}
	ttl := link.PresenceTTL
	if p.TTLMS > 0 {
		ttl = time.Duration(min(p.TTLMS, devicewire.DesktopTTLMax.Milliseconds())) * time.Millisecond
	}
	since := now
	if m.presence != nil && m.presence.until.After(now) {
		since = m.presence.since
	}
	m.presence = &presence{since: since, until: now.Add(ttl), notifies: p.Notifies}
}

// takePresence is a presence message of the machine's live link l. Every one is reported, the same as the last
// included: the hub needs each renewal.
func (m *Machine) takePresence(l *mgrLink, msg link.Message) {
	m.mu.Lock()
	if m.link != l {
		m.mu.Unlock()
		return // a link being replaced
	}
	m.keepPresenceLocked(&link.PresenceInfo{Active: msg.Active, Notifies: msg.Notifies, TTLMS: msg.TTLMS}, time.Now())
	m.mu.Unlock()
	m.d.requestDesktops(false)
}

// summaryHeld is what a link's Persona Link holds of the summary and what its app did with it: the Rev it took last
// (sent) and its cards (listed), the cards of the one on its way (sending), whether a delivery failed since, and the
// cards the app confirmed it handed to Notification Center (rang, only ones of the summary it holds or the one on its
// way, so a few times link.AttentionMaxItems at most).
type summaryHeld struct {
	sent    string
	listed  map[string]bool
	sending map[string]bool
	failed  bool
	rang    []string
}

// cardsOf is the cards a lists (not the ones in More).
func cardsOf(a *link.Attention) map[string]bool {
	out := map[string]bool{}
	for _, it := range a.Items {
		if it.InteractionID != "" {
			out[it.InteractionID] = true
		}
	}
	return out
}

// begin: a is on its way. The app may confirm one of its cards before the send returns (the link reads on its own).
func (h *summaryHeld) begin(a *link.Attention) { h.sending = cardsOf(a) }

// done: the delivery of a ended, taken or not. A summary taken drops the confirmations of cards it no longer lists.
func (h *summaryHeld) done(a *link.Attention, ok bool) {
	h.sending = nil
	if !ok {
		h.failed = true
		return
	}
	h.sent, h.failed = a.Rev, false
	h.listed = cardsOf(a)
	h.rang = slices.DeleteFunc(h.rang, func(card string) bool { return !h.listed[card] })
}

// confirm: the app handed card's notification to Notification Center. Only a card of the summary it holds, or of the
// one on its way, counts.
func (h *summaryHeld) confirm(card string) {
	if !h.listed[card] && !h.sending[card] || slices.Contains(h.rang, card) {
		return
	}
	h.rang = append(h.rang, card)
}

// holds is whether it keeps up with the summary: it took one, and no delivery failed since. A newer one is then on its
// way or about to be (every change goes out at once): counting it as held keeps a desktop from dropping out and back in
// at each change. A failure ends it until a delivery is taken again. Which cards a desktop stands in for is the app's
// confirmations' (LinkRang).
func (h *summaryHeld) holds() bool { return h.sent != "" && !h.failed }

// takeRang is a rang message of link l: its app handed card's notification to Notification Center. It is kept on l,
// so it counts only while l is the machine's link (LinkRang): a new link starts with none. It changes no computer's
// being a desktop, so only LinkRang hears of it.
func (m *Machine) takeRang(l *mgrLink, card string) {
	l.heldMu.Lock()
	l.held.confirm(card)
	l.heldMu.Unlock()
	m.d.wakeRang()
}

// desktopNow is a computer that is a desktop now: what the hub is told, and its deadline on this manager's clock.
type desktopNow struct {
	devicewire.Desktop
	until time.Time
}

// desktop is the machine as a desktop at now, and its live link: nil unless its link's Persona Link said it is active
// within the TTL, takes the summary (its hello listed attention) and keeps up with it (it took one: there is one), and
// the computer is known to get the summary as it is. While the hub cannot say which computers wait to be confirmed the
// summary freezes: no new card reaches the computer, so it does not count then (the phone keeps them).
func (m *Machine) desktop(now time.Time) (*desktopNow, *mgrLink) {
	eligible, _ := m.d.summaryEligible(m.cfg.ID) // false while unknown
	if !eligible {
		return nil, nil
	}
	m.mu.Lock()
	l, p := m.link, m.presence // p is never changed in place: keepPresenceLocked makes a new one
	m.mu.Unlock()
	if l == nil || !l.attention || p == nil || !p.until.After(now) {
		return nil, nil
	}
	l.heldMu.Lock()
	held := l.held.holds()
	l.heldMu.Unlock()
	if !held {
		return nil, nil
	}
	// An entry another one absorbed: the hub knows its computer by that one's id.
	return &desktopNow{Desktop: devicewire.Desktop{MachineID: m.d.Canonical(m.cfg.ID), Since: p.since, Notifies: p.notifies,
		TTLMS: max(1, p.until.Sub(now).Milliseconds())}, until: p.until}, l
}

// desktopsReporter is the state of the one task that sends devices.desktops (reportDesktopsLoop): requests asked so
// far and dealt with (done, reported closed at each), whether the next must go out unchanged (force), and the last sent
// (sent, the reporter's alone).
type desktopsReporter struct {
	start    sync.Once
	due      chan struct{}
	asked    atomic.Uint64
	force    atomic.Bool
	mu       sync.Mutex
	done     uint64
	reported chan struct{}
	sent     string
}

// runCtx is the context the devices run in (Run's; a test's), background before there is one.
func (d *Devices) runCtx() context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx == nil {
		return context.Background()
	}
	return d.ctx
}

// requestDesktops asks the desktops reporter for a devices.desktops with the computers as they are once it gets to it,
// and returns the request's number (desktopsReported waits for it). force: also when they did not change (a
// handshake: the hub may not have them).
func (d *Devices) requestDesktops(force bool) uint64 {
	r := &d.desktopsRep
	if force {
		r.force.Store(true)
	}
	n := r.asked.Add(1)
	r.start.Do(func() {
		r.due = make(chan struct{}, 1)
		r.mu.Lock()
		r.reported = make(chan struct{})
		r.mu.Unlock()
		go d.reportDesktopsLoop(d.runCtx())
	})
	select {
	case r.due <- struct{}{}:
	default: // one due already covers this one
	}
	return n
}

// reportDesktopsLoop is the desktops reporter: one devices.desktops at a time, in order, each with the computers as they
// are then, a run of requests coalesced into one. It holds no lock while the hub takes it. A report says the TTL left
// (the hub's own clock counts it down) and goes out when the computers, a since or a deadline changed, or a request
// forced it. On a new connection to the hub one may go out before the connection's full devices.state: the hub drops
// the entry of a computer it does not yet have as this manager's, and the full report that follows that state (report)
// brings it back.
func (d *Devices) reportDesktopsLoop(ctx context.Context) {
	r := &d.desktopsRep
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.due:
		}
		asked, force := r.asked.Load(), r.force.Swap(false)
		if !d.stopping.Load() {
			now := time.Now()
			list := devicewire.Desktops{Desktops: []devicewire.Desktop{}}
			key := "" // the list but for what time alone changes (the TTL left)
			for _, m := range d.cfgMachines() {
				if p, _ := m.desktop(now); p != nil {
					list.Desktops = append(list.Desktops, p.Desktop)
					key += p.MachineID + " " + strconv.FormatInt(p.Since.UnixNano(), 10) + " " +
						strconv.FormatInt(p.until.UnixNano(), 10) + " " + strconv.FormatBool(p.Notifies) + "\n"
				}
			}
			if force || key != r.sent {
				b, _ := json.Marshal(list)
				if d.opts.Hub.Notify(devicewire.EventDesktops, json.RawMessage(b)) == nil {
					r.sent = key
				}
			}
		}
		r.mu.Lock()
		if asked > r.done {
			r.done = asked
			close(r.reported)
			r.reported = make(chan struct{})
		}
		r.mu.Unlock()
	}
}

// desktopsReported waits until the reporter has dealt with request n, or ctx ends (false).
func (d *Devices) desktopsReported(ctx context.Context, n uint64) bool {
	r := &d.desktopsRep
	for {
		r.mu.Lock()
		done, wake := r.done >= n, r.reported
		r.mu.Unlock()
		if done {
			return true
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}

// wakeRang wakes LinkRang: a delivery ended, or an app confirmed a card.
func (d *Devices) wakeRang() {
	d.ringMu.Lock()
	if d.ring != nil {
		close(d.ring)
	}
	d.ring = make(chan struct{})
	d.ringMu.Unlock()
}

// delivered is told when a link took a summary or a delivery failed: it wakes LinkRang, and asks for the desktops when
// which ones they are may have changed: only a computer whose Persona Link said it is active can be one.
func (d *Devices) delivered() {
	d.wakeRang()
	if slices.ContainsFunc(d.cfgMachines(), func(m *Machine) bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.presence != nil
	}) {
		d.requestDesktops(false)
	}
}

// deliveries is closed at the next delivery or confirmation (wakeRang).
func (d *Devices) deliveries() <-chan struct{} {
	d.ringMu.Lock()
	defer d.ringMu.Unlock()
	if d.ring == nil {
		d.ring = make(chan struct{})
	}
	return d.ring
}

// rangOn is the live link of one of the manager's Persona Link desktops with notifications showing that confirmed card,
// nil without one, and whether there is such a desktop at all. only, when not nil: that link's desktop alone counts.
func (d *Devices) rangOn(card string, now time.Time, only *mgrLink) (on *mgrLink, desktops bool) {
	for _, m := range d.cfgMachines() {
		p, l := m.desktop(now)
		if p == nil || !p.Notifies || only != nil && l != only {
			continue
		}
		desktops = true
		l.heldMu.Lock()
		rang := slices.Contains(l.held.rang, card)
		l.heldMu.Unlock()
		if rang {
			return l, true
		}
	}
	return nil, desktops
}

// LinkRang waits, all within one deadline (within) and as long as ctx lasts, for one of the manager's Persona Link
// desktops with notifications showing to have confirmed card (its interaction id) on its live link, then for the
// desktops reporter to have dealt with the desktops as they are, and then checks that the same link still is such a
// desktop with that confirmation: a link replaced or stopping meanwhile does not count, and the wait goes on for a
// confirmation that does. False at once while there is no such desktop (one that stops counting while it waits does
// not end the wait), and when the time is up or ctx ends: the push then goes to the phone
// (assistantwire.Notification.LinkCovered). A confirmation that came on an earlier link, or on a link whose computer is
// no desktop now, does not count. The app's confirmation is what lets the hub mute the phone at all; the hub then
// mutes only for a Persona Link desktop it has a valid record of, and pushes the phone when it has none (a report it
// did not take included).
func (d *Devices) LinkRang(ctx context.Context, card string, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for first := true; ; first = false {
		wake := d.deliveries()
		l, desktops := d.rangOn(card, time.Now(), nil)
		if l != nil {
			if !d.desktopsReported(ctx, d.requestDesktops(false)) {
				return false
			}
			if again, _ := d.rangOn(card, time.Now(), l); again != nil {
				return true
			}
			continue
		}
		if !desktops && first {
			return false
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}
