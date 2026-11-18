package claudeplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Op is what a check does.
type Op string

const (
	// OpStatus looks and changes nothing (beyond writing the status it found).
	OpStatus Op = "status"
	// OpCheck is the resident node's periodic check: with the automatic install on, install or update the plugin where
	// the user has not opted out, and note the opt-outs it finds.
	OpCheck Op = "check"
	// OpInstall installs or updates it now in every config directory, turns it back on in Claude Code where it was
	// turned off, clears the opt-outs and turns the automatic install on: the user asked for it on this computer.
	OpInstall Op = "install"
	// OpUninstall removes it and its marketplace from every config directory and turns the automatic install off.
	OpUninstall Op = "uninstall"
	// OpOn turns the automatic install on, forgets earlier opt-outs, and checks (a plugin turned off in Claude Code
	// stays off).
	OpOn Op = "on"
	// OpOff turns the automatic install off and leaves what is installed as it is (no more updates).
	OpOff Op = "off"
)

// Ops are the operations a command can name.
var Ops = []Op{OpStatus, OpCheck, OpInstall, OpUninstall, OpOn, OpOff}

// Claude is the Claude Code CLI a check uses: its executable and version.
type Claude struct {
	Exe     string
	Version string
}

// Manager checks and changes the plugin on this computer.
type Manager struct {
	// DataDir is Persona's data directory (~/.persona): the marketplace, the choices and status files and the lock.
	DataDir string
	// Bundle is the plugin this build carries; nil when it carries none.
	Bundle *Bundle
	// Dirs are the Claude Code config directories to manage, the user's default first; those that do not exist (Claude
	// Code never ran with them) are left alone.
	Dirs []string
	// FindClaude finds the CLI; ok false when there is none.
	FindClaude func(ctx context.Context) (c Claude, ok bool)
	// Run runs it (ExecRunner by default).
	Run Runner
	// Logf says what was changed (nil: nothing is said).
	Logf func(format string, args ...any)
	// Now is the clock (time.Now by default).
	Now func() time.Time
	// DefaultAuto: the automatic install is on until the user turns it off (macOS and Windows, where the desktop app's
	// Code sessions need the plugin); elsewhere it is off until turned on.
	DefaultAuto bool
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// MarketRoot is the local marketplace's directory.
func (m *Manager) MarketRoot() string { return filepath.Join(m.DataDir, MarketDir) }

// Do runs op and returns what it found, also kept in the status file. err is set when op could not be done as asked
// (a command that failed); the status then says so too (State error, Error).
func (m *Manager) Do(ctx context.Context, op Op) (proto.ClaudePlugin, error) {
	if err := os.MkdirAll(m.DataDir, 0o755); err != nil {
		return proto.ClaudePlugin{State: proto.ClaudePluginError, Error: err.Error()}, err
	}
	unlock, err := lockFile(filepath.Join(m.DataDir, lockName))
	if err != nil {
		return proto.ClaudePlugin{State: proto.ClaudePluginError, Error: err.Error()}, err
	}
	defer unlock()
	ch, cerr := readChoices(m.DataDir)
	if cerr != nil {
		m.logf("claude plugin: %s unreadable (%v): taken as the defaults", ChoicesFile, cerr)
	}
	switch op {
	case OpInstall, OpOn:
		ch.setAuto(true)
		for key, rec := range ch.Dirs {
			rec.OptedOut = ""
			ch.Dirs[key] = rec
		}
	case OpUninstall, OpOff:
		ch.setAuto(false)
	}
	st, err := m.do(ctx, op, &ch)
	if op == OpUninstall && err == nil {
		// Out of every config directory (or there was no Claude Code to take it out of): the folder goes too.
		if rerr := os.RemoveAll(m.MarketRoot()); rerr != nil {
			err = rerr
		}
	}
	st.Auto = ch.auto(m.DefaultAuto)
	st.CheckedAt = m.now().UTC()
	if op != OpStatus || cerr == nil {
		if werr := writeChoices(m.DataDir, ch); werr != nil && err == nil {
			err = werr
		}
	}
	if err != nil && st.State != proto.ClaudePluginError {
		st.State, st.Error = proto.ClaudePluginError, err.Error()
	}
	if werr := writeStatus(m.DataDir, st); werr != nil {
		m.logf("claude plugin: %s: %v", StatusFile, werr)
	}
	return st, err
}

func (m *Manager) do(ctx context.Context, op Op, ch *choices) (proto.ClaudePlugin, error) {
	st := proto.ClaudePlugin{}
	if m.Bundle != nil {
		st.Bundled = m.Bundle.Version
	}
	if m.Bundle == nil && op != OpUninstall && op != OpOff {
		st.State = proto.ClaudePluginUnavailable
		return st, nil
	}
	if m.FindClaude == nil {
		st.State = proto.ClaudePluginClaudeMissing
		return st, nil
	}
	claude, ok := m.FindClaude(ctx)
	dirs := m.existingDirs()
	if !ok || len(dirs) == 0 {
		st.State = proto.ClaudePluginClaudeMissing
		if op == OpInstall {
			return st, errors.New("no Claude Code on this computer: install it and start it once, then try again")
		}
		return st, nil
	}
	st.ClaudeVersion = claude.Version
	run := m.Run
	if run == nil {
		run = ExecRunner(m.DataDir)
	}
	if ch.Dirs == nil {
		ch.Dirs = map[string]dirRecord{}
	}
	var firstErr error
	for _, dir := range dirs {
		c := cli{run: run, exe: claude.Exe, dir: dir}
		key := dirKey(dir)
		rec := ch.Dirs[key]
		ds, changed, err := m.dir(ctx, op, c, &rec, ch.auto(m.DefaultAuto))
		if err != nil {
			ds.State, ds.Error = proto.ClaudePluginError, err.Error()
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", dir, err)
			}
			m.logf("claude plugin: %s: %v", dir, err)
		}
		ds.Path = dir
		st.Changed = st.Changed || changed
		if rec == (dirRecord{}) {
			delete(ch.Dirs, key)
		} else {
			ch.Dirs[key] = rec
		}
		st.ConfigDirs = append(st.ConfigDirs, ds)
	}
	st.State, st.Version = overall(st.ConfigDirs)
	if firstErr != nil {
		st.Error = firstErr.Error()
	}
	return st, firstErr
}

