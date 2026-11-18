//go:build darwin

package sysmon

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/unix"
)

// fakeModelCommands stands in for ioreg and system_profiler: ioreg prints out (nothing: an Intel Mac), and
// system_profiler answers with the fixture once release closes, or fails when its context ends (#6028). Its first
// fail runs fail at once instead.
type fakeModelCommands struct {
	ioreg   []byte
	release chan struct{}
	fail    int

	mu       sync.Mutex
	ran      []string
	profiler int
}

func (f *fakeModelCommands) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.ran = append(f.ran, name)
	failNow := false
	if name == "system_profiler" {
		f.profiler++
		failNow = f.profiler <= f.fail
	}
	f.mu.Unlock()
	switch name {
	case "ioreg":
		return f.ioreg, nil
	case "system_profiler":
		if failNow {
			return nil, errors.New("system_profiler: exit status 1")
		}
		select {
		case <-f.release:
			return []byte("Hardware:\n\n    Hardware Overview:\n\n      Model Name: MacBook Pro\n"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}

func (f *fakeModelCommands) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ran)
}

// A system_profiler that takes its time (over 10 s on a Mac whose Activation Lock check does not answer) holds the
// first read for modelWait at most and later ones not at all; once it answers, the name is there for every read.
func TestModelLookupDoesNotWaitForSystemProfiler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{release: make(chan struct{})}
		l := newModelLookup(f.run)
		ctx := context.Background()

		start := time.Now()
		if m := l.get(ctx); m != "" {
			t.Fatalf("first read = %q while system_profiler runs", m)
		}
		if waited := time.Since(start); waited != modelWait {
			t.Fatalf("the first read waited %v, want %v", waited, modelWait)
		}
		start = time.Now()
		if m := l.get(ctx); m != "" || time.Since(start) != 0 {
			t.Fatalf("a later read = %q after %v, want none at once", m, time.Since(start))
		}

		close(f.release)
		synctest.Wait()
		start = time.Now()
		if m := l.get(ctx); m != "MacBook Pro" || time.Since(start) != 0 {
			t.Fatalf("read after system_profiler answered = %q after %v", m, time.Since(start))
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler"}) {
			t.Fatalf("ran %v, want ioreg then system_profiler once", got)
		}

		time.Sleep(3 * modelRetry) // sleep-ok: synctest's fake clock, no real wait
		if m := l.get(ctx); m != "MacBook Pro" {
			t.Fatalf("read long after the name came in = %q", m)
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler"}) {
			t.Fatalf("ran %v after a name was found, want no second lookup", got)
		}
	})
}

// On Apple silicon the IORegistry has the name: system_profiler never runs and nothing waits.
func TestModelLookupTakesTheIORegistryName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{ioreg: fixture(t, "ioreg_product"), release: make(chan struct{})}
		l := newModelLookup(f.run)
		start := time.Now()
		if m := l.get(context.Background()); m != "Mac Studio" || time.Since(start) != 0 {
			t.Fatalf("read = %q after %v", m, time.Since(start))
		}
		time.Sleep(3 * modelRetry) // sleep-ok: synctest's fake clock, no real wait
		if m := l.get(context.Background()); m != "Mac Studio" {
			t.Fatalf("later read = %q", m)
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg"}) {
			t.Fatalf("ran %v, want ioreg once", got)
		}
	})
}

// A system_profiler that never answers is given up after profilerTimeout, and the identifier stays until modelRetry
// has passed since; then the next read starts a fresh lookup.
func TestModelLookupGivesUpOnAHungSystemProfiler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{release: make(chan struct{})}
		l := newModelLookup(f.run)
		start := time.Now()
		_ = l.get(context.Background())
		time.Sleep(profilerTimeout - modelWait - time.Nanosecond) // sleep-ok: synctest's fake clock, no real wait
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler"}) {
			t.Fatalf("ran %v while system_profiler runs, want one lookup", got)
		}
		synctest.Wait()
		time.Sleep(time.Nanosecond) // sleep-ok: synctest's fake clock, no real wait
		synctest.Wait()
		if took := time.Since(start); took != profilerTimeout {
			t.Fatalf("now %v after the start, want %v", took, profilerTimeout)
		}
		if m := l.get(context.Background()); m != "" {
			t.Fatalf("read = %q after system_profiler was cut off", m)
		}

		time.Sleep(modelRetry) // sleep-ok: synctest's fake clock, no real wait
		close(f.release)
		if m := l.get(context.Background()); m != "MacBook Pro" {
			t.Fatalf("read modelRetry after the cut-off = %q, want a fresh lookup's name", m)
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler", "ioreg", "system_profiler"}) {
			t.Fatalf("ran %v, want a second lookup after modelRetry", got)
		}
	})
}

