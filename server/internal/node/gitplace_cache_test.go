package node

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitPlaceBlockedReadsAreBounded(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, gitPlaceReaders)
	var calls atomic.Int32
	c := newGitPlaceCache(func(string) gitPlace {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return gitPlace{branch: "main"}
	})
	defer func() {
		close(release)
		for i := range gitPlaceReaders {
			awaitGitPlace(t, c, fmt.Sprint(i))
		}
	}()
	for i := range gitPlaceReaders {
		c.lookup(fmt.Sprint(i))
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("reader did not start")
		}
	}
	for i := range gitPlaceLimit * 2 {
		c.lookup(fmt.Sprint(i))
	}
	if got := calls.Load(); got != gitPlaceReaders {
		t.Fatalf("started %d readers", got)
	}
	if len(c.entries) != gitPlaceReaders {
		t.Fatalf("unbounded pending entries: %d", len(c.entries))
	}
}

func TestGitPlaceRefreshRetainsLabelsAndRecovers(t *testing.T) {
	release, entered := make(chan struct{}), make(chan struct{}, 1)
	var calls atomic.Int32
	c := newGitPlaceCache(func(string) gitPlace {
		if calls.Add(1) == 1 {
			return gitPlace{branch: "main"}
		}
		entered <- struct{}{}
		<-release
		return gitPlace{branch: "topic"}
	})
	if got := awaitGitPlace(t, c, "/repo"); got.branch != "main" {
		t.Fatal(got)
	}
	c.mu.Lock()
	c.entries["/repo"].at = time.Now().Add(-gitPlaceTTL)
	c.mu.Unlock()
	if got := c.lookup("/repo"); got.branch != "main" {
		t.Fatal(got)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	for range 100 {
		if got := c.lookup("/repo"); got.branch != "main" {
			t.Fatal(got)
		}
	}
	close(release)
	if got := awaitGitPlace(t, c, "/repo"); got.branch != "topic" {
		t.Fatal(got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("duplicate refresh: %d", got)
	}
}

func TestGitPlaceCacheBoundsCompletedEntries(t *testing.T) {
	c := newGitPlaceCache(func(string) gitPlace { return gitPlace{} })
	for i := range gitPlaceLimit + 2 {
		awaitGitPlace(t, c, fmt.Sprint(i))
	}
	if len(c.entries) != gitPlaceLimit {
		t.Fatalf("entries: %d", len(c.entries))
	}
}
