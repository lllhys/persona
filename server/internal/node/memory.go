package node

import (
	"errors"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/lazyre"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The assistant's memory is Claude Code's auto-memory for its directory: one Markdown file per fact under
// ~/.claude/projects/<encoded cwd>/memory, indexed by MEMORY.md. The app lists, edits and deletes the files. On a
// Codex launcher the assistant reads and writes the same directory (codexMemoryPrompt), so it is one memory either way.

type memoryOp struct {
	Op            string  `json:"op"` // list | write | delete
	Cwd           string  `json:"cwd"`
	Name          string  `json:"name"`
	Content       string  `json:"content"`
	Previous      *string `json:"previous"`
	PreviousIndex string  `json:"previous_index"`
}

type MemoryFile struct {
	Name      string    `json:"name"` // file name, e.g. "lark-access.md"
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
	// CreatedAt is when the file was made, where the file system keeps it (fileBirth); absent otherwise.
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

var (
	projectKeyRe = lazyre.New(`[^a-zA-Z0-9]`)
	memoryNameRe = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._-]{0,100}\.md$`)
)

// memoryDir is where Claude Code keeps auto-memory for cwd (it encodes the resolved path).
func (n *Node) memoryDir(cwd string) string {
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	key := projectKeyRe.ReplaceAllString(cwd, "-")
	dirs := n.projectDirs()
	for _, p := range dirs { // the config directory the assistant's claude actually writes to
		if _, err := os.Stat(filepath.Join(p, key)); err == nil {
			return filepath.Join(p, key, "memory")
		}
	}
	if len(dirs) == 0 {
		return filepath.Join(n.defaultClaudeDir(), "projects", key, "memory")
	}
	return filepath.Join(dirs[0], key, "memory")
}

func (n *Node) assistantMemory(a memoryOp) (any, error) {
	if a.Cwd == "" || !identity.IsAssistantDir(filepath.Clean(a.Cwd)) {
		return nil, errors.New("memory is only available for the assistant directory")
	}
	n.memoryMu.Lock()
	defer n.memoryMu.Unlock()
	dir := n.memoryDir(a.Cwd)
	switch a.Op {
	case "list":
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		files := []MemoryFile{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			info, _ := e.Info()
			f := MemoryFile{Name: e.Name(), Content: string(b)}
			if info != nil {
				f.UpdatedAt = info.ModTime().UTC()
				if born, ok := fileBirth(path, info); ok {
					f.CreatedAt = &born
				}
			}
			files = append(files, f)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		return map[string]any{"files": files, "dir": dir}, nil
	case "dream-read", "dream-write":
		if err := checkDreamMemoryDir(a.Cwd); err != nil {
			return nil, err
		}
		return dreamMemory(dir, a)
	case "write":
		if !memoryNameRe.MatchString(a.Name) {
			return nil, errors.New("bad memory file name")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		return ok(os.WriteFile(filepath.Join(dir, a.Name), []byte(a.Content), 0o600))
	case "delete":
		if !memoryNameRe.MatchString(a.Name) || a.Name == "MEMORY.md" {
			return nil, errors.New("bad memory file name")
		}
		if err := os.Remove(filepath.Join(dir, a.Name)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return ok(dropIndexLine(filepath.Join(dir, "MEMORY.md"), a.Name))
	}
	return nil, errors.New("unknown memory op " + a.Op)
}

// dropIndexLine removes the MEMORY.md pointer to a deleted file.
func dropIndexLine(index, name string) error {
	b, err := os.ReadFile(index)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "("+name+")") {
			continue
		}
		keep = append(keep, l)
	}
	return os.WriteFile(index, []byte(strings.Join(keep, "\n")), 0o600)
}
