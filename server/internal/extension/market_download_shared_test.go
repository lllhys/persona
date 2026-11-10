package extension

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// halfThenWait serves data's first half, then the rest once release is closed; stopped says a request ended
// before that (its client went away).
type halfThenWait struct {
	srv      *httptest.Server
	requests atomic.Int32
	stopped  atomic.Int32
	release  chan struct{}
}

func newHalfThenWait(t *testing.T, data []byte) *halfThenWait {
	h := &halfThenWait{release: make(chan struct{})}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		select {
		case <-h.release:
			_, _ = w.Write(data[len(data)/2:])
		case <-r.Context().Done():
			h.stopped.Add(1)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *halfThenWait) market(t *testing.T, cache string) *Market {
	t.Helper()
	spec, err := ParseMarket(h.srv.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}
	return &Market{Spec: spec, Client: h.srv.Client(), Cache: cache}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// downloadSeen is ctx whose download progress (bytes, and whether from the cache) is kept.
func downloadSeen(ctx context.Context) (context.Context, func() StepEvent) {
	var mu sync.Mutex
	var last StepEvent
	return WithProgress(ctx, func(step string, ev StepEvent) {
			if step == StepDownload {
				mu.Lock()
				last = ev
				mu.Unlock()
			}
		}), func() StepEvent {
			mu.Lock()
			defer mu.Unlock()
			return last
		}
}

// A phone that gave up on a slow preview took the download with it, and asking again downloaded the package a
// second time beside the first (#3692). The download now outlives a caller who went away, the next caller joins it
// where it is (one request), and the one after takes the package from the cache.
func TestSharedDownloadOutlivesItsCallerAndIsJoined(t *testing.T) {
	data := bytes.Repeat([]byte("package "), 8<<10)
	h := newHalfThenWait(t, data)
	cache := t.TempDir()
	m := h.market(t, cache)
	pkg := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}

	first, leave := context.WithCancel(t.Context())
	first, seenFirst := downloadSeen(first)
	errc := make(chan error, 1)
	go func() { _, _, err := m.Download(first, pkg, t.TempDir()); errc <- err }()
	waitUntil(t, "data to arrive", func() bool { return seenFirst().Bytes > 0 })
	leave() // the phone's request timed out
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that left: %v", err)
	}

	second, seenSecond := downloadSeen(t.Context())
	type result struct {
		file string
		err  error
	}
	done := make(chan result, 1)
	go func() { file, _, err := m.Download(second, pkg, t.TempDir()); done <- result{file, err} }()
	waitUntil(t, "the second caller to join where it is", func() bool { return seenSecond().Bytes > 0 })
	close(h.release)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if b, _ := os.ReadFile(r.file); !bytes.Equal(b, data) {
		t.Fatalf("got %d bytes", len(b))
	}
	if n := h.requests.Load(); n != 1 || h.stopped.Load() != 0 {
		t.Fatalf("requests %d, stopped %d: the download started again", n, h.stopped.Load())
	}
	third, seenThird := downloadSeen(t.Context())
	if _, _, err := m.Download(third, pkg, t.TempDir()); err != nil || !seenThird().Cached || h.requests.Load() != 1 {
		t.Fatalf("not from the cache: %v %+v %d", err, seenThird(), h.requests.Load())
	}
	waitUntil(t, "its directory to go", func() bool { dirs, _ := filepath.Glob(filepath.Join(cache, ".fetch-*")); return len(dirs) == 0 })
}

// Nobody waiting any more: the download still finishes into the cache.
func TestSharedDownloadFinishesIntoTheCacheWithNobodyWaiting(t *testing.T) {
	data := bytes.Repeat([]byte("package "), 4<<10)
	h := newHalfThenWait(t, data)
	cache := t.TempDir()
	m := h.market(t, cache)
	pkg := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}
	ctx, leave := context.WithCancel(t.Context())
	ctx, seen := downloadSeen(ctx)
	errc := make(chan error, 1)
	go func() { _, _, err := m.Download(ctx, pkg, t.TempDir()); errc <- err }()
	waitUntil(t, "data to arrive", func() bool { return seen().Bytes > 0 })
	leave()
	<-errc
	close(h.release)
	waitUntil(t, "the package in the cache", func() bool { from, _ := cached(cache, pkg.SHA256); return from != "" })
	waitUntil(t, "its directory to go", func() bool { dirs, _ := filepath.Glob(filepath.Join(cache, ".fetch-*")); return len(dirs) == 0 })
	if h.stopped.Load() != 0 {
		t.Fatal("the download stopped with its caller")
	}
}

