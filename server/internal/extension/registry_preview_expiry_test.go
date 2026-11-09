package extension

import (
	"context"
	"os"
	"testing"
	"time"
)

// DropExpiredPreviews removes a preview past its time with its staged package, without another preview being made or
// used; a preview still in time stays.
func TestDropExpiredPreviewsRemovesTheirStaging(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newTestRegistry(t)
	pkg := t.TempDir()
	writePackage(t, pkg, nil)
	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.staging == "" {
		t.Fatal("a package preview has no staging directory")
	}

	r.DropExpiredPreviews()
	if _, err := os.Stat(pv.staging); err != nil {
		t.Fatal("a preview still in time is removed:", err)
	}
	r.mu.Lock()
	kept := r.previews[pv.ID] != nil
	r.mu.Unlock()
	if !kept {
		t.Fatal("a preview still in time is forgotten")
	}

	r.now = func() time.Time { return time.Now().Add(previewFor + time.Minute) }
	r.DropExpiredPreviews()
	if _, err := os.Stat(pv.staging); !os.IsNotExist(err) {
		t.Fatal("an expired preview's staged package is left:", err)
	}
	r.mu.Lock()
	gone := r.previews[pv.ID] == nil
	r.mu.Unlock()
	if !gone {
		t.Fatal("an expired preview is still waiting")
	}
}
