package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The swap installs each part's staged bundle database under the part's own
// name, parking the database that had the name aside first. A rename can fail
// in a way that does not say whether it took effect (the connection is lost
// after the server committed it), and so can the existence checks that would
// tell. So what the swap knows about a part is kept as the set of layouts it
// may be in, narrowed by every result that establishes something and widened
// by every result that does not; every decision (what to undo, what to drop,
// what to name, whether a bundle database may be live) is read from that set.
//
// Layouts of one part: O is the original database, B the bundle's copy; T the
// part's own name, P the persona_previous_* name, S the persona_staged_* name.
//
//	L1 {T: O, S: B}  before the swap, or put back
//	L2 {P: O, S: B}  original parked, B not installed (T free)
//	L3 {P: O, T: B}  swapped: B is live
//	L4 {S: B}        no original; not installed
//	L5 {T: B}        no original; installed: B is live
//
// Renames (the server does one only if its source exists and its destination
// does not, and then atomically):
//
//	park     T->P  L1->L2
//	install  S->T  L2->L3, L4->L5
//	undo     T->S  L3->L2, L5->L4
//	put back P->T  L2->L1
//
// A rename that works leaves the images of the layouts it was possible from; one
// that reports an error (failed without effect, committed and then failed, or
// the server is gone: the code cannot tell these apart) leaves the layouts as
// they were plus those images. A working existence check keeps the layouts that
// agree with it; a failed one changes nothing. Rollback (manager first): undo
// if some layout allows it, put back if some layout allows it, check what is
// still in doubt, and drop S only when every layout has B there (L1, L2, L4).
// Restore keeps the bundle's files when a part may be in L3 or L5. A part is
// undecided about that only when every check of S after its last rename failed:
// B is under S or T in every layout, so one working check of S settles it.

// place is a name one of a part's databases can be under.
type place uint8

const (
	nowhere  place = iota // the part had no database before the restore
	atTarget              // the part's own name, the live one
	atAside               // the persona_previous_* name the original is parked under
	atStage               // the persona_staged_* name the bundle's copy was restored into
)

// layout is where a part's original database and the bundle's copy are.
type layout struct{ original, bundle place }

// layouts are all the layouts the swap can reach. Renames move one database
// between names and never overwrite one, so the two are never under one name.
var layouts = [...]layout{
	{atTarget, atStage}, // before the swap, or put back
	{atAside, atStage},  // the original parked; the bundle's copy not installed
	{atAside, atTarget}, // swapped
	{nowhere, atStage},  // no original; not installed (or put back)
	{nowhere, atTarget}, // no original; installed
}

func (l layout) holds(at place) bool {
	return at != nowhere && (l.original == at || l.bundle == at)
}

// rename is the layout after renaming the database under one name to another,
// and whether the server could do it: the source exists and the destination
// does not (PostgreSQL refuses anything else and leaves both names alone).
func (l layout) rename(from, to place) (layout, bool) {
	if !l.holds(from) || l.holds(to) {
		return l, false
	}
	if l.original == from {
		l.original = to
	}
	if l.bundle == from {
		l.bundle = to
	}
	return l, true
}

// knowledge is the set of layouts a part may still be in: bit i is layouts[i].
type knowledge uint8

func knowing(match func(layout) bool) knowledge {
	var k knowledge
	for i, l := range layouts {
		if match(l) {
			k |= 1 << i
		}
	}
	return k
}

func (k knowledge) may(match func(layout) bool) bool {
	for i, l := range layouts {
		if k&(1<<i) != 0 && match(l) {
			return true
		}
	}
	return false
}

func (k knowledge) surely(match func(layout) bool) bool {
	return k != 0 && !k.may(func(l layout) bool { return !match(l) })
}

// renamed is what is known after a rename returned err. Success rules out the
// layouts the server could not have renamed from; an error rules out nothing,
// because the rename may have been committed before it was reported.
func (k knowledge) renamed(from, to place, err error) knowledge {
	var next knowledge
	for i, l := range layouts {
		if k&(1<<i) == 0 {
			continue
		}
		if err != nil {
			next |= 1 << i
		}
		if after, ok := l.rename(from, to); ok {
			next |= knowing(func(candidate layout) bool { return candidate == after })
		}
	}
	return next
}

// observed is what is known once a check established whether a name exists.
func (k knowledge) observed(at place, exists bool) knowledge {
	return k & knowing(func(l layout) bool { return l.holds(at) == exists })
}

func (k knowledge) mayRename(from, to place) bool {
	return k.may(func(l layout) bool { _, ok := l.rename(from, to); return ok })
}

// unsure: the layouts still possible disagree on match.
func (k knowledge) unsure(match func(layout) bool) bool {
	return k.may(match) && k.may(func(l layout) bool { return !match(l) })
}

