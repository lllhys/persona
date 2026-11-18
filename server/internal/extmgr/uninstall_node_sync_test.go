package extmgr

import (
	"context"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

type failingNodeSyncPeer struct{ nodeCaller }

func (p failingNodeSyncPeer) Call(ctx context.Context, method string, params, out any) error {
	if method == "plugin.node.sync" {
		return errors.New("computer refused the node sync")
	}
	return p.nodeCaller.Call(ctx, method, params, out)
}

func TestReproUninstallReportsNodeSyncFailure(t *testing.T) {
	h, _ := extensionTestHub(t)
	reg := h.extensions.reg
	pv, err := reg.Preview(t.Context(), "dir:"+helperPackage(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	m, _ := extensionNodeMachine(t, h, "box", "linux", "amd64", false)
	if err = reg.SetConfig(t.Context(), "example-helper", []byte(`{"computers":["box"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	h.syncExtensionNodes(t.Context(), m) // finish the config change's initial start
	p := reg.Get("example-helper")
	m.mu.Lock()
	m.peer = failingNodeSyncPeer{m.peer}
	m.mu.Unlock()

	op := h.startOp(opUninstall, p.Record.ID, "test", "", true)
	if err = reg.Uninstall(t.Context(), p.Record.ID, false, "test"); err != nil {
		t.Fatal(err)
	}
	h.syncNodesFor(context.Background(), op, p)

	v := h.snapshot(op)
	step := stepOf(&v, extension.StepNodes)
	if step == nil || step.Machines == nil || step.Machines.Done != 1 || step.Machines.Total != 1 {
		t.Fatalf("unexpected machine progress: %+v", step)
	}
	if got := step.Machines.Errors["box"]; got == "" {
		t.Fatalf("uninstall lost the computer's sync failure: %+v", step.Machines)
	}
}
