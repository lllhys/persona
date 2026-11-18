package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/sessionlist"
)

// A plugin's list call that ran past its call timeout, as the extension host reports it.
var pluginTimeout = &rpc.Error{Code: "timeout", Message: "maso: agentsrc.sessions timed out after 30s"}

// A source that timed out answers the list reads after it with that failure at once, instead of each waiting out its
// timeout again; the source that answers is listed as before.
func TestATimedOutSourceIsNotAskedAgainByEveryRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plugin call timeout", pluginTimeout},
		{"deadline", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			good := &memSource{name: "first", sessions: []AgentSession{{SessionKey: "a"}}}
			hung := &memSource{name: "second", sessions: []AgentSession{{SessionKey: "b"}}, fail: tc.err}
			s := NewService(nil, nil, nil, good, hung)
			ctx := context.Background()
			for i := 0; i < 4; i++ {
				page, err := s.ListPageWithCounts(ctx, sessionlist.Options{})
				if err != nil || page.Page.Total != 1 {
					t.Fatalf("read %d: %+v %v, want the answering source's row", i, page.Page, err)
				}
				s.cache.invalidate("first", nil) // the answering source's list is asked again each time
			}
			if hung.listN != 1 {
				t.Fatalf("the timed-out source was asked %d times, want once", hung.listN)
			}
			if good.listN != 4 {
				t.Fatalf("the answering source was asked %d times, want 4", good.listN)
			}
			if _, err := s.Sessions(ctx, "second", ""); !errors.Is(err, tc.err) {
				t.Fatalf("asked by name: %v, want its timeout", err)
			}
			if hung.listN != 1 {
				t.Fatalf("asked by name, the source was asked again (%d)", hung.listN)
			}
		})
	}
}

// Any other failure comes back at once, so each read asks again and finds a recovered source at once.
func TestAQuickFailureIsAskedAgain(t *testing.T) {
	src := &memSource{name: "second", fail: errors.New("not configured")}
	s := NewService(nil, nil, nil, src)
	for i := 0; i < 3; i++ {
		if _, err := s.Sessions(context.Background(), "second", ""); err == nil {
			t.Fatal("the failure is answered")
		}
	}
	if src.listN != 3 {
		t.Fatalf("asked %d times, want 3", src.listN)
	}
}

// The timeout kept ends when the poll finds the source answering, when the source reports a change, when it is
// added again, or after timeoutMemo.
func TestAKeptTimeoutEnds(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(s *Service, src *memSource)
	}{
		{"the poll's answer", func(s *Service, src *memSource) { s.PollOnce(context.Background()) }},
		{"a change the source reports", func(s *Service, src *memSource) { s.Updated(src.name, nil) }},
		{"the source added again", func(s *Service, src *memSource) { s.Remove(src.name); s.Add(src) }},
		{"its time", func(s *Service, src *memSource) {
			s.cache.mu.Lock()
			e := s.cache.timeouts[src.name]
			e.at = e.at.Add(-timeoutMemo)
			s.cache.timeouts[src.name] = e
			s.cache.mu.Unlock()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "s1"}}, fail: pluginTimeout}
			s := NewService(&memKV{m: map[string][]byte{}}, nil, nil, src)
			ctx := context.Background()
			if _, err := s.Sessions(ctx, "grokbot", ""); err == nil {
				t.Fatal("the timeout is answered")
			}
			if _, err := s.Sessions(ctx, "grokbot", ""); !errors.Is(err, pluginTimeout) || src.listN != 1 {
				t.Fatalf("kept: %v after %d asks", err, src.listN)
			}
			src.fail = nil
			tc.end(s, src)
			list, err := s.Sessions(ctx, "grokbot", "")
			if err != nil || len(list) != 1 {
				t.Fatalf("after %s: %v %v, want the source's list", tc.name, list, err)
			}
		})
	}
}

// A timeout the poll ran into is kept too: the page's next read does not wait it out again.
func TestThePollsTimeoutIsKept(t *testing.T) {
	src := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "s1"}}, fail: pluginTimeout}
	s := NewService(&memKV{m: map[string][]byte{}}, nil, nil, src)
	s.PollOnce(context.Background())
	if _, err := s.Sessions(context.Background(), "grokbot", ""); !errors.Is(err, pluginTimeout) || src.listN != 1 {
		t.Fatalf("after the poll's timeout: %v, %d asks, want its timeout and no new ask", err, src.listN)
	}
}

// The asker's own deadline is not the source's: the next read asks again.
func TestTheAskersDeadlineIsNotKept(t *testing.T) {
	src := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "s1"}}}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	src.fail = ctx.Err()
	s := NewService(&memKV{m: map[string][]byte{}}, nil, nil, src)
	if _, err := s.Sessions(ctx, "grokbot", ""); err == nil {
		t.Fatal("the expired asker got an answer")
	}
	src.fail = nil
	if list, err := s.Sessions(context.Background(), "grokbot", ""); err != nil || len(list) != 1 || src.listN != 2 {
		t.Fatalf("next read: %v %v after %d asks, want a new ask", list, err, src.listN)
	}
}
