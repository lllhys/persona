package proto

import "time"

// ClaudePlugin is the Persona plugin for Claude Code on a computer, as its node last checked it
// (docs/operations/link.md "Claude Code plugin"): the node carries the plugin of its own build and keeps it installed
// at the user scope of each Claude Code config directory it manages. Sent with node.info and, when it changes, as the
// node's "claude.plugin" notification; the manager puts it on the machine (Machine.ClaudePlugin). Older nodes send
// nothing.
type ClaudePlugin struct {
	// State is the whole computer's: one of the ClaudePlugin* states below.
	State string `json:"state"`
	// Auto: the node installs and updates the plugin by itself (on unless turned off on the computer).
	Auto bool `json:"auto"`
	// Bundled is the plugin version this node build carries ("" when the build carries none: State unavailable);
	// Version, the installed one (the first config directory that has it).
	Bundled string `json:"bundled,omitempty"`
	Version string `json:"version,omitempty"`
	// ClaudeVersion is the Claude Code that ran the plugin commands.
	ClaudeVersion string `json:"claude_version,omitempty"`
	// ConfigDirs is each Claude Code config directory managed here, the default (~/.claude) first.
	ConfigDirs []ClaudePluginDir `json:"config_dirs,omitempty"`
	// Changed: the last check installed, updated or removed the plugin. Claude Code loads plugins when a session
	// starts: new sessions have it, sessions already open keep what they loaded.
	Changed bool `json:"changed,omitempty"`
	// Error is why the last check failed (State error), in English; NextCheck, when the node tries again.
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	NextCheck time.Time `json:"next_check,omitzero"`
}

// ClaudePluginDir is the plugin in one Claude Code config directory.
type ClaudePluginDir struct {
	Path    string `json:"path"`
	State   string `json:"state"` // ClaudePluginInstalled, …Outdated, …Disabled, …NotInstalled, …Error
	Version string `json:"version,omitempty"`
	// OptedOut: the user turned the plugin off in Claude Code (disabled) or removed it (uninstalled) after the node
	// installed it; the node leaves it so until the plugin is installed again on the computer.
	OptedOut string `json:"opted_out,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ClaudePlugin states.
const (
	ClaudePluginInstalled     = "installed"      // installed, enabled, the bundled version
	ClaudePluginOutdated      = "outdated"       // installed and enabled, another version than the bundled one
	ClaudePluginDisabled      = "disabled"       // installed, turned off in Claude Code
	ClaudePluginNotInstalled  = "not_installed"  // not installed
	ClaudePluginError         = "error"          // the last check failed (Error says why)
	ClaudePluginClaudeMissing = "claude_missing" // no Claude Code on this computer (no CLI, or never set up)
	ClaudePluginUnavailable   = "unavailable"    // this node build carries no plugin (built without go generate)
)

// ClaudePlugin opt-outs (ClaudePluginDir.OptedOut).
const (
	ClaudePluginOptedOutDisabled    = "disabled"
	ClaudePluginOptedOutUninstalled = "uninstalled"
)
