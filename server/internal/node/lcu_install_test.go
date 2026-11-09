package node

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestLCUVersionFromTheReleaseDirectory(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "lcu-0.13.5-darwin-arm64")
	if err := os.MkdirAll(filepath.Join(release, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "bin", "lcu"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(dir, "current")
	if err := os.Symlink(release, current); err != nil {
		t.Fatal(err)
	}
	if got := lcuVersion(filepath.Join(current, "bin", "lcu")); got != "0.13.5" {
		t.Fatal(got)
	}
}

func TestLCUReadySaysWhatIsMissing(t *testing.T) {
	oldOS, oldArch := installOS, installArch
	installOS, installArch = "darwin", "arm64"
	t.Cleanup(func() { installOS, installArch = oldOS, oldArch })
	if ok, note := lcuReady(proto.Computer{}); ok || !strings.Contains(note, "没有装 LCU") {
		t.Fatalf("%v %s", ok, note)
	}
	if ok, note := lcuReady(proto.Computer{Installed: true}); ok || !strings.Contains(note, "ChatGPT") {
		t.Fatalf("%v %s", ok, note)
	}
	if ok, note := lcuReady(proto.Computer{Installed: true, ChatGPT: true, Desktop: "active"}); !ok || note != "" {
		t.Fatalf("ready: %v %s", ok, note)
	}
	installArch = "amd64"
	if ok, note := lcuReady(proto.Computer{Installed: true, ChatGPT: true}); ok || !strings.Contains(note, "Apple") {
		t.Fatalf("%v %s", ok, note)
	}
}

func TestLCUInstallVerifiesTheArchiveAndDoesNotRegisterAHarness(t *testing.T) {
	script := []byte("#!/bin/sh\n# --runtime-only is the install mode that skips harness registration\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := script
	hdr := &tar.Header{Name: "lcu-1.2.3-darwin-arm64/scripts/install.sh", Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil || gz.Close() != nil {
		t.Fatal("tar")
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	sidecar := hex.EncodeToString(sum[:]) + "  lcu-1.2.3-darwin-arm64.tar.gz\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/amontlabs/lcu/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": []map[string]string{
			{"name": "lcu-1.2.3-darwin-arm64.tar.gz", "browser_download_url": "http://" + r.Host + "/archive"},
			{"name": "lcu-1.2.3-darwin-arm64.tar.gz.sha256", "browser_download_url": "http://" + r.Host + "/sha"},
		}})
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/sha", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(sidecar)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldAPI, oldBin, oldApp, oldOS, oldArch, oldRun := lcuReleaseAPI, lcuBin, chatgptApp, installOS, installArch, lcuInstallRun
	lcuReleaseAPI = srv.URL + "/repos/amontlabs/lcu/releases/latest"
	lcuBin = func() string { return "" }
	chatgptApp = func() bool { return true }
	installOS, installArch = "darwin", "arm64"
	var root string
	lcuInstallRun = func(ctx context.Context, dir string) error {
		root = dir
		if b, err := os.ReadFile(filepath.Join(dir, "scripts", "install.sh")); err != nil || !bytes.Contains(b, []byte("--runtime-only")) {
			t.Errorf("extracted installer: %v %s", err, b)
		}
		args := lcuInstallArgs(dir)
		if strings.Join(args, " ") != "/bin/bash "+filepath.Join(dir, "scripts", "install.sh")+" --runtime-only --yes" {
			t.Errorf("args: %q", args)
		}
		for _, a := range args {
			if a == "--agent" || strings.Contains(a, "claude") || strings.Contains(a, "codex") {
				t.Errorf("harness registration: %q", args)
			}
		}
		return nil
	}
	t.Cleanup(func() {
		lcuReleaseAPI, lcuBin, chatgptApp, installOS, installArch, lcuInstallRun = oldAPI, oldBin, oldApp, oldOS, oldArch, oldRun
	})
	if err := installLCU(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if root == "" {
		t.Fatal("installer did not run")
	}

	chatgptApp = func() bool { return false }
	if err := installLCU(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "ChatGPT") {
		t.Fatalf("missing app: %v", err)
	}
}

func TestLCUArchiveChecksumAndTraversal(t *testing.T) {
	if err := checkSHA256([]byte("hello"), []byte("nope  file\n")); err == nil {
		t.Fatal("bad checksum")
	}
	sum := sha256.Sum256([]byte("hello"))
	if err := checkSHA256([]byte("hello"), []byte(hex.EncodeToString(sum[:])+"  file\n")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("x")
	_ = tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1})
	_, _ = tw.Write(payload)
	_ = tw.Close()
	_ = gz.Close()
	if _, err := extractLCU(t.TempDir(), buf.Bytes()); err == nil || !strings.Contains(err.Error(), "不安全") {
		t.Fatalf("%v", err)
	}
}
