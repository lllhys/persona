package hub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/update"
)

// Abruptly exit a separate process at the real adoption boundaries: no deferred
// cleanup or in-memory pending record may assist the subsequent boot.
func TestDevelopmentCrashRecovery(t *testing.T) {
	if root := os.Getenv("PERSONA_TEST_ADOPTION_ROOT"); root != "" {
		var p update.Pending
		if ok, err := update.Load(filepath.Join(root, "fixture.json"), &p); !ok || err != nil {
			t.Fatal(err)
		}
		u := newUpdater(&Hub{cfg: &Config{dir: filepath.Join(root, "runtime")}})
		u.dir = filepath.Join(root, "update")
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		cut := func(point string) {
			if os.Getenv("PERSONA_TEST_ADOPTION_CUT") == point {
				os.Exit(23)
			}
		}
		must(update.BeginDevelopmentAdoption(u.dir, p))
		must(update.PrepareDevelopmentAdoption(u.dir, &p))
		cut("prepared")
		must(update.InstallPrepared(p.Target, filepath.Join(root, "new")))
		cut("hub")
		for _, program := range p.Programs {
			must(update.InstallPrepared(program.Target, filepath.Join(root, "new")))
		}
		cut("programs")
		if os.Getenv("PERSONA_TEST_ADOPTION_CUT") == "web-intent" {
			p.Web = filepath.Join(u.h.cfg.dir, "web")
			p.WebPrev = filepath.Join(u.dir, "development", "web-previous-2")
			must(update.Save(update.DevelopmentAdoptionPath(u.dir), p))
			cut("web-intent")
		}
		must(u.detachDevelopmentWeb(&p))
		cut("web") // after rename, before pending.json
		t.Fatal("unknown crash point")
	}
	for _, point := range []string{"prepared", "hub", "programs", "web-intent", "web"} {
		for _, runningBuild := range []int64{1, 2} {
			t.Run(fmt.Sprintf("%s/build-%d", point, runningBuild), func(t *testing.T) {
				root := t.TempDir()
				web := filepath.Join(root, "runtime", "web", "index.html")
				target := filepath.Join(root, "personad")
				program := filepath.Join(root, "persona-hub")
				added := filepath.Join(root, "persona-manager")
				data := filepath.Join(root, "runtime", "history")
				for path, content := range map[string]string{target: "old hub", program: "old program", web: "old Web", data: "current history", filepath.Join(root, "new"): "new program"} {
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0700); err != nil {
						t.Fatal(err)
					}
				}
				p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target,
					Programs:    []update.Swapped{{Target: program}, {Target: added}},
					Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
				if err := update.Save(filepath.Join(root, "fixture.json"), p); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDevelopmentCrashRecovery$")
				command.Env = append(os.Environ(), "PERSONA_TEST_ADOPTION_ROOT="+root, "PERSONA_TEST_ADOPTION_CUT="+point)
				if out, err := command.CombinedOutput(); err == nil || command.ProcessState == nil || command.ProcessState.ExitCode() != 23 {
					t.Fatalf("crash fixture: %v %s", err, out)
				}
				h := newTestHub(t)
				h.build.Number = runningBuild
				u := h.upd
				u.dir = filepath.Join(root, "update")
				executed := false
				u.exec = func(path string) error {
					if path != target {
						t.Fatal("wrong restored executable", path)
					}
					executed = true
					return nil
				}
				var journal update.Pending
				if ok, err := update.Load(update.DevelopmentAdoptionPath(u.dir), &journal); !ok || err != nil {
					t.Fatal(err)
				}
				if !journal.RecoveryReady || journal.Prev == "" || len(journal.Programs) != 2 {
					t.Fatal("incomplete recovery journal")
				}
				staged := u.stagedDir(2)
				if err := os.MkdirAll(staged, 0700); err != nil {
					t.Fatal(err)
				}
				if pending := bootUpdater(t, u); pending != (runningBuild == 2) || executed != (runningBuild == 2) {
					t.Fatal("wrong recovery/re-exec disposition", pending, executed)
				}
				for path, want := range map[string]string{target: "old hub", program: "old program", web: "old Web", data: "current history"} {
					if got, err := os.ReadFile(path); err != nil || string(got) != want {
						t.Errorf("%s: %q %v", filepath.Base(path), got, err)
					}
				}
				for _, path := range []string{added, staged, update.DevelopmentAdoptionPath(u.dir)} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("recovery did not finish %s: %v", path, err)
					}
				}
				// A retry after recovery (or a crash between restoring Web and clearing
				// the journal) must not damage the already restored override.
				if err := update.RestoreAll(journal); err != nil {
					t.Fatal(err)
				}
				if bootUpdater(t, u) {
					t.Fatal("second boot replayed adoption")
				}
			})
		}
	}
}

