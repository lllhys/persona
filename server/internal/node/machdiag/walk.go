package machdiag

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// walker sizes folders the bounded way: it stays on one filesystem (a mounted disk or network share under a folder is
// not this disk's), never follows a symbolic link, counts a hard-linked file once across all its walks, and stops when
// its context ends.
type walker struct {
	dev uint64 // the filesystem walks stay on

	mu   sync.Mutex
	seen map[fileID]bool // files with more than one link, already counted
}

func newWalker(dev uint64) *walker { return &walker{dev: dev, seen: map[fileID]bool{}} }

// size is the bytes root holds on the walker's filesystem; complete is false when the context ended first (the bytes
// are then what was counted by then). A root that is a symbolic link, or on another filesystem, holds nothing.
// Folders that cannot be read are skipped: they are not the user's to clean up anyway.
func (w *walker) size(ctx context.Context, root string) (bytes int64, complete bool) {
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return 0, true
	}
	st, ok := statOf(info)
	if !ok || st.dev != w.dev {
		return 0, true
	}
	if !info.IsDir() {
		return w.count(st), true
	}
	bytes = w.count(st)
	stack := []string{root}
	for len(stack) > 0 {
		if ctx.Err() != nil {
			return bytes, false
		}
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := readDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			fi, err := e.Info() // lstat: a link is never followed
			if err != nil {
				continue
			}
			st, ok := statOf(fi)
			if !ok || st.dev != w.dev {
				continue // a mount point: another filesystem
			}
			bytes += w.count(st)
			if fi.IsDir() {
				stack = append(stack, filepath.Join(dir, e.Name()))
			}
		}
	}
	return bytes, true
}

// count is a file's allocated bytes, or 0 for a hard link to a file already counted.
func (w *walker) count(st fileStat) int64 {
	if st.links > 1 && !st.dir {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.seen[st.id] {
			return 0
		}
		w.seen[st.id] = true
	}
	return st.bytes
}

// readDir lists a folder in the order the filesystem gives (os.ReadDir would sort every one).
func readDir(dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// fileID tells files apart across one filesystem.
type fileID struct{ dev, ino uint64 }

// fileStat is what the walker needs of a file (statOf, per OS).
type fileStat struct {
	id    fileID
	dev   uint64
	links uint64
	dir   bool
	bytes int64 // allocated on disk, which is what frees when it is removed
}
