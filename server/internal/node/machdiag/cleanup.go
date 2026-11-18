package machdiag

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// errOutsideHome: a cache folder that is not really inside the home directory (a link somewhere along its path, or a
// variable pointing elsewhere). It is never emptied.
var errOutsideHome = errors.New("the folder is not inside the home folder")

// cleanup empties the caches named by id. Each id must be in the catalog; its folder is resolved here and checked
// against the real home directory again; one whose toolchain runs is left alone. What it freed is measured on the
// home directory's volume, before and after.
func (s *system) cleanup(ctx context.Context, ids []string) (proto.CleanupResult, error) {
	if len(ids) == 0 {
		return proto.CleanupResult{}, &rpc.Error{Code: codeBadRequest, Message: "no cache ids"}
	}
	for _, id := range ids {
		if !slices.Contains(proto.CacheIDs, id) {
			return proto.CleanupResult{}, &rpc.Error{Code: codeBadRequest, Message: fmt.Sprintf("unknown cache %q", id)}
		}
	}
	procs, perr := s.processes(ctx)
	running := processNames(procs, perr)
	caches := resolveCaches(s.goos, s.home, s.getenv)
	before, _ := s.available(s.home)
	out := proto.CleanupResult{AvailableBefore: before, Items: []proto.CleanupItem{}}
	done := map[string]bool{}
	for _, id := range ids {
		if done[id] {
			continue
		}
		done[id] = true
		out.Items = append(out.Items, s.cleanOne(ctx, id, caches, running))
	}
	after, _ := s.available(s.home)
	out.AvailableAfter = after
	out.FreedBytes = max(after-before, 0)
	return out, nil
}

func (s *system) cleanOne(ctx context.Context, id string, caches []cacheEntry, running map[string]bool) proto.CleanupItem {
	item := proto.CleanupItem{ID: id, Status: proto.CleanupMissing}
	i := slices.IndexFunc(caches, func(c cacheEntry) bool { return c.spec.id == id })
	if i < 0 {
		return item // not on this system, or moved out of the home directory
	}
	c := caches[i]
	dir, err := guardCacheDir(s.home, c.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return item
	case err != nil:
		item.Status, item.Error = proto.CleanupFailed, s.clean(err)
		return item
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return item
	}
	st, ok := statOf(info)
	if !ok {
		return item // a system whose files are not sized has no catalog either
	}
	sizing, cancel := context.WithTimeout(ctx, diskDeadline)
	item.Bytes, _ = newWalker(st.dev).size(sizing, dir)
	cancel()
	if item.Bytes == 0 || isEmptyDir(dir) {
		item.Bytes = 0
		return item
	}
	if inUse(c.spec, running) {
		item.Status = proto.CleanupInUse
		return item
	}
	if err := s.remove(ctx, dir); err != nil {
		item.Status, item.Error = proto.CleanupFailed, s.clean(err)
		return item
	}
	item.Status = proto.CleanupCleaned
	return item
}

// clean writes an error the way paths leave the node: the home directory as "~".
func (s *system) clean(err error) string {
	if s.home == "" {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), filepath.Clean(s.home), "~")
}

// guardCacheDir is the real folder of a catalog path, after checking that it is one this package may empty: a folder
// (not a link) strictly inside the real home directory, reached through no link along the way. A missing folder is
// fs.ErrNotExist.
func guardCacheDir(home, p string) (string, error) {
	rel, ok := insideHome(home, p)
	if !ok {
		return "", errOutsideHome
	}
	info, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return "", errOutsideHome
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	// Equal to the home's real path joined with the same relative path: nothing between them was a link.
	if want := filepath.Join(realHome, rel); real != want {
		return "", errOutsideHome
	}
	if _, ok := insideHome(realHome, real); !ok {
		return "", errOutsideHome
	}
	return real, nil
}

func isEmptyDir(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) == 0
}

// removeContents empties dir and keeps it (its owner may hold it open, and some toolchains expect it to exist). A link
// inside is removed as a link, never followed. Go's module cache is read-only on purpose, so a folder that refuses is
// made writable by its owner and tried again.
func removeContents(ctx context.Context, dir string) error {
	entries, err := readDir(dir)
	if err != nil {
		return err
	}
	var first error
	failed := 0
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := filepath.Join(dir, e.Name())
		err := os.RemoveAll(p)
		if err != nil && e.IsDir() {
			makeWritable(p)
			err = os.RemoveAll(p)
		}
		if err != nil {
			first = cmp.Or(first, err)
			failed++
		}
	}
	if failed > 1 {
		return fmt.Errorf("%w (and %d more)", first, failed-1)
	}
	return first
}

// makeWritable adds the owner's write and search bits to every folder under root (links are not followed: WalkDir
// reports a link as a link).
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if d == nil || !d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			_ = os.Chmod(p, info.Mode().Perm()|0o700)
		}
		return nil
	})
}