// undecided: something the swap acts on is still in doubt: whether the bundle's
// copy is installed or still staged, and whether the original is parked.
func (k knowledge) undecided() bool {
	return k.unsure(bundleAt(atTarget)) || k.unsure(bundleAt(atStage)) || k.unsure(originalAt(atAside))
}

func bundleAt(at place) func(layout) bool { return func(l layout) bool { return l.bundle == at } }
func originalAt(at place) func(layout) bool {
	return func(l layout) bool { return l.original == at }
}

// back: the part is as it was before the swap.
func back(l layout) bool { return l.bundle == atStage && l.original != atAside }

// swapPart is one part (hub or built-in manager) of the swap.
type swapPart struct {
	target Target
	label  string // for messages
	role   string // the temporary-name letter
	stage  string // the persona_staged_* name
	aside  string // the persona_previous_* name, once one is chosen
	known  knowledge
	// confused: a result contradicted every layout, so something outside the
	// restore changed these databases. Nothing is then dropped or renamed to the
	// staged name (it could hold something that is not the bundle's copy).
	confused bool
	// stageDropped: the staged copy was dropped (it was known to be there and
	// not installed).
	stageDropped bool
	// faulted: a rename of this part reported an error.
	faulted bool
	errs    []error
}

func (p *swapPart) name(at place) string {
	switch at {
	case atTarget:
		return p.target.Database
	case atAside:
		return p.aside
	case atStage:
		return p.stage
	}
	return ""
}

func (p *swapPart) learn(k knowledge) {
	if k == 0 {
		p.confused = true
		k = knowing(func(layout) bool { return true })
	}
	p.known = k
}

// settled: back as before and nothing of the bundle's left behind.
func (p *swapPart) settled() bool {
	return !p.confused && p.known.surely(back) && p.stageDropped
}

// move renames the database under one name to another and updates what is
// known from the result; a failed rename is followed by checks.
func (o *Operator) move(ctx context.Context, part *swapPart, from, to place) error {
	err := o.backend.Rename(ctx, part.target, part.name(from), part.name(to))
	part.learn(part.known.renamed(from, to, err))
	if err != nil {
		part.faulted = true
		o.probe(part, false)
	}
	return err
}

// probe checks the names whose existence is still in doubt while something
// the swap acts on is; a check that fails establishes nothing. With every set,
// it checks each name: a state no layout explains (something outside the
// restore changed the databases) then shows as a contradiction.
func (o *Operator) probe(part *swapPart, every bool) {
	ctx, cancel := cleanupContext()
	defer cancel()
	for _, at := range []place{atStage, atTarget, atAside} {
		if part.confused || !every && !part.known.undecided() {
			return
		}
		if part.name(at) == "" || !every && !part.known.unsure(func(l layout) bool { return l.holds(at) }) {
			continue
		}
		if exists, err := o.backend.Exists(ctx, part.target, part.name(at)); err == nil {
			part.learn(part.known.observed(at, exists))
		}
	}
}

// partOutcome is what a swap left of one part.
type partOutcome struct {
	label, target string
	// parked: the name the previous database is parked under, while it may
	// still be there.
	parked string
	// bundleMayBeLive: the part's own name may hold the bundle's copy;
	// bundleLive: it surely does.
	bundleMayBeLive, bundleLive bool
	// stage: a staged copy of the bundle's database that may remain under this
	// name. It is not in use: the name holds that copy or nothing.
	stage string
	// unconfirmed: the staged name of a part whose checks contradicted every
	// layout (something outside the restore changed its databases). It may
	// remain, but what it holds could not be confirmed: it is never called
	// safe to drop.
	unconfirmed string
}

type swapOutcome [2]partOutcome

func (s swapOutcome) bundleMayBeLive() bool { return s[0].bundleMayBeLive || s[1].bundleMayBeLive }
func (s swapOutcome) previous() [2]string   { return [2]string{s[0].parked, s[1].parked} }

func (p *swapPart) outcome() partOutcome {
	out := partOutcome{label: p.label, target: p.target.Database}
	if p.aside != "" && p.known.may(originalAt(atAside)) {
		out.parked = p.aside
	}
	out.bundleMayBeLive = p.known.may(bundleAt(atTarget))
	out.bundleLive = !p.confused && p.known.surely(bundleAt(atTarget))
	switch {
	case p.stageDropped:
	case p.confused:
		out.unconfirmed = p.stage
	case p.known.may(bundleAt(atStage)):
		out.stage = p.stage
	}
	return out
}

// swapError: the swap failed and the rollback did not put everything back.
type swapError struct {
	cause, undo error
	outcome     swapOutcome
}

func (e *swapError) Unwrap() []error { return []error{e.cause, e.undo} }

