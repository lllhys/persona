package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

func TestUpdateDelayedProbeCannotForceAnotherAttempt(t *testing.T) {
	h := serviceCase{programs: []string{"persona-manager"}, files: map[string]string{"user/old.service": unitRunningHub}}.hub(t)
	u := h.upd
	if _, err := u.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	setSessions(h.machine, proto.Session{SessionID: "busy", Status: proto.StatusWorking})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probing, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var calls atomic.Int32
	u.services.state = func(bool, string) (bool, bool) {
		if calls.Add(1) == 1 {
			close(probing)
			<-release
		}
		return false, true
	}
	done := make(chan error, 1)
	go func() { done <- u.begin(ctx, true) }()
	select {
	case <-probing:
	case <-time.After(time.Second):
		t.Fatal("delayed request did not reach service discovery")
	}
	if err := u.begin(ctx, false); err != nil {
		t.Fatal(err)
	}
	u.mu.Lock()
	acceptedNormally := u.running && !u.now
	u.mu.Unlock()
	if !acceptedNormally {
		t.Fatal("ordinary attempt was not reserved with its own idle policy")
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		u.mu.Lock()
		forced := u.now
		u.mu.Unlock()
		if !errors.Is(err, errUpdateBusy) || forced {
			t.Fatalf("delayed request changed another attempt: err=%v now=%v", err, forced)
		}
	case <-time.After(time.Second):
		t.Fatal("delayed request did not finish")
	}
	cancel()
	waitFor(t, "cancelled ordinary attempt", func() bool { u.mu.Lock(); defer u.mu.Unlock(); return !u.running })
}

func TestRestoreUpdateFailureRetainsRecovery(t *testing.T) {
	for _, failure := range []string{"hub", "companion", "web"} {
		t.Run(failure, func(t *testing.T) {
			h := newTestHub(t)
			u := h.upd
			if err := os.MkdirAll(u.dir, 0700); err != nil {
				t.Fatal(err)
			}
			target, companion := filepath.Join(u.dir, "personad"), filepath.Join(u.dir, "persona-hub")
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{target, companion} {
				write(path, "new")
				write(path+".prev", "old")
			}
			p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target, Prev: target + ".prev", Run: companion,
				Programs: []update.Swapped{{Target: companion, Prev: companion + ".prev"}}, RecoveryReady: true,
				Development: &update.DevelopmentCandidate{Commit: gitA}}
			if failure == "web" {
				p.Web, p.WebPrev = filepath.Join(u.dir, "web"), filepath.Join(u.dir, "web-previous")
				for _, path := range []string{p.Web, p.WebPrev} {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					write(filepath.Join(path, "index.html"), "Web")
				}
			} else {
				missing := p.Prev
				if failure == "companion" {
					missing = companion + ".prev"
				}
				if err := os.Remove(missing); err != nil {
					t.Fatal(err)
				}
			}
			if err := update.Save(update.PendingPath(u.dir), p); err != nil {
				t.Fatal(err)
			}
			if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
				t.Fatal(err)
			}
			staged := u.stagedDir(2)
			if err := os.MkdirAll(staged, 0700); err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(staged, "node"), "retained package")
			before := map[string][]byte{}
			for _, path := range []string{update.PendingPath(u.dir), update.DevelopmentAdoptionPath(u.dir)} {
				before[path], _ = os.ReadFile(path)
			}
			if bin, ok, err := restoreUpdateIn(u.dir, 2, errors.New("fixture activation failure")); err == nil || ok || bin != "" {
				t.Fatalf("failed restore selected executable %q %v", bin, ok)
			}
			u.pruneDownloads(0)
			for path, want := range before {
				if got, err := os.ReadFile(path); err != nil || string(got) != string(want) {
					t.Fatal("failed recovery changed journal", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join(staged, "node")); err != nil {
				t.Fatal("staging lost", err)
			}
			if _, err := os.Stat(update.RolledBackPath(u.dir)); !os.IsNotExist(err) {
				t.Fatal("failed restore reported rollback", err)
			}
			if failure == "web" {
				if err := os.Rename(p.Web, p.Web+".conflict"); err != nil {
					t.Fatal(err)
				}
			} else {
				write(p.Prev, "old")
				write(companion+".prev", "old")
			}
			if bin, ok, err := restoreUpdateIn(u.dir, 2, nil); err != nil || !ok || bin != companion {
				t.Fatal("repaired recovery did not select restored Hub", bin, ok)
			}
			for _, path := range []string{target, companion} {
				if got, err := os.ReadFile(path); err != nil || string(got) != "old" {
					t.Fatal("program not restored", path, err)
				}
			}
			for path := range before {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("successful recovery kept journal", path, err)
				}
			}
		})
	}
}

func TestUpdateSourceRejectsEveryInvalidValueWithoutSideEffects(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, current := range []string{"", "release", "web-dev", "invalid-existing"} {
			for _, invalid := range []string{"", "invalid-existing", "Release", " web-dev"} {
				t.Run(fmt.Sprintf("active=%v/current=%q/input=%q", active, current, invalid), func(t *testing.T) {
					u, st, _ := updaterWithStoredSettings(t)
					u.h.cfg.Update.Development = &update.DevelopmentConfig{Backup: update.DevelopmentBackupConfig{Quiesce: []string{"pause"}, Resume: []string{"resume"}}}
					if err := u.load(context.Background()); err != nil {
						t.Fatal(err)
					}
					u.settings.Source = current
					u.running = active
					before := u.settings
					stored := append([]byte(nil), st.vals[updateSettingsKey]...)
					candidate := &update.Published{Manifest: &update.Manifest{Version: "selected", Build: 2}}
					u.release = candidate
					body, _ := json.Marshal(map[string]any{"source": invalid, "auto": true, "auto_update_nodes": true})
					w := nodePolicyRequest(t, u, http.MethodPut, string(body))
					if w.Code != http.StatusConflict || !reflect.DeepEqual(u.settings, before) || string(st.vals[updateSettingsKey]) != string(stored) || u.release != candidate {
						t.Fatalf("invalid source changed settings/selection: status=%d before=%+v after=%+v", w.Code, before, u.settings)
					}
				})
			}
		}
	}
}

func TestUpdateNowConfirmsAcceptedWaitingAttempt(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, _, execs := updatingHub(t, p, "release")
	setSessions(m, proto.Session{SessionID: "busy", Status: proto.StatusWorking})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "accepted idle wait", func() bool { return h.upd.view().State == "waiting_idle" })
	if len(execs()) != 0 {
		t.Fatal("ordinary attempt interrupted a busy session")
	}
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "explicitly confirmed restart", func() bool { return len(execs()) == 1 })
}
