package kbhistory

import (
	"path/filepath"
	"sync"
	"time"
)

// ---- the log and the status ----

func (d *historyDriver) log(l LogLine) {
	if l.At.IsZero() {
		l.At = d.now()
	}
	if l.Source == "" {
		l.Source = d.source
	}
	l.historyReason = l.historyReason.logged()
	if err := AppendLog(d.data, d.zone, l); err != nil {
		d.logf("kb history: log: %v", err)
	}
}

// SourceStatus is a source's line of history/status.json.
type SourceStatus struct {
	State     string    `json:"state,omitempty"` // running, resting, quiet, paused, stopped, done, idle, unavailable
	Since     time.Time `json:"since,omitzero"`
	Until     time.Time `json:"until,omitzero"` // the end of a rest, quiet period or pause
	Calls     int       `json:"calls,omitempty"`
	LastCall  *LogLine  `json:"last_call,omitempty"`
	LastSlow  *Event    `json:"last_slow,omitempty"`
	LastPause *Event    `json:"last_pause,omitempty"`
	LastStop  *Event    `json:"last_stop,omitempty"`
	// SlowUntil: kb_history_resume with slow: the slowest pace until then. SlowPending: asked for, and the two hours
	// start with the next call (the first after the resume).
	SlowUntil   time.Time `json:"slow_until,omitzero"`
	SlowPending bool      `json:"slow_pending,omitempty"`
	// Pace is the pacer's state, so a restart goes on at the same pace (kbhistory/pace.go).
	Pace *historyPaceState `json:"pace,omitempty"`
}

type Event struct {
	At time.Time `json:"at"`
	historyReason
}

// paceState is the pacer's state with the pause the driver counts, for status.json.
func (d *historyDriver) paceState() *historyPaceState {
	if d.pacer == nil {
		return nil
	}
	ps := d.pacer.snapshot()
	ps.PausedSince = d.pausedSince
	return &ps
}

func historyStatusPath(data string) string { return filepath.Join(Dir(data), "status.json") }

var historyStatusMu sync.Mutex

func ReadStatus(data string) map[string]SourceStatus {
	out := map[string]SourceStatus{}
	_, _ = ReadJSONFile(historyStatusPath(data), &out)
	return out
}

func UpdateStatus(data, source string, f func(*SourceStatus)) {
	historyStatusMu.Lock()
	defer historyStatusMu.Unlock()
	all := ReadStatus(data)
	st := all[source]
	f(&st)
	all[source] = st
	_ = writeJSONFile(historyStatusPath(data), all)
}

func (d *historyDriver) status(source string) SourceStatus {
	return ReadStatus(d.data)[source]
}

func (d *historyDriver) setStatus(source string, f func(*SourceStatus)) {
	if source == "" {
		return
	}
	UpdateStatus(d.data, source, f)
}

func OrText(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
