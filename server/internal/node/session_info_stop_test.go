package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestSessionInfoReportsSavedStopCause(t *testing.T) {
	for _, source := range []string{"archived rollout", "history unavailable"} {
		t.Run(source, func(t *testing.T) {
			n := remoteTestNode(t)
			if source == "archived rollout" {
				writeRollout(t, n.home, remoteRaw, n.home, time.Now(), time.Now())
				active := rolloutPath(n.codexHome(), remoteRaw)
				archived := filepath.Join(n.codexHome(), "archived_sessions", filepath.Base(active))
				if err := os.MkdirAll(filepath.Dir(archived), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(active, archived); err != nil {
					t.Fatal(err)
				}
				if rows := n.codexSessions(map[string]bool{}); len(rows) != 0 {
					t.Fatalf("archived rollout still listed: %+v", rows)
				}
			} else {
				n.codices.threads[remoteRaw] = codexSource{home: n.codexHome(), thread: remoteRaw}
			}
			for _, tc := range []struct {
				name  string
				cause proto.StopCause
				saved bool
			}{
				{"no saved state", proto.StopUnknown, false},
				{"completed", proto.StopTaskCompleted, true},
				{"idle", proto.StopIdle, true},
				{"restart", proto.StopRestart, true},
				{"user", proto.StopUser, true},
				{"unknown", proto.StopUnknown, true},
				{"unsupported", "future-cause", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.saved {
						n.state.put(remoteRaw, sessionState{Launcher: "codex", Cwd: n.home, StopCause: tc.cause})
					}
					info, err := n.sessionInfo(t.Context(), remoteRaw)
					if err != nil {
						t.Fatal(err)
					}
					if got, present := info["stop_cause"]; !present || got != tc.cause {
						t.Fatalf("stop_cause=%v present=%v, want saved %q", got, present, tc.cause)
					}
					if source == "history unavailable" && info["history_unavailable"] != true {
						t.Fatalf("did not exercise missing-history reply: %v", info)
					}
					if got := n.stopCause(remoteRaw); got != tc.cause {
						t.Fatalf("info changed saved provenance: %q", got)
					}
				})
			}
		})
	}
}
