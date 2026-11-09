package hub

import (
	"encoding/json"
	"sort"
	"time"
)

// Desktop apps (the Mac app) on the hub, like WeChat's "WeChat for Mac is logged in": a desktop registers as one (register_desktop)
// instead of as the phone, so the phone keeps the phone_* tools and its wake pushes; every client hears which
// desktops are connected (the desktops event, docs/protocol/protocol.md), and a phone that turned on "Phone Muted"
// (NotifyPrefs.MuteWhileDesktop) gets no alert pushes while one is.

// desktopView is one connected desktop as the apps see it.
type desktopView struct {
	// Device is its device token's id (what "Sign out" revokes); empty when it connected with the pairing token.
	Device string    `json:"device"`
	Name   string    `json:"name"`
	Since  time.Time `json:"since"`
}

// desktops lists the connected desktops, one per device (its newest connection: a Mac reconnecting keeps the old
// socket for a while), earliest first.
func (h *Hub) desktops() []desktopView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.desktopsLocked()
}

func (h *Hub) desktopsLocked() []desktopView {
	byDevice := map[string]desktopView{}
	var loose []desktopView // the pairing token: no device to tell them apart
	for c := range h.clients {
		if !c.desktop {
			continue
		}
		v := desktopView{Device: c.device, Name: c.name, Since: c.since}
		if c.device == "" {
			loose = append(loose, v)
		} else if cur, ok := byDevice[c.device]; !ok || v.Since.After(cur.Since) {
			byDevice[c.device] = v
		}
	}
	out := loose
	for _, v := range byDevice {
		out = append(out, v)
	}
	if out == nil {
		out = []desktopView{}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].Device < out[j].Device
	})
	return out
}

// desktopsMessageLocked is the desktops event as it is now. Callers hold h.mu and queue it before letting go, so
// every client gets the lists in the order they changed (computed and queued apart, an older list could land last).
func (h *Hub) desktopsMessageLocked() []byte {
	b, _ := json.Marshal(map[string]any{"type": "desktops", "desktops": h.desktopsLocked()})
	return b
}

// sendDesktops queues the current list to one client (it has just connected).
func (h *Hub) sendDesktops(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue(c, h.desktopsMessageLocked())
}

// announceDesktopsLocked queues the current list to every client; h.mu is held.
func (h *Hub) announceDesktopsLocked() {
	b := h.desktopsMessageLocked()
	for c := range h.clients {
		queue(c, b)
	}
}

// queue drops the message for a client too slow to take it, like broadcast's events: it gets the list again with the
// next change or when it reconnects.
func queue(c *client, b []byte) {
	select {
	case c.out <- b:
	default:
	}
}

// desktopOnline: some desktop app is connected ("Phone Muted" holds the phone's pushes back).
func (h *Hub) desktopOnline() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.desktop {
			return true
		}
	}
	return false
}

// desktopOpen: the desktop app with this push id is connected. It posts its own notifications for agents that turn
// needs-you or finish (the Mac's MacAttentionWatcher, on the same Settings › Notifications), so those pushes would show twice.
func (h *Hub) desktopOpen(device string) bool {
	if device == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.desktop && c.pushDevice == device {
			return true
		}
	}
	return false
}

// registerDesktop marks c as a desktop app and tells every client. Not for an answer-only device (a watch): it would
// mute the phone. pushDevice is its id for pushes (PushRegistrar.deviceID; empty from an older app): while it is
// connected the hub leaves the agents' alerts to it (desktopOpen).
func (h *Hub) registerDesktop(c *client, name, pushDevice string) {
	if c.role != roleFull {
		return
	}
	h.mu.Lock()
	c.desktop, c.name, c.since, c.pushDevice = true, name, time.Now(), pushDevice
	h.announceDesktopsLocked()
	h.mu.Unlock()
	go h.fleetClientsChanged() // Phone Muted holds back a phone's MCP form pushes now (fleet_link.go)
	h.logf("desktop %q connected (device %s)", name, c.device)
}

// dropClient forgets a client whose connection ended (also as one the shutdown has to close); a desktop's going is
// announced.
func (h *Hub) dropClient(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	delete(h.wsConnections, c)
	desktop := c.desktop
	if desktop {
		h.announceDesktopsLocked()
	}
	phone, device := c.phone, c.pushDevice
	h.mu.Unlock()
	h.fsWatchRelease(c) // the folders its panels had open are no longer watched (fswatch.go)
	go h.fleetClientsChanged()
	if desktop {
		h.logf("desktop %q disconnected (device %s)", c.name, c.device)
	}
	if phone && !h.phoneOpen(device) {
		h.live.phoneLeft(device, c.clean)
	}
}

// phoneOpen: the app on the phone with this push id is open and connected. An app closes its connection when it goes
// to the background, so it is open while connected (a suspended one with the socket left open is dropped by the
// pings within ~35 s).
func (h *Hub) phoneOpen(device string) bool {
	if device == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.phone && c.pushDevice == device {
			return true
		}
	}
	return false
}

// alertWanted is which devices an alert push goes to: what their settings pick (wants; nil: every device, for what
// was asked for explicitly), less those muted while a desktop is online.
func alertWanted(wants func(NotifyPrefs) bool, desktopOnline bool) func(NotifyPrefs) bool {
	return func(p NotifyPrefs) bool {
		if wants == nil {
			return true
		}
		return wants(p) && !(desktopOnline && p.MuteWhileDesktop)
	}
}
