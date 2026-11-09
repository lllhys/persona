package hub

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// What a device has open through the hub: requests to the manager (relayToManager), port forwards (including the
// live screen) and previews are kept under the id of the credential that made them. Revoking that credential ends
// them at once instead of when the phone hangs up (docs/protocol/protocol.md "Authentication"). Closing the hub's
// side also closes the stream to the manager or node. An extension's token is a credential too: its manager tells the
// hub when it drops one (dropRevokedToken), and what its tokens relay ends when it leaves the bus, as one it dropped
// meanwhile could not be told (dropLeftManager).

// relayDroppedMost bounds what relayedCalls remembers of one manager's dropped tokens: past it the oldest is
// forgotten first.
const relayDroppedMost = 1 << 10

// relayedCalls are the requests being relayed now, by credential id.
type relayedCalls struct {
	mu   sync.Mutex
	byID map[string]map[*context.CancelFunc]struct{}
	// dropped is, by manager, when it dropped each of its extension tokens; left, when each manager last left the bus.
	// A call of one let in before then ends as it is kept. Each is kept tokenRevokedFor: a call let in longer ago ends
	// anyway.
	dropped map[string]*droppedTokens
	left    map[string]time.Time
}

// droppedTokens are when one manager dropped each of its extension tokens, by hash: oldest first in order, at most
// relayDroppedMost.
type droppedTokens struct {
	at    map[string]time.Time
	order []string
}

// trackRelayed keeps r, about to be relayed, under its caller's credential so that revoking it ends the call
// (dropRelayed): relay the request it returns, then call done. The master token's and an admin session's are not kept:
// neither is revoked. A pairing code's call also ends when the code does (its expiry, or the redemption grace once a
// device redeemed it, which revoking that device ends), as the code's /api/v1/ws does (pairContext).
func (h *Hub) trackRelayed(r *http.Request) (_ *http.Request, done func()) {
	p := principalOf(r.Context())
	id := relayID(p)
	if id == "" {
		return r, func() {}
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if p.PairHash != "" {
		ctx, cancel = h.pairContext(r.Context(), p)
	} else {
		ctx, cancel = context.WithCancel(r.Context())
	}
	r = r.WithContext(ctx)
	calls := &h.ext.calls
	key := &cancel
	calls.mu.Lock()
	if calls.byID == nil {
		calls.byID = map[string]map[*context.CancelFunc]struct{}{}
	}
	if calls.byID[id] == nil {
		calls.byID[id] = map[*context.CancelFunc]struct{}{}
	}
	calls.byID[id][key] = struct{}{}
	// Revoked or dropped between the auth and here: that has already ended what was kept, so this one ends by itself.
	dropped := p.Role == roleExtension && p.extHash != "" && calls.droppedSince(p.Manager, p.extHash, p.extAt)
	calls.mu.Unlock()
	if dropped || h.isDeviceRevoked(id) {
		cancel()
	}
	return r, func() {
		calls.mu.Lock()
		if m := calls.byID[id]; m != nil {
			delete(m, key)
			if len(m) == 0 {
				delete(calls.byID, id)
			}
		}
		calls.mu.Unlock()
		cancel()
	}
}

// relayID is what p's relayed calls are kept under: an extension's token by its manager and hash (the extension's
// next token is another), any other credential by its id.
func relayID(p principal) string {
	if p.Role == roleExtension && p.extHash != "" {
		return extRelayPrefix(p.Manager) + p.extHash
	}
	return p.ID
}

// extRelayPrefix begins the relayID of each of manager's extension tokens.
func extRelayPrefix(manager string) string { return "pext:" + manager + ":" }

// dropRevokedToken ends what manager's extension token of hash has relayed now (the manager dropped it), and what a
// request let in before that would start relaying. The number ended.
func (h *Hub) dropRevokedToken(manager, hash string) int {
	calls := &h.ext.calls
	calls.mu.Lock()
	calls.rememberDropped(manager, hash, time.Now())
	calls.mu.Unlock()
	return h.dropRelayed(extRelayPrefix(manager) + hash)
}

// dropLeftManager ends what manager's extension tokens have relayed now, and what a request let in before that would
// start relaying: it left the bus, and a token it dropped meanwhile could not be told (dropRevokedToken). The number
// ended.
func (h *Hub) dropLeftManager(manager string) int {
	prefix := extRelayPrefix(manager)
	calls := &h.ext.calls
	calls.mu.Lock()
	calls.rememberLeft(manager, time.Now())
	var ids []string
	for id := range calls.byID {
		if hash, ok := strings.CutPrefix(id, prefix); ok && !strings.Contains(hash, ":") { // not manager "<manager>:x"'s
			ids = append(ids, id)
		}
	}
	calls.mu.Unlock()
	n := 0
	for _, id := range ids {
		n += h.dropRelayed(id)
	}
	return n
}

// rememberDropped notes manager's token of hash as dropped at now, forgetting that manager's past tokenRevokedFor and,
// past relayDroppedMost, its oldest. c.mu held.
func (c *relayedCalls) rememberDropped(manager, hash string, now time.Time) {
	if c.dropped == nil {
		c.dropped = map[string]*droppedTokens{}
	}
	d := c.dropped[manager]
	if d == nil {
		d = &droppedTokens{at: map[string]time.Time{}}
		c.dropped[manager] = d
	}
	forget := func() {
		delete(d.at, d.order[0])
		d.order = d.order[1:]
	}
	for len(d.order) > 0 && now.Sub(d.at[d.order[0]]) >= tokenRevokedFor {
		forget()
	}
	if _, ok := d.at[hash]; ok { // told again: the first time fences as much (the manager has not let it in since)
		return
	}
	if len(d.order) >= relayDroppedMost {
		forget()
	}
	d.at[hash] = now
	d.order = append(d.order, hash)
}

// rememberLeft notes manager as having left the bus at now, forgetting each manager's leaving past tokenRevokedFor.
// Each token it dropped before is fenced by that too, so they are forgotten. c.mu held.
func (c *relayedCalls) rememberLeft(manager string, now time.Time) {
	if c.left == nil {
		c.left = map[string]time.Time{}
	}
	for m, at := range c.left { // one per manager with a bus credential
		if now.Sub(at) >= tokenRevokedFor {
			delete(c.left, m)
		}
	}
	c.left[manager] = now
	delete(c.dropped, manager)
}

// droppedSince says whether manager dropped its token of hash, or left the bus, at or after at (a call let in at at may
// have been let in before), or at is too long ago to tell. c.mu held.
func (c *relayedCalls) droppedSince(manager, hash string, at time.Time) bool {
	if time.Since(at) >= tokenRevokedFor {
		return true
	}
	if l, ok := c.left[manager]; ok && !l.Before(at) {
		return true
	}
	d := c.dropped[manager]
	if d == nil {
		return false
	}
	t, ok := d.at[hash]
	return ok && !t.Before(at)
}

// dropRelayed ends what credential id has relayed now (it was revoked): the request's context ending closes both
// sides of the relay. The number ended.
func (h *Hub) dropRelayed(id string) int {
	if id == "" {
		return 0
	}
	calls := &h.ext.calls
	calls.mu.Lock()
	var cancels []context.CancelFunc
	for k := range calls.byID[id] {
		cancels = append(cancels, *k)
	}
	delete(calls.byID, id)
	calls.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return len(cancels)
}
