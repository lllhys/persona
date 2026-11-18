// Package machdiag answers a computer's diagnosis and empties its rebuildable caches (#8037, docs/server/machines.md
// "Diagnosis and cleanup"): node methods machine.diagnose and machine.cleanup.
//
// It describes; it is not a tunnel. Every question is fixed and bounded: the largest process groups by name, the
// largest folders directly under the home directory, a fixed catalog of caches and a fixed catalog of tools. A caller
// names sections and catalog ids, never a path or a command, and every path is resolved here, on the node, each time.
// Process arguments, command lines and environment are never read.
//
// The parsers and the rules (grouping, kinds, the walker, the catalog, the cleanup guard) are plain functions over
// injected readers, tested on any OS; the files with an OS suffix only read the system.
package machdiag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/sysmon"
)

// diskDeadline bounds the disk section's walks; what is not reached by then is left out and the report says partial.
const diskDeadline = 20 * time.Second

// codeBadRequest is the node's refusal of an unknown section or id (the manager checks first; this is the node's own).
const codeBadRequest = "bad_request"

// system is what the package reads from the computer; tests replace each part.
type system struct {
	goos      string
	home      string
	getenv    func(string) string
	processes func(ctx context.Context) ([]process, error)
	memTotal  func() int64
	volumes   func() []proto.DiskVolume
	available func(path string) (int64, error)
	remove    func(ctx context.Context, dir string) error
	tools     *toolProber
	now       func() time.Time
}

var (
	stdOnce sync.Once
	std     *system
)

// host is the real computer, made on first use (the tool probe's cache lives in it).
func host() *system {
	stdOnce.Do(func() {
		home, _ := os.UserHomeDir()
		std = &system{
			goos:      runtime.GOOS,
			home:      home,
			getenv:    os.Getenv,
			processes: readProcesses,
			memTotal:  readMemTotal,
			volumes:   sysmon.Volumes,
			available: availableBytes,
			remove:    removeContents,
			tools:     newToolProber(),
			now:       time.Now,
		}
	})
	return std
}

// Diagnose is machine.diagnose: {sections?} → proto.Diagnosis.
func Diagnose(ctx context.Context, params json.RawMessage) (any, error) {
	var p proto.DiagnoseParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpc.Error{Code: codeBadRequest, Message: err.Error()}
		}
	}
	return host().diagnose(ctx, p.Sections)
}

// Cleanup is machine.cleanup: {ids} → proto.CleanupResult.
func Cleanup(ctx context.Context, params json.RawMessage) (any, error) {
	var p proto.CleanupParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: codeBadRequest, Message: err.Error()}
	}
	return host().cleanup(ctx, p.IDs)
}

// diagnose answers the sections asked for. The tools (a few seconds the first time) are probed beside the processes
// (half a second) and the disk (up to diskDeadline), which needs the processes first.
func (s *system) diagnose(ctx context.Context, sections []string) (proto.Diagnosis, error) {
	if len(sections) == 0 {
		sections = proto.DiagnoseSections
	}
	for _, sec := range sections {
		if !slices.Contains(proto.DiagnoseSections, sec) {
			return proto.Diagnosis{}, &rpc.Error{Code: codeBadRequest, Message: fmt.Sprintf("unknown section %q", sec)}
		}
	}
	out := proto.Diagnosis{At: s.now().Unix()}
	var wg sync.WaitGroup
	if slices.Contains(sections, proto.DiagnoseTools) {
		wg.Go(func() {
			t := s.tools.list(ctx)
			out.Tools = &t
		})
	}
	wantProcs, wantDisk := slices.Contains(sections, proto.DiagnoseProcesses), slices.Contains(sections, proto.DiagnoseDisk)
	var (
		procs []process
		err   error
	)
	if wantProcs || wantDisk { // the disk section needs them too: a cache is in use while its toolchain runs
		procs, err = s.processes(ctx)
	}
	if wantProcs && err == nil {
		r := report(procs)
		r.MemTotal = s.memTotal()
		out.Processes = &r
	}
	if wantDisk && diskSupported(s.goos) {
		d := s.disk(ctx, processNames(procs, err))
		out.Disk = &d
	}
	wg.Wait()
	return out, nil
}

// processNames are the running executables' names, or nil when they could not be read (every cache then counts as in
// use: it is not known that nothing uses it).
func processNames(procs []process, err error) map[string]bool {
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(procs))
	for _, p := range procs {
		names[p.name] = true
	}
	return names
}
