package node

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestSendWaitsForTheStart(t *testing.T) {
	var l startLog
	l.add("s1", &startRec{done: make(chan struct{}), session: proto.Session{SessionID: "s1", Starting: true}})
	if got := l.listed(nil); len(got) != 1 || !got[0].Starting {
		t.Fatalf("a starting session is listed as starting: %+v", got)
	}
	if l.listed(map[string]bool{"s1": true}) != nil {
		t.Fatal("one herdr lists already is not listed twice")
	}
	waited := make(chan error, 1)
	go func() { waited <- l.wait(context.Background(), "s1") }()
	select {
	case <-waited:
		t.Fatal("a send did not wait for the start")
	case <-time.After(50 * time.Millisecond):
	}
	l.finish("s1", nil)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the send never went")
	}
	if l.get("s1") != nil || l.listed(nil) != nil {
		t.Fatal("a started session is left to herdr's list")
	}
	if err := l.wait(context.Background(), "other"); err != nil {
		t.Fatal("a session that is not starting does not wait")
	}
}

func TestFailedStartIsShownAndAnswersSends(t *testing.T) {
	var l startLog
	l.add("s1", &startRec{done: make(chan struct{}), session: proto.Session{SessionID: "s1", Starting: true}})
	l.finish("s1", errors.New("claude did not become ready"))
	err := l.wait(context.Background(), "s1")
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("send after a failed start: %v", err)
	}
	got := l.listed(nil)
	if len(got) != 1 || got[0].Starting || got[0].StartError == "" || got[0].Status != proto.StatusOffline {
		t.Fatalf("a failed start is listed with its error: %+v", got)
	}
}

func TestRecoveryKeepsPendingStartsWaiting(t *testing.T) {
	var l startLog
	r := &startRec{done: make(chan struct{}), session: proto.Session{SessionID: "s1", Starting: true}}
	l.add("s1", r)
	l.recovered("s1")
	if l.get("s1") != r {
		t.Fatal("finding a pane before an async start finishes must not release queued messages")
	}
	l.finish("s1", nil)
	if err := l.wait(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
}

func TestTabNameBeforeAndAfterTheTab(t *testing.T) {
	var l startLog
	l.add("s1", &startRec{done: make(chan struct{})})
	if kept, _ := l.nameTab("s1", "fix-login"); !kept {
		t.Fatal("a name before the tab exists is kept for it")
	}
	if name := l.tabCreated("s1", "t1"); name != "fix-login" {
		t.Fatalf("the new tab gets the kept name, got %q", name)
	}
	if kept, tab := l.nameTab("s1", "other"); kept || tab != "t1" {
		t.Fatalf("once the tab exists it is renamed directly: %v %q", kept, tab)
	}
	l.finish("s1", nil)
	if kept, tab := l.nameTab("s1", "x"); kept || tab != "" {
		t.Fatal("a started session is renamed the normal way")
	}
}
