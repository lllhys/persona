package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

// Something outside the restore drops the hub's staged copy while its install
// rename fails: the checks then contradict every layout. The staged name may
// by now hold anything (whoever dropped it may make another database there),
// so the error must not call it safe to drop; it must say its contents are
// unconfirmed.
func TestConfusedSwapDoesNotCallAnUnconfirmedNameSafeToDrop(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	backend := &scriptedBackend{fakeBackend: fixture.backend}
	interfered := false
	backend.decide = func(op, from, to string) int {
		if op == "rename" && strings.HasPrefix(from, "persona_staged_h_") && to == fixture.hub.Database && !interfered {
			interfered = true
			delete(fixture.backend.server(fixture.hub), from) // another tool drops it
			return callFails
		}
		return callWorks
	}
	_, err := (&Operator{backend: backend, now: time.Now, random: rand.Reader}).Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if !interfered {
		fatal(t, "the interference never happened")
	}
	var swap *swapError
	if !errors.As(err, &swap) {
		fatalf(t, "restore error = %v, want a swap error", err)
	}
	message := err.Error()
	stage := swap.outcome[0].unconfirmed
	if stage == "" || swap.outcome[0].stage != "" || !strings.Contains(message, stage) {
		fatalf(t, "the unconfirmed staged name is not named: %v", err)
	}
	if strings.Contains(message, "safe to drop") {
		errorf(t, "a name whose contents are unconfirmed is called safe to drop: %v", err)
	}
	if !strings.Contains(message, "check what") || !strings.Contains(message, "before dropping") {
		errorf(t, "the error does not say to check before dropping: %v", err)
	}
	for _, name := range backend.dropped {
		if !strings.HasPrefix(name, "persona_staged_m_") { // the manager's was known to be its staged copy
			errorf(t, "%s was dropped", name)
		}
	}
}

// The wording follows what is known of each name: a staged copy known to hold
// the bundle's copy (or nothing) is safe to drop; a confused part's is not.
func TestSwapErrorWordingSeparatesConfirmedAndUnconfirmedStages(t *testing.T) {
	cause, undo := errors.New("install failed"), errors.New("rollback failed")
	known := &swapPart{target: Target{Database: "persona_manager"}, label: "built-in manager", stage: "persona_staged_m_1", known: knowing(func(l layout) bool { return l == layout{nowhere, atStage} })}
	confused := &swapPart{target: Target{Database: "persona"}, label: "hub", stage: "persona_staged_h_1", aside: "persona_previous_h_1"}
	confused.learn(0)
	message := (&swapError{cause: cause, undo: undo, outcome: swapOutcome{confused.outcome(), known.outcome()}}).Error()
	_, list, found := strings.Cut(message, "safe to drop: ")
	list, _, _ = strings.Cut(list, ";")
	if !found || !strings.Contains(list, "persona_staged_m_1") || strings.Contains(list, "persona_staged_h_1") {
		errorf(t, "the safe-to-drop list is %q, want the manager's staged copy only: %s", list, message)
	}
	if !strings.Contains(message, "could not be confirmed") || !strings.Contains(message, "persona_staged_h_1") {
		errorf(t, "the confused part's staged name is not reported as unconfirmed: %s", message)
	}
	if got := known.outcome(); got.unconfirmed != "" || got.stage != "persona_staged_m_1" {
		errorf(t, "a known staged copy has outcome %+v", got)
	}
}
