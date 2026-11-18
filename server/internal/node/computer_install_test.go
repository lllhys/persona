package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fakeInstaller serves Cua's three installer scripts (scripts replaces some; "" leaves one out): install.sh checks its two helpers are beside it on disk, records
// its arguments and the telemetry setting, and installs a cua-driver into home that records telemetry and grant.
func fakeInstaller(t *testing.T, home string, scripts map[string]string) {
	t.Helper()
	bin := filepath.Join(home, ".local", "bin", "cua-driver")
	install := `#!/bin/bash
set -e
here="$(cd "$(dirname "$0")" && pwd)"
[ -f "$here/_install-rust.sh" ] && [ -f "$here/_install-common.sh" ] || { echo "error: helpers not on disk" >&2; exit 1; }
echo "args=$* telemetry=$CUA_DRIVER_RS_TELEMETRY_ENABLED" > "` + home + `/installed"
case "$*" in *slow*) sleep 30 ;; esac
mkdir -p "` + filepath.Dir(bin) + `"
cat > "` + bin + `" <<'BIN'
#!/bin/sh
echo "$*" >> "` + home + `/calls"
BIN
chmod +x "` + bin + `"
echo "==> cua-driver-rs 0.38.0 installed."
`
	files := map[string]string{"install.sh": install, "_install-rust.sh": "#!/bin/bash\n# checks the archive against SHA256SUMS\n",
		"_install-common.sh": "# _install-common.sh: sourced\n"}
	for k, v := range scripts {
		if v == "" {
			delete(files, k) // not published: 404
		} else {
			files[k] = v
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/driver/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	oldBase, oldOS := cuaInstallBase, installOS
	cuaInstallBase, installOS = srv.URL+"/driver/", "darwin"
	cuaBin = func() string {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
		return ""
	}
	t.Cleanup(func() {
		cuaInstallBase, installOS, cuaBin = oldBase, oldOS, findCua
		cuaJob.mu.Lock()
		if cuaJob.job != nil && cuaJob.job.State == "running" {
			cuaJob.cancel()
		}
		cuaJob.job = nil
		cuaJob.mu.Unlock()
	})
}

// waitJob waits for the node's job to be of kind and not running, and returns it.
func waitJob(t *testing.T, kind string) proto.ComputerJob {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if j := currentJob(); j != nil && j.Kind == kind && j.State != "running" {
			return *j
		}
	}
	t.Fatalf("no finished %s job: %+v", kind, currentJob())
	return proto.ComputerJob{}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

// Installed from the scripts on disk, its statistics off from the start, then asked for its permissions.
func TestInstallCua(t *testing.T) {
	home := t.TempDir()
	fakeInstaller(t, home, nil)
	if err := startJob("install", installCua); err != nil {
		t.Fatal(err)
	}
	if err := startJob("grant", grantCua); err == nil || !strings.Contains(err.Error(), "正在安装") {
		t.Fatalf("one at a time: %v", err)
	}
	if j := waitJob(t, "grant"); j.State != "done" {
		t.Fatalf("installed, then the grant ran: %+v", j)
	}
	if got := read(t, filepath.Join(home, "installed")); !strings.Contains(got, "--no-modify-path") || !strings.Contains(got, "telemetry=0") {
		t.Fatalf("the installer's arguments: %q", got)
	}
	if got := read(t, filepath.Join(home, "calls")); got != "telemetry disable\npermissions grant\n" {
		t.Fatalf("after installing: %q", got)
	}
	if err := installCua(t.Context(), func(string) {}); err == nil || !strings.Contains(err.Error(), "已经装好了") {
		t.Fatalf("not twice: %v", err)
	}
}

func TestInstallCuaRefusesWhatIsNotTheInstaller(t *testing.T) {
	for name, scripts := range map[string]map[string]string{
		"not a script":       {"install.sh": "<html>moved</html>"},
		"no checksum step":   {"_install-rust.sh": "#!/bin/bash\ncurl … | tar x\n"},
		"a helper not found": {"_install-common.sh": ""},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			fakeInstaller(t, home, scripts)
			if err := startJob("install", installCua); err != nil {
				t.Fatal(err)
			}
			if j := waitJob(t, "install"); j.State != "failed" || j.Error == "" {
				t.Fatalf("%+v", j)
			}
			if _, err := os.Stat(filepath.Join(home, "installed")); err == nil {
				t.Fatal("nothing ran")
			}
		})
	}
}

func TestInstallCuaCanBeCancelled(t *testing.T) {
	home := t.TempDir()
	fakeInstaller(t, home, nil)
	if err := startJob("install", func(ctx context.Context, step func(string)) error {
		step("下载并校验 Cua Driver，装进「应用程序」")
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if j := currentJob(); j.State != "running" {
		t.Fatalf("%+v", j)
	}
	if !cancelJob() {
		t.Fatal("a running install stops")
	}
	if j := waitJob(t, "install"); j.State != "cancelled" {
		t.Fatalf("%+v", j)
	}
	if cancelJob() {
		t.Fatal("nothing left to stop")
	}
}

// An install's last operation can succeed as cancellation lands. The cancelled install must not start a grant.
func TestInstallCuaCancellationDoesNotGrant(t *testing.T) {
	fakeInstaller(t, t.TempDir(), nil)

	// Make the asynchronous handoff deterministic: once the terminal state is visible, yield so a misplaced success
	// callback and its grant goroutine run before the assertions below. No real Cua binary is involved.
	procs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(procs) })
	grantCalled := make(chan struct{})
	cuaBin = func() string {
		close(grantCalled)
		return "/bin/true"
	}

	if err := startJob("install", func(ctx context.Context, step func(string)) error {
		<-ctx.Done()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !cancelJob() {
		t.Fatal("a running install stops")
	}
	waitFor(t, "the install job to finish", func() bool {
		j := currentJob()
		return j != nil && j.State != "running"
	})
	runtime.Gosched()
	select {
	case <-grantCalled:
		t.Fatal("grant ran after the install was cancelled")
	default:
	}
	if j := currentJob(); j == nil || j.Kind != "install" || j.State != "cancelled" {
		t.Fatalf("the cancelled install was replaced: %+v", j)
	}
}

// Cua's real installer passes the checks (PERSONA_CUA_INSTALLER_CHECK=1: fetched from cua.ai, never run).
func TestCuaInstallerPassesTheChecks(t *testing.T) {
	if os.Getenv("PERSONA_CUA_INSTALLER_CHECK") == "" {
		t.Skip("fetches from cua.ai: PERSONA_CUA_INSTALLER_CHECK=1")
	}
	for _, name := range cuaScripts {
		data, err := fetchScript(t.Context(), cuaInstallBase+name)
		if err != nil {
			t.Fatal(name, err)
		}
		if err := checkScript(name, data); err != nil {
			t.Errorf("%s (%d bytes): %v", name, len(data), err)
		}
	}
}
