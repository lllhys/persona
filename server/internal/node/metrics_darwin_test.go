//go:build darwin

package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// node.info, which the hub's handshake waits for before it shows the computer online, does not wait for
// system_profiler (#6028). On some Macs it sits for over 10 s on its Activation Lock check, and a resident node makes a
// new Node, and so asks again, on every hub connection. The fakes stand in for an Intel Mac (no IORegistry name) whose
// system_profiler hangs; internal/sysmon's tests pin the model lookup's timing.
func TestNodeInfoDoesNotWaitForSystemProfiler(t *testing.T) {
	bin := t.TempDir()
	for name, script := range map[string]string{
		"ioreg":           "#!/bin/sh\nexit 0\n",
		"system_profiler": "#!/bin/sh\nexec sleep 30\n", // hangs; killed by the caller's timeout, or gone in 30 s
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	n, _, _ := herdrNode(t)

	type reply struct {
		res map[string]any
		err error
	}
	done := make(chan reply, 1)
	go func() {
		res, err := request(t, n, "node.info", map[string]any{})
		done <- reply{res, err}
	}()
	// The old read waited for system_profiler up to its 10 s timeout; the model lookup now waits a second at most.
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		sys, _ := r.res["system"].(map[string]any)
		if sys == nil || sys["model"] == "" || sys["model"] == nil {
			t.Fatalf("node.info system = %v, want a model (the identifier while the name is out)", r.res["system"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("node.info waited over 5 s for system_profiler")
	}
}
