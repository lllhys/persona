package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingFileOps makes the file rollback's renames and removals fail for the
// paths fail selects, and restores the real ones after the test.
func failingFileOps(t *testing.T, failRename func(from, to string) bool, failRemove func(name string) bool) {
	t.Helper()
	realRename, realRemoveAll := renameFile, removeAll
	t.Cleanup(func() { renameFile, removeAll = realRename, realRemoveAll })
	renameFile = func(from, to string) error {
		if failRename != nil && failRename(from, to) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errors.New("injected failure")}
		}
		return realRename(from, to)
	}
	removeAll = func(name string) error {
		if failRemove != nil && failRemove(name) {
			return &os.PathError{Op: "unlinkat", Path: name, Err: errors.New("injected failure")}
		}
		return realRemoveAll(name)
	}
}

func previousFilesDirectories(t *testing.T, home string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(home, ".persona-backup-previous-*"))
	if err != nil {
		fatal(t, err)
	}
	return found
}

// The most important rollback property: when an original cannot be put back,
// the previous-files directory and everything in it stay, and the error names
// it, the file it holds, and what to do.
func TestFailedRollbackKeepsAndNamesThePreviousFiles(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	hubJSON := filepath.Join(fixture.home, "hub.json")
	failingFileOps(t, func(from, to string) bool {
		return to == hubJSON && strings.Contains(from, ".persona-backup-previous-")
	}, nil)
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil {
		fatal(t, "restore succeeded")
	}
	dirs := previousFilesDirectories(t, fixture.home)
	if len(dirs) != 1 {
		fatalf(t, "previous-files directories after a failed rollback: %v", dirs)
	}
	kept := previousFilesHolding(t, fixture.home, "hub.json")
	if len(kept) != 1 {
		fatalf(t, "the preserved hub.json is gone from %s: %v", dirs[0], kept)
	}
	assertFile(t, kept[0], []byte(`{"database_url":"`+fixture.hub.URL+`","token":"changed-token"}`))
	message := err.Error()
	for _, want := range []string{dirs[0], kept[0], hubJSON, "by hand"} {
		if !strings.Contains(message, want) {
			errorf(t, "the rollback error does not name %s: %v", want, err)
		}
	}
	// Everything else was put back.
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "the knowledge base was not rolled back: %q", got)
	}
	assertDatabases(t, fixture, "hub-two", "manager-two")
}

// Same for an external knowledge base: its own previous copy stays and is named.
func TestFailedRollbackKeepsAndNamesThePreviousExternalKnowledgeBase(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	external := filepath.Join(t.TempDir(), "knowledge")
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external original"))
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(`{"database_name":"persona_manager","kb_dir":`+strconvQuote(external)+`}`))
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external changed"))
	fixture.backend.failInstallPart = "manager"
	failingFileOps(t, func(from, to string) bool {
		return to == external && strings.Contains(from, "persona-backup-previous-")
	}, nil)
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true, AllowExternalKB: true})
	if err == nil {
		fatal(t, "restore succeeded")
	}
	roots, _ := filepath.Glob(filepath.Join(filepath.Dir(external), ".knowledge.persona-backup-previous-*"))
	if len(roots) != 1 {
		fatalf(t, "the previous external knowledge base directory is gone: %v", roots)
	}
	kept, _ := filepath.Glob(filepath.Join(roots[0], "*-knowledge", "fact.md"))
	if len(kept) != 1 {
		fatalf(t, "the preserved external knowledge base is gone from %s", roots[0])
	}
	assertFile(t, kept[0], []byte("external changed"))
	if !strings.Contains(err.Error(), roots[0]) || !strings.Contains(err.Error(), "by hand") {
		errorf(t, "the rollback error does not name %s: %v", roots[0], err)
	}
	// The runtime's own previous files all went back, so their directory is gone.
	if dirs := previousFilesDirectories(t, fixture.home); len(dirs) != 0 {
		errorf(t, "the emptied previous-files directory remains: %v", dirs)
	}
}