// A lookup that ends without a name (system_profiler failed once, say right after login) is tried again by the
// first read modelRetry after it ended, not by reads before that, and the name it then finds is kept (#6063 review).
func TestModelLookupRetriesAnEmptyResultAfterTheBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{release: make(chan struct{}), fail: 1}
		close(f.release)
		l := newModelLookup(f.run)
		ctx := context.Background()
		once := []string{"ioreg", "system_profiler"}

		if m := l.get(ctx); m != "" {
			t.Fatalf("first read = %q with a failing system_profiler", m)
		}
		synctest.Wait()
		time.Sleep(modelRetry - time.Nanosecond) // sleep-ok: synctest's fake clock, no real wait
		start := time.Now()
		if m := l.get(ctx); m != "" || time.Since(start) != 0 {
			t.Fatalf("read within modelRetry = %q after %v, want none at once", m, time.Since(start))
		}
		if got := f.commands(); !slices.Equal(got, once) {
			t.Fatalf("ran %v within modelRetry, want no second system_profiler", got)
		}

		time.Sleep(time.Nanosecond) // sleep-ok: synctest's fake clock, no real wait
		if m := l.get(ctx); m != "MacBook Pro" {
			t.Fatalf("read after modelRetry = %q, want the retried lookup's name", m)
		}
		if got := f.commands(); !slices.Equal(got, append(once, once...)) {
			t.Fatalf("ran %v, want one retry", got)
		}
		time.Sleep(3 * modelRetry) // sleep-ok: synctest's fake clock, no real wait
		if m := l.get(ctx); m != "MacBook Pro" {
			t.Fatalf("read long after the retry = %q", m)
		}
		if got := f.commands(); !slices.Equal(got, append(once, once...)) {
			t.Fatalf("ran %v after the name was found, want no third lookup", got)
		}
	})
}

// Readers that arrive together, first and again after a failed lookup's backoff, share one lookup and wait no
// longer than modelWait.
func TestModelLookupConcurrentReadersShareOneLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{release: make(chan struct{}), fail: 1}
		l := newModelLookup(f.run)
		readAll := func() []string {
			start := time.Now()
			got := make([]string, 8)
			var wg sync.WaitGroup
			for i := range got {
				wg.Go(func() { got[i] = l.get(context.Background()) })
			}
			wg.Wait()
			if waited := time.Since(start); waited > modelWait {
				t.Fatalf("readers waited %v, want at most %v", waited, modelWait)
			}
			return got
		}

		for i, m := range readAll() {
			if m != "" {
				t.Fatalf("reader %d = %q with a failing system_profiler", i, m)
			}
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler"}) {
			t.Fatalf("ran %v, want one lookup for all readers", got)
		}

		time.Sleep(modelRetry) // sleep-ok: synctest's fake clock, no real wait
		for i, m := range readAll() {
			if m != "" {
				t.Fatalf("reader %d = %q while the retried system_profiler runs", i, m)
			}
		}
		close(f.release)
		synctest.Wait()
		for i, m := range readAll() {
			if m != "MacBook Pro" {
				t.Fatalf("reader %d = %q after the retry answered", i, m)
			}
		}
		if got := f.commands(); !slices.Equal(got, []string{"ioreg", "system_profiler", "ioreg", "system_profiler"}) {
			t.Fatalf("ran %v, want one retried lookup for all readers", got)
		}
	})
}

// ReadSystem answers with the identifier while system_profiler is still out, and with the name once it is in.
func TestReadSystemFallsBackToTheIdentifierWhileTheNameIsOut(t *testing.T) {
	hw, err := unix.Sysctl("hw.model")
	if err != nil || hw == "" {
		t.Skipf("hw.model: %q, %v", hw, err)
	}
	synctest.Test(t, func(t *testing.T) {
		f := &fakeModelCommands{release: make(chan struct{})}
		l := newModelLookup(f.run)
		if s := readSystem(context.Background(), l); s.Model != hw || s.Name == "" || s.Cores == 0 {
			t.Fatalf("system while system_profiler runs = %+v, want model %q", s, hw)
		}
		close(f.release)
		synctest.Wait()
		if s := readSystem(context.Background(), l); s.Model != "MacBook Pro" {
			t.Fatalf("model after system_profiler answered = %q", s.Model)
		}
	})
}
