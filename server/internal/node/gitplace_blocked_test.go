//go:build !windows

package node

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// A FIFO models a filesystem read that never answers. Its writer is released
// only after inventory has returned, so increasing an RPC timeout cannot pass.
func TestBlockedGitHeadDoesNotHideWorkingSession(t *testing.T) {
	n, _ := testNode(t)
	repo := filepath.Join(t.TempDir(), "repo")
	head := filepath.Join(repo, ".git", "HEAD")
	if err := os.MkdirAll(filepath.Dir(head), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(head, 0o600); err != nil {
		t.Fatal(err)
	}
	old := gitPlaces
	gitPlaces = newGitPlaceCache(readGitPlace)
	defer func() { gitPlaces = old }()
	const id = "0d000000-0000-4000-8000-000000002750"
	n.agents[id] = host.Agent{Agent: "claude", Cwd: repo, AgentStatus: proto.StatusWorking}
	n.onAgentsList = map[string]bool{id: true}
	received := false
	rows := make(chan []proto.Session, 1)
	go func() { rows <- n.sessionList(context.Background(), false) }()
	// Always release the diagnostic read, including the old synchronous path
	// used by the negative control, before restoring the shared cache.
	defer func() {
		f, err := os.OpenFile(head, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("ref: refs/heads/main\n"); err != nil {
			t.Error(err)
		}
		if err := f.Close(); err != nil {
			t.Error(err)
		}
		if !received {
			select {
			case <-rows:
			case <-time.After(2 * time.Second):
				t.Error("inventory did not recover")
			}
		}
		gitPlaces.mu.Lock()
		var pending <-chan struct{}
		if e := gitPlaces.entries[repo]; e != nil {
			pending = e.pending
		}
		gitPlaces.mu.Unlock()
		if pending != nil {
			select {
			case <-pending:
			case <-time.After(2 * time.Second):
				t.Error("Git read did not recover")
			}
		}
	}()
	select {
	case got := <-rows:
		received = true
		if len(got) != 1 || got[0].SessionID != id || got[0].Status != proto.StatusWorking {
			t.Fatalf("lost working session: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("optional HEAD read blocked authoritative inventory")
	}
}
