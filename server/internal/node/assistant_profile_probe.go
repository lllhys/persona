package node

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// The launcher probes on a message's path are kept instead of taken again before every message (#8947) and at every
// start of its conversation's process (#9097). A probe is a login shell that sources the user's startup files, about a
// second with an ordinary ~/.zshrc and more on a busy machine. A message to an assistant conversation needs the
// launcher's profile (assistantLauncherProfile), the agent it runs (launcherProbeCached) and whether it passes the model
// on (launcherModelCheck); a start of its process needs its compaction settings (assistantCompactionPolicy) and, on a
// resume, the model it would run on (resumeModel). Each used to take the same probe itself: a start after a quiet
// spell waited for four or five login shells one after another.
//
// keptProbe is that probe, one per launcher and arguments, shared by all of them. The checks themselves still run on
// every message, on a probe at most profileProbeFresh old. The probe is taken again at once when what it depends on
// changes (shellStamp): a launcher edited in ~/.zshrc to set another config directory is refused on the very next
// message, as before. A failed probe is kept only profileProbeRetry, so a shell that was slow once is not taken for a
// launcher that sets nothing for long. Callers asking at the same time share one shell (the hub's model list and a
// message, the profile and agent checks of one start), which runs to its end even when the caller that started it gives
// up, so what it found is kept for the next one.
//
// A kept probe used in the last profileProbeKeep is taken again in the background profileProbeRenew before it would
// age out (keepProbesFresh), so a message after a quiet spell finds it fresh rather than waiting for the shell. Nothing
// about what a message sees changes: the probe it reads is never older than profileProbeFresh, and an edited startup
// file is seen at once either way. A renewal that fails leaves the kept probe as it was, to age out.
var (
	profileProbeFresh = 10 * time.Minute
	profileProbeRetry = time.Minute
	// profileProbeSettle: a file written this recently may be written again within one timestamp tick at the same
	// size, which its stamp would not tell apart (docs/lessons/agents.md, #5659): until then every message probes.
	profileProbeSettle = 2 * time.Second
	profileProbeRenew  = 2 * time.Minute
	profileProbeKeep   = time.Hour
	// profileProbeTick: how often keepProbesFresh looks for probes due for renewal.
	profileProbeTick = time.Minute
)

var profileProbes = struct {
	sync.Mutex
	got    map[string]profileProbe
	flight singleflight.Group
}{got: map[string]profileProbe{}}

type profileProbe struct {
	at    time.Time
	stamp string
	probe Probe
	err   error
	used  time.Time // the last time a caller read it: renewed only while it is in use
	// home, launcher, args: what it is a probe of, for its renewal
	home, launcher string
	args           []string
}

func keptProbeKey(home, launcher string, args []string) string {
	key := home + "\x00" + launcher
	for _, a := range args {
		key += "\x00" + a
	}
	return key
}

// keptProbe is launcherProbe(ctx, launcher, args...), kept as described above. The Probe is shared: read it, never
// change it. ctx bounds only the wait: the shell is not stopped with it.
func (n *Node) keptProbe(ctx context.Context, launcher string, args ...string) (Probe, error) {
	key := keptProbeKey(n.home, launcher, args)
	stamp, settled := shellStamp(n.home, launcher)
	now := time.Now()
	profileProbes.Lock()
	c, hit := profileProbes.got[key]
	if hit && settled && c.stamp == stamp && now.Sub(c.at) < profileProbeFresh && (c.err == nil || now.Sub(c.at) < profileProbeRetry) {
		c.used = now
		profileProbes.got[key] = c
		profileProbes.Unlock()
		return c.probe, c.err
	}
	profileProbes.Unlock()
	// One shell for the callers that see the same stamp; one that sees a newer stamp does not take the answer of a
	// shell that may have read the startup files before the edit.
	got := profileProbes.flight.DoChan(key+"\x00"+stamp, func() (any, error) {
		return n.takeProbe(context.WithoutCancel(ctx), key, stamp, launcher, args, false), nil
	})
	select {
	case r := <-got:
		k := r.Val.(profileProbe)
		return k.probe, k.err
	case <-ctx.Done():
		return Probe{}, ctx.Err()
	}
}

