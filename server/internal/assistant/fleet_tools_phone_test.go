package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// flakyPhoneHub fails the phone's tool list while fails is not 0 (counting down; below 0: always), then lists tools.
type flakyPhoneHub struct {
	*fakeHub
	mu    sync.Mutex
	fails int
	asked int
	tools []proto.Tool
}

func (h *flakyPhoneHub) AppCall(ctx context.Context, p AppCall) (json.RawMessage, error) {
	if p.AppID != assistantwire.AppPhone || p.Method != assistantwire.PhoneTools {
		return h.fakeHub.AppCall(ctx, p)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked++
	if h.fails != 0 {
		if h.fails > 0 {
			h.fails--
		}
		return nil, errors.New("the hub's link is down")
	}
	return json.Marshal(map[string]any{"tools": h.tools})
}

func (h *flakyPhoneHub) set(fails int, tools ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fails, h.asked, h.tools = fails, 0, []proto.Tool{}
	for _, name := range tools {
		h.tools = append(h.tools, proto.Tool{Name: name})
	}
}

func (h *flakyPhoneHub) askedTimes() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asked
}

func flakyPhoneService(t *testing.T) (*Service, *flakyPhoneHub) {
	t.Helper()
	retry := phoneToolRetry
	phoneToolRetry = time.Millisecond
	t.Cleanup(func() { phoneToolRetry = retry })
	h := &flakyPhoneHub{fakeHub: newFakeHub()}
	return New(Options{Hub: h, Logf: t.Logf}), h
}

func phoneToolNames(tools []proto.Tool) []string {
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// A conversation that starts while the hub's link comes back still gets the phone's tools (#6422).
func TestPhoneToolsAskTheHubAgain(t *testing.T) {
	s, h := flakyPhoneService(t)
	h.set(2, "phone_reminders_create")
	if got := phoneToolNames(s.phoneTools(context.Background())); !slices.Equal(got, []string{"phone_reminders_create"}) {
		t.Fatalf("tools = %v", got)
	}
	if n := h.askedTimes(); n != 3 {
		t.Fatalf("asked %d times, want 3", n)
	}
	// The conversation's list itself carries them.
	h.set(1, "phone_reminders_create")
	if got := phoneToolNames(s.mcpTools(context.Background(), false)); !slices.Contains(got, "phone_reminders_create") {
		t.Fatalf("mcpTools = %v", got)
	}
}

// A hub that keeps failing: the list it last gave, not none for the conversation's whole life.
func TestPhoneToolsTakeTheHubsLastList(t *testing.T) {
	s, h := flakyPhoneService(t)
	h.set(0, "phone_reminders_create", "phone_location_current")
	s.phoneTools(context.Background())
	h.set(-1)
	if got := phoneToolNames(s.phoneTools(context.Background())); !slices.Equal(got, []string{"phone_reminders_create", "phone_location_current"}) {
		t.Fatalf("tools = %v", got)
	}
	if n := h.askedTimes(); n != phoneToolTries {
		t.Fatalf("asked %d times, want %d", n, phoneToolTries)
	}
}

// What the hub answers is what the phone offers now: an empty list (every source turned off) replaces the kept one.
func TestPhoneToolsAnEmptyAnswerIsKeptToo(t *testing.T) {
	s, h := flakyPhoneService(t)
	h.set(0, "phone_reminders_create")
	s.phoneTools(context.Background())
	h.set(0)
	if got := s.phoneTools(context.Background()); len(got) != 0 {
		t.Fatalf("tools = %v, want none", phoneToolNames(got))
	}
	h.set(-1)
	if got := s.phoneTools(context.Background()); len(got) != 0 {
		t.Fatalf("after a failure: %v, want the empty list kept", phoneToolNames(got))
	}
}

// A manager the hub never answered has nothing to fall back on; a request that stopped waiting is not retried.
func TestPhoneToolsNoneBeforeTheHubAnswered(t *testing.T) {
	s, h := flakyPhoneService(t)
	h.set(-1)
	if got := s.phoneTools(context.Background()); got != nil {
		t.Fatalf("tools = %v, want none", phoneToolNames(got))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.set(-1)
	s.phoneTools(ctx)
	if n := h.askedTimes(); n != 1 {
		t.Fatalf("a cancelled request asked %d times, want 1", n)
	}
}
