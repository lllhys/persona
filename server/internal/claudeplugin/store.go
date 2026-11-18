package claudeplugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Files in the data directory. claude-plugin.json is the computer's choices and what the node did (written by whoever
// checks, under claude-plugin.lock); claude-plugin.status.json the last check's result, which `persona-link status`
// reads without running anything and the resident node passes on to the hub when a command changed it.
const (
	ChoicesFile = "claude-plugin.json"
	StatusFile  = "claude-plugin.status.json"
	lockName    = "claude-plugin.lock"
)

// choices is claude-plugin.json.
type choices struct {
	// Auto: install and update the plugin by itself. Absent: the platform's default (Manager.DefaultAuto).
	Auto *bool `json:"auto,omitempty"`
	// Dirs is what the node knows of each config directory, by its real path.
	Dirs map[string]dirRecord `json:"dirs,omitempty"`
}

// dirRecord is the node's part in one config directory.
type dirRecord struct {
	// Installed: the node installed the plugin here and has not seen it removed since. Seen gone, it was removed
	// outside Persona: that is an opt-out, not a reason to install it again.
	Installed bool `json:"installed,omitempty"`
	// OptedOut: proto.ClaudePluginOptedOutDisabled | …Uninstalled, "" none.
	OptedOut string    `json:"opted_out,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

func (c choices) auto(def bool) bool {
	if c.Auto == nil {
		return def
	}
	return *c.Auto
}

func (c *choices) setAuto(on bool) { c.Auto = &on }

// readChoices reads claude-plugin.json; a missing or unreadable one is the defaults (an unreadable one is logged by
// the caller and written over by the next change).
func readChoices(dir string) (choices, error) {
	var c choices
	b, err := os.ReadFile(filepath.Join(dir, ChoicesFile))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil {
		return choices{}, err
	}
	return c, nil
}

func writeChoices(dir string, c choices) error { return writeJSON(filepath.Join(dir, ChoicesFile), c) }

// ReadStatus is the last check's result as claude-plugin.status.json keeps it (dir: the data directory).
func ReadStatus(dir string) (*proto.ClaudePlugin, error) {
	b, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return nil, err
	}
	var st proto.ClaudePlugin
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func writeStatus(dir string, st proto.ClaudePlugin) error {
	return writeJSON(filepath.Join(dir, StatusFile), st)
}

// writeJSON writes v to path whole or not at all (a temporary file renamed over it).
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
