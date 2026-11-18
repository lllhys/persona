package orcart_test

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
)

func TestCreateTerminal(t *testing.T) {
	fake := orcarttest.New(t)
	c := &orcart.Client{MetaPath: fake.MetaPath}

	created, err := c.CreateTerminal(context.Background(), "id:wt-1", "my-tab", "mut-1", "")
	if err != nil {
		t.Fatalf("CreateTerminal failed: %v", err)
	}
	if created.Handle == "" {
		t.Errorf("expected non-empty handle, got empty")
	}
	if created.WorktreeID != "wt-1" {
		t.Errorf("expected WorktreeID wt-1, got %s", created.WorktreeID)
	}
	if created.Title != "my-tab" {
		t.Errorf("expected Title my-tab, got %s", created.Title)
	}
	if !created.IsLocal() {
		t.Errorf("expected terminal to be local")
	}
	// The user's default shell: no shell in the request, which an Orca without shell selection would ignore anyway.
	if _, ok := fake.Params("terminal.create")[0]["shell"]; ok {
		t.Errorf("asked for a shell nobody chose: %v", fake.Params("terminal.create")[0])
	}
	if _, err := c.CreateTerminal(context.Background(), "id:wt-1", "bash-tab", "mut-2", orcart.GitBash); err != nil {
		t.Fatalf("CreateTerminal with a shell failed: %v", err)
	}
	if got := fake.Params("terminal.create")[1]["shell"]; got != "git-bash" {
		t.Errorf("shell = %v, want git-bash", got)
	}
}

func TestRenameAndPaneTitle(t *testing.T) {
	fake := orcarttest.New(t)
	c := &orcart.Client{MetaPath: fake.MetaPath}

	created, err := c.CreateTerminal(context.Background(), "id:wt-1", "old-name", "mut-1", "")
	if err != nil {
		t.Fatalf("CreateTerminal failed: %v", err)
	}

	target := created.Target()
	if err := c.RenameTerminal(context.Background(), target, "new-tab-name"); err != nil {
		t.Fatalf("RenameTerminal failed: %v", err)
	}

	if err := c.SetPaneTitle(context.Background(), target, "pane-name"); err != nil {
		t.Fatalf("SetPaneTitle failed: %v", err)
	}
}
