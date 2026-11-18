package node

import (
	"os"
	"path/filepath"
	"testing"
)

func herdrDeletionFixture(t *testing.T, linked bool) (worktreeFixture, string, string) {
	t.Helper()
	w := worktreeNode(t)
	path, id := w.repo, "w1"
	if linked {
		path, id = filepath.Join(w.home, "topic"), "w2"
		runPullGit(t, w.repo, "worktree", "add", "-b", "topic", path)
	}
	show := func(present bool) {
		spaces, tabs, panes := []any{}, []any{}, []any{}
		if present {
			spaces = append(spaces, map[string]any{"workspace_id": id, "label": "Example", "cwd": path})
			tabs = append(tabs, map[string]any{"workspace_id": id, "tab_id": id + ":t1"})
			panes = append(panes, map[string]any{"workspace_id": id, "tab_id": id + ":t1", "pane_id": id + ":p1", "cwd": path})
		}
		w.f.reply("session.snapshot", snapshot(spaces, tabs, nil, panes))
	}
	show(true)
	w.f.handle("workspace.close", func(p map[string]any) any {
		if str(p["workspace_id"]) != id {
			return herdrFail{Code: "wrong_target", Message: "Wrong workspace"}
		}
		show(false)
		return map[string]any{}
	})
	w.f.handle("worktree.remove", func(p map[string]any) any {
		if str(p["workspace_id"]) != id {
			return herdrFail{Code: "wrong_target", Message: "Wrong workspace"}
		}
		args := []string{"worktree", "remove"}
		if p["force"] == true {
			args = append(args, "--force")
		}
		if _, err := git(t.Context(), w.repo, append(args, "--", path)...); err != nil {
			return herdrFail{Code: "remove_failed", Message: err.Error()}
		}
		show(false)
		return map[string]any{}
	})
	return w, id, path
}

func herdrDeletionPreview(t *testing.T, w worktreeFixture, id string, closeOnly bool) string {
	t.Helper()
	res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview": true, "close_only": closeOnly})
	if err != nil {
		t.Fatal(err)
	}
	if res["kind"] != "herdr" || res["folder"] != closeOnly {
		t.Fatal(res)
	}
	return str(res["preview_token"])
}

func TestHerdrWorkspaceCloseKeepsMainCheckoutAndConsumesConfirmation(t *testing.T) {
	w, id, path := herdrDeletionFixture(t, false)
	token := herdrDeletionPreview(t, w, id, true)
	body := map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "close_only": true}
	res, err := request(t, w.n, "worktree.delete", body)
	if err != nil || res["removed"] != true {
		t.Fatalf("%v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(path, "app.txt")); err != nil {
		t.Fatal(err)
	}
	if branchOf(t, path) != "main" {
		t.Fatal("main branch changed")
	}
	if _, err := request(t, w.n, "worktree.delete", body); err == nil {
		t.Fatal("confirmation dispatched twice")
	}
	body["reconcile"] = true
	if res, err := request(t, w.n, "worktree.delete", body); err != nil || res["removed"] != true {
		t.Fatalf("reconciliation: %v %v", res, err)
	}
	if got := w.f.trace("workspace.close", "worktree.remove"); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestHerdrLinkedWorktreeRemovalRetainsBranchByDefault(t *testing.T) {
	for _, deleteBranch := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "merged-branch"}[deleteBranch], func(t *testing.T) {
			w, id, path := herdrDeletionFixture(t, true)
			token := herdrDeletionPreview(t, w, id, false)
			res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "delete_branch": deleteBranch})
			if err != nil || res["removed"] != true || res["branch_deleted"] == true != deleteBranch {
				t.Fatalf("%v %v", res, err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("checkout remains: %v", err)
			}
			if branchExists(t.Context(), w.repo, "topic") == deleteBranch {
				t.Fatal("branch policy mismatch")
			}
		})
	}
}

