package errreport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Windows and rates of the Gate.
const (
	IDWindow   = 24 * time.Hour   // the same report id again (a client's retry) is dropped this long
	FoldWindow = 10 * time.Minute // the same fingerprint again is folded into the next one after this long
	DeviceRate = 30               // reports a minute from one device
	HubRate    = 60               // reports a minute from the hub and the manager
	TotalRate  = 300              // reports a minute from everything
	idsMost    = 20000            // ids remembered at most (the oldest go first)
)

var idPattern = lazyre.New(`^[A-Za-z0-9._:\-]{1,64}$`)

// Normalize checks the fields a report must have and fills the ones it may leave out; an error: drop it.
func Normalize(r *wire.ErrorReport, now time.Time) error {
	if !idPattern.MatchString(r.ID) {
		return errors.New("id: 1-64 of A-Z a-z 0-9 . _ : -")
	}
	if !slices.Contains(wire.ErrorSources, r.Source) {
		return errors.New("source: one of " + strings.Join(wire.ErrorSources, ", "))
	}
	if !slices.Contains(wire.ErrorKinds, r.Kind) {
		return errors.New("kind: one of " + strings.Join(wire.ErrorKinds, ", "))
	}
	if r.Level == "" {
		r.Level = wire.ErrorLevelError
	}
	if !slices.Contains(wire.ErrorLevels, r.Level) {
		return errors.New("level: one of " + strings.Join(wire.ErrorLevels, ", "))
	}
	t, err := time.Parse(time.RFC3339Nano, r.Time)
	if err != nil || t.After(now.Add(time.Hour)) || t.Before(now.Add(-30*24*time.Hour)) {
		t = now // a clock that is off, or no time: when it got here
	}
	r.Time = t.UTC().Format(time.RFC3339Nano)
	r.ReceivedAt = now.UTC().Format(time.RFC3339Nano)
	r.Count = 1
	return nil
}

var (
	digits  = lazyre.New(`0x[0-9a-fA-F]+|\d+`)
	spaces  = lazyre.New(`\s+`)
	logTime = lazyre.New(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? `)
)

// Fingerprint says which problem r is: its source, kind, the type of the innermost exception and its nearest in-app
// frames; without frames, its message with the numbers taken out (two "timeout after 31s" are the same problem).
func Fingerprint(r *wire.ErrorReport) string {
	parts := []string{r.Source, r.Kind}
	frames := 0
	if n := len(r.Exceptions); n > 0 {
		e := r.Exceptions[n-1]
		parts = append(parts, e.Type)
		for i := len(e.Frames) - 1; i >= 0 && frames < 3; i-- {
			if f := e.Frames[i]; f.InApp {
				parts = append(parts, f.Module+"."+f.Function)
				frames++
			}
		}
		if frames == 0 {
			parts = append(parts, normalizeText(e.Value))
		}
	} else {
		parts = append(parts, normalizeText(r.Message))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func normalizeText(s string) string {
	s = logTime.ReplaceAllString(s, "")
	s = digits.ReplaceAllString(s, "#")
	s = spaces.ReplaceAllString(s, " ")
	return cut(strings.TrimSpace(s), 200)
}

// Gate deduplicates and rate limits reports: retries by id, the same problem by fingerprint within FoldWindow (the
// next one after it carries the count), and per sender, the hub's own and everything per minute.
type Gate struct {
	mu     sync.Mutex
	now    func() time.Time
	ids    map[string]time.Time
	order  []string
	folds  map[string]*fold
	minute time.Time
	counts map[string]int
	total  int
}

type fold struct {
	sent   time.Time
	folded int
}

// NewGate is a Gate on the clock now (nil: time.Now).
func NewGate(now func() time.Time) *Gate {
	if now == nil {
		now = time.Now
	}
	return &Gate{now: now, ids: map[string]time.Time{}, folds: map[string]*fold{}, counts: map[string]int{}}
}

// Admit says whether r (normalized, with its fingerprint) goes on; sender is who sent it ("hub" for the hub and the
// manager, else the device). One it lets on carries in Count what it folded before.
func (g *Gate) Admit(r *wire.ErrorReport, sender string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if t, ok := g.ids[r.ID]; ok && now.Sub(t) < IDWindow {
		return false
	}
	g.remember(r.ID, now)
	if f := g.folds[r.Fingerprint]; f != nil && now.Sub(f.sent) < FoldWindow {
		f.folded++
		return false
	}
	if now.Sub(g.minute) >= time.Minute {
		g.minute, g.total = now, 0
		clear(g.counts)
	}
	rate := DeviceRate
	if sender == "hub" {
		rate = HubRate
	}
	if g.total >= TotalRate || g.counts[sender] >= rate {
		if f := g.folds[r.Fingerprint]; f != nil {
			f.folded++ // the next one let on counts it (a problem not seen yet gets no fold: no room for a flood)
		}
		return false
	}
	g.total++
	g.counts[sender]++
	f := g.folds[r.Fingerprint]
	if f == nil {
		f = &fold{}
		g.folds[r.Fingerprint] = f
	}
	r.Count = 1 + f.folded
	f.sent, f.folded = now, 0
	if len(g.folds) > idsMost {
		for k, v := range g.folds {
			if now.Sub(v.sent) >= FoldWindow {
				delete(g.folds, k)
			}
		}
	}
	return true
}

// Unsent gives back a report Admit let on that was not delivered after all (no room for it): the next one of its
// problem is not folded into it and carries its count. Its id stays: a retry would count it twice.
func (g *Gate) Unsent(r *wire.ErrorReport) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.folds[r.Fingerprint]
	if f == nil {
		f = &fold{}
		g.folds[r.Fingerprint] = f
	}
	f.sent = time.Time{}
	f.folded += max(r.Count, 1)
}

func (g *Gate) remember(id string, now time.Time) {
	if _, ok := g.ids[id]; ok { // again after its window but not forgotten yet: once in the order, at its new time
		if i := slices.Index(g.order, id); i >= 0 {
			g.order = slices.Delete(g.order, i, i+1)
		}
	}
	g.ids[id] = now
	g.order = append(g.order, id)
	for len(g.order) > idsMost || len(g.order) > 0 && now.Sub(g.ids[g.order[0]]) >= IDWindow {
		delete(g.ids, g.order[0])
		g.order = g.order[1:]
	}
}
