package extmgr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

// An automatic upgrade whose plan is refused drops the package it staged: nobody retries that preview (the next try
// waits for a fresh index and makes its own), so it would stay on disk until another preview or a restart.
func TestExtensionAutoUpgradeRefusedPlanDropsStaging(t *testing.T) {
	h, mux, phone, mk, dir, mid := upgradeHub(t)
	ctx := context.Background()
	u := h.extensionUpgrades()

	setUIVersion(t, dir, "1.1.0")
	mk.publish(dir)
	if _, err := h.marketRead(ctx, mid); err != nil {
		t.Fatal(err)
	}
	h.extensions.reg.PlanGuard = func(context.Context, []*extension.Preview) (func(), error) {
		return nil, errors.New("held")
	}
	u.upgrade(ctx)

	if v := installedVersion(h); v != "1.0.0" {
		t.Fatalf("upgraded through a refused plan: %s", v)
	}
	if left := stagedPreviews(t, filepath.Join(h.cfg.dir, "extensions", "staging")); len(left) > 0 {
		t.Fatalf("staged package left behind: %v", left)
	}
	// The failure is still recorded and shown, so the next try waits for a fresh index.
	u.mu.Lock()
	failed, note := u.failed["example-ui"], u.settings.Last["example-ui"]
	u.mu.Unlock()
	if failed != "1.1.0" || note.Kind != upgradeFailed || note.To != "1.1.0" || !strings.Contains(note.Detail, "held") {
		t.Fatalf("failure not recorded: failed=%q note=%+v", failed, note)
	}
	if up := upgradeOf(t, mux, phone); up == nil || up["state"] != upgradeFailed {
		t.Fatalf("failure not exposed: %v", up)
	}
}

// stagedPreviews are the preview directories under root (plan-* backups and other prefixed names are not previews).
func stagedPreviews(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "plan-") {
			out = append(out, e.Name())
		}
	}
	return out
}
