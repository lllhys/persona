package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/proto"
)

// The Persona plugin for Claude Code (docs/operations/link.md "Claude Code plugin") comes with the resident node: the
// node carries the plugin of its own build and keeps it installed (internal/claudeplugin). `persona-link plugin` runs
// the node's program (`claude-plugin <op>`, which prints JSON) and says the result in words; `persona-link status`
// reads the last result the node left (claudePluginStatusFile) without running anything.

// claudePluginStatusFile is claudeplugin.StatusFile: read here directly, so persona-link does not carry the plugin.
const claudePluginStatusFile = "claude-plugin.status.json"

var pluginOps = map[string]bool{"status": true, "install": true, "uninstall": true, "on": true, "off": true}

// pluginCmd is `persona-link plugin status|install|uninstall|on|off [--json]`.
func pluginCmd(e *env, args []string) error {
	op, asJSON := "", false
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case op == "" && pluginOps[a]:
			op = a
		default:
			return errUsage
		}
	}
	if op == "" {
		return errUsage
	}
	bin := nodeStatus(e).Bin
	if !e.exists(bin) {
		return link.NewTextError("link-cli.plugin_no_node", nil, i18n.Args{"path": bin})
	}
	stdout, stderr, err := e.output(bin, "claude-plugin", op)
	var st proto.ClaudePlugin
	if jerr := json.Unmarshal([]byte(stdout), &st); jerr != nil || st.State == "" {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 2 || err == nil {
			// The node's program answered with its usage, or with no status: a build from before the plugin.
			return link.NewTextError("link-cli.plugin_node_too_old", nil, i18n.Args{"path": bin})
		}
		return link.NewTextError("link-cli.plugin_failed", nil, i18n.Args{"error": orErr(firstLine(stderr), err)})
	}
	if asJSON {
		fmt.Fprint(e.stdout, stdout)
	} else {
		fmt.Fprint(e.stdout, pluginText(&st, true))
	}
	if err != nil {
		return exitCode(1)
	}
	return nil
}

// pluginText is a status in words: the computer's state, the automatic install, each directory when there is more
// to say than the state, and what a change means for sessions already open (detail).
func pluginText(st *proto.ClaudePlugin, detail bool) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line(pluginStateText(st))
	if st.State == proto.ClaudePluginUnavailable || st.State == proto.ClaudePluginClaudeMissing && !detail {
		return b.String()
	}
	if st.Auto {
		line(terminalText("link-cli.plugin_auto_on"))
	} else {
		line(terminalText("link-cli.plugin_auto_off"))
	}
	if !detail {
		return b.String()
	}
	for _, d := range st.ConfigDirs {
		if len(st.ConfigDirs) > 1 || d.OptedOut != "" || d.Error != "" {
			line(terminalText("link-cli.plugin_dir", i18n.Args{"path": d.Path, "state": pluginDirText(d)}))
		}
	}
	if st.Changed {
		line(terminalText("link-cli.plugin_new_sessions"))
	}
	return b.String()
}

func pluginStateText(st *proto.ClaudePlugin) string {
	switch st.State {
	case proto.ClaudePluginInstalled:
		return terminalText("link-cli.plugin_state_installed", i18n.Args{"version": st.Version})
	case proto.ClaudePluginOutdated:
		return terminalText("link-cli.plugin_state_outdated", i18n.Args{"version": st.Version, "bundled": st.Bundled})
	case proto.ClaudePluginDisabled:
		return terminalText("link-cli.plugin_state_disabled")
	case proto.ClaudePluginNotInstalled:
		return terminalText("link-cli.plugin_state_not_installed")
	case proto.ClaudePluginClaudeMissing:
		return terminalText("link-cli.plugin_state_claude_missing")
	case proto.ClaudePluginUnavailable:
		return terminalText("link-cli.plugin_state_unavailable")
	}
	return terminalText("link-cli.plugin_state_error", i18n.Args{"error": st.Error})
}

func pluginDirText(d proto.ClaudePluginDir) string {
	switch {
	case d.State == proto.ClaudePluginError:
		return terminalText("link-cli.plugin_word_error", i18n.Args{"error": d.Error})
	case d.State == proto.ClaudePluginNotInstalled && d.OptedOut == proto.ClaudePluginOptedOutUninstalled:
		return terminalText("link-cli.plugin_word_removed")
	case d.State == proto.ClaudePluginDisabled:
		return terminalText("link-cli.plugin_word_disabled")
	case d.State == proto.ClaudePluginOutdated:
		return terminalText("link-cli.plugin_word_outdated", i18n.Args{"version": d.Version})
	case d.State == proto.ClaudePluginInstalled:
		return terminalText("link-cli.plugin_word_installed", i18n.Args{"version": d.Version})
	}
	return terminalText("link-cli.plugin_word_not_installed")
}

// savedPluginStatus is the last status the node left (nil when it never checked).
func (e *env) savedPluginStatus() *proto.ClaudePlugin {
	b, err := os.ReadFile(filepath.Join(e.personaDir(), claudePluginStatusFile))
	if err != nil {
		return nil
	}
	var st proto.ClaudePlugin
	if json.Unmarshal(b, &st) != nil || st.State == "" {
		return nil
	}
	return &st
}
