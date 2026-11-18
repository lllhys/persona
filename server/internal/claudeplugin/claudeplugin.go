// Package claudeplugin keeps the Persona plugin for Claude Code installed on a computer (docs/operations/link.md
// "Claude Code plugin"). The node carries the plugin of its own build (runtimepkg.ClaudeCodePlugin, packed from
// integrations/claude-code-plugin, whose folder is its own marketplace), writes it out to a stable folder under the
// data directory (~/.persona/claude-code-plugin) and has Claude Code's own CLI install it at the user scope of each
// Claude Code config directory it manages:
//
//	claude plugin marketplace add <data dir>/claude-code-plugin
//	claude plugin install persona@persona --scope user
//
// A plugin from a folder marketplace is read from that folder, so writing a new build there and running `claude plugin
// update` is the whole update. Claude Code loads plugins when a session starts: new sessions (terminal and the desktop
// app's Code sessions alike) get it, sessions already open keep what they loaded; nothing here restarts a session.
//
// Only the official CLI changes Claude Code's state (settings.json's enabledPlugins and extraKnownMarketplaces,
// plugins/installed_plugins.json, plugins/known_marketplaces.json): nothing here edits its files, and every command
// names this plugin or its marketplace alone, so other plugins and settings are never touched.
//
// What the user decides wins: a plugin turned off in Claude Code stays off and is not updated; one removed there after
// the node installed it is not put back (an opt-out recorded in claude-plugin.json); the automatic install can be
// turned off for the computer (`persona-link plugin off` / `uninstall`). Without Claude Code (no CLI, or a config
// directory it never created) nothing is done.
package claudeplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwverse/persona/server/internal/runtimepkg"
)

const (
	// DefaultName is the plugin's and its marketplace's name when no build is at hand to say them (an uninstall by a
	// build without the plugin): integrations/claude-code-plugin/README.md, "Packaging".
	DefaultName = "persona"
	// MarketDir is the plugin's folder under the data directory: the marketplace Claude Code reads it from.
	MarketDir = "claude-code-plugin"
	// packagePrefix is where the plugin's files sit in the runtime package (integrations/claude-code-plugin).
	packagePrefix = "claude-code-plugin/"
	// manifestPath is the plugin's manifest, marketPath its marketplace's, relative to its root.
	manifestPath = ".claude-plugin/plugin.json"
	marketPath   = ".claude-plugin/marketplace.json"
)

// Bundle is the plugin a build carries: its files, relative to the plugin's root, what its manifest says, and the
// marketplace its folder is.
type Bundle struct {
	Name        string
	Version     string
	Description string
	Market      string
	Files       []runtimepkg.File
}

// Embedded is the plugin this build carries; nil without an error when the build carries none (built without
// `go generate ./internal/runtimepkg`).
func Embedded() (*Bundle, error) {
	pkg, err := runtimepkg.Load(runtimepkg.ClaudeCodePlugin)
	if pkg == nil || err != nil {
		return nil, err
	}
	files := make([]runtimepkg.File, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		files = append(files, runtimepkg.File{Path: strings.TrimPrefix(f.Path, packagePrefix), Data: f.Data})
	}
	return NewBundle(files)
}

// BundleFromDir reads a plugin from a directory (its root holds .claude-plugin/plugin.json): tests and development.
func BundleFromDir(dir string) (*Bundle, error) {
	var files []runtimepkg.File
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, runtimepkg.File{Path: filepath.ToSlash(rel), Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewBundle(files)
}

// NewBundle is the plugin made of files: its manifest must name it and give a version.
func NewBundle(files []runtimepkg.File) (*Bundle, error) {
	b := &Bundle{Files: append([]runtimepkg.File(nil), files...)}
	sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Path < b.Files[j].Path })
	var manifest, market []byte
	for _, f := range b.Files {
		if !runtimepkg.ValidPath(f.Path) {
			return nil, fmt.Errorf("claude plugin: bad path %q", f.Path)
		}
		switch f.Path {
		case manifestPath:
			manifest = f.Data
		case marketPath:
			market = f.Data
		}
	}
	if manifest == nil {
		return nil, errors.New("claude plugin: no " + manifestPath)
	}
	var m struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("claude plugin: %s: %w", manifestPath, err)
	}
	if !validName(m.Name) || strings.TrimSpace(m.Version) == "" {
		return nil, fmt.Errorf("claude plugin: %s names no plugin or version", manifestPath)
	}
	b.Name, b.Version, b.Description = m.Name, strings.TrimSpace(m.Version), m.Description
	if market == nil {
		// A plugin folder without a marketplace file of its own: one listing the plugin from the folder itself.
		data, err := json.MarshalIndent(map[string]any{"name": b.Name, "owner": map[string]string{"name": "Persona"},
			"plugins": []any{map[string]any{"name": b.Name, "source": "./", "description": b.Description}}}, "", "  ")
		if err != nil {
			return nil, err
		}
		b.Files = append(b.Files, runtimepkg.File{Path: marketPath, Data: append(data, '\n')})
		b.Market = b.Name
		return b, nil
	}
	var mk struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source any    `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(market, &mk); err != nil {
		return nil, fmt.Errorf("claude plugin: %s: %w", marketPath, err)
	}
	listed := false
	for _, p := range mk.Plugins {
		listed = listed || p.Name == b.Name && (p.Source == "./" || p.Source == ".")
	}
	if !validName(mk.Name) || !listed {
		return nil, fmt.Errorf("claude plugin: %s does not list %s from its own folder", marketPath, b.Name)
	}
	b.Market = mk.Name
	return b, nil
}

// ID is the plugin's id in Claude Code: <name>@<marketplace>.
func (b *Bundle) ID() string {
	if b == nil {
		return DefaultName + "@" + DefaultName
	}
	return b.Name + "@" + b.Market
}

// market is the marketplace's name.
func (b *Bundle) market() string {
	if b == nil {
		return DefaultName
	}
	return b.Market
}

// validName: a plugin name Claude Code takes (and a safe directory name).
func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return s != "." && s != ".."
}

// marketFiles is the folder's content: the plugin's files, its marketplace file among them.
func (b *Bundle) marketFiles() map[string][]byte {
	out := make(map[string][]byte, len(b.Files))
	for _, f := range b.Files {
		out[f.Path] = f.Data
	}
	return out
}

// syncDir makes root hold exactly files (slash-separated paths): each file that differs is written whole (a temporary
// file renamed over it), files that are not in it are removed, then empty directories. root is the node's own
// directory: nothing outside it is touched. changed says whether anything was written or removed.
func syncDir(root string, files map[string][]byte) (changed bool, err error) {
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return changed, err
		}
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return changed, err
		}
		if err := os.Rename(tmp, p); err != nil {
			os.Remove(tmp)
			return changed, err
		}
		changed = true
	}
	var dirs []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		if _, ok := files[filepath.ToSlash(rel)]; !ok {
			if err := os.Remove(p); err != nil {
				return err
			}
			changed = true
		}
		return nil
	})
	if err != nil {
		return changed, err
	}
	for i := len(dirs) - 1; i >= 0; i-- { // deepest first; a directory that still holds files stays
		_ = os.Remove(dirs[i])
	}
	return changed, nil
}