// existingDirs are Dirs that exist, each once.
func (m *Manager) existingDirs() []string {
	var out []string
	for _, d := range m.Dirs {
		if d == "" {
			continue
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		dup := false
		for _, o := range out {
			dup = dup || SameDir(o, d)
		}
		if !dup {
			out = append(out, filepath.Clean(d))
		}
	}
	return out
}

// dirKey is a config directory's key in claude-plugin.json: its real path.
func dirKey(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return filepath.Clean(dir)
}

// dir runs op in one config directory. rec is the node's record of it, updated in place; changed says whether the
// plugin was installed, updated, turned on or removed there.
func (m *Manager) dir(ctx context.Context, op Op, c cli, rec *dirRecord, auto bool) (ds proto.ClaudePluginDir, changed bool, err error) {
	id := m.Bundle.ID()
	entry, err := c.list(ctx, id)
	if err != nil {
		return view(nil, *rec, m.Bundle), false, err
	}
	now := m.now().UTC()
	switch op {
	case OpStatus, OpOff:
		return view(entry, *rec, m.Bundle), false, nil
	case OpUninstall:
		if entry != nil {
			if err := c.do(ctx, "plugin", "uninstall", id, "--scope", "user"); err != nil && !errors.Is(err, errNotFound) {
				return view(entry, *rec, m.Bundle), false, err
			}
			changed = true
			m.logf("claude plugin: removed %s from %s", id, c.dir)
		}
		if err := c.do(ctx, "plugin", "marketplace", "remove", m.Bundle.market()); err != nil && !errors.Is(err, errNotFound) {
			return view(nil, *rec, m.Bundle), changed, err
		}
		*rec = dirRecord{}
		return view(nil, *rec, m.Bundle), changed, nil
	case OpInstall:
		if err := m.install(ctx, c, entry); err != nil {
			return view(entry, *rec, m.Bundle), false, err
		}
		*rec = dirRecord{Installed: true, At: now}
		changed = entry == nil || !entry.Enabled || entry.Version != m.Bundle.Version
	default: // OpCheck, OpOn: the automatic install, as far as the user lets it
		switch {
		case entry == nil && rec.Installed:
			// Installed by the node, gone now: removed in Claude Code. Not put back.
			*rec = dirRecord{OptedOut: proto.ClaudePluginOptedOutUninstalled, At: now}
			m.logf("claude plugin: %s was removed from %s outside Persona; leaving it removed", id, c.dir)
		case entry == nil:
			if !auto || rec.OptedOut != "" {
				break
			}
			if err := m.install(ctx, c, nil); err != nil {
				return view(nil, *rec, m.Bundle), false, err
			}
			*rec, changed = dirRecord{Installed: true, At: now}, true
		case !entry.Enabled:
			// Turned off in Claude Code: left off, and not updated.
			if rec.OptedOut != proto.ClaudePluginOptedOutDisabled {
				*rec = dirRecord{Installed: true, OptedOut: proto.ClaudePluginOptedOutDisabled, At: now}
				m.logf("claude plugin: %s is turned off in %s; leaving it off", id, c.dir)
			}
		default:
			rec.Installed, rec.OptedOut = true, "" // on in Claude Code (again): the user's choice too
			if !auto {
				break
			}
			if err := m.install(ctx, c, entry); err != nil {
				return view(entry, *rec, m.Bundle), false, err
			}
			changed = entry.Version != m.Bundle.Version
		}
	}
	if changed {
		if entry, err = c.list(ctx, id); err != nil {
			return view(nil, *rec, m.Bundle), changed, err
		}
	}
	return view(entry, *rec, m.Bundle), changed, nil
}

// install brings the plugin in c's directory to the bundled build: the marketplace written out and registered, then
// installed (entry nil), turned on, or updated. A plugin already at the bundled version only gets its files written
// (a build of the same version with other files, or a marketplace directory someone deleted).
func (m *Manager) install(ctx context.Context, c cli, entry *listed) error {
	wrote, err := syncDir(m.MarketRoot(), m.Bundle.marketFiles())
	if err != nil {
		return fmt.Errorf("writing %s: %w", m.MarketRoot(), err)
	}
	if wrote {
		m.logf("claude plugin: wrote %s %s to %s", m.Bundle.Name, m.Bundle.Version, m.MarketRoot())
	}
	if entry != nil && entry.Enabled && entry.Version == m.Bundle.Version {
		return nil
	}
	if err := c.do(ctx, "plugin", "marketplace", "add", m.MarketRoot()); err != nil {
		return err
	}
	id := m.Bundle.ID()
	switch {
	case entry == nil:
		if err := c.do(ctx, "plugin", "install", id, "--scope", "user"); err != nil {
			return err
		}
		m.logf("claude plugin: installed %s %s in %s", id, m.Bundle.Version, c.dir)
		return nil
	case !entry.Enabled:
		if err := c.do(ctx, "plugin", "enable", id, "--scope", "user"); err != nil {
			return err
		}
		m.logf("claude plugin: turned %s on in %s", id, c.dir)
	}
	if entry.Version != m.Bundle.Version {
		if err := c.do(ctx, "plugin", "update", id, "--scope", "user"); err != nil {
			return err
		}
		m.logf("claude plugin: updated %s from %s to %s in %s", id, entry.Version, m.Bundle.Version, c.dir)
	}
	return nil
}

// view is a config directory's state from its install entry (nil: none) and the node's record.
func view(entry *listed, rec dirRecord, b *Bundle) proto.ClaudePluginDir {
	ds := proto.ClaudePluginDir{OptedOut: rec.OptedOut}
	switch {
	case entry == nil:
		ds.State = proto.ClaudePluginNotInstalled
	case !entry.Enabled:
		ds.State, ds.Version = proto.ClaudePluginDisabled, entry.Version
	case b != nil && entry.Version != b.Version:
		ds.State, ds.Version = proto.ClaudePluginOutdated, entry.Version
	default:
		ds.State, ds.Version = proto.ClaudePluginInstalled, entry.Version
	}
	return ds
}

// overall is the computer's state from its directories': an error first, then the first that is not installed as it
// should be, else installed. version is the first installed one's.
func overall(dirs []proto.ClaudePluginDir) (state, version string) {
	rank := map[string]int{proto.ClaudePluginError: 0, proto.ClaudePluginOutdated: 1, proto.ClaudePluginNotInstalled: 2,
		proto.ClaudePluginDisabled: 3, proto.ClaudePluginInstalled: 4}
	state = proto.ClaudePluginClaudeMissing
	best := 5
	for _, d := range dirs {
		if r, ok := rank[d.State]; ok && r < best {
			state, best = d.State, r
		}
		if version == "" && d.Version != "" {
			version = d.Version
		}
	}
	return state, version
}
