package assistantskills

import (
	"context"
	"encoding/json"
	"testing"
)

func TestImportStatusReconcilesLostInstallAndUndoResponses(t *testing.T) {
	i, target := importFixture(t)
	ctx := context.Background()
	archive := skillZIP(t, zipEntry{"SKILL.md", importManifest, 0})
	preview, err := i.Import(ctx, target, ImportRequest{Op: "preview", Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	status := func(reference, want string) ImportResult {
		t.Helper()
		out, err := i.Import(ctx, target, ImportRequest{Op: "status", Reference: reference})
		raw, _ := json.Marshal(out)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		if err != nil || fields["status"] != want {
			t.Fatalf("status %s: %s %v; want %s", reference, raw, err, want)
		}
		return out
	}
	status(preview.Reference, "previewed")
	installed, err := i.Import(ctx, target, ImportRequest{Op: "install", Reference: preview.Reference, Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	// The caller may have lost the install response, including its new undo receipt.
	if got := status(preview.Reference, "installed"); got.Reference != installed.Reference {
		t.Fatal("original preview receipt did not recover the install receipt")
	}
	other := target
	other.Scope = "another assistant"
	_, err = i.Import(ctx, other, ImportRequest{Op: "status", Reference: preview.Reference})
	importErrorCode(t, err, "skill_environment_changed")
	_, err = i.Import(ctx, target, ImportRequest{Op: "install", Reference: preview.Reference, Archive: archive})
	importErrorCode(t, err, "skill_import_changed")
	undone, err := i.Import(ctx, target, ImportRequest{Op: "undo", Reference: installed.Reference})
	if err != nil {
		t.Fatal(err)
	}
	if got := status(installed.Reference, "undone"); got.Recovery != undone.Recovery {
		t.Fatal("lost undo response did not preserve recovery location")
	}
	status(preview.Reference, "undone")
	_, err = i.Import(ctx, target, ImportRequest{Op: "undo", Reference: installed.Reference})
	importErrorCode(t, err, "skill_import_request")
}
