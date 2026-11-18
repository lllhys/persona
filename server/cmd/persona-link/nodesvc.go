package main

import (
	"context"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/update"

	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The resident node (`personad node --attach`, docs/server/server.md "Persistent node") is what the connector forwards to. Setting
// it up here is packaging only: `persona-link install` sets up a computer's node service alongside the connector on
// every supported platform. It writes the same service scripts/deploy/node-launchd.sh does and leaves an existing one
// as it is. What the node does stays in personad.

const (
	nodeUnitName = "personad-node.service" // systemd user unit (Linux), as scripts/deploy/node-launchd.sh names it
	nodeLabel    = "com.persona.node"      // launchd agent (Mac), likewise
)

func (e *env) nodeLog() string { return filepath.Join(e.personaDir(), "node-attach.log") }

func (e *env) nodeUnitPath() string {
	dir := filepath.Dir(e.unitPath())
	return existingPath(filepath.Join(dir, nodeUnitName), filepath.Join(dir, legacyNodeUnit))
}
func (e *env) nodePlistPath() string {
	dir := filepath.Join(e.home, "Library", "LaunchAgents")
	return existingPath(filepath.Join(dir, nodeLabel+".plist"), filepath.Join(dir, legacyNodeLabel+".plist"))
}

// nodeServiceFile is where this computer's resident node service is described; "" where there is none to write.
func (e *env) nodeServiceFile() string {
	switch e.goos {
	case "linux":
		return e.nodeUnitPath()
	case "darwin":
		return e.nodePlistPath()
	case "windows":
		return e.taskFile() // the node is part of persona-link's scheduled task (winservice.go)
	}
	return ""
}

// nodeUnitFile is the resident node's systemd user unit: scripts/deploy/node-launchd.sh's, without network-online.target
// (a user instance has none, docs/lessons/server.md).
func nodeUnitFile(bin, logPath string) string {
	return `# Written by persona-link (docs/operations/link.md); persona-link node uninstall removes it.
[Unit]
Description=Persona resident node (docs/server/server.md)

[Service]
ExecStart=` + systemdQuote(bin) + ` node --attach
` + stateEnvironment(filepath.Dir(logPath)) + `Restart=always
RestartSec=10
StandardError=append:` + strings.ReplaceAll(logPath, "%", "%%") + `

[Install]
WantedBy=default.target
`
}

// nodePlist is the resident node's launchd agent: scripts/deploy/node-launchd.sh's. ProcessType Interactive: without it
// launchd throttles the node and the Claude Code it starts like a background job, and claude may not start in time
// (#5944; `personad node doctor --fix` adds it to an older plist).
func nodePlist(bin, logPath string) string {
	x := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + nodeLabel + `</string>
  <key>ProgramArguments</key><array><string>` + x(bin) + `</string><string>node</string><string>--attach</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>PERSONA_HOME</key><string>` + x(filepath.Dir(logPath)) + `</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardErrorPath</key><string>` + x(logPath) + `</string>
</dict></plist>
`
}

// nodeInstall sets up the resident node service unless it is there already: the node program (nodeBin: persona-node
// when this computer has it, else personad; from is copied in when it has neither, to the name of the program it is),
// then the service, started. One there already stays as it is (node switch moves one that runs personad over).
// fromAny: from may be either program (--node-bin; it is asked which), else it is personad (--personad).
func nodeInstall(e *env, from string, fromAny ...bool) error {
	if err := e.installationConflict(true); err != nil {
		return err
	}
	if e.goos == "windows" {
		return link.NewTextError("link-cli.windows_node_hint", nil)
	}
	file := e.nodeServiceFile()
	if file == "" {
		return link.NewTextError("link-cli.node_service_unsupported", nil, i18n.Args{"goos": e.goos})
	}
	if fileExists(file) {
		fmt.Fprintln(e.stdout, terminalText("link-cli.node_service_existing", i18n.Args{"path": file}))
		if b, err := os.ReadFile(file); err == nil && e.serviceProgram(string(b)) == e.personadBin() &&
			e.programOf(e.personaNodeBin()) == programPersonaNode && !e.hostsHubOrManager() {
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_switch_hint", i18n.Args{"path": e.personaNodeBin()}))
		}
		return nil
	}
	fromProg := programPersonad // --personad: personad
	if len(fromAny) > 0 && fromAny[0] && from != "" {
		// --node-bin: put in under the name of the program it says it is; one that does not say is not guessed at.
		if fromProg = e.programOf(from); fromProg == "" {
			return link.NewTextError("link-cli.node_program_unknown", nil, i18n.Args{"path": from})
		}
	} else if from != "" && update.ProgramOf(from) == programPersonaNode { // read from its build info: nothing is run
		return link.NewTextError("link-cli.node_wrong_flag", nil, i18n.Args{"path": from})
	}
	if _, err := identity.PrepareHome(e.home); err != nil {
		return err
	}
	if from != "" && fromProg == programPersonaNode && !e.hostsHubOrManager() && e.programOf(e.personaNodeBin()) != programPersonaNode {
		// The device's own program, under its own name, beside a personad this computer may have.
		if err := copyExecutable(from, e.personaNodeBin()); err != nil {
			return link.NewTextError("link-cli.binary_install_failed", err, i18n.Args{"path": e.personaNodeBin()})
		}
		fmt.Fprintln(e.stdout, terminalText("link-cli.binary_installed", i18n.Args{"path": e.personaNodeBin()}))
	}
	bin := e.nodeBin()
	if !fileExists(bin) {
		if from != "" && bin == e.personadBin() && fromProg == programPersonaNode {
			if !e.hostsHubOrManager() {
				return link.NewTextError("link-cli.node_program_not_runnable", nil, i18n.Args{"path": e.personaNodeBin()})
			}
			return link.NewTextError("link-cli.node_hub_requires_personad", nil, i18n.Args{"path": from})
		}
		if from == "" {
			return link.NewTextError("link-cli.node_binary_required", nil, i18n.Args{"path": bin})
		}
		if err := copyExecutable(from, bin); err != nil {
			return link.NewTextError("link-cli.binary_install_failed", err, i18n.Args{"path": bin})
		}
		fmt.Fprintln(e.stdout, terminalText("link-cli.binary_installed", i18n.Args{"path": bin}))
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	switch e.goos {
	case "linux":
		if why := e.systemdMissing(); why != "" {
			return link.NewTextError("link-cli.node_no_systemd", nil, i18n.Args{"error": systemdReason(why), "binary": shellQuote(bin), "log": shellQuote(e.nodeLog())})
		}
		if err := os.WriteFile(file, []byte(nodeUnitFile(bin, e.nodeLog())), 0o644); err != nil {
			return err
		}
		for _, step := range [][]string{{"daemon-reload"}, {"enable", "--now", e.residentUnitName()}} {
			if out, err := e.systemctl(step...); err != nil {
				return link.NewTextError("link-cli.systemctl_step_failed", nil, i18n.Args{"step": strings.Join(step, " "), "error": orErr(out, err)})
			}
		}
	case "darwin":
		if err := os.WriteFile(file, []byte(nodePlist(bin, e.nodeLog())), 0o644); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(e.uid)
		_, _ = e.command("launchctl", "bootout", domain+"/"+e.residentLabel())
		if out, err := e.command("launchctl", "bootstrap", domain, file); err != nil {
			return link.NewTextError("link-cli.launchctl_bootstrap_failed", nil, i18n.Args{"error": orErr(out, err)})
		}
	}
	fmt.Fprintln(e.stdout, terminalText("link-cli.node_service_installed", i18n.Args{"path": file, "log": e.nodeLog()}))
	return nil
}

// nodeState is `persona-link node status --json`.
type nodeState struct {
	Installed bool   `json:"installed"`
	File      string `json:"file,omitempty"`
	Bin       string `json:"bin"`
	HasBin    bool   `json:"has_bin"`
	// Runs is the program the installed service runs (its file says), when that can be read; SwitchPending, a node
	// switch that did not finish (the next node install or switch, or persona-link run, puts the old service back).
	Runs          string `json:"runs,omitempty"`
	SwitchPending bool   `json:"switch_pending,omitempty"`
	// Running: launchd has the agent loaded / systemd says active; unknown ("") where that cannot be asked.
	Running string `json:"running,omitempty"`
}

func nodeStatus(e *env) nodeState {
	st := nodeState{File: e.nodeServiceFile(), Bin: e.nodeBin(), HasBin: fileExists(e.nodeBin())}
	st.Installed = st.File != "" && fileExists(st.File)
	if b, err := os.ReadFile(st.File); st.Installed && err == nil {
		if runs := e.serviceProgram(string(b)); runs != "" {
			st.Runs, st.Bin, st.HasBin = runs, runs, fileExists(runs)
		}
	}
	st.SwitchPending = e.switchPending()
	if e.goos == "windows" {
		st.Installed = e.taskRunsNode()
	}
	if !st.Installed {
		return st
	}
	switch e.goos {
	case "linux":
		if e.systemdMissing() == "" {
			out, _ := e.systemctl("is-active", e.residentUnitName())
			st.Running = firstLine(out)
		}
	case "darwin":
		// Read-only: whether launchd has it. Not by connecting to the node's socket, which would take the node from the hub.
		if _, err := e.command("launchctl", "print", "gui/"+strconv.Itoa(e.uid)+"/"+e.residentLabel()); err == nil {
			st.Running = "loaded"
		} else {
			st.Running = "not loaded"
		}
	case "windows":
		st.Running, _ = e.taskStatus()
	}
	return st
}

// nodeCmd is `persona-link node install [--node-bin <path> | --personad <path>] | switch | status [--json] | uninstall`.
func nodeCmd(e *env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "install":
		var from string
		var fromAny bool
		for i := 1; i < len(args); i++ {
			v, next, ok, err := flagValue(args, i, "--node-bin")
			isAny := ok
			if !ok && err == nil {
				v, next, ok, err = flagValue(args, i, "--personad")
			}
			switch {
			case err != nil:
				return err
			case !ok:
				return errUsage
			}
			from, fromAny, i = v, isAny, next
		}
		return lockedNodeCommand(e, true, func() error { return nodeInstall(e, from, fromAny) })
	case "switch":
		to := programPersonaNode
		switch {
		case len(args) == 1:
		case len(args) == 3 && args[1] == "--to" && (args[2] == programPersonad || args[2] == programPersonaNode):
			to = args[2]
		default:
			return errUsage
		}
		return nodeSwitch(context.Background(), e, to)
	case "status":
		st := nodeStatus(e)
		if len(args) > 1 && args[1] == "--json" {
			enc := json.NewEncoder(e.stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		switch {
		case st.File == "":
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_status_unsupported", i18n.Args{"goos": e.goos}))
		case !st.Installed && e.goos == "windows":
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_status_windows_missing", i18n.Args{"task": taskName}))
		case !st.Installed:
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_status_missing"))
		default:
			running := ""
			if st.Running != "" {
				running = " · " + nodeRunningText(st.Running)
			}
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_status_installed", i18n.Args{"path": st.File, "running": running}))
			if st.Runs != "" {
				fmt.Fprintln(e.stdout, terminalText("link-cli.node_status_runs", i18n.Args{"program": st.Runs}))
			}
			if st.SwitchPending {
				fmt.Fprintln(e.stdout, terminalText("link-cli.node_switch_pending"))
			}
		}
		if !st.HasBin {
			fmt.Fprintln(e.stdout, terminalText("link-cli.binary_missing", i18n.Args{"path": st.Bin}))
		}
		if st.Installed {
			return nil
		}
		return exitCode(1)
	case "uninstall":
		return lockedNodeCommand(e, false, func() error {
			if e.goos == "windows" {
				return link.NewTextError("link-cli.windows_node_hint", nil)
			}
			file := e.nodeServiceFile()
			if file == "" || !fileExists(file) {
				fmt.Fprintln(e.stdout, terminalText("link-cli.node_uninstall_missing"))
				return nil
			}
			switch e.goos {
			case "linux":
				if e.systemdMissing() == "" {
					_, _ = e.systemctl("disable", "--now", e.residentUnitName())
				}
			case "darwin":
				_, _ = e.command("launchctl", "bootout", "gui/"+strconv.Itoa(e.uid)+"/"+e.residentLabel())
			}
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if e.goos == "linux" {
				_, _ = e.systemctl("daemon-reload")
			}
			fmt.Fprintln(e.stdout, terminalText("link-cli.node_uninstalled", i18n.Args{"path": file, "binary": e.nodeBin()}))
			return nil
		})
	}
	return errUsage
}

// nodeRunningText localizes only the two values nodeStatus synthesizes. Service-manager output stays as reported.
func nodeRunningText(running string) string {
	switch running {
	case "loaded":
		return terminalText("link-cli.node_running_loaded")
	case "not loaded":
		return terminalText("link-cli.node_running_not_loaded")
	}
	return running
}

// copyExecutable puts a copy of src at dst (0755), whole or not at all.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