func TestDevelopmentUnknownRecoveryIsRetained(t *testing.T) {
	for _, journal := range []string{"unready", "{", "{}"} {
		t.Run(journal, func(t *testing.T) {
			h := newTestHub(t)
			u := h.upd
			p := update.Pending{FromBuild: h.build.Number, ToBuild: 2, Target: filepath.Join(t.TempDir(), "personad"),
				Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
			if err := os.WriteFile(p.Target, []byte("untouched"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
				t.Fatal(err)
			}
			if journal != "unready" {
				if err := os.WriteFile(update.DevelopmentAdoptionPath(u.dir), []byte(journal), 0600); err != nil {
					t.Fatal(err)
				}
			}
			staged := u.stagedDir(p.ToBuild)
			if err := os.MkdirAll(staged, 0700); err != nil {
				t.Fatal(err)
			}
			if bootUpdater(t, u) {
				t.Fatal("unknown adoption replayed")
			}
			u.pruneDownloads(0)
			if got, _ := os.ReadFile(p.Target); string(got) != "untouched" {
				t.Fatal("unknown preimage restored")
			}
			if !u.adoptionUnresolved() {
				t.Fatal("unknown journal cleared")
			}
			if _, err := os.Stat(staged); err != nil {
				t.Fatal("unknown staging removed", err)
			}
		})
	}
}

func TestDevelopmentFailedRestoreRetainsRecovery(t *testing.T) {
	h := newTestHub(t)
	u := h.upd
	target := filepath.Join(t.TempDir(), "personad")
	p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target, Prev: target + ".prev",
		Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
	if err := os.WriteFile(target, []byte("new program"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := update.Save(update.PendingPath(u.dir), p); err != nil {
		t.Fatal(err)
	}
	if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
		t.Fatal(err)
	}
	staged := u.stagedDir(p.ToBuild)
	if err := os.MkdirAll(staged, 0700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(staged, "saved-node")
	if err := os.WriteFile(artifact, []byte("verified staged package"), 0600); err != nil {
		t.Fatal(err)
	}
	u.exec = func(string) error { t.Fatal("executed after failed restore"); return nil }
	u.rollback(p, "fixture: missing preimage")
	// A refused retry of the same build also must not delete retained staging.
	u.installCandidate(context.Background(), false, &update.Published{
		Manifest: &update.Manifest{Build: p.ToBuild}, Development: p.Development})
	// A normal startup must not discard material retained by a failed restore.
	u.pruneDownloads(0)
	for _, path := range []string{update.PendingPath(u.dir), update.DevelopmentAdoptionPath(u.dir), artifact} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("lost recovery file %s: %v", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(update.RolledBackPath(u.dir)); !os.IsNotExist(err) {
		t.Fatal("reported rollback before programs were restored", err)
	}
}

func TestDevelopmentCrashRecoveryFailureBlocksAttemptedBuild(t *testing.T) {
	for _, failure := range []string{"preimage", "exec"} {
		t.Run(failure, func(t *testing.T) {
			h := newTestHub(t)
			h.build.Number = 2
			u := h.upd
			target := filepath.Join(t.TempDir(), "personad")
			p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target, Prev: target + ".prev", RecoveryReady: true,
				Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
			if err := os.WriteFile(target, []byte("attempted program"), 0700); err != nil {
				t.Fatal(err)
			}
			if failure == "exec" {
				if err := os.WriteFile(p.Prev, []byte("previous program"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
				t.Fatal(err)
			}
			staged := u.stagedDir(2)
			if err := os.MkdirAll(staged, 0700); err != nil {
				t.Fatal(err)
			}
			executed := false
			u.exec = func(string) error { executed = true; return fmt.Errorf("fixture exec failed") }
			if pending, err := u.boot(); pending || err == nil {
				t.Fatal("failed crash recovery allowed attempted build startup", pending, err)
			}
			if executed != (failure == "exec") {
				t.Fatal("wrong restoration/exec boundary", executed)
			}
			u.pruneDownloads(0)
			if !u.adoptionUnresolved() {
				t.Fatal("failed recovery discarded adoption journal")
			}
			if _, err := os.Stat(staged); err != nil {
				t.Fatal("failed recovery discarded staging", err)
			}
			if err := os.WriteFile(p.Prev, []byte("previous program"), 0700); err != nil {
				t.Fatal(err)
			}
			u.exec = func(string) error { return nil }
			if !bootUpdater(t, u) || u.adoptionUnresolved() {
				t.Fatal("repaired recovery could not finish")
			}
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Fatal("successful recovery kept obsolete staging", err)
			}
		})
	}
}

// Most startup fixtures expect recovery to complete without an error.
func bootUpdater(t *testing.T, u *updater) bool {
	t.Helper()
	pending, err := u.boot()
	if err != nil {
		t.Fatal(err)
	}
	return pending
}
