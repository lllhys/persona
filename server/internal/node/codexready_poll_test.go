package node

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

func TestCodexReadyPollFindsOnlyTheMatchingRollout(t *testing.T) {
	for _, fixture := range []bool{true, false} {
		name, poll := "production", 500*time.Millisecond
		if fixture {
			name, poll = "fixture", 20*time.Millisecond
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				n := &Node{home: t.TempDir()}
				if fixture {
					n, _ = testNode(t)
				}
				h := &readinessFrames{stateless: stateless{screenHost{bareHost: bareHost{kind: host.Orca}}},
					frames: []string{codexStartError + " no matching rollout"}}
				n.hosts = host.NewRegistry(h)
				cwd := mkdir(t, filepath.Join(n.home, "project"))
				writeThread(t, n.home, testThread, cwd, "another prompt")
				writeThread(t, n.home, startThread, cwd, "the requested prompt")
				began := time.Now()
				id, err := n.waitCodexThread(context.Background(), "unused", cwd, "the requested prompt", began)
				if err != nil || id != startThread || time.Since(began) != poll || len(h.reads) != 0 {
					t.Fatalf("matching rollout: id=%q, elapsed=%v, error=%v", id, time.Since(began), err)
				}
			})
		})
	}
}

func TestCodexReadyPollReportsLauncherFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, _ := testNode(t)
		h := &readinessFrames{stateless: stateless{screenHost{bareHost: bareHost{kind: host.Orca}}},
			frames: []string{codexStartError + " scripted launcher failure"}}
		n.hosts = host.NewRegistry(h)
		began := time.Now()
		_, err := n.waitCodexThread(context.Background(), "orca:t1:l1", n.home, "prompt", began)
		if err == nil || !strings.Contains(err.Error(), "scripted launcher failure") || len(h.reads) != 1 || time.Since(began) != 20*time.Millisecond {
			t.Fatalf("launcher failure: reads=%d, elapsed=%v, error=%v", len(h.reads), time.Since(began), err)
		}
	})
}

func TestCodexReadyPollKeepsCancellationAndDeadline(t *testing.T) {
	for _, cancellation := range []bool{true, false} {
		name := "deadline"
		if cancellation {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			old := codexStartWait
			codexStartWait = 100 * time.Millisecond
			t.Cleanup(func() { codexStartWait = old })
			synctest.Test(t, func(t *testing.T) {
				n, _ := testNode(t)
				h := &readinessFrames{stateless: stateless{screenHost{bareHost: bareHost{kind: host.Orca}}},
					frames: []string{"waiting for a rollout"}}
				n.hosts = host.NewRegistry(h)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				began := time.Now()
				if cancellation {
					go func() {
						time.Sleep(10 * time.Millisecond) // sleep-ok: cancel inside the pending virtual poll
						cancel()
					}()
				}
				_, err := n.waitCodexThread(ctx, "orca:t1:l1", n.home, "prompt", began)
				if cancellation {
					if !errors.Is(err, context.Canceled) || len(h.reads) != 0 || time.Since(began) != 10*time.Millisecond {
						t.Fatalf("cancelled poll: reads=%d, elapsed=%v, error=%v", len(h.reads), time.Since(began), err)
					}
				} else if err == nil || len(h.reads) != 5 || time.Since(began) != codexStartWait {
					t.Fatalf("deadline: reads=%d, elapsed=%v, error=%v", len(h.reads), time.Since(began), err)
				}
			})
		})
	}
}