// A file the install did not have and the restore installed is removed by the
// rollback; one that cannot be removed is reported, never silently left.
func TestRollbackRemovesOrReportsBundleOnlyFiles(t *testing.T) {
	for _, removalFails := range []bool{false, true} {
		fixture := newBackupFixture(t, "persona_manager")
		writeFixtureFile(t, filepath.Join(fixture.home, "apns.json"), []byte(`{"team":"bundle"}`))
		operator := fixture.operator()
		if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
			fatal(t, err)
		}
		if err := os.Remove(filepath.Join(fixture.home, "apns.json")); err != nil {
			fatal(t, err)
		}
		fixture.mutate(t)
		fixture.backend.failInstallPart = "manager"
		apns := filepath.Join(fixture.home, "apns.json")
		if removalFails {
			failingFileOps(t, nil, func(name string) bool { return name == apns })
		}
		_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
		if err == nil {
			fatal(t, "restore succeeded")
		}
		_, statErr := os.Stat(apns)
		switch {
		case !removalFails && !errors.Is(statErr, os.ErrNotExist):
			errorf(t, "the bundle-only apns.json was left after the rollback: %v", statErr)
		case removalFails && (!strings.Contains(err.Error(), apns) || !strings.Contains(err.Error(), "remove")):
			errorf(t, "a bundle-only file the rollback could not remove is not reported: %v", err)
		}
	}
}

