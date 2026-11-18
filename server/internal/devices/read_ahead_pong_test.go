package devices

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// The pong and a blocking node event arrive in one raw read. The read completes
// before the ping deadline, but Serve cannot dispatch the pong until the event
// callback returns. PingActivity must nevertheless include the pong already read.
func TestReadAheadPongAtProductionCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldWait, oldWindow := pingWait, pingActivityWindow
		pingWait, pingActivityWindow = 40*time.Millisecond, 240*time.Millisecond
		t.Cleanup(func() { pingWait, pingActivityWindow = oldWait, oldWindow })

		r, w := io.Pipe()
		writes := &signalWriter{ch: make(chan string, 4)}
		peer := rpc.New("h", writes)
		entered := make(chan struct{}, 2)
		release := make(chan struct{}, 2)
		peer.OnEvent = func(string, json.RawMessage) {
			entered <- struct{}{}
			<-release
		}
		serveDone := make(chan error, 1)
		go func() { serveDone <- peer.Serve(context.Background(), r) }()
		t.Cleanup(func() {
			close(release)
			_ = w.Close()
			_ = r.Close()
			peer.Close(io.EOF)
			select {
			case <-serveDone:
			case <-time.After(time.Second):
				t.Error("Serve did not stop")
			}
		})
		d, m := pingMachine(t, peer)
		wantPingLine := func() string {
			t.Helper()
			select {
			case got := <-writes.ch:
				return got
			case <-time.After(time.Second):
				t.Fatal("ping was not written")
				return ""
			}
		}

		for round := 1; round <= 2; round++ {
			measureDone := make(chan struct{})
			go func() {
				d.measureNodes(context.Background())
				close(measureDone)
			}()
			var ping struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(wantPingLine()), &ping); err != nil {
				t.Fatal(err)
			}
			before := peer.PingActivity()
			writeDone := make(chan error, 1)
			go func() {
				_, err := io.WriteString(w, `{"event":"block","params":{}}`+"\n"+`{"id":"`+ping.ID+`","result":{}}`+"\n")
				writeDone <- err
			}()
			select {
			case err := <-writeDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("combined event and pong did not reach the reader")
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("event callback was not entered")
			}
			<-measureDone // the event remains blocked past the ping deadline

			m.mu.Lock()
			misses := m.pingMisses
			m.mu.Unlock()
			closed := false
			select {
			case <-peer.Done():
				closed = true
			default:
			}
			t.Logf("round %d after timeout: pong bytes already read, activity advanced=%v, misses=%d, closed=%v", round, peer.PingActivity().After(before), misses, closed)
			release <- struct{}{}

			deadline := time.Now().Add(time.Second)
			for !peer.PingActivity().After(before) && time.Now().Before(deadline) {
				synctest.Sleep(time.Millisecond)
			}
			if round == 1 {
				// The production loop waits 30s before the next measurement. Make
				// the previous pong stale just as it is after the real 30s + 5s.
				synctest.Sleep(240 * time.Millisecond)
			}
		}

		select {
		case <-peer.Done():
			t.Fatalf("closed although both pongs were read before their deadlines: %v", peer.Err())
		default:
		}
	})
}
