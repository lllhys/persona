package backup

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// Outcomes of one scripted Exists, Rename or Drop call.
const (
	callWorks       = iota
	callFails       // fails without effect
	callCommitsThen // takes effect, then reports an error (Exists: a plain failure)
	callServerDown  // fails without effect, and so does every later call
	callOutcomes
)

// scriptedCall is one call as the code saw it: what it asked and whether it
// was told the call worked (and, for Exists, the answer).
type scriptedCall struct {
	op       string // "exists", "rename", "drop"
	from, to string // the name (from) or the rename's names
	ok       bool
	exists   bool
}

// scriptedBackend fails the Exists, Rename and Drop calls it receives
// according to a script; past its end every call works (unless the server went
// down). It logs every such call for the oracle, and records every database a
// Drop removed.
type scriptedBackend struct {
	*fakeBackend
	script []int
	// decide, when set, picks each call's outcome instead of the script.
	decide  func(op, from, to string) int
	next    int
	down    bool
	calls   []scriptedCall
	dropped []string // "<name>=<dump>" of every database a Drop removed
}

func (s *scriptedBackend) outcome(op, from, to string) int {
	if s.down {
		return callServerDown
	}
	if s.decide != nil {
		return s.decide(op, from, to)
	}
	if s.next < len(s.script) {
		s.next++
		if s.script[s.next-1] == callServerDown {
			s.down = true
		}
		return s.script[s.next-1]
	}
	return callWorks
}

func (s *scriptedBackend) heal() { s.script, s.decide, s.next, s.down = nil, nil, 0, false }

func (s *scriptedBackend) Exists(ctx context.Context, target Target, name string) (bool, error) {
	if s.outcome("exists", name, "") != callWorks {
		s.calls = append(s.calls, scriptedCall{op: "exists", from: name})
		return false, errors.New("scripted: cannot check")
	}
	exists, err := s.fakeBackend.Exists(ctx, target, name)
	s.calls = append(s.calls, scriptedCall{op: "exists", from: name, ok: err == nil, exists: exists})
	return exists, err
}

func (s *scriptedBackend) Rename(ctx context.Context, target Target, from, to string) error {
	err := errors.New("scripted: rename failed")
	switch s.outcome("rename", from, to) {
	case callWorks:
		err = s.fakeBackend.Rename(ctx, target, from, to)
	case callCommitsThen:
		if err = s.fakeBackend.Rename(ctx, target, from, to); err == nil {
			err = errors.New("scripted: rename committed, then failed")
		}
	}
	s.calls = append(s.calls, scriptedCall{op: "rename", from: from, to: to, ok: err == nil})
	return err
}

func (s *scriptedBackend) Drop(ctx context.Context, target Target, name string) error {
	outcome := s.outcome("drop", name, "")
	if outcome == callWorks || outcome == callCommitsThen {
		if database := s.fakeBackend.server(target)[name]; database != nil {
			s.dropped = append(s.dropped, name+"="+database.dump)
		}
		_ = s.fakeBackend.Drop(ctx, target, name)
	}
	ok := outcome == callWorks
	s.calls = append(s.calls, scriptedCall{op: "drop", from: name, ok: ok})
	if !ok {
		return errors.New("scripted: drop failed")
	}
	return nil
}

type counterReader struct{ n byte }

func (c *counterReader) Read(p []byte) (int, error) {
	for i := range p {
		c.n++
		p[i] = c.n
	}
	return len(p), nil
}

// world is one possible content of the server: database name to dump.
type world map[string]string

