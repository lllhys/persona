package extmgr

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
)

// A plugin serving HTTP only is asked for its manifest on every preview and plan recheck: each read must leave no
// connection behind, or every one keeps two of the hub's goroutines and a descriptor until the plugin closes it.
func TestRemoteManifestLeavesNoConnectionOpen(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "http.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var served atomic.Int64
	var mu sync.Mutex
	open := map[net.Conn]bool{}
	openConns := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(open)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/manifest" {
				http.NotFound(w, r)
				return
			}
			switch served.Add(1) {
			case 5:
				http.Error(w, "down", http.StatusInternalServerError)
			case 9:
				_, _ = w.Write([]byte(`{"id":`))
			default:
				_, _ = w.Write([]byte(`{"manifest_version":1,"id":"remote-probe","name":"Remote probe","version":"1.0.0",` +
					`"engines":{"extension_protocol":1},"contributes":{"tools":[{"name":"x","description":"x"}]}}`))
			}
		}),
		ConnState: func(c net.Conn, s http.ConnState) {
			mu.Lock()
			defer mu.Unlock()
			switch s {
			case http.StateNew:
				open[c] = true
			case http.StateClosed, http.StateHijacked:
				delete(open, c)
			}
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
	})

	goroutines, fds := runtime.NumGoroutine(), openFDs()
	ps := &extensions{}
	const calls = 20
	var failed []string
	for i := range calls {
		m, err := ps.remoteManifest(context.Background(), extension.Remote{HTTPSocket: sock})
		switch i + 1 {
		case 5, 9:
			if err == nil {
				t.Fatalf("call %d: a failed manifest read returned no error", i+1)
			}
			failed = append(failed, err.Error())
		default:
			if err != nil || m == nil || m.ID != "remote-probe" {
				t.Fatalf("call %d: %v %+v", i+1, err, m)
			}
		}
	}
	if !strings.Contains(failed[0], "GET /manifest: HTTP 500") {
		t.Fatal("a non-200 answer is reported by its status:", failed[0])
	}

	// The slack covers the runtime's own goroutines coming and going; a leak is two per call here plus the server's one.
	const slack = 5
	var conns, g, f int
	// Logged on success and on a timeout alike: the counts are what a failure needs.
	t.Cleanup(func() {
		t.Logf("after %d manifest reads: %d server connections open, goroutines %d -> %d, descriptors %d -> %d",
			calls, conns, goroutines, g, fds, f)
	})
	eventually(t, 2*time.Second, "the server connections, goroutines and descriptors to settle", func() bool {
		conns, g, f = openConns(), runtime.NumGoroutine(), openFDs()
		return conns == 0 && g <= goroutines+slack && f <= fds+slack
	})
}

// openFDs is how many descriptors this process holds open; 0 where /proc/self/fd does not list them (not Linux).
func openFDs() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return len(entries)
}
