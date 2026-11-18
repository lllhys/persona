package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Hold the real approval response until the request is waiting. Delivering that
// response must wake the fixture immediately, without advancing its fake clock.
func TestFakeCodexRequestWakesOnItsReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := codexTestNode(t)
		fh := withFakeHub(t, n)
		p, c, f := newFakeCodex(t, n, t.TempDir())
		p.id, c.threadID = testThread, testThread
		n.noteModes(testThread, "default", "")
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		releaseAnswer := func() { once.Do(func() { close(release) }) }
		t.Cleanup(releaseAnswer)
		fh.answer = func(proto.Interaction) proto.Resolution {
			close(entered)
			<-release
			yes := true
			return proto.Resolution{Allow: &yes}
		}
		done := make(chan map[string]any, 1)
		began := time.Now()
		go func() {
			done <- f.request(1, "item/fileChange/requestApproval", map[string]any{
				"threadId": testThread, "itemId": "fixture-approval", "grantRoot": p.cwd,
			})
		}()
		<-entered
		synctest.Wait()
		releaseAnswer()
		answer := <-done
		if answer["decision"] != "accept" || len(fh.interactions()) != 1 {
			t.Fatalf("real approval was not answered: %v", answer)
		}
		if elapsed := time.Since(began); elapsed != 0 {
			t.Fatalf("an available reply waited for a polling timer: %v", elapsed)
		}
	})
}

// Keep an approval outstanding over the real node/hub pipes so cancellation and
// unrelated responses are tested while a request is actually being processed.
func heldCodexApproval(t *testing.T) (*fakeAppServer, *codexConn, *assistantProc, <-chan struct{}, func()) {
	t.Helper()
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	p, c, f := newFakeCodex(t, n, t.TempDir())
	p.id, c.threadID = testThread, testThread
	n.noteModes(testThread, "default", "")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	fh.answer = func(proto.Interaction) proto.Resolution {
		close(entered)
		<-release
		yes := true
		return proto.Resolution{Allow: &yes}
	}
	return f, c, p, entered, finish
}

func fixtureApprovalParams(p *assistantProc) map[string]any {
	return map[string]any{"threadId": testThread, "itemId": "held-approval", "grantRoot": p.cwd}
}

func requireNoFixtureWaiters(t *testing.T, f *fakeAppServer) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.waiting) != 0 {
		t.Fatalf("completed request left %d waiters", len(f.waiting))
	}
}

func TestFakeCodexReplyMatchesItsRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, c, p, entered, release := heldCodexApproval(t)
		done := make(chan error, 1)
		go func() {
			r, err := f.requestReply(t.Context(), 1, "item/fileChange/requestApproval", fixtureApprovalParams(p))
			if err == nil && (string(r.ID) != "1" || !strings.Contains(string(r.Result), "accept")) {
				err = errors.New("approval response was replaced by an unrelated reply")
			}
			done <- err
		}()
		<-entered
		c.reply(json.RawMessage("2"), map[string]any{"decision": "decline"})
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("unrelated response completed approval: %v", err)
		default:
		}
		if _, err := f.requestReply(t.Context(), 1, "duplicate", nil); err == nil {
			t.Fatal("duplicate outstanding request ID was accepted")
		}
		release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		requireNoFixtureWaiters(t, f)
		// A duplicate/late response must not block the reader or poison a later request.
		c.reply(json.RawMessage("1"), map[string]any{"decision": "decline"})
		r, err := f.requestReply(t.Context(), 3, "fixture/unsupported", nil)
		if err != nil || r.Error == nil || r.Error.Code != -32601 || !strings.Contains(r.Error.Message, "fixture/unsupported") {
			t.Fatalf("JSON-RPC error was lost: reply=%+v error=%v", r, err)
		}
		requireNoFixtureWaiters(t, f)
	})
}

func TestFakeCodexRequestCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f, _, p, entered, release := heldCodexApproval(t)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				done := make(chan error, 1)
				began := time.Now()
				go func() {
					_, err := f.requestReply(ctx, 1, "item/fileChange/requestApproval", fixtureApprovalParams(p))
					done <- err
				}()
				<-entered
				synctest.Wait()
				want, elapsed := context.Canceled, time.Duration(0)
				if deadline {
					want, elapsed = context.DeadlineExceeded, 5*time.Second
				} else {
					cancel()
				}
				if err := <-done; !errors.Is(err, want) || time.Since(began) != elapsed {
					t.Fatalf("wait ended with %v after %v; want %v after %v", err, time.Since(began), want, elapsed)
				}
				requireNoFixtureWaiters(t, f)
				release()
				synctest.Wait()
				r, err := f.requestReply(t.Context(), 2, "fixture/unsupported", nil)
				if err != nil || r.Error == nil {
					t.Fatalf("late approval blocked the next reply: %+v, %v", r, err)
				}
				requireNoFixtureWaiters(t, f)
			})
		})
	}
}

func TestFakeCodexRequestTransportFailures(t *testing.T) {
	t.Run("EOF while approval is pending", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f, _, p, entered, release := heldCodexApproval(t)
			done := make(chan error, 1)
			go func() {
				_, err := f.requestReply(t.Context(), 1, "item/fileChange/requestApproval", fixtureApprovalParams(p))
				done <- err
			}()
			<-entered
			_ = p.stdin.Close()
			if err := <-done; !errors.Is(err, io.EOF) {
				t.Fatalf("closed response pipe: %v", err)
			}
			requireNoFixtureWaiters(t, f)
			release()
		})
	})
	t.Run("closed request pipe", func(t *testing.T) {
		n := codexTestNode(t)
		_, _, f := newFakeCodex(t, n, t.TempDir())
		_ = f.out.(*io.PipeWriter).Close()
		if _, err := f.requestReply(t.Context(), 1, "fixture/unsupported", nil); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("write failure: %v", err)
		}
		requireNoFixtureWaiters(t, f)
	})
	t.Run("already cancelled", func(t *testing.T) {
		n := codexTestNode(t)
		_, _, f := newFakeCodex(t, n, t.TempDir())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := f.requestReply(ctx, 1, "fixture/unsupported", nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request: %v", err)
		}
		requireNoFixtureWaiters(t, f)
	})
}
