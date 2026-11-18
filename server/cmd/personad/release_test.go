package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release manifest no longer takes --min-node: it wrote a key nothing read, so a release could ask for a node order
// that never happened (docs/operations/hub-update.md, #3226).
func TestReleaseManifestHasNoMinNodeFlag(t *testing.T) {
	err := runRelease([]string{"manifest", "--min-node", "209901010000"})
	if err == nil || !strings.Contains(err.Error(), "min-node") {
		t.Fatalf("--min-node is still accepted: %v", err)
	}
}

// A served latest.json that is there but cannot be read fails check-next instead of counting as nothing served: a
// nil manifest passes the check whatever the build and commit, and publish.sh would publish an older build.
func TestReleaseCheckNextUnreadableServed(t *testing.T) {
	served := t.TempDir() // a directory: reading it fails
	err := runRelease([]string{"check-next", served, "--build", "209901010000", "--commit", "abc"})
	if err == nil || !strings.Contains(err.Error(), served) {
		t.Fatalf("want an error naming %s, got %v", served, err)
	}
	var ue usageError
	if errors.As(err, &ue) {
		t.Fatalf("a read failure reported as a usage mistake: %v", err)
	}
}

// Nothing served yet, a missing file or the empty one publish.sh writes when the fetch fails, still passes.
func TestReleaseCheckNextNothingServed(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "served.json")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, served := range []string{filepath.Join(dir, "missing.json"), empty} {
		if err := runRelease([]string{"check-next", served, "--build", "209901010000", "--commit", "abc"}); err != nil {
			t.Fatalf("%s: %v", served, err)
		}
	}
}

// An argument left after the flags is a usage mistake: flag parsing stops there and would drop the flags after it.
func TestReleaseRefusesStrayArguments(t *testing.T) {
	served := filepath.Join(t.TempDir(), "missing.json")
	for _, args := range [][]string{
		{"check-next", served, "--build", "209901010000", "extra", "--commit", "abc"},
		{"manifest", "--dir", "d", "--out", "o", "--build", "209901010000", "extra"},
	} {
		var ue usageError
		if err := runRelease(args); !errors.As(err, &ue) || !strings.Contains(err.Error(), `"extra"`) {
			t.Fatalf("%v: want a usage error naming the argument, got %v", args, err)
		}
	}
}

// manifest without --dir or --out is a usage mistake naming the flag, before anything is read or written.
func TestReleaseManifestNeedsDirAndOut(t *testing.T) {
	for _, tc := range []struct {
		args []string
		flag string
	}{
		{[]string{"manifest", "--out", "o", "--build", "209901010000", "--commit", "abc"}, "--dir"},
		{[]string{"manifest", "--dir", "d", "--build", "209901010000", "--commit", "abc"}, "--out"},
	} {
		var ue usageError
		if err := runRelease(tc.args); !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.flag) {
			t.Fatalf("%v: want a usage error naming %s, got %v", tc.args, tc.flag, err)
		}
	}
}

func TestReleaseManifestBetaChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the binary is a shell script")
	}
	commit, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not in a git checkout:", err)
	}
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	fmt.Fprintf(zw, "#!/bin/sh\necho 0.7.0-beta.1\necho 'channel release, build 202611170910'\n")
	zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "personad-"+runtime.GOOS+"-"+runtime.GOARCH+".gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "latest.json")
	if err := runRelease([]string{"manifest", "--dir", dir, "--version", "0.7.0-beta.1", "--build", "202611170910",
		"--commit", strings.TrimSpace(string(commit)), "--channel", "beta", "--out", out}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(body), `"channel": "beta"`) {
		t.Fatalf("beta manifest: %s, %v", body, err)
	}
	if err := runRelease([]string{"verify", "--dir", dir, "--manifest", out}); err != nil {
		t.Fatal(err)
	}
}

// release manifest refuses downloads of another build than the one it is told, and writes nothing: v0.2.1's
// latest.json said build 202611110749 over binaries of 202611110745, and every hub refused it after downloading
// (#4522). release verify checks a written latest.json the same way, as publish.sh does before uploading.
func TestReleaseManifestAndVerifyRefuseAnotherBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the binary is a shell script")
	}
	commit, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not in a git checkout:", err)
	}
	dir := t.TempDir()
	writeBinary := func(build string) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		fmt.Fprintf(zw, "#!/bin/sh\necho 0.2.1\necho 'channel release, build %s'\n", build)
		zw.Close()
		if err := os.WriteFile(filepath.Join(dir, "personad-"+runtime.GOOS+"-"+runtime.GOARCH+".gz"), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "latest.json")
	manifest := func(build string) error {
		return runRelease([]string{"manifest", "--dir", dir, "--version", "0.2.1", "--build", build,
			"--commit", strings.TrimSpace(string(commit)), "--out", out})
	}
	writeBinary("202611110745")
	if err := manifest("202611110749"); err == nil || !strings.Contains(err.Error(), "another build") {
		t.Fatalf("a manifest over another build's binary: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("latest.json written anyway: %v", err)
	}
	writeBinary("202611110749")
	if err := manifest("202611110749"); err != nil {
		t.Fatal(err)
	}
	if err := runRelease([]string{"verify", "--dir", dir}); err != nil {
		t.Fatalf("the manifest it wrote: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, bytes.Replace(b, []byte("202611110749"), []byte("202611110750"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRelease([]string{"verify", "--dir", dir, "--manifest", out}); err == nil || !strings.Contains(err.Error(), "another build") {
		t.Fatalf("a latest.json edited to another build: %v", err)
	}
	var ue usageError
	if err := runRelease([]string{"verify"}); !errors.As(err, &ue) || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("verify without --dir: %v", err)
	}
}
