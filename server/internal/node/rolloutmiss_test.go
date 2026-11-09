package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// expireRollouts makes the next rollouts call list again as if 5 seconds (recent days) or a minute (older days too)
// had passed.
func expireRollouts(home string, older bool) {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	c := cacheFor(home)
	c.at = time.Time{}
	if older {
		c.olderAt = time.Time{}
	}
}

// ageDirs dates every directory under root an hour back, as directories last changed long before a look.
func ageDirs(t testing.TB, root string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return os.Chtimes(p, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A thread with no rollout is globbed once, not on every look; a rollout written later is still found: in a recent
// day or archived_sessions at once, in an older day once the older days are listed again (a minute).
func TestRolloutPathMissCached(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	now := time.Now()
	writeRollout(t, home, "11111111-1111-1111-1111-111111111111", "/w", now, now)
	ageDirs(t, codex)
	const old, fresh = "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"

	if p := rolloutPath(codex, old); p != "" {
		t.Fatalf("no rollout yet: %q", p)
	}
	// Written into a day before the window (a resumed old thread's directory) right after the miss.
	writeRollout(t, home, old, "/w", now.AddDate(0, 0, -40), now)
	if p := rolloutPath(codex, old); p != "" {
		t.Fatalf("the miss is kept until the older days are listed again: %q", p)
	}
	expireRollouts(codex, false)
	if p := rolloutPath(codex, old); p != "" {
		t.Fatalf("listing the recent days does not see an older day: %q", p)
	}
	expireRollouts(codex, true)
	if p := rolloutPath(codex, old); filepath.Base(filepath.Dir(p)) != now.AddDate(0, 0, -40).Format("02") {
		t.Fatalf("after the older days are listed again: %q", p)
	}

	ageDirs(t, codex)
	if p := rolloutPath(codex, fresh); p != "" {
		t.Fatalf("no rollout yet: %q", p)
	}
	writeRollout(t, home, fresh, "/w", now, now)
	if p := rolloutPath(codex, fresh); p == "" {
		t.Fatal("a rollout written today is found at once")
	}
}

// A rollout archived after a miss (moved out of sessions/) is found at once.
func TestRolloutPathArchived(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	const id = "44444444-4444-4444-4444-444444444444"
	day := time.Now().AddDate(0, 0, -60)
	writeRollout(t, home, id, "/w", day, day)
	src := filepath.Join(codex, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"),
		"rollout-"+day.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	dst := filepath.Join(codex, "archived_sessions", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	ageDirs(t, codex)
	const other = "55555555-5555-5555-5555-555555555555"
	if p := rolloutPath(codex, other); p != "" {
		t.Fatalf("no rollout: %q", p)
	}
	// The archived rollout's metadata must identify the thread being looked up as well as its filename.
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(strings.ReplaceAll(string(data), id, other)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(filepath.Dir(dst), "rollout-"+day.Format("2006-01-02T15-04-05")+"-"+other+".jsonl")); err != nil {
		t.Fatal(err)
	}
	if p := rolloutPath(codex, other); p == "" {
		t.Fatal("archived after the miss: not found")
	}
}

// A miss looked up while a directory had just changed is not kept: within its mtime's granularity, a file created next
// may leave the mtime as it was.
func TestRolloutPathMissRacy(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	now := time.Now()
	writeRollout(t, home, "11111111-1111-1111-1111-111111111111", "/w", now, now)
	dir := filepath.Join(codex, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	tick := time.Now()
	if err := os.Chtimes(dir, tick, tick); err != nil {
		t.Fatal(err)
	}
	const id = "66666666-6666-6666-6666-666666666666"
	if p := rolloutPath(codex, id); p != "" {
		t.Fatalf("no rollout yet: %q", p)
	}
	writeRollout(t, home, id, "/w", now, now)
	if err := os.Chtimes(dir, tick, tick); err != nil { // the same clock tick: the mtime did not move
		t.Fatal(err)
	}
	if p := rolloutPath(codex, id); p == "" {
		t.Fatal("written in the same tick as the miss: not found")
	}
}

// BenchmarkRolloutPathMiss: one look for a thread with no rollout, in a home of 200 rollouts over 70 days (the
// session list does this for every such thread every second).
func BenchmarkRolloutPathMiss(b *testing.B) {
	home := b.TempDir()
	codex := filepath.Join(home, ".codex")
	now := time.Now()
	for i := range 200 {
		day := now.AddDate(0, 0, -(i % 70))
		writeRollout(b, home, fmt.Sprintf("%08d-0000-0000-0000-000000000000", i), "/w", day, day)
	}
	ageDirs(b, codex)
	const missing = "99999999-9999-9999-9999-999999999999"
	rolloutPath(codex, missing)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if rolloutPath(codex, missing) != "" {
			b.Fatal("found")
		}
	}
}
