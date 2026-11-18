package streams

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"testing"
	"testing/synctest"
)

// An open stream must not keep chunks it already delivered, whether more chunks remain queued or it is idle.
func TestStreamReleasesDeliveredChunks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		acks := make(chan dataMsg, window/chunk)
		m := New(func(event string, params any) error {
			if event == EventAck {
				acks <- params.(dataMsg)
			}
			return nil
		})
		defer m.CloseAll()
		if err := m.Open("s1"); err != nil {
			t.Fatal(err)
		}
		// Queue a full window before Start: the writer cannot consume the backing array as it grows.
		for i := range window / chunk {
			p, err := json.Marshal(dataMsg{ID: "s1", Data: bytes.Repeat([]byte{byte(i)}, chunk)})
			if err != nil {
				t.Fatal(err)
			}
			if !m.Event(EventData, p) {
				t.Fatal("data was not taken")
			}
		}
		s := m.streams["s1"]
		backing := s.queue[:len(s.queue)]
		conn, writer := net.Pipe()
		defer conn.Close()
		m.Start("s1", writer)
		read := func(i int) {
			t.Helper()
			got := make([]byte, chunk)
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, bytes.Repeat([]byte{byte(i)}, chunk)) {
				t.Fatalf("chunk %d differs", i)
			}
			ack := <-acks
			if ack.ID != "s1" || ack.N != chunk {
				t.Fatalf("chunk %d acknowledgement: %+v", i, ack)
			}
		}
		check := func(delivered int) {
			t.Helper()
			// The writer is now blocked on the next pipe write, or waiting for more data after a full drain.
			synctest.Wait()
			s.mu.Lock()
			defer s.mu.Unlock()
			for i := range delivered {
				if backing[i] != nil {
					t.Errorf("delivered chunk %d is retained after %d deliveries", i, delivered)
				}
			}
			remaining := window/chunk - delivered
			if remaining > 0 {
				remaining-- // one chunk is in conn.Write rather than the queue
			}
			if len(s.queue) != remaining || s.queued != remaining*chunk {
				t.Errorf("queue has %d chunks / %d bytes, want %d chunks", len(s.queue), s.queued, remaining)
			}
			for i, b := range s.queue {
				if len(b) != chunk {
					t.Errorf("pending chunk %d has %d bytes", i, len(b))
				}
			}
			if s.done || m.Len() != 1 {
				t.Error("the idle stream closed")
			}
		}
		for i := range window / chunk / 2 {
			read(i)
		}
		check(window / chunk / 2)
		for i := window / chunk / 2; i < window/chunk; i++ {
			read(i)
		}
		check(window / chunk)
	})
}
