package node

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/devicewire"
	"golang.org/x/text/unicode/norm"
)

const (
	knowledgeFileBytes    = 64 * 1024
	knowledgeMaxEntries   = 10000
	knowledgeMaxReadBytes = 16 * 1024 * 1024
)

func (n *Node) knowledgeRoot(folder devicewire.KnowledgeFolder) (*os.Root, error) {
	for _, excluded := range folder.ExcludeDirs {
		name := strings.TrimSuffix(excluded, "/")
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, `\`) {
			return nil, errors.New("knowledge folder exclusions must be relative directories")
		}
	}
	for _, excluded := range folder.ExcludeExtensions {
		ext := strings.TrimPrefix(excluded, ".")
		if ext == "" || strings.ContainsAny(ext, `/\`) {
			return nil, errors.New("knowledge extension exclusions must be filename suffixes")
		}
	}
	dir := folder.Path
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(n.home, strings.TrimPrefix(dir, "~"))
	}
	real, err := n.realPath(dir)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		// Expand Windows short names before checking the root against secret paths.
		real, err = filepath.EvalSymlinks(real)
		if err != nil {
			return nil, err
		}
		if err := n.offLimitsResolved(real); err != nil {
			return nil, err
		}
	}
	st, err := os.Lstat(real)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	if opened, err := root.Stat("."); err != nil || !st.IsDir() || !os.SameFile(st, opened) {
		root.Close()
		return nil, fs.ErrPermission
	}
	if resolved, err := n.realPath(real); err != nil || resolved != real {
		root.Close()
		return nil, fs.ErrPermission
	}
	return root, nil
}

func knowledgeExcluded(folder devicewire.KnowledgeFolder, name string, directory bool) bool {
	name = norm.NFC.String(strings.ToLower(name))
	for _, dir := range folder.ExcludeDirs {
		dir = norm.NFC.String(strings.ToLower(strings.TrimSuffix(dir, "/")))
		if name == dir || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	return !directory && slices.ContainsFunc(folder.ExcludeExtensions, func(ext string) bool {
		ext = norm.NFC.String(strings.TrimPrefix(strings.ToLower(ext), "."))
		return ext != "" && strings.HasSuffix(name, "."+ext)
	})
}

// A candidate must be a regular file below the configured root. Check every
// component for exclusions and the node's ordinary remote-file restrictions.
func (n *Node) knowledgeFile(root *os.Root, folder devicewire.KnowledgeFolder, name string) (devicewire.KnowledgeReadResult, int, error) {
	out := devicewire.KnowledgeReadResult{}
	if name == "." || !fs.ValidPath(name) || strings.Contains(name, `\`) || knowledgeExcluded(folder, name, false) {
		return out, 0, fs.ErrPermission
	}
	local, err := filepath.Localize(name)
	if err != nil {
		return out, 0, fs.ErrPermission
	}
	parts := strings.Split(filepath.ToSlash(local), "/")
	dir := root
	defer func() {
		if dir != root {
			dir.Close()
		}
	}()
	for _, part := range parts[:len(parts)-1] {
		next, err := n.openKnowledgeDir(dir, part)
		if err != nil {
			return out, 0, err
		}
		if dir != root {
			dir.Close()
		}
		dir = next
	}
	base := parts[len(parts)-1]
	if err := n.checkKnowledgePath(dir, base); err != nil {
		return out, 0, err
	}
	st, err := dir.Lstat(base)
	if err != nil {
		return out, 0, err
	}
	if !st.Mode().IsRegular() {
		return out, 0, fs.ErrPermission
	}
	f, err := openKnowledgeFile(dir, base)
	if err != nil {
		return out, 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(opened, st) {
		return out, 0, fs.ErrPermission
	}
	b, err := io.ReadAll(io.LimitReader(f, knowledgeFileBytes+1))
	if err != nil {
		return out, len(b), err
	}
	out.Truncated = len(b) > knowledgeFileBytes
	text := string(b[:min(len(b), knowledgeFileBytes)])
	if out.Truncated {
		// A byte limit can split a character at the boundary.
		for range utf8.UTFMax - 1 {
			if utf8.ValidString(text) || len(text) == 0 {
				break
			}
			text = text[:len(text)-1]
		}
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return out, len(b), errors.New("knowledge file is not UTF-8 text")
	}
	out.Text = text
	return out, len(b), nil
}

func (n *Node) checkKnowledgePath(parent *os.Root, name string) error {
	full := filepath.Join(parent.Name(), name)
	if err := n.offLimitsResolved(full); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// An alternate DOS name can otherwise bypass a directory or suffix exclusion.
		canonical, err := filepath.EvalSymlinks(full)
		if err != nil {
			return err
		}
		if !strings.EqualFold(canonical, full) {
			return fs.ErrPermission
		}
	}
	return nil
}

func (n *Node) openKnowledgeDir(parent *os.Root, name string) (*os.Root, error) {
	if err := n.checkKnowledgePath(parent, name); err != nil {
		return nil, err
	}
	st, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fs.ErrPermission
	}
	dir, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	if opened, err := dir.Stat("."); err != nil || !os.SameFile(st, opened) {
		dir.Close()
		return nil, fs.ErrPermission
	}
	return dir, nil
}

func (n *Node) readKnowledge(p devicewire.KnowledgeReadRequest) (devicewire.KnowledgeReadResult, error) {
	root, err := n.knowledgeRoot(p.KnowledgeFolder)
	if err != nil {
		return devicewire.KnowledgeReadResult{}, err
	}
	defer root.Close()
	out, _, err := n.knowledgeFile(root, p.KnowledgeFolder, p.Path)
	return out, err
}

func (n *Node) searchKnowledge(ctx context.Context, p devicewire.KnowledgeSearchRequest) (devicewire.KnowledgeSearchResult, error) {
	out := devicewire.KnowledgeSearchResult{Paths: []string{}}
	root, err := n.knowledgeRoot(p.KnowledgeFolder)
	if err != nil {
		return out, err
	}
	defer root.Close()
	query := strings.ToLower(strings.TrimSpace(p.Query))
	if query == "" {
		return out, errors.New("knowledge search needs a query")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	entries, totalBytes := 0, 0
	limit := min(max(p.Limit, 1), 200)
	var walk func(*os.Root, string, int) error
	walk = func(dir *os.Root, prefix string, depth int) error {
		f, err := dir.Open(".")
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			batch, err := f.ReadDir(64)
			for _, entry := range batch {
				entries++
				if entries > knowledgeMaxEntries || totalBytes >= knowledgeMaxReadBytes || ctx.Err() != nil {
					return fs.SkipAll
				}
				relative := path.Join(prefix, entry.Name())
				if knowledgeExcluded(p.KnowledgeFolder, relative, entry.IsDir()) {
					continue
				}
				if entry.IsDir() {
					if depth == 64 {
						out.Truncated = true
						continue
					}
					child, err := n.openKnowledgeDir(dir, entry.Name())
					if err != nil {
						out.Truncated = out.Truncated || !errors.Is(err, errOffLimits)
						continue
					}
					err = walk(child, relative, depth+1)
					child.Close()
					if errors.Is(err, fs.SkipAll) {
						return err
					}
					out.Truncated = out.Truncated || err != nil
					continue
				}
				if !entry.Type().IsRegular() {
					continue
				}
				file, size, err := n.knowledgeFile(dir, devicewire.KnowledgeFolder{}, entry.Name())
				totalBytes += size
				if err != nil {
					continue
				}
				out.Truncated = out.Truncated || file.Truncated
				if strings.Contains(strings.ToLower(relative), query) || strings.Contains(strings.ToLower(file.Text), query) {
					if len(out.Paths) == limit {
						return fs.SkipAll
					}
					out.Paths = append(out.Paths, relative)
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	err = walk(root, "", 0)
	if errors.Is(err, fs.SkipAll) {
		out.Truncated, err = true, nil
	}
	slices.Sort(out.Paths)
	return out, err
}