// The previous-files directory is removed when it ends up empty: after a
// restore that replaced nothing, and after a rollback that put everything back.
func TestEmptyPreviousFilesDirectoryIsRemoved(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	emptyHome := filepath.Join(t.TempDir(), "new-runtime")
	result, err := operator.Restore(context.Background(), Options{Home: emptyHome, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	if result.PreviousFiles != "" || len(previousFilesDirectories(t, emptyHome)) != 0 {
		errorf(t, "a restore into an empty runtime left previous files %q: %v", result.PreviousFiles, previousFilesDirectories(t, emptyHome))
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err == nil {
		fatal(t, "restore succeeded")
	}
	if dirs := previousFilesDirectories(t, fixture.home); len(dirs) != 0 {
		errorf(t, "the rollback left previous files: %v", dirs)
	}
}

// Something unexpected in the previous-files directory after a complete
// rollback (it is the restore's own directory, so this should not happen) is
// kept, never deleted with it.
func TestRollbackNeverDeletesAPreviousFilesDirectoryThatIsNotEmpty(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	hubJSON := filepath.Join(fixture.home, "hub.json")
	stray := ""
	failingFileOps(t, func(from, to string) bool {
		if to == hubJSON && strings.Contains(from, ".persona-backup-previous-") && stray == "" {
			stray = filepath.Join(filepath.Dir(from), "stray.txt")
			if err := os.WriteFile(stray, []byte("not the restore's"), 0o600); err != nil {
				fatal(t, err)
			}
		}
		return false
	}, nil)
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil {
		fatal(t, "restore succeeded")
	}
	if stray == "" {
		fatal(t, "the rollback never put hub.json back")
	}
	assertFile(t, stray, []byte("not the restore's"))
	if !strings.Contains(err.Error(), filepath.Dir(stray)) {
		errorf(t, "the kept previous-files directory is not named: %v", err)
	}
}

// The !confused guards of the rollback: a part whose checks contradicted every
// layout never has its staged name dropped or renamed to, even when what it
// learnt says the bundle's copy is staged (the drop) or installed (the move
// back to the staged name).
func TestRollbackNeverActsOnAConfusedPartsStage(t *testing.T) {
	for _, believed := range []layout{{nowhere, atStage}, {nowhere, atTarget}} {
		backend := &scriptedBackend{fakeBackend: newFakeBackend()}
		hub := Target{Part: "hub", Database: "persona"}
		manager := Target{Part: "manager", Database: "persona_manager"}
		backend.add(hub, "persona_staged_h_1", "hub-one")
		backend.add(hub, "persona", "hub-two")
		backend.add(manager, "persona_staged_m_1", "manager-one")
		confused := &swapPart{target: hub, label: "hub", role: "h", stage: "persona_staged_h_1", known: knowing(func(l layout) bool { return l == believed }), confused: true}
		known := &swapPart{target: manager, label: "built-in manager", role: "m", stage: "persona_staged_m_1", known: knowing(func(l layout) bool { return l == layout{nowhere, atStage} })}
		_, err := (&Operator{backend: backend}).rollBack([]*swapPart{confused, known}, errors.New("install failed"))
		for _, call := range backend.calls {
			if call.op != "exists" && (call.from == "persona_staged_h_1" || call.to == "persona_staged_h_1") {
				errorf(t, "believing %+v, the confused part's staged name was acted on: %+v", believed, call)
			}
		}
		if got := backend.server(hub)["persona_staged_h_1"]; got == nil || got.dump != "hub-one" {
			errorf(t, "believing %+v, the confused part's staged name was dropped or replaced: %+v", believed, got)
		}
		if got := backend.server(hub)["persona"]; got == nil || got.dump != "hub-two" {
			errorf(t, "believing %+v, the confused part's live database was moved: %+v", believed, got)
		}
		if _, ok := backend.server(manager)["persona_staged_m_1"]; ok {
			errorf(t, "the known part's staged copy was not dropped")
		}
		var swap *swapError
		if !errors.As(err, &swap) || swap.outcome[0].unconfirmed != "persona_staged_h_1" {
			errorf(t, "the confused part is not reported unsettled with its staged name unconfirmed: %v", err)
		}
	}
}

// What the manifest records as absent or empty (no knowledge base, a listed
// runtime directory that held nothing) is not reproduced: restore leaves the
// install's copy in place, untouched, and says so. (Moving it aside was
// dropped pending a decision; see docs/server/server.md.)
func TestRestoreLeavesWhatTheBundleRecordsAsAbsentInPlaceAndSaysSo(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if err := os.RemoveAll(filepath.Join(fixture.home, "data")); err != nil {
		fatal(t, err)
	}
	if err := os.RemoveAll(filepath.Join(fixture.home, "extensions")); err != nil {
		fatal(t, err)
	}
	if err := os.MkdirAll(filepath.Join(fixture.home, "extensions"), 0o700); err != nil { // listed, empty
		fatal(t, err)
	}
	operator := fixture.operator()
	created, err := operator.Create(context.Background(), fixture.options())
	if err != nil {
		fatal(t, err)
	}
	if len(created.Notes) != 1 || !strings.Contains(created.Notes[0], "warning: the knowledge base") || !strings.Contains(created.Notes[0], "does not exist") {
		errorf(t, "create does not warn that the knowledge base is not there: %q", created.Notes)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	if manifest.KnowledgeBase.Present || !strings.Contains(strings.Join(manifest.RuntimeDirectories, ","), "extensions") {
		fatalf(t, "fixture manifest: knowledge base %+v, runtime directories %v", manifest.KnowledgeBase, manifest.RuntimeDirectories)
	}
	kept := map[string][]byte{
		filepath.Join("data", "facts", "later.md"):            []byte("knowledge written after the backup"),
		filepath.Join("extensions", "later", "manifest.json"): []byte("an extension installed later"),
		filepath.Join("skills", "s", "SKILL.md"):              []byte("a skill (skills is not listed)"),
		"apns.json":                                           []byte("an optional file the bundle lacks"),
	}
	for name, content := range kept {
		writeFixtureFile(t, filepath.Join(fixture.home, name), content)
	}
	fixture.mutate(t)
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	for name, content := range kept {
		assertFile(t, filepath.Join(fixture.home, name), content)
		if moved := previousFilesHolding(t, fixture.home, strings.SplitN(name, string(filepath.Separator), 2)[0]); len(moved) != 0 {
			errorf(t, "%s was moved to the previous files: %v", name, moved)
		}
	}
	notes := strings.Join(result.Notes, "\n")
	if !strings.Contains(notes, "records no knowledge base") || !strings.Contains(notes, filepath.Join(fixture.home, "data")+" was left in place, untouched") ||
		!strings.Contains(notes, "records an empty extensions; this install's extensions was left in place, untouched") || len(result.Notes) != 2 {
		errorf(t, "the result does not name what was left in place: %q", result.Notes)
	}
	var stdout bytes.Buffer
	printResult(&stdout, "restore", result)
	if !strings.Contains(stdout.String(), "note: the bundle records no knowledge base") {
		errorf(t, "restore output = %q", stdout.String())
	}
}

// A failed restore leaves what the bundle records as absent where it was, and
// no previous files.
func TestFailedRestoreLeavesWhatTheBundleRecordsAsAbsent(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if err := os.RemoveAll(filepath.Join(fixture.home, "data")); err != nil {
		fatal(t, err)
	}
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err == nil {
		fatal(t, "restore succeeded")
	}
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "the knowledge base the bundle records as absent changed: %q", got)
	}
	if dirs := previousFilesDirectories(t, fixture.home); len(dirs) != 0 {
		errorf(t, "previous files remain: %v", dirs)
	}
}

// The command prints every note after the result, for create and restore.
func TestCommandOutputPrintsTheNotes(t *testing.T) {
	for _, result := range []Result{
		{Bundle: "b", Verified: true, Restored: true, PreviousFiles: "/r/.persona-backup-previous-1", Notes: []string{"note: one", "note: two"}},
		{Bundle: "b", Verified: true, Notes: []string{"note: one", "note: two"}},
	} {
		var stdout bytes.Buffer
		command := "create"
		if result.Restored {
			command = "restore"
		}
		printResult(&stdout, command, result)
		if got := stdout.String(); !strings.HasSuffix(got, "\nnote: one\nnote: two\n") {
			errorf(t, "%s output = %q", command, got)
		}
	}
}