func TestHerdrDeletionRefusesMainCheckoutAndChangedDirtyPreview(t *testing.T) {
	w, id, _ := herdrDeletionFixture(t, false)
	if _, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview": true}); err == nil {
		t.Fatal("main checkout accepted")
	}
	w, id, path := herdrDeletionFixture(t, true)
	token := herdrDeletionPreview(t, w, id, false)
	if err := os.WriteFile(filepath.Join(path, "app.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "force": true}); err == nil {
		t.Fatal("changed preview was forced")
	}
	token = herdrDeletionPreview(t, w, id, false)
	if _, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token}); err == nil {
		t.Fatal("dirty work removed without confirmation")
	}
	res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "force": true})
	if err != nil || res["removed"] != true {
		t.Fatalf("confirmed discard: %v %v", res, err)
	}
}

func TestHerdrClosingDoesNotRequireExistingDirectoryOrAuthorizeDeletion(t *testing.T) {
	w, id, path := herdrDeletionFixture(t, false)
	token := herdrDeletionPreview(t, w, id, true)
	if _, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token}); err == nil {
		t.Fatal("close confirmation authorized checkout deletion")
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "close_only": true})
	if err != nil || res["removed"] != true {
		t.Fatalf("close missing folder: %v %v", res, err)
	}
}

func TestHerdrDeletionRequiresObservedRemoval(t *testing.T) {
	w, id, _ := herdrDeletionFixture(t, true)
	w.f.reply("worktree.remove", map[string]any{})
	token := herdrDeletionPreview(t, w, id, false)
	body := map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token}
	res, err := request(t, w.n, "worktree.delete", body)
	if err != nil || res["removed"] == true || res["state"] != "present" {
		t.Fatalf("unverified acknowledgement: %v %v", res, err)
	}
	body["reconcile"] = true
	if _, err := request(t, w.n, "worktree.delete", body); err != nil {
		t.Fatal(err)
	}
	if got := w.f.trace("worktree.remove"); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestHerdrClosePreviewUsesTheCatalogsCanonicalDirectory(t *testing.T) {
	w, id, path := herdrDeletionFixture(t, false)
	link := filepath.Join(w.home, "alias")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	w.f.reply("session.snapshot", snapshot([]any{map[string]any{"workspace_id": id, "cwd": link}}, nil, nil, nil))
	res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview": true, "close_only": true})
	if err != nil || res["path"] != realPath(path) {
		t.Fatalf("canonical preview: %v %v", res, err)
	}
}

func TestHerdrParentCloseExplainsNativeGroupRequirement(t *testing.T) {
	w, id, _ := herdrDeletionFixture(t, false)
	w.f.reply("workspace.close", herdrFail{Code: "workspace_group_close_required", Message: "Group confirmation required"})
	token := herdrDeletionPreview(t, w, id, true)
	_, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "close_only": true})
	if err == nil || refusalCode(err) != "worktree_herdr_children" {
		t.Fatal(err)
	}
}

func TestHerdrDeletionKeepsUnmergedBranchAndRejectsChangedWorkspace(t *testing.T) {
	w, id, path := herdrDeletionFixture(t, true)
	if err := os.WriteFile(filepath.Join(path, "app.txt"), []byte("topic commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPullGit(t, path, "add", "app.txt")
	runPullGit(t, path, "commit", "-m", "topic work")
	token := herdrDeletionPreview(t, w, id, false)
	res, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "delete_branch": true})
	if err != nil || res["removed"] != true || res["branch_deleted"] == true || !branchExists(t.Context(), w.repo, "topic") {
		t.Fatalf("unmerged branch: %v %v", res, err)
	}
	w, id, _ = herdrDeletionFixture(t, false)
	token = herdrDeletionPreview(t, w, id, true)
	w.f.reply("session.snapshot", snapshot([]any{map[string]any{"workspace_id": id, "cwd": "/different"}}, nil, nil, nil))
	if _, err := request(t, w.n, "worktree.delete", map[string]any{"kind": "herdr", "workspace_id": id, "preview_token": token, "close_only": true}); err == nil {
		t.Fatal("replaced workspace accepted")
	}
	if len(w.f.trace("workspace.close")) != 0 {
		t.Fatal("replaced workspace closed")
	}
}
