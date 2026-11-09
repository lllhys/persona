package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// benchSpoolEntries are spoolMost hook events of about 2 KB each: a full spool (about 1 MiB).
func benchSpoolEntries() []spoolEntry {
	out := make([]spoolEntry, spoolMost)
	for i := range out {
		in, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": reportSession, "tool_name": "Read",
			"cwd": "/work/project", "tool_response": strings.Repeat("x", 1900)})
		out[i] = spoolEntry{ID: newUUID(), At: time.Now(), Kind: "hook", Request: sockRequest{Kind: "hook", Input: in}}
	}
	return out
}

// Standby filling the spool, one hook at a time.
func BenchmarkSpoolFill(b *testing.B) {
	entries := benchSpoolEntries()
	for b.Loop() {
		b.StopTimer()
		sp := openSpool(filepath.Join(b.TempDir(), spoolFile))
		b.StartTimer()
		for _, e := range entries {
			if _, err := sp.add(e); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Standby keeping a hook while the spool is full: the oldest entry goes.
func BenchmarkSpoolAddFull(b *testing.B) {
	entries := benchSpoolEntries()
	sp := openSpool(filepath.Join(b.TempDir(), spoolFile))
	for _, e := range entries {
		if _, err := sp.add(e); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; b.Loop(); i++ {
		e := entries[i%len(entries)]
		e.ID = newUUID()
		if dropped, err := sp.add(e); err != nil || dropped == 0 {
			b.Fatal(dropped, err)
		}
	}
}

// A connection replaying a full spool.
func BenchmarkSpoolReplay(b *testing.B) {
	entries := benchSpoolEntries()
	var file []byte
	for _, e := range entries {
		line, _ := json.Marshal(e)
		file = append(append(file, line...), '\n')
	}
	n, _ := testNode(b)
	n.logger = log.New(io.Discard, "", 0)
	n.panes = map[string]string{}
	newReplayHub(b, n)
	for b.Loop() {
		b.StopTimer()
		path := filepath.Join(b.TempDir(), spoolFile)
		if err := os.WriteFile(path, file, 0o600); err != nil {
			b.Fatal(err)
		}
		sp := openSpool(path)
		n.replayed, n.stateReady, n.ahead = make(chan struct{}), make(chan struct{}), replayAhead{}
		close(n.stateReady)
		b.StartTimer()
		n.replaySpool(context.Background(), sp)
		if left := sp.load(); len(left) != 0 {
			b.Fatalf("%d left", len(left))
		}
	}
}
