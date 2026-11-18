//go:build linux || darwin

package node

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/screen"
)

// Ctrl+D enters Codex's shutdown-first TUI path. Process signals do not: never
// replace this with SIGTERM/SIGKILL, or treat a timeout as a released writer.
func (n *Node) stopCodexWriter(ctx context.Context, writer codexWriter) error {
	send, err := prepareCodexWriterQuit(ctx, writer)
	if err != nil {
		return err
	}
	if !sameProcess(writer.PID, writer.Start) {
		return takeoverRefusal("changed")
	}
	if err := send(); err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, takeoverExitWait)
	defer cancel()
	for sameProcess(writer.PID, writer.Start) {
		select {
		case <-wait.Done():
			// No escalation: a stuck writer stays in its original terminal.
			return takeoverRefusal("writer_busy")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

func prepareCodexWriterQuit(ctx context.Context, writer codexWriter) (func() error, error) {
	if writer.PID <= 1 || writer.Start == "" || !sameProcess(writer.PID, writer.Start) {
		return nil, takeoverRefusal("changed")
	}
	procs, err := scanOrcaProcs()
	if err != nil {
		return nil, takeoverRefusal("unavailable")
	}
	pane := ""
	for key, ps := range procs {
		for _, p := range ps {
			if p.pid == writer.PID {
				pane = key
			}
		}
	}
	if pane == "" {
		return nil, takeoverRefusal("unavailable")
	}
	return codexQuitTerminal(ctx, writer, pane)
}

// Prefer the incarnation-fenced native socket. An SSH terminal can instead carry
// its host's CLI bridge in the selected writer's own environment, not the node's.
func codexQuitTerminal(ctx context.Context, writer codexWriter, pane string) (func() error, error) {
	c := orcart.New()
	if list, err := c.Terminals(ctx, false); err == nil {
		for _, t := range list.Terminals {
			if t.PaneKey() == pane && t.Local() {
				target := t.Target()
				check := func() error {
					s, err := c.Read(ctx, target)
					if err != nil || !codexQuitComposer(s.Text()) {
						return takeoverRefusal("unavailable")
					}
					return nil
				}
				if err := check(); err != nil {
					return nil, err
				}
				return func() error {
					if err := check(); err != nil {
						return err
					}
					return c.Send(ctx, target, "\x04")
				}, nil
			}
		}
	}
	if runtime.GOOS != "linux" {
		return nil, takeoverRefusal("unavailable")
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(writer.PID) + "/environ")
	if err != nil || len(raw) > 1<<20 {
		return nil, takeoverRefusal("unavailable")
	}
	env := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	value := func(name string) string {
		for _, entry := range env {
			if v, ok := strings.CutPrefix(entry, name+"="); ok {
				return v
			}
		}
		return ""
	}
	command, handle := value("ORCA_CLI_COMMAND"), value("ORCA_TERMINAL_HANDLE")
	if !filepath.IsAbs(command) || handle == "" || value(orcaPaneEnv) != pane {
		return nil, takeoverRefusal("unavailable")
	}
	type terminalInfo struct {
		Handle, IncarnationID, TabID, LeafID string
		Writable                             bool
	}
	inspect := func() (terminalInfo, error) {
		var out struct {
			OK     bool `json:"ok"`
			Result struct {
				Terminal struct {
					Handle        string `json:"handle"`
					IncarnationID string `json:"incarnationId"`
					TabID         string `json:"tabId"`
					LeafID        string `json:"leafId"`
					Writable      bool   `json:"writable"`
				} `json:"terminal"`
			} `json:"result"`
		}
		call := exec.CommandContext(ctx, command, "terminal", "show", "--terminal", handle, "--json")
		call.Env = env
		b, err := call.Output()
		if err != nil || len(b) > 1<<20 || json.Unmarshal(b, &out) != nil || !out.OK {
			return terminalInfo{}, takeoverRefusal("unavailable")
		}
		t := out.Result.Terminal
		if t.Handle != handle || t.TabID+":"+t.LeafID != pane || t.IncarnationID == "" || !t.Writable {
			return terminalInfo{}, takeoverRefusal("changed")
		}
		return terminalInfo{t.Handle, t.IncarnationID, t.TabID, t.LeafID, t.Writable}, nil
	}
	before, err := inspect()
	if err != nil {
		return nil, err
	}
	checkComposer := func() error {
		var out struct {
			OK     bool `json:"ok"`
			Result struct {
				Terminal struct {
					Tail  []string `json:"tail"`
					Draft *string  `json:"draft"`
				} `json:"terminal"`
			} `json:"result"`
		}
		call := exec.CommandContext(ctx, command, "terminal", "read", "--terminal", handle, "--limit", "120", "--json")
		call.Env = env
		b, err := call.Output()
		if err != nil || len(b) > 1<<20 || json.Unmarshal(b, &out) != nil || !out.OK {
			return takeoverRefusal("unavailable")
		}
		s := out.Result.Terminal
		if s.Draft != nil && strings.TrimSpace(*s.Draft) != "" || !codexQuitComposer(strings.Join(s.Tail, "\n")) {
			return takeoverRefusal("unavailable")
		}
		return nil
	}
	if err := checkComposer(); err != nil {
		return nil, err
	}
	return func() error {
		now, err := inspect()
		if err != nil || now != before || !sameProcess(writer.PID, writer.Start) {
			return takeoverRefusal("changed")
		}
		if err := checkComposer(); err != nil {
			return err
		}
		call := exec.CommandContext(ctx, command, "terminal", "send", "--terminal", handle, "--text", "\x04", "--json")
		call.Env = env
		if err := call.Run(); err != nil {
			return takeoverRefusal("unavailable")
		}
		return nil
	}, nil
}

func codexQuitComposer(text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	// Some CLI status bars show remaining context instead of a directory. Keep
	// every draft row, but do not mistake this exact final footer pair for one.
	if len(lines) >= 3 {
		status := strings.TrimRight(screen.Plain(lines[len(lines)-2]), " \t")
		hint := strings.TrimSpace(screen.Plain(lines[len(lines)-1]))
		if strings.HasPrefix(status, "  ") && strings.Contains(status, " \u00b7 Context ") &&
			strings.HasSuffix(status, " window") && hint == "\u2190 for agents \u00b7 ? for shortcuts" {
			lines[len(lines)-2] = ""
			text = strings.Join(lines, "\n")
		}
	}
	s := screen.ReadCodex(text)
	return s.View == screen.CodexComposer && strings.TrimSpace(s.Draft) == "" && len(s.Queued) == 0
}
