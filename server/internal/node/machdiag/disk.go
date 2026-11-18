package machdiag

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/nwverse/persona/server/internal/proto"
)

const (
	// mostFolders is how many of the home directory's folders a report lists.
	mostFolders = 12
	// walkers is how many folders are sized at once: enough to keep a fast disk busy, few enough not to starve the
	// Agents on it.
	walkers = 4
)

// disk is the disk section: the volumes, the home directory's largest folders and the caches of the catalog, all
// sized within diskDeadline. running are the processes' names (nil: unknown).
func (s *system) disk(ctx context.Context, running map[string]bool) proto.DiskReport {
	ctx, cancel := context.WithTimeout(ctx, diskDeadline)
	defer cancel()
	out := proto.DiskReport{Volumes: s.volumes(), Folders: []proto.DiskFolder{}, Caches: []proto.DiskCache{}}
	if out.Volumes == nil {
		out.Volumes = []proto.DiskVolume{}
	}
	info, err := os.Stat(s.home) // the home directory itself may be a link (to a data disk): its target is the filesystem
	if err != nil {
		return out
	}
	home, ok := statOf(info)
	if !ok {
		return out
	}
	folders := homeFolders(s.home)
	caches := resolveCaches(s.goos, s.home, s.getenv)

	// The folders and the caches are separate walks (a cache is inside one of the folders): each has its own record
	// of hard links, so a file counted in a folder is counted in its cache too.
	folderWalk, cacheWalk := newWalker(home.dev), newWalker(home.dev)
	folderBytes := make([]int64, len(folders))
	cacheBytes := make([]int64, len(caches))
	var (
		mu       sync.Mutex
		complete = true
	)
	jobs := make(chan func(), walkers)
	var wg sync.WaitGroup
	for range walkers {
		wg.Go(func() {
			for job := range jobs {
				job()
			}
		})
	}
	sized := func(w *walker, path string, into *int64) func() {
		return func() {
			n, done := w.size(ctx, path)
			*into = n
			if !done {
				mu.Lock()
				complete = false
				mu.Unlock()
			}
		}
	}
	for i, c := range caches { // the caches first: they are what the user can act on
		jobs <- sized(cacheWalk, c.path, &cacheBytes[i])
	}
	for i, f := range folders {
		jobs <- sized(folderWalk, f, &folderBytes[i])
	}
	close(jobs)
	wg.Wait()

	out.Partial = !complete
	for i, f := range folders {
		if folderBytes[i] > 0 {
			out.Folders = append(out.Folders, proto.DiskFolder{Path: tilde(s.home, f), Bytes: folderBytes[i]})
		}
	}
	slices.SortFunc(out.Folders, func(a, b proto.DiskFolder) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(a.Path, b.Path))
	})
	out.Folders = out.Folders[:min(mostFolders, len(out.Folders))]
	for i, c := range caches {
		if cacheBytes[i] > 0 {
			out.Caches = append(out.Caches, proto.DiskCache{ID: c.spec.id, Path: tilde(s.home, c.path), Bytes: cacheBytes[i], InUse: inUse(c.spec, running)})
		}
	}
	return out
}

// homeFolders are the folders directly in home (not links to folders elsewhere).
func homeFolders(home string) []string {
	entries, err := readDir(home)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			out = append(out, filepath.Join(home, e.Name()))
		}
	}
	return out
}
