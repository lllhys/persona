package claudeplugin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Keeper is the resident node's side: a check soon after it starts, then every Every, sooner after a failure
// (Backoff), and the latest status for node.info and the hub. A status a `claude-plugin` command wrote meanwhile (the
// status file changed) is taken and passed on too.
type Keeper struct {
	m *Manager
	// First, Every, Backoff and Poll are the timings (defaults below; tests shorten them).
	First   time.Duration
	Every   time.Duration
	Backoff []time.Duration
	Poll    time.Duration

	mu      sync.Mutex
	status  *proto.ClaudePlugin
	written time.Time // the status file's time when this keeper last wrote or read it
	subs    map[int]func(proto.ClaudePlugin)
	nextSub int
	wake    chan struct{}
}

// Default timings: the first check a little after the node starts (it has connections to take first), then hourly;
// a failed one again after 1, 5 and 15 minutes, then hourly. The status file is looked at every 30 s.
var (
	DefaultFirst   = 20 * time.Second
	DefaultEvery   = time.Hour
	DefaultBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}
	DefaultPoll    = 30 * time.Second
)

// NewKeeper keeps the plugin with m. The last status a check left is the status until this keeper's first check.
func NewKeeper(m *Manager) *Keeper {
	k := &Keeper{m: m, First: DefaultFirst, Every: DefaultEvery, Backoff: DefaultBackoff, Poll: DefaultPoll,
		subs: map[int]func(proto.ClaudePlugin){}, wake: make(chan struct{}, 1)}
	if st, err := ReadStatus(m.DataDir); err == nil {
		k.status = st
		k.written = k.statusTime()
	}
	return k
}

// Status is the latest status (nil before any check here or before).
func (k *Keeper) Status() *proto.ClaudePlugin {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.status == nil {
		return nil
	}
	st := *k.status
	return &st
}

// Subscribe calls f with every new status until the returned function is called.
func (k *Keeper) Subscribe(f func(proto.ClaudePlugin)) func() {
	k.mu.Lock()
	defer k.mu.Unlock()
	id := k.nextSub
	k.nextSub++
	k.subs[id] = f
	return func() {
		k.mu.Lock()
		delete(k.subs, id)
		k.mu.Unlock()
	}
}

// Wake asks for a check now.
func (k *Keeper) Wake() {
	select {
	case k.wake <- struct{}{}:
	default:
	}
}

// Run checks until ctx ends.
func (k *Keeper) Run(ctx context.Context) {
	failures := 0
	next := k.First
	for {
		timer := time.NewTimer(next)
		poll := time.NewTicker(k.Poll)
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				poll.Stop()
				return
			case <-timer.C:
				break wait
			case <-k.wake:
				timer.Stop()
				break wait
			case <-poll.C:
				k.takeWritten()
			}
		}
		poll.Stop()
		st, err := k.m.Do(ctx, OpCheck)
		if ctx.Err() != nil {
			return
		}
		if err != nil || st.State == proto.ClaudePluginError {
			next = k.Backoff[min(failures, len(k.Backoff)-1)]
			if failures >= len(k.Backoff) {
				next = k.Every
			}
			failures++
		} else {
			failures, next = 0, k.Every
		}
		st.NextCheck = time.Now().Add(next).UTC()
		k.publish(st, true)
	}
}

// takeWritten passes on a status a command wrote since this keeper last looked.
func (k *Keeper) takeWritten() {
	t := k.statusTime()
	k.mu.Lock()
	same := t.IsZero() || t.Equal(k.written)
	k.mu.Unlock()
	if same {
		return
	}
	st, err := ReadStatus(k.m.DataDir)
	if err != nil {
		return
	}
	k.publish(*st, false)
}

func (k *Keeper) statusTime() time.Time {
	fi, err := os.Stat(filepath.Join(k.m.DataDir, StatusFile))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// publish makes st the status and tells the subscribers when it says something new. save writes it to the status
// file first (the check wrote it without NextCheck).
func (k *Keeper) publish(st proto.ClaudePlugin, save bool) {
	if save {
		_ = writeStatus(k.m.DataDir, st)
	}
	t := k.statusTime()
	k.mu.Lock()
	k.written = t
	news := k.status == nil || !sameStatus(*k.status, st)
	k.status = &st
	subs := make([]func(proto.ClaudePlugin), 0, len(k.subs))
	for _, f := range k.subs {
		subs = append(subs, f)
	}
	k.mu.Unlock()
	if !news {
		return
	}
	for _, f := range subs {
		f(st)
	}
}

// sameStatus: a and b say the same but for when they were checked.
func sameStatus(a, b proto.ClaudePlugin) bool {
	a.CheckedAt, a.NextCheck, b.CheckedAt, b.NextCheck = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}
