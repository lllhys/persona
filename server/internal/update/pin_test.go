package update

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/update/updatetest"
)

const pinName = "personad-linux-amd64.gz"

// pinRelease is release version as a source serves it: pinName, whose binary is body, and latest.json describing it.
func pinRelease(t *testing.T, version string, build int64, body string) map[string][]byte {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(body))
	zw.Close()
	outer, inner := sha256.Sum256(gz.Bytes()), sha256.Sum256([]byte(body))
	m := Manifest{Version: version, Build: build, Files: map[string]File{"linux-amd64": {Name: pinName, Size: int64(gz.Len()),
		SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"latest.json": data, pinName: gz.Bytes()}
}

// checkThenPublish reads base's release (0.6.1, published under tag), then publishes 0.6.2 as the latest release
// before anything is downloaded: what a hub saw when it last checked, and what its source serves when Update is pressed.
func checkThenPublish(t *testing.T, src *updatetest.Releases, base, tag string) *Manifest {
	t.Helper()
	src.Publish(tag, pinRelease(t, "0.6.1", 202611160928, "personad 0.6.1"), true)
	p, err := Check(context.Background(), src.Client(), base)
	if err != nil || p.Manifest.Version != "0.6.1" {
		t.Fatalf("check: %+v %v", p, err)
	}
	src.Publish("v0.6.2", pinRelease(t, "0.6.2", 202611161034, "personad 0.6.2, a newer and longer build"), true)
	return p.Manifest
}

func TestReleaseBase(t *testing.T) {
	for _, tc := range []struct{ base, version, want string }{
		{"https://github.com/nwverse/persona/releases/latest/download", "0.6.1", "https://github.com/nwverse/persona/releases/download/v0.6.1"},
		{"https://github.com/nwverse/persona/releases/latest/download/", "0.6.1", "https://github.com/nwverse/persona/releases/download/v0.6.1"},
		{"https://github.com/nwverse/persona/releases/latest/download", "../0.6.1", ""}, // not a version
		{"https://github.com/nwverse/persona/releases/latest/download", "", ""},
		{"https://github.com/persona/releases/latest/download", "0.6.1", ""},             // not a repository
		{"https://mirror.example/nwverse/persona/releases/latest/download", "0.6.1", ""}, // not GitHub
		{"http://github.com/nwverse/persona/releases/latest/download", "0.6.1", ""},
		{"https://github.com/nwverse/persona/releases/download/v0.6.1", "0.6.1", ""}, // fixed already
		{"https://downloads.example/dl", "0.6.1", ""},
		{"file:///srv/persona/dl", "0.6.1", ""},
	} {
		if got := releaseBase(tc.base, tc.version); got != tc.want {
			t.Errorf("releaseBase(%q, %q) = %q, want %q", tc.base, tc.version, got, tc.want)
		}
	}
}

// From GitHub's moving base, a release's file comes from its own tag: the release checked is installed although a
// newer one was published since, and the moving base is not asked for it at all.
func TestDownloadReleaseFromItsTag(t *testing.T) {
	src := updatetest.NewReleases(t)
	m := checkThenPublish(t, src, src.GitHub(), "v0.6.1")
	bin, err := DownloadRelease(context.Background(), src.Client(), src.GitHub(), m, m.Files["linux-amd64"], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "personad 0.6.1" {
		t.Fatalf("installed %q", b)
	}
	if got := src.Fetched(pinName); !slices.Equal(got, []string{updatetest.TagPath("v0.6.1", pinName)}) {
		t.Fatalf("fetched %v", got)
	}
}

// A repository whose tags are not v<version> has no such tag: the file comes from the moving base as before, and a
// release published since is said as such there too.
func TestDownloadReleaseWithoutItsTag(t *testing.T) {
	ctx := context.Background()
	t.Run("the same release", func(t *testing.T) {
		src := updatetest.NewReleases(t)
		src.Publish("release-0.6.1", pinRelease(t, "0.6.1", 202611160928, "personad 0.6.1"), true)
		p, err := Check(ctx, src.Client(), src.GitHub())
		if err != nil {
			t.Fatal(err)
		}
		bin, err := DownloadRelease(ctx, src.Client(), src.GitHub(), p.Manifest, p.Manifest.Files["linux-amd64"], t.TempDir())
		if b, _ := os.ReadFile(bin); err != nil || string(b) != "personad 0.6.1" {
			t.Fatalf("installed %q: %v", b, err)
		}
		if got := src.Fetched(pinName); len(got) == 0 || got[len(got)-1] != updatetest.TagPath("release-0.6.1", pinName) {
			t.Fatalf("fetched %v", got)
		}
	})
	t.Run("a newer release", func(t *testing.T) {
		src := updatetest.NewReleases(t)
		m := checkThenPublish(t, src, src.GitHub(), "release-0.6.1")
		_, err := DownloadRelease(ctx, src.Client(), src.GitHub(), m, m.Files["linux-amd64"], t.TempDir())
		var moved *MovedError
		if !errors.As(err, &moved) || moved.Now.Manifest.Version != "0.6.2" {
			t.Fatalf("%v", err)
		}
	})
}

// A source that is not GitHub's: what it serves after a newer release came out does not match the release checked.
// It is read once more, and the error says a newer release is there instead of that the download is broken; the file
// is fetched once and never again.
func TestDownloadReleaseFromAMovingSource(t *testing.T) {
	src := updatetest.NewReleases(t)
	m := checkThenPublish(t, src, src.DL(), "v0.6.1")
	dir := t.TempDir()
	_, err := DownloadRelease(context.Background(), src.Client(), src.DL(), m, m.Files["linux-amd64"], dir)
	var moved *MovedError
	if !errors.As(err, &moved) || moved.Checked != "0.6.1" || moved.Now.Manifest.Version != "0.6.2" || moved.Now.Manifest.Build != 202611161034 {
		t.Fatalf("%v", err)
	}
	if !IsMismatch(err) || strings.Contains(err.Error(), errMismatch.Error()) || !strings.Contains(err.Error(), "0.6.2") {
		t.Fatalf("error %q", err)
	}
	if got := src.Fetched(pinName); len(got) != 1 {
		t.Fatalf("fetched %v", got)
	}
	if got := src.Fetched("latest.json"); len(got) != 2 {
		t.Fatalf("latest.json read %d times, want the check and once more: %v", len(got), got)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// A download that is broken while the release stays the one checked still fails with the mismatch: from a moving
// source after reading it once more, from a release's tag without reading anything else.
func TestDownloadReleaseThatIsBroken(t *testing.T) {
	ctx := context.Background()
	broken := func(t *testing.T, tag string) (*updatetest.Releases, *Manifest) {
		t.Helper()
		src := updatetest.NewReleases(t)
		files := pinRelease(t, "0.6.1", 202611160928, "personad 0.6.1")
		files[pinName] = append(bytes.Clone(files[pinName]), "garbage"...)
		src.Publish(tag, files, true)
		m, err := ParseManifest(files["latest.json"])
		if err != nil {
			t.Fatal(err)
		}
		return src, m
	}
	for _, tc := range []struct {
		name   string
		base   func(*updatetest.Releases) string
		checks int // latest.json reads
	}{
		{"a moving source", (*updatetest.Releases).DL, 1},
		{"a release's tag", (*updatetest.Releases).GitHub, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, m := broken(t, "v0.6.1")
			_, err := DownloadRelease(ctx, src.Client(), tc.base(src), m, m.Files["linux-amd64"], t.TempDir())
			var moved *MovedError
			if !IsMismatch(err) || errors.As(err, &moved) || !strings.Contains(err.Error(), errMismatch.Error()) {
				t.Fatalf("%v", err)
			}
			if got := src.Fetched(pinName); len(got) != 1 {
				t.Fatalf("fetched %v", got)
			}
			if got := src.Fetched("latest.json"); len(got) != tc.checks {
				t.Fatalf("latest.json read %v, want %d", got, tc.checks)
			}
		})
	}
}
