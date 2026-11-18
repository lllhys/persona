package assistant

import (
	"context"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The inbox on the bus: the hub's calls about it (BusHub.request hands the ones it does not know here) and what the
// inbox tells the hub.

// inboxRequest answers the hub's inbox calls; ok false for a method that is not one of them.
func inboxRequest(ctx context.Context, s *Service, method string, decode func(any) error) (any, bool, error) {
	switch method {
	case assistantwire.MethodInboxCollected:
		var p assistantwire.InboxRound
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		if err := s.InboxCollected(ctx, p); err != nil {
			return nil, true, err
		}
		return map[string]any{}, true, nil
	case assistantwire.MethodInboxItem:
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		raw, ok := s.InboxItemJSON(ctx, p.ID)
		return assistantwire.InboxItemResult{Item: raw, OK: ok}, true, nil
	case assistantwire.MethodInboxForget:
		var p struct {
			Source string `json:"source"`
		}
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		n, err := s.InboxForget(ctx, p.Source)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"deleted": n}, true, nil
	case assistantwire.MethodInboxRepoMutes:
		var p assistantwire.InboxRepoMutes
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		repos, err := s.InboxRepoMutes(ctx, p)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"repos": repos}, true, nil
	case assistantwire.MethodInboxMutes:
		var p struct {
			Source string `json:"source"`
		}
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		mutes, err := s.InboxSourceMutes(ctx, p.Source)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"mutes": mutes}, true, nil
	case assistantwire.MethodInboxPost:
		var p struct {
			On *bool `json:"on"`
		}
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		if p.On != nil {
			if _, err := s.updateSettings(ctx, func(v *Settings) error { v.InboxPost = p.On; return nil }); err != nil {
				return nil, true, err
			}
		}
		return map[string]any{"on": s.inboxPostOn(ctx)}, true, nil
	case assistantwire.MethodInboxTriagePreview:
		var p assistantwire.InboxTriagePreview
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		return map[string]any{"verdicts": s.InboxTriagePreview(ctx, p)}, true, nil
	case assistantwire.MethodInboxUnmute:
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(&p); err != nil {
			return nil, true, err
		}
		if err := s.InboxUnmute(ctx, p.ID); err != nil {
			return nil, true, err
		}
		return map[string]any{}, true, nil
	}
	return nil, false, nil
}

// InboxChanged tells the hub inbox items changed (inbox.changed, one way).
func (b *BusHub) InboxChanged(_ context.Context, c assistantwire.InboxChanged) {
	if err := b.notifyHub(assistantwire.EventInboxChanged, c); err != nil {
		b.opts.Logf("assistant: %s: %v", assistantwire.EventInboxChanged, err)
	}
}

// sendFromCard is "Send" on a message card (POST /api/v1/inbox/{id}/reply with card): the reply goes out once per
// card however many taps, devices or retries ask — a card already sent answers with the item as it is (already).
func (s *Service) sendFromCard(ctx context.Context, cardID, itemID string, send func() (InboxItem, error)) (it InboxItem, already bool, err error) {
	b := &s.inbox
	b.cardSends.acquire(cardID)
	defer b.cardSends.release(cardID)
	sent, err := s.cardSent(ctx, cardID, itemID)
	if err != nil {
		return InboxItem{}, false, err
	}
	if sent {
		b.mu.Lock()
		if ib := b.items[itemID]; ib != nil {
			it = ib.listed()
		}
		b.mu.Unlock()
		return it, true, nil
	}
	// A card put in before its item was filed ("Done", "Ignore", read elsewhere, archived) is not a way to reply to it:
	// the reply would go out unasked and the item's mark would become "Replied". Moved back to the inbox, it sends.
	b.mu.Lock()
	ib := b.items[itemID]
	filed := ib != nil && !ib.open()
	b.mu.Unlock()
	if filed {
		return InboxItem{}, false, errInboxItemDone
	}
	it, err = send()
	return it, false, err
}

// cardLocks are the locks of the message cards whose "Send" is going on or waited for (sendFromCard). An entry lives
// while someone holds or waits on its lock, so the assistant keeps none for the cards it is done with. The count makes
// letting go safe: a tap waiting on a card's lock keeps that lock in the table, and a tap that comes later shares it
// rather than making a second lock that would let both sends run at once.
type cardLocks struct {
	mu sync.Mutex
	m  map[string]*cardLock
}

// cardLock is one card's lock and how many hold or wait on it (under cardLocks.mu).
type cardLock struct {
	mu sync.Mutex
	n  int
}

// acquire takes card id's lock, waiting for whoever holds it.
func (c *cardLocks) acquire(id string) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*cardLock{}
	}
	l := c.m[id]
	if l == nil {
		l = &cardLock{}
		c.m[id] = l
	}
	l.n++
	c.mu.Unlock()
	l.mu.Lock()
}

// release lets card id's lock go (taken with acquire), forgetting it when nobody else holds or waits on it.
func (c *cardLocks) release(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := c.m[id]
	l.mu.Unlock()
	if l.n--; l.n == 0 {
		delete(c.m, id)
	}
}
