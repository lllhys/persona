package node

import (
	"fmt"
	"os"
	"path/filepath"
)

// The Grok CLI's own settings (Settings › Agents › Grok, #8353), in $GROK_HOME/config.toml (default ~/.grok/config.toml).
// Only these keys are read or written, through the table-aware helpers (tomltable.go), so the rest of the file stays as it
// was. Same shape as Claude's (claudeconfig.go); the web shows each row in the page's language by its key and falls back to
// the English title and detail here.
var grokSettings = []ClaudeSetting{
	{Key: "models.default", Title: "Default model", Detail: "The model new Grok sessions use. Empty uses Grok's own default.", Type: "string", Default: ""},
	{Key: "models.default_reasoning_effort", Title: "Reasoning effort", Detail: "Default reasoning effort for the default model, when the model supports it, such as low or high.", Type: "string", Default: ""},
	{Key: "ui.permission_mode", Title: "Permission mode", Detail: "Whether Grok asks before it uses a tool. always-approve approves every tool call without asking.", Type: "enum", Options: []string{"default", "ask", "auto", "always-approve"}, Default: "default"},
	{Key: "ui.yolo", Title: "Approve all tool calls", Detail: "Always approves tool calls without asking (yolo mode).", Type: "bool", Default: false},
	{Key: "ui.compact_mode", Title: "Compact mode", Detail: "Denser message padding in the terminal.", Type: "bool", Default: false},
}

func (n *Node) grokConfigPath() string {
	dir := os.Getenv("GROK_HOME")
	if dir == "" {
		dir = filepath.Join(n.home, ".grok")
	}
	p := filepath.Join(dir, "config.toml")
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real // a symlink into a dotfiles repo: edit the target
	}
	return p
}

// getGrokSettings: the whitelist with the file's values; a machine without the file gets the defaults, as the other agents do.
func (n *Node) getGrokSettings() ([]ClaudeSetting, error) {
	b, err := os.ReadFile(n.grokConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	vals := tomlValues(string(b))
	out := make([]ClaudeSetting, len(grokSettings))
	for i, s := range grokSettings {
		if v, ok := vals[s.Key]; ok {
			s.Value = v
		}
		out[i] = s
	}
	return out, nil
}

func (n *Node) setGrokSetting(key string, value any) error {
	var def *ClaudeSetting
	for i := range grokSettings {
		if grokSettings[i].Key == key {
			def = &grokSettings[i]
		}
	}
	if def == nil {
		return fmt.Errorf("setting %q is not editable from the app", key)
	}
	if err := checkSettingValue(*def, value); err != nil {
		return err
	}
	path := n.grokConfigPath()
	agentConfigMu.Lock()
	defer agentConfigMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text, err := tomlSetDotted(string(b), key, value)
	if err != nil {
		return err
	}
	return writeConfigFile(path, []byte(text), 0o600)
}
