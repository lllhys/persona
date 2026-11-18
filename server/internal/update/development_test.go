package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentWebRecoveryPreservesConflictingOverride(t *testing.T) {
	dir := t.TempDir()
	p := Pending{RecoveryReady: true, Web: filepath.Join(dir, "web"), WebPrev: filepath.Join(dir, "previous")}
	for _, path := range []string{p.Web, p.WebPrev} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "index.html"), []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RestoreAll(p); err == nil {
		t.Fatal("overwrote a changed override")
	}
	for _, path := range []string{p.Web, p.WebPrev} {
		if got, err := os.ReadFile(filepath.Join(path, "index.html")); err != nil || string(got) != path {
			t.Fatal("changed Web recovery contents", err)
		}
	}
}

func TestDevelopmentWatchRetainsJournalUntilRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"build":2,"update":{"confirmed":false}}`))
	}))
	defer srv.Close()
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "restarted", true: "restart failed"}[failed], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "personad")
			if err := os.WriteFile(target, []byte("old program"), 0700); err != nil {
				t.Fatal(err)
			}
			p := Pending{FromBuild: 1, ToBuild: 2, Target: target, HealthURL: srv.URL,
				Development: &DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
			if err := BeginDevelopmentAdoption(dir, p); err != nil {
				t.Fatal(err)
			}
			if err := PrepareDevelopmentAdoption(dir, &p); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("new program"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := Save(PendingPath(dir), p); err != nil {
				t.Fatal(err)
			}
			journal, err := os.ReadFile(DevelopmentAdoptionPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			restartFailure := errors.New("restored program could not start")
			restarted := false
			err = Watch(context.Background(), srv.Client(), p, PendingPath(dir), time.Now(), time.Millisecond,
				func() error { return RestoreAll(p) }, func() error {
					restarted = true
					if got, err := os.ReadFile(target); err != nil || string(got) != "old program" {
						t.Fatal("restart did not receive the restored program", err)
					}
					if got, err := os.ReadFile(DevelopmentAdoptionPath(dir)); err != nil || string(got) != string(journal) {
						t.Error("recovery journal was removed before restart", err)
					}
					if failed {
						return restartFailure
					}
					return nil
				})
			if !restarted || (failed && !errors.Is(err, restartFailure)) || (!failed && err != nil) {
				t.Fatal("unexpected restart outcome", err)
			}
			got, readErr := os.ReadFile(DevelopmentAdoptionPath(dir))
			if failed {
				if readErr != nil || string(got) != string(journal) {
					t.Fatal("failed restart lost recovery journal", readErr)
				}
				// The retained preimage remains reusable by the next Hub boot.
				if err := RestoreAll(p); err != nil {
					t.Fatal("recovery could not retry", err)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatal("successful restart retained the journal", readErr)
			}
		})
	}
}

func TestDevelopmentAdoptionFenceMatchesExactAttempt(t *testing.T) {
	dir := t.TempDir()
	p := Pending{ToBuild: 202611130059, Development: &DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
	if err := BeginDevelopmentAdoption(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := BeginDevelopmentAdoption(dir, p); err == nil {
		t.Fatal("replayed unknown adoption")
	}
	other := p
	other.ToBuild++
	ClearDevelopmentAdoption(dir, other)
	if _, err := os.Stat(DevelopmentAdoptionPath(dir)); err != nil {
		t.Fatal("another attempt cleared the fence")
	}
	ClearDevelopmentAdoption(dir, p)
	if _, err := os.Stat(DevelopmentAdoptionPath(dir)); !os.IsNotExist(err) {
		t.Fatal("matching fence retained", err)
	}
	if got := DevelopmentNumber(p.ToBuild); got <= p.ToBuild {
		t.Fatal("non-monotonic identity", got)
	}
}