func (e *swapError) Error() string {
	message := e.cause.Error() + "; rollback: " + e.undo.Error()
	var live, stages, unconfirmed []string
	for _, part := range e.outcome {
		switch {
		case part.bundleLive:
			live = append(live, fmt.Sprintf("the %s database %s is the bundle's", part.label, part.target))
		case part.bundleMayBeLive:
			live = append(live, fmt.Sprintf("the %s database %s may be the bundle's", part.label, part.target))
		}
		if part.stage != "" {
			stages = append(stages, part.stage)
		}
		if part.unconfirmed != "" {
			unconfirmed = append(unconfirmed, part.unconfirmed)
		}
	}
	if len(live) > 0 {
		message += "; " + strings.Join(live, ", ")
	}
	message += fmt.Sprintf("; Previous hub database: %s; previous built-in manager database: %s (a name is a database that may still exist; (none) was put back or never made)",
		nameOrNone(e.outcome[0].parked), nameOrNone(e.outcome[1].parked))
	if len(stages) > 0 {
		message += "; staged copies of the bundle's databases that may remain, not in use and safe to drop: " + strings.Join(stages, ", ")
	}
	if len(unconfirmed) > 0 {
		message += "; something outside the restore changed these databases, so what these staged names hold could not be confirmed (they may remain, and may not be the bundle's copy): " +
			strings.Join(unconfirmed, ", ") + "; check what each holds before dropping it"
	}
	return message
}

// swapDatabases installs both staged databases, or rolls back and disposes of
// the staged copies. It owns the staged databases from the start: whatever it
// returns, a staged copy that may remain is named in the outcome.
func (o *Operator) swapDatabases(ctx context.Context, hub, manager Target, hubStage, managerStage string) (swapOutcome, error) {
	notInstalled := knowing(func(l layout) bool { return l.bundle == atStage && l.original != atAside })
	parts := []*swapPart{
		{target: hub, label: "hub", role: "h", stage: hubStage, known: notInstalled},
		{target: manager, label: "built-in manager", role: "m", stage: managerStage, known: notInstalled},
	}
	for _, part := range parts {
		if err := o.install(ctx, part); err != nil {
			return o.rollBack(parts, err)
		}
	}
	return swapOutcome{parts[0].outcome(), parts[1].outcome()}, nil
}

func (o *Operator) install(ctx context.Context, part *swapPart) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	exists, err := o.backend.Exists(ctx, part.target, part.target.Database)
	if err != nil {
		return err
	}
	part.learn(part.known.observed(atTarget, exists))
	if exists {
		if part.aside, err = o.uniqueName(ctx, part.target, "persona_previous_"+part.role+"_"); err != nil {
			return err
		}
		if err := o.move(ctx, part, atTarget, atAside); err != nil {
			return err
		}
	}
	return o.move(ctx, part, atStage, atTarget)
}

// rollBack undoes what may have happened, last part first: the install, then
// the parking of the previous database; then it drops the staged copies known
// to be there and not installed.
func (o *Operator) rollBack(parts []*swapPart, cause error) (swapOutcome, error) {
	ctx, cancel := cleanupContext()
	defer cancel()
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if !part.confused && part.known.mayRename(atTarget, atStage) {
			if err := o.move(ctx, part, atTarget, atStage); err != nil {
				part.errs = append(part.errs, fmt.Errorf("move the %s database %s back to %s: %w", part.label, part.target.Database, part.stage, err))
			}
		}
		// The parked database is only ever the original, so renaming it back is
		// safe even when the layout is not understood: at worst it is refused.
		if part.aside != "" && (part.confused || part.known.mayRename(atAside, atTarget)) {
			if err := o.move(ctx, part, atAside, atTarget); err != nil {
				part.errs = append(part.errs, fmt.Errorf("rename the previous %s database %s back: %w", part.label, part.aside, err))
			}
		}
		// Before dropping anything, a part that saw a fault is checked in full.
		o.probe(part, part.faulted)
		if !part.confused && part.known.surely(bundleAt(atStage)) {
			if err := o.backend.Drop(ctx, part.target, part.stage); err != nil {
				part.errs = append(part.errs, fmt.Errorf("drop the staged %s database %s: %w", part.label, part.stage, err))
			} else {
				part.stageDropped = true
			}
		}
	}
	outcome := swapOutcome{parts[0].outcome(), parts[1].outcome()}
	var errs []error
	settled := true
	for _, part := range parts {
		if !part.settled() {
			settled = false
			errs = append(errs, part.errs...)
			if len(part.errs) == 0 {
				errs = append(errs, fmt.Errorf("could not establish where the %s databases are", part.label))
			}
		}
	}
	if settled {
		return outcome, cause
	}
	return outcome, &swapError{cause: cause, undo: errors.Join(errs...), outcome: outcome}
}
