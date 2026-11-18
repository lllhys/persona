//go:build linux || darwin

package machdiag

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestDiagnoseAllSections(t *testing.T) {
	s := testSystem(t, "claude", "go")
	writeFile(t, filepath.Join(s.home, "code", "big"), 256<<10)
	writeFile(t, filepath.Join(s.home, "notes", "small"), 4<<10)
	writeFile(t, filepath.Join(cacheDir(t, s, "go-build"), "x"), 64<<10)
	writeFile(t, filepath.Join(cacheDir(t, s, "cargo-registry"), "x"), 8<<10)
	writeFile(t, filepath.Join(s.home, "loose-file"), 1<<20) // a file directly in home is not a folder

	d, err := s.diagnose(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.At != 1794000000 || d.Processes == nil || d.Disk == nil || d.Tools == nil {
		t.Fatalf("diagnosis = %+v", d)
	}
	if d.Processes.MemTotal != 8<<30 || d.Processes.Count != 2 || len(d.Processes.Groups) != 2 {
		t.Fatalf("processes = %+v", d.Processes)
	}
	disk := d.Disk
	if disk.Partial || len(disk.Volumes) != 1 || len(disk.Folders) < 2 || disk.Folders[0].Path != "~/code" {
		t.Fatalf("disk = %+v", disk)
	}
	for _, f := range disk.Folders {
		if !strings.HasPrefix(f.Path, "~/") || f.Path == "~/loose-file" {
			t.Errorf("folder %q", f.Path)
		}
	}
	caches := map[string]proto.DiskCache{}
	for _, c := range disk.Caches {
		caches[c.ID] = c
	}
	if len(caches) != 2 || !caches["go-build"].InUse || caches["cargo-registry"].InUse || !strings.HasPrefix(caches["go-build"].Path, "~/") {
		t.Fatalf("caches = %+v", disk.Caches)
	}
	// On the wire: lists never null, tools present even when none were found.
	b, _ := json.Marshal(d)
	for _, want := range []string{`"tools":[]`, `"partial":false`, `"in_use":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", b, want)
		}
	}
	if strings.Contains(string(b), s.home) {
		t.Errorf("the home directory's path left the node: %s", b)
	}
}

func TestDiagnoseYarnCacheInUseByNode(t *testing.T) {
	s := testSystem(t, "node")
	writeFile(t, filepath.Join(cacheDir(t, s, "yarn-cache"), "v6", "npm-example-1.0.0", "node_modules", "example", "package.json"), 4096)

	d, err := s.diagnose(context.Background(), []string{proto.DiagnoseDisk})
	if err != nil {
		t.Fatal(err)
	}
	if d.Disk == nil || len(d.Disk.Caches) != 1 || d.Disk.Caches[0].ID != "yarn-cache" || !d.Disk.Caches[0].InUse {
		t.Fatalf("Yarn running as node: disk = %+v", d.Disk)
	}
}

func TestDiagnoseOnlyWhatIsAsked(t *testing.T) {
	s := testSystem(t)
	d, err := s.diagnose(context.Background(), []string{proto.DiagnoseTools})
	if err != nil || d.Processes != nil || d.Disk != nil || d.Tools == nil {
		t.Fatalf("%+v %v", d, err)
	}
	_, err = s.diagnose(context.Background(), []string{"processes", "env"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != codeBadRequest {
		t.Fatalf("unknown section: %v", err)
	}
}

// Without a process list, the processes are left out and every cache counts as in use.
func TestDiagnoseWithoutProcesses(t *testing.T) {
	s := testSystem(t)
	s.processes = func(context.Context) ([]process, error) { return nil, errors.New("no /proc") }
	writeFile(t, filepath.Join(cacheDir(t, s, "go-build"), "x"), 4096)
	d, err := s.diagnose(context.Background(), []string{proto.DiagnoseProcesses, proto.DiagnoseDisk})
	if err != nil || d.Processes != nil || d.Disk == nil || len(d.Disk.Caches) != 1 || !d.Disk.Caches[0].InUse {
		t.Fatalf("%+v %v", d, err)
	}
}

// A system whose disk is not described (Windows) leaves the section out.
func TestDiagnoseDiskUnsupported(t *testing.T) {
	s := testSystem(t)
	s.goos = "windows"
	d, err := s.diagnose(context.Background(), []string{proto.DiagnoseDisk})
	if err != nil || d.Disk != nil {
		t.Fatalf("%+v %v", d, err)
	}
}

// The RPC entry points decode their params and refuse what they do not know before touching the computer.
func TestEntryPointsRefuseBadParams(t *testing.T) {
	for _, call := range []func() (any, error){
		func() (any, error) { return Diagnose(context.Background(), json.RawMessage(`{"sections":"disk"}`)) },
		func() (any, error) { return Diagnose(context.Background(), json.RawMessage(`{"sections":["argv"]}`)) },
		func() (any, error) { return Cleanup(context.Background(), json.RawMessage(`{"ids":["/etc"]}`)) },
		func() (any, error) { return Cleanup(context.Background(), json.RawMessage(`{"paths":["/"]}`)) },
		func() (any, error) { return Cleanup(context.Background(), nil) },
	} {
		_, err := call()
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != codeBadRequest {
			t.Errorf("%v", err)
		}
	}
}

// A scan whose time ran out says partial: its sizes are what it counted by then.
func TestDiskScanCutShortIsPartial(t *testing.T) {
	s := testSystem(t)
	writeFile(t, filepath.Join(s.home, "code", "a", "f"), 4096)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d := s.disk(ctx, map[string]bool{}); !d.Partial {
		t.Fatalf("%+v", d)
	}
}
