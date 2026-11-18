package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestTranscriptionReadinessInvalidatedOnRegistryChange(t *testing.T) {
	h, _ := extensionTestHub(t)
	var changes atomic.Int64
	h.opts.TranscriptionChanged = func() { changes.Add(1) }
	mux, token := h.routes(), h.cfg.Token
	pkg := notesPackage(t, "1.0.0")
	code, preview := call(t, mux, "POST", "/api/v1/extensions/-/preview", token, `{"source":"dir:`+pkg+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, preview)
	}
	body := fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, preview["preview_id"], preview["permissions_digest"])
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", token, body); code != 200 {
		t.Fatalf("install: %d %v", code, out)
	}
	if changes.Load() == 0 {
		t.Fatal("install did not invalidate readiness")
	}
	ctx := context.Background()
	for _, change := range []struct {
		name string
		run  func() error
	}{
		{"configuration", func() error {
			return h.extensions.reg.SetConfig(ctx, "example-notes", json.RawMessage(`{"folders":["work"]}`), "test")
		}},
		{"disabled", func() error { return h.extensions.reg.SetEnabled(ctx, "example-notes", false, "test") }},
		{"enabled", func() error { return h.extensions.reg.SetEnabled(ctx, "example-notes", true, "test") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			before := changes.Load()
			if err := change.run(); err != nil {
				t.Fatal(err)
			}
			if changes.Load() <= before {
				t.Fatal("registry change did not invalidate readiness")
			}
		})
	}
}