// takeProbe runs the probe and keeps what it found under key, stamped with stamp (shellStamp taken before it ran, so an
// edit while it runs is seen by the next caller). A renewal is no use of the probe, and one that failed is returned but
// leaves what is kept.
func (n *Node) takeProbe(ctx context.Context, key, stamp, launcher string, args []string, renewal bool) profileProbe {
	p, err := n.launcherProbe(ctx, launcher, args...)
	now := time.Now()
	k := profileProbe{at: now, stamp: stamp, probe: p, err: err, used: now, home: n.home, launcher: launcher, args: args}
	if err != nil && renewal {
		return k
	}
	profileProbes.Lock()
	if old, ok := profileProbes.got[key]; ok && renewal {
		k.used = old.used
	}
	if len(profileProbes.got) > 256 {
		profileProbes.got = map[string]profileProbe{}
	}
	profileProbes.got[key] = k
	profileProbes.Unlock()
	return k
}

// keepProbesFresh renews this node's kept probes in the background while it runs (see the top).
func (n *Node) keepProbesFresh(ctx context.Context) {
	t := time.NewTicker(profileProbeTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.renewKeptProbes(ctx)
		}
	}
}

// renewKeptProbes takes again, one at a time, every kept probe of this node's home that worked, is within
// profileProbeRenew of aging out and was used in the last profileProbeKeep. A launcher whose startup files were
// written in the last profileProbeSettle waits for the next round. It returns how many it took.
func (n *Node) renewKeptProbes(ctx context.Context) int {
	due := func(c profileProbe, now time.Time) bool {
		age := now.Sub(c.at)
		return c.home == n.home && c.err == nil && age >= profileProbeFresh-profileProbeRenew && age < profileProbeFresh &&
			now.Sub(c.used) < profileProbeKeep
	}
	now := time.Now()
	var keys []string
	profileProbes.Lock()
	for key, c := range profileProbes.got {
		if due(c, now) {
			keys = append(keys, key)
		}
	}
	profileProbes.Unlock()
	slices.Sort(keys)
	renewed := 0
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		profileProbes.Lock()
		c, ok := profileProbes.got[key]
		profileProbes.Unlock()
		if !ok || !due(c, time.Now()) { // a message took it again meanwhile
			continue
		}
		stamp, settled := shellStamp(n.home, c.launcher)
		if !settled {
			continue
		}
		profileProbes.flight.Do(key+"\x00"+stamp, func() (any, error) {
			return n.takeProbe(context.WithoutCancel(ctx), key, stamp, c.launcher, c.args, true), nil
		})
		renewed++
	}
	return renewed
}

// launcherProfileEnv is the environment the probe saw launcher set for the agent (Probe.Env), false when the probe
// failed. The map is shared: read it, never change it.
func (n *Node) launcherProfileEnv(ctx context.Context, launcher string) (map[string]string, bool) {
	p, err := n.keptProbe(ctx, launcher)
	return p.Env, err == nil
}

// shellStamp is what a launcher probe depends on besides the launcher's name, so a change to any of it is seen at once:
// the node's environment (the login shell inherits it), the startup files an interactive zsh or bash reads, and the
// launcher's own file (a wrapper script, which the probe reads instead of running). Files are compared by size and
// modification time; settled is false while one of them was written within profileProbeSettle. A file one of them
// sources is not followed: a change there is seen after profileProbeFresh.
func shellStamp(home, launcher string) (stamp string, settled bool) {
	h := sha256.New()
	env := os.Environ()
	slices.Sort(env)
	for _, kv := range env {
		h.Write([]byte(kv))
		h.Write([]byte{0})
	}
	zdot := cmp.Or(os.Getenv("ZDOTDIR"), home)
	files := []string{
		filepath.Join(zdot, ".zshenv"), filepath.Join(zdot, ".zshrc"),
		filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile"), filepath.Join(home, ".profile"),
		"/etc/zshenv", "/etc/zshrc", "/etc/zsh/zshenv", "/etc/zsh/zshrc", "/etc/bash.bashrc",
	}
	if p, err := exec.LookPath(launcher); err == nil {
		files = append(files, p)
	}
	launcherCache.Lock()
	if p := launcherCache.scripts[launcher]; p != "" {
		files = append(files, p)
	}
	launcherCache.Unlock()
	settled = true
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(h, "%s\x00%d\x00%d\n", f, st.Size(), st.ModTime().UnixNano())
			if time.Since(st.ModTime()) < profileProbeSettle {
				settled = false
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), settled
}
