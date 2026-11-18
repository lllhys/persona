package assistantskills

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Root is supplied by the execution adapter, never by the client.
type Root struct{ Path, Source string }

// LocalCatalog reads only immediate skill directories and at most 16 KiB of frontmatter per file.
// It never walks arbitrary trees, reads skill bodies, follows metadata commands or changes permissions.
func LocalCatalog(roots []Root) ([]Entry, error) {
	out := []Entry{}
	seen := map[string]bool{}
	for _, root := range roots {
		dirs, err := os.ReadDir(root.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, dir := range dirs {
			if strings.HasPrefix(dir.Name(), ".") {
				continue
			}
			file := filepath.Join(root.Path, dir.Name(), "SKILL.md")
			st, err := os.Stat(file)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			f, err := os.Open(file)
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrInvalid) {
				continue
			}
			if err != nil {
				// Non-directory files beside skills are not catalog entries.
				if !dir.IsDir() && dir.Type()&os.ModeSymlink == 0 {
					continue
				}
				return nil, err
			}
			st, statErr := f.Stat()
			if statErr != nil || !st.Mode().IsRegular() {
				f.Close()
				continue
			}
			header, readErr := readHeader(f)
			f.Close()
			if readErr != nil {
				return nil, readErr
			}
			if header == "" {
				continue
			}
			// Unknown scalar forms cannot opt a skill into explicit invocation.
			if invocable := HeaderValue(header, "user-invocable"); invocable != "" && invocable != "true" {
				continue
			}
			real, err := filepath.EvalSymlinks(file)
			if err != nil {
				continue
			}
			if seen[real] {
				continue
			}
			seen[real] = true
			name := HeaderValue(header, "name")
			if name == "" {
				name = dir.Name()
			}
			out = append(out, Entry{Name: name, Description: description(header), Source: root.Source, Path: real})
			if len(out) >= 512 {
				return out, nil // callers label local inventories as bounded, not exhaustive
			}
		}
	}
	return out, nil
}

func readHeader(r io.Reader) (string, error) {
	sc := bufio.NewScanner(io.LimitReader(r, 16<<10))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", sc.Err()
	}
	lines := []string{"---"}
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.TrimSpace(sc.Text()) == "---" {
			return strings.Join(lines, "\n"), nil
		}
	}
	if sc.Err() != nil {
		return "", sc.Err()
	}
	return "", errors.New("skill frontmatter is missing its closing delimiter or exceeds 16 KiB")
}

// HeaderValue reads a scalar frontmatter field without evaluating YAML tags or substitutions.
func HeaderValue(header, key string) string {
	for _, line := range strings.Split(header, "\n") {
		if value, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}