func (w world) key() string {
	names := make([]string, 0, len(w))
	for name, dump := range w {
		names = append(names, name+"="+dump)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (w world) with(change func(world)) world {
	next := make(world, len(w))
	for name, dump := range w {
		next[name] = dump
	}
	change(next)
	return next
}

// possibleWorlds is the oracle: independently of the code under test, it
// replays the calls the code made, as the code saw them, from the known
// starting state, and returns every server content consistent with what it was
// told. A call that reported success happened; a call that reported an error
// may or may not have taken effect; a failed check establishes nothing.
func possibleWorlds(start world, calls []scriptedCall) []world {
	worlds := []world{start}
	for _, call := range calls {
		var next []world
		for _, w := range worlds {
			switch call.op {
			case "exists":
				if !call.ok || (w[call.from] != "") == call.exists {
					next = append(next, w)
				}
			case "rename":
				possible := w[call.from] != "" && w[call.to] == ""
				renamed := func(w world) { w[call.to] = w[call.from]; delete(w, call.from) }
				if !call.ok {
					next = append(next, w)
				}
				if possible {
					next = append(next, w.with(renamed))
				}
			case "drop":
				if !call.ok {
					next = append(next, w)
				}
				next = append(next, w.with(func(w world) { delete(w, call.from) }))
			}
		}
		seen := map[string]bool{}
		worlds = worlds[:0]
		for _, w := range next {
			if key := w.key(); !seen[key] {
				seen[key] = true
				worlds = append(worlds, w)
			}
		}
	}
	return worlds
}

type faultStats struct {
	runs, failures, kept, unknowable, unknowableDown int
}

// faultPart is one part of a fault-script run and its names.
type faultPart struct {
	target           Target
	label            string
	original, bundle string // dumps
	stage, rerun     string
}

// runFaultScript runs the swap (with the rollback and the disposal of the
// staged copies it owns) against one script, then what Restore decides about
// the files, and checks against the fake's ground truth and the oracle that:
// no bundle database is live while the files roll back; the files are kept
// only when the calls cannot tell that no bundle database is live (unknowable)
// and then every possibly-live database is named; no original is lost; every
// original that may be parked, and every staged copy that may remain, is named
// (and nothing that cannot be); every Drop hits only a bundle's staged copy;
// and running the swap again with a healthy server converges.
func runFaultScript(t *testing.T, script []int, originals [2]bool, stats *faultStats) {
	t.Helper()
	fake := newFakeBackend()
	backend := &scriptedBackend{fakeBackend: fake, script: script}
	hub := Target{Part: "hub", URL: "postgres://u@db.example/persona", Database: "persona"}
	manager := Target{Part: "manager", URL: "postgres://u@db.example/persona_manager", Database: "persona_manager"}
	parts := []faultPart{
		{hub, "hub", "orig-hub", "bundle-hub", "persona_staged_h_0", "persona_staged_h_1"},
		{manager, "built-in manager", "orig-manager", "bundle-manager", "persona_staged_m_0", "persona_staged_m_1"},
	}
	for i, part := range parts {
		if originals[i] {
			fake.add(part.target, part.target.Database, part.original)
		}
		fake.add(part.target, part.stage, part.bundle)
	}
	operator := &Operator{backend: backend, random: &counterReader{}}
	label := fmt.Sprintf("%v originals=%v", script, originals)
	server := fake.server(hub)
	start := world{}
	for name, database := range server {
		start[name] = database.dump
	}

	outcome, err := operator.swapDatabases(context.Background(), hub, manager, parts[0].stage, parts[1].stage)
	stats.runs++
	worlds := possibleWorlds(start, backend.calls)
	truth := world{}
	for name, database := range server {
		truth[name] = database.dump
	}
	found := false
	for _, w := range worlds {
		found = found || w.key() == truth.key()
	}
	if !found {
		fatalf(t, "script %s: the oracle lost the real state %v among %v", label, truth, worlds)
	}
	for _, drop := range backend.dropped {
		if !strings.Contains(drop, "=bundle-") || !strings.Contains(drop, "persona_staged_") {
			fatalf(t, "script %s: a Drop removed %s, which is not a staged copy of the bundle", label, drop)
		}
	}
	// Whatever a run tells about a name must agree with what the calls allow.
	mayHold := func(name, dump string) bool {
		for _, w := range worlds {
			if w[name] == dump {
				return true
			}
		}
		return false
	}
	previous := outcome.previous()
	for i, part := range parts {
		got := outcome[i]
		// Never: an original is lost.
		var homes []string
		for name, dump := range truth {
			if dump == part.original {
				homes = append(homes, name)
			}
		}
		if originals[i] && len(homes) == 0 {
			fatalf(t, "script %s: the original %s is lost (err %v)", label, part.original, err)
		}
		// Every original that may be parked is named, and nothing else is.
		for _, name := range homes {
			if name != part.target.Database && previous[i] != name {
				fatalf(t, "script %s: the original %s is parked as %s, which is not named (previous %v, err %v)", label, part.original, name, previous, err)
			}
		}
		if previous[i] != "" && !mayHold(previous[i], part.original) {
			fatalf(t, "script %s: %s is named as the previous %s database, but the calls rule that out (err %v)", label, previous[i], part.label, err)
		}
		// Every staged copy that may remain is named, and nothing else is.
		// No script changes the databases behind the restore's back, so no
		// result contradicts every layout.
		if got.unconfirmed != "" {
			fatalf(t, "script %s: the %s outcome is confused without outside interference (err %v)", label, part.label, err)
		}
		if truth[part.stage] != "" && got.stage != part.stage {
			fatalf(t, "script %s: the staged copy %s remains and is not named (err %v)", label, part.stage, err)
		}
		if got.stage != "" && !mayHold(part.stage, part.bundle) {
			fatalf(t, "script %s: %s is named as a staged copy that may remain, but the calls rule that out (err %v)", label, part.stage, err)
		}
		// A part is said to be possibly live exactly when the calls allow it,
		// and surely live exactly when they establish it.
		mayBeLive, surelyLive := mayHold(part.target.Database, part.bundle), true
		for _, w := range worlds {
			surelyLive = surelyLive && w[part.target.Database] == part.bundle
		}
		if got.bundleMayBeLive != mayBeLive || got.bundleLive != surelyLive {
			fatalf(t, "script %s: %s bundle may be live %v, surely %v; the calls allow %v, establish %v (err %v)", label, part.label, got.bundleMayBeLive, got.bundleLive, mayBeLive, surelyLive, err)
		}
		// Unknowable: the bundle's copy is always under the staged name or the
		// part's own name, so one working check of the staged name after the
		// part's last rename settles whether it is live. Undecided is allowed
		// only when every such check failed, and the code made at least two
		// (right after the failed rename, and again before deciding).
		if mayBeLive && !surelyLive {
			last := -1
			for index, call := range backend.calls {
				if call.op == "rename" && (call.from == part.stage || call.to == part.stage || call.from == part.target.Database || call.to == part.target.Database) {
					last = index
				}
			}
			checks := 0
			for _, call := range backend.calls[last+1:] {
				if call.op == "exists" && call.from == part.stage {
					if call.ok {
						fatalf(t, "script %s: the %s staged copy was checked after the last rename, yet it is undecided whether the bundle's copy is live (calls %+v)", label, part.label, backend.calls)
					}
					checks++
				}
			}
			if checks < 2 {
				fatalf(t, "script %s: the %s is left undecided after %d check(s) of its staged copy (calls %+v)", label, part.label, checks, backend.calls)
			}
		}
	}
	if err == nil {
		for i, part := range parts {
			if truth[part.target.Database] != part.bundle {
				fatalf(t, "script %s: success, but %s is %q", label, part.target.Database, truth[part.target.Database])
			}
			if originals[i] != (previous[i] != "") || (previous[i] != "" && truth[previous[i]] != part.original) {
				fatalf(t, "script %s: success with the previous databases %v in %v", label, previous, truth)
			}
		}
		return
	}
	stats.failures++
	message := err.Error()
	kept := outcome.bundleMayBeLive() // what Restore decides about the files
	live := false
	for _, part := range parts {
		live = live || truth[part.target.Database] == part.bundle
	}
	// Never: a bundle database is live and the files are rolled back.
	if live && !kept {
		fatalf(t, "script %s: a bundle database is live and the files would be rolled back (truth %v, err %v)", label, truth, err)
	}
	if kept {
		stats.kept++
		if !live {
			// Files kept while no bundle database is live: allowed only because
			// the calls cannot tell (checked per part above: kept means some
			// part's bundle may be live as far as the calls show).
			stats.unknowable++
			if backend.down {
				stats.unknowableDown++
			}
		}
		for i, part := range parts {
			if outcome[i].bundleMayBeLive && !strings.Contains(message, "the "+part.label+" database "+part.target.Database+" ") {
				fatalf(t, "script %s: the possibly-live %s database is not named: %v", label, part.label, err)
			}
		}
	}
	for i, part := range parts {
		if previous[i] != "" && !strings.Contains(message, previous[i]) {
			fatalf(t, "script %s: the error does not name the parked %s: %v", label, previous[i], err)
		}
		if outcome[i].stage != "" && !strings.Contains(message, part.stage) {
			fatalf(t, "script %s: the error does not name the staged copy %s: %v", label, part.stage, err)
		}
	}
	// Re-running converges: with the server healthy, staging again and
	// swapping puts the bundle's databases in place and keeps the originals.
	backend.heal()
	for _, part := range parts {
		fake.add(part.target, part.rerun, part.bundle)
	}
	if _, rerunErr := operator.swapDatabases(context.Background(), hub, manager, parts[0].rerun, parts[1].rerun); rerunErr != nil {
		fatalf(t, "script %s: re-running does not converge: %v", label, rerunErr)
	}
	for i, part := range parts {
		if server[part.target.Database] == nil || server[part.target.Database].dump != part.bundle {
			fatalf(t, "script %s: after re-running %s is %+v", label, part.target.Database, server[part.target.Database])
		}
		have := false
		for _, database := range server {
			have = have || database.dump == part.original
		}
		if originals[i] && !have {
			fatalf(t, "script %s: re-running lost the original %s", label, part.original)
		}
	}
}

// Every combination of the first eight calls working, failing without effect,
// failing after taking effect, or the server going down for good (with both
// parts having a database before the restore), and a fixed random set of
// longer scripts over every combination of the parts having one or not.
func TestSwapSurvivesEveryFaultScript(t *testing.T) {
	stats := &faultStats{}
	exhaustive := 8
	if testing.Short() {
		exhaustive = 7
	}
	total := 1
	for i := 0; i < exhaustive; i++ {
		total *= callOutcomes
	}
	script := make([]int, exhaustive)
	for code := 0; code < total; code++ {
		for i, rest := 0, code; i < exhaustive; i, rest = i+1, rest/callOutcomes {
			script[i] = rest % callOutcomes
		}
		runFaultScript(t, script, [2]bool{true, true}, stats)
	}
	random := rand.New(rand.NewSource(20261115))
	for run := 0; run < 24000; run++ {
		script := make([]int, 20)
		for i := range script {
			if random.Intn(3) == 0 {
				script[i] = 1 + random.Intn(2)
			} else if random.Intn(40) == 0 {
				script[i] = callServerDown
			}
		}
		runFaultScript(t, script, [2]bool{run%2 == 0, run%4 < 2}, stats)
	}
	// The scripts must really exercise the failure, keep-the-files and
	// cannot-tell paths.
	if stats.failures < 10000 || stats.kept < 1000 || stats.unknowable == 0 {
		fatalf(t, "scripts too tame: %+v", *stats)
	}
	logf(t, "%d scripts, %d failed swaps, %d kept the files, %d of them while no bundle database was live (unknowable: every check of the staged copy failed; %d with the server down for good)",
		stats.runs, stats.failures, stats.kept, stats.unknowable, stats.unknowableDown)
}
