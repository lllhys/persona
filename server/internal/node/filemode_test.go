package node

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestChangeMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits on Windows")
	}
	n, home := testNode(t)
	makeFile(t, filepath.Join(home, "proj", "run.sh"), "#!/bin/sh\n")
	out, err := makeCall(t, n, "file.chmod", map[string]any{"path": "~/proj/run.sh", "mode": 0o755})
	if err != nil || out["mode"] != float64(0o755) {
		t.Fatalf("chmod = %v, %v", out, err)
	}
	if st, _ := os.Stat(filepath.Join(home, "proj", "run.sh")); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	// Folders too; a link changes what it leads to.
	if err := os.Symlink("run.sh", filepath.Join(home, "proj", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := makeCall(t, n, "file.chmod", map[string]any{"path": "~/proj/link", "mode": 0o700}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(home, "proj", "run.sh")); st.Mode().Perm() != 0o700 {
		t.Fatalf("through the link: mode %o", st.Mode().Perm())
	}
	// No special bits, nothing past 0777, nothing outside the home, not the home itself.
	for _, bad := range []int{0o4755, 0o1777, -1} {
		if _, err := makeCall(t, n, "file.chmod", map[string]any{"path": "~/proj/run.sh", "mode": bad}); err == nil {
			t.Fatalf("mode %o accepted", bad)
		}
	}
	_, err = makeCall(t, n, "file.chmod", map[string]any{"path": "~", "mode": 0o700})
	wantCode(t, err, proto.CodeReadOnly)
	other := filepath.Join(t.TempDir(), "x")
	makeFile(t, other, "x")
	_, err = makeCall(t, n, "file.chmod", map[string]any{"path": other, "mode": 0o600})
	wantCode(t, err, proto.CodeReadOnly)
	_, err = makeCall(t, n, "file.chmod", map[string]any{"path": "~/proj/nope", "mode": 0o600})
	wantCode(t, err, proto.CodeNotFound)
	if _, err := makeCall(t, n, "file.chmod", map[string]any{"path": "~/proj/run.sh"}); err == nil {
		t.Fatal("no mode accepted")
	}
}