// The user's Cancel stops a download nobody else waits for; one another caller waits for goes on.
func TestSharedDownloadStopsOnTheUsersCancel(t *testing.T) {
	data := bytes.Repeat([]byte("package "), 4<<10)
	h := newHalfThenWait(t, data)
	cache := t.TempDir()
	m := h.market(t, cache)
	pkg := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}

	// Two wait; one cancels: the other still gets it, from the same request.
	a, cancelA := context.WithCancelCause(t.Context())
	a, seenA := downloadSeen(a)
	errA := make(chan error, 1)
	go func() { _, _, err := m.Download(a, pkg, t.TempDir()); errA <- err }()
	waitUntil(t, "data to arrive", func() bool { return seenA().Bytes > 0 })
	b, seenB := downloadSeen(t.Context())
	errB := make(chan error, 1)
	go func() { _, _, err := m.Download(b, pkg, t.TempDir()); errB <- err }()
	waitUntil(t, "the second to join", func() bool { return seenB().Bytes > 0 })
	cancelA(ErrUserCanceled)
	if err := <-errA; err == nil {
		t.Fatal("canceled, and still downloaded")
	}
	close(h.release)
	if err := <-errB; err != nil || h.requests.Load() != 1 || h.stopped.Load() != 0 {
		t.Fatalf("the one still waiting: %v, requests %d, stopped %d", err, h.requests.Load(), h.stopped.Load())
	}

	// Alone, it stops: nothing in the cache, nothing left behind.
	other := data[1:]
	h2 := newHalfThenWait(t, other)
	cache2 := t.TempDir()
	m2 := h2.market(t, cache2)
	pkg2 := &MarketPackage{URL: h2.srv.URL + "/pkg.tar.gz", SHA256: ociSum(other), Size: int64(len(other))}
	c, cancelC := context.WithCancelCause(t.Context())
	c, seenC := downloadSeen(c)
	errC := make(chan error, 1)
	go func() { _, _, err := m2.Download(c, pkg2, t.TempDir()); errC <- err }()
	waitUntil(t, "data to arrive", func() bool { return seenC().Bytes > 0 })
	cancelC(ErrUserCanceled)
	<-errC
	waitUntil(t, "the request to stop", func() bool { return h2.stopped.Load() == 1 })
	waitUntil(t, "its directory to go", func() bool { dirs, _ := filepath.Glob(filepath.Join(cache2, ".fetch-*")); return len(dirs) == 0 })
	if from, _ := cached(cache2, pkg2.SHA256); from != "" {
		t.Fatal("a canceled download was cached")
	}
}

// A download that keeps trickling past its budget says it was too slow, and how much arrived (it was cut at five
// minutes with a bare "context deadline exceeded", #3692).
func TestDownloadTooSlowSaysSo(t *testing.T) {
	defer func(was time.Duration) { DownloadBudget = was }(DownloadBudget)
	DownloadBudget = 300 * time.Millisecond
	data := bytes.Repeat([]byte("p"), 4<<10)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		for _, c := range data {
			if _, err := w.Write([]byte{c}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-time.After(20 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer srv.Close()
	spec, _ := ParseMarket(srv.URL + "/index.json")
	pkg := &MarketPackage{URL: srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}
	for _, cache := range []string{"", t.TempDir()} {
		m := &Market{Spec: spec, Client: srv.Client(), Cache: cache}
		_, _, err := m.Download(t.Context(), pkg, t.TempDir())
		if !errors.Is(err, ErrDownloadSlow) {
			t.Fatalf("cache %q: %v", cache, err)
		}
		if text := i18n.Localize(err, i18n.En); !strings.Contains(text, "too slow") || !strings.Contains(text, " / 0.0 MB") {
			t.Fatalf("cache %q: %q", cache, text)
		}
		if zh := i18n.Localize(err, "zh-Hans"); zh == i18n.Localize(err, i18n.En) {
			t.Fatalf("not translated: %q", zh)
		}
	}
}

// What a run that ended mid-way left in the cache goes; a download going on stays.
func TestSharedDownloadDropsStaleDirectories(t *testing.T) {
	cache := t.TempDir()
	old, live := filepath.Join(cache, ".fetch-old"), filepath.Join(cache, ".fetch-live")
	for _, dir := range []string{old, live} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "download"), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour)
	for _, p := range []string{old, filepath.Join(old, "download")} {
		if err := os.Chtimes(p, then, then); err != nil {
			t.Fatal(err)
		}
	}
	dropStaleFetches(cache)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the stale one stays")
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("the live one went")
	}
}
