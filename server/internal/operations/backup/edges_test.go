package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateRefusesActiveDatabaseConnections(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.active["manager:"+fixture.manager.Database] = 1
	_, err := fixture.operator().Create(context.Background(), fixture.options())
	if err == nil || !strings.Contains(err.Error(), "active") {
		fatalf(t, "active-connection error = %v", err)
	}
	if _, statErr := os.Stat(fixture.output); !errors.Is(statErr, os.ErrNotExist) {
		fatalf(t, "bundle exists with active writers: %v", statErr)
	}
}

func TestInterruptedRestoreRollsBack(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	fixture.backend.installErr = context.Canceled
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if !errors.Is(err, context.Canceled) {
		fatalf(t, "restore interruption = %v", err)
	}
	if got := fixture.backend.server(fixture.hub)[fixture.hub.Database].dump; got != "hub-two" {
		fatalf(t, "hub was not rolled back after interruption: %q", got)
	}
	if got := fixture.backend.server(fixture.manager)[fixture.manager.Database].dump; got != "manager-two" {
		fatalf(t, "manager was not rolled back after interruption: %q", got)
	}
	raw, _ := os.ReadFile(filepath.Join(fixture.home, "data", "facts", "note.md"))
	if string(raw) != "changed knowledge" {
		fatalf(t, "files were not rolled back after interruption: %q", raw)
	}
	assertNoTemporaryDatabases(t, fixture.backend)
}

func TestExternalKnowledgeBaseNeedsExplicitRestoreApproval(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	external := filepath.Join(t.TempDir(), "knowledge")
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external original"))
	managerConfig := `{"database_name":"persona_manager","kb_dir":` + strconvQuote(external) + `}`
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(managerConfig))
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external changed"))
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil || !strings.Contains(err.Error(), "--allow-external-kb") {
		fatalf(t, "external restore without approval = %v", err)
	}
	if got := fixture.backend.server(fixture.hub)[fixture.hub.Database].dump; got != "hub-one" {
		fatalf(t, "database changed without external approval: %q", got)
	}
	raw, _ := os.ReadFile(filepath.Join(external, "fact.md"))
	if string(raw) != "external changed" {
		fatalf(t, "external files changed without approval: %q", raw)
	}
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true, AllowExternalKB: true}); err != nil {
		fatal(t, err)
	}
	raw, _ = os.ReadFile(filepath.Join(external, "fact.md"))
	if string(raw) != "external original" {
		fatalf(t, "external files were not restored: %q", raw)
	}
	assertNoTemporaryDatabases(t, fixture.backend)
}

func TestExternallyHostedBuiltinIsOutOfScope(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	writeFixtureFile(t, filepath.Join(fixture.home, "hub.json"), []byte(`{"database_url":"`+fixture.hub.URL+`","assistant":{"external":true}}`))
	_, _, err := Discover(Options{Home: fixture.home})
	if err == nil || !strings.Contains(err.Error(), "outside this backup's scope") {
		fatalf(t, "external built-in discovery = %v", err)
	}
}

func TestVerifyAndRestoreCanDiscoverFromBundleAlone(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	emptyHome := filepath.Join(t.TempDir(), "new-runtime")
	if _, err := operator.Verify(context.Background(), Options{Home: emptyHome, Bundle: fixture.output}); err != nil {
		fatal(t, err)
	}
	result, err := operator.Restore(context.Background(), Options{Home: emptyHome, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	if !result.Restored {
		fatal(t, "bare restore did not complete")
	}
	raw, err := os.ReadFile(filepath.Join(emptyHome, "hub.json"))
	if err != nil || !bytes.Contains(raw, []byte("hub-token-secret")) {
		fatalf(t, "bare restored hub.json = %q, %v", raw, err)
	}
	assertNoTemporaryDatabases(t, fixture.backend)
}

func TestManifestRejectsMismatchedBundledKey(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	manifest.ManagerKeyFingerprint = strings.Repeat("0", 32)
	if err := manifest.validate(); err == nil || !strings.Contains(err.Error(), "do not match") {
		fatalf(t, "mismatched manifest key = %v", err)
	}
}

func TestManifestContainsFingerprintsNotKeyMaterial(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	if bytes.Contains(manifestJSON(t, fixture.output), fixture.managerKey) {
		fatal(t, "manifest contains manager key bytes")
	}
	if manifest.ManagerKeyFingerprint == string(fixture.managerKey) {
		fatal(t, "manifest stores the key rather than its fingerprint")
	}
}

func manifestJSON(t *testing.T, bundle string) []byte {
	t.Helper()
	manifest, _, err := readBundleManifest(bundle)
	if err != nil {
		fatal(t, err)
	}
	staging := t.TempDir()
	if err := extractBundle(bundle, staging, manifest); err != nil {
		fatal(t, err)
	}
	raw, err := os.ReadFile(filepath.Join(staging, manifestPath))
	if err != nil {
		fatal(t, err)
	}
	return raw
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

// A rename whose error arrives after the server committed it must not be taken
// for "nothing happened": rollback has to put the databases back from what
// actually exists, and must not leave files and databases from different
// installs.
func TestUncertainRenameResultRollsBackToTheRealState(t *testing.T) {
	for _, fault := range []string{"manager:install", "hub:install", "manager:previous", "hub:previous"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			operator := fixture.operator()
			if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
				fatal(t, err)
			}
			fixture.mutate(t)
			fixture.backend.commitThenFail = fault
			_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
			if err == nil || !strings.Contains(err.Error(), "injected connection loss") {
				fatalf(t, "restore error = %v", err)
			}
			if !fixture.backend.commitThenFired {
				fatal(t, "the fault was never injected")
			}
			if got := fixture.backend.server(fixture.hub)[fixture.hub.Database]; got == nil || got.dump != "hub-two" {
				fatalf(t, "hub database after rollback = %+v", got)
			}
			if got := fixture.backend.server(fixture.manager)[fixture.manager.Database]; got == nil || got.dump != "manager-two" {
				fatalf(t, "manager database after rollback = %+v", got)
			}
			raw, _ := os.ReadFile(filepath.Join(fixture.home, "data", "facts", "note.md"))
			if string(raw) != "changed knowledge" {
				fatalf(t, "files were not rolled back: %q", raw)
			}
			assertNoTemporaryDatabases(t, fixture.backend)
			for _, server := range fixture.backend.servers {
				for name := range server {
					if strings.HasPrefix(name, "persona_previous_") {
						errorf(t, "previous database %s was not renamed back", name)
					}
				}
			}
		})
	}
}

// Without usable pg_dump/pg_restore every command stops before it touches the
// bundle, the files or the databases.
func TestUnsupportedClientsFailBeforeAnyWork(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.clientErr = errors.New("compose is not supported")
	fixture.backend.created, fixture.backend.renamed = nil, nil
	if _, err := operator.Verify(context.Background(), Options{Home: fixture.home, Bundle: fixture.output}); err == nil || !strings.Contains(err.Error(), "compose is not supported") {
		errorf(t, "verify = %v", err)
	}
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err == nil || !strings.Contains(err.Error(), "compose is not supported") {
		errorf(t, "restore = %v", err)
	}
	other := fixture.options()
	other.Output = filepath.Join(filepath.Dir(fixture.output), "second.backup")
	if _, err := operator.Create(context.Background(), other); err == nil || !strings.Contains(err.Error(), "compose is not supported") {
		errorf(t, "create = %v", err)
	}
	if len(fixture.backend.created) != 0 || len(fixture.backend.renamed) != 0 {
		errorf(t, "databases were touched: created %v renamed %v", fixture.backend.created, fixture.backend.renamed)
	}
	if _, err := os.Stat(other.Output); !errors.Is(err, os.ErrNotExist) {
		errorf(t, "bundle published: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(fixture.home, "data", "facts", "note.md"))
	if string(raw) != "changed knowledge" {
		errorf(t, "files touched: %q", raw)
	}
}

// A bundle from a non-Docker install has no docker/db-password. Restoring it on
// a Docker install must keep that file: the database role's password is not
// part of the bundle, and the file is how the containers authenticate.
func TestRestoreKeepsDockerPasswordFileAbsentFromBundle(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	password := filepath.Join(fixture.home, "docker", "db-password")
	writeFixtureFile(t, password, []byte("compose-password"))
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err != nil {
		fatal(t, err)
	}
	raw, err := os.ReadFile(password)
	if err != nil || string(raw) != "compose-password" {
		fatalf(t, "docker/db-password after restore = %q, %v", raw, err)
	}
}

func restoreWithFault(t *testing.T, fixture *backupFixture, fault string) (Result, error) {
	t.Helper()
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.dropped = nil // only what the restore drops counts
	fixture.backend.commitThenFail = fault
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if !fixture.backend.commitThenFired {
		fatal(t, "the fault was never injected")
	}
	return result, err
}

func assertNothingDropped(t *testing.T, fixture *backupFixture, tolerated ...string) {
	t.Helper()
	for _, name := range fixture.backend.dropped {
		ok := false
		for _, prefix := range tolerated {
			ok = ok || strings.HasPrefix(name, prefix)
		}
		if !ok {
			errorf(t, "database %s was dropped after an uncertain rename", name)
		}
	}
}

// When the server cannot be asked whether the rename committed, it is taken to
// have: the undo runs, and nothing is dropped.
func TestRenameCheckFailureIsNotTakenForNotCommitted(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.existsErr = true
	_, err := restoreWithFault(t, fixture, "manager:install")
	if err == nil {
		fatal(t, "restore succeeded")
	}
	if got := fixture.backend.server(fixture.manager)[fixture.manager.Database]; got == nil || got.dump != "manager-two" {
		fatalf(t, "manager after rollback = %+v", got)
	}
	if got := fixture.backend.server(fixture.hub)[fixture.hub.Database]; got == nil || got.dump != "hub-two" {
		fatalf(t, "hub after rollback = %+v", got)
	}
	assertNothingDropped(t, fixture, "persona_staged_") // undone, so the staged copies are the ones to discard
}

// Both names existing, or neither, is not "the rename did not happen": nothing
// is dropped, the previous database stays, and the operator is told which
// staged database may remain.
func TestAmbiguousRenameStateDropsNothing(t *testing.T) {
	for _, shape := range []string{"both", "neither"} {
		t.Run(shape, func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			fixture.backend.faultShape = shape
			_, err := restoreWithFault(t, fixture, "manager:install")
			if err == nil {
				fatal(t, "restore succeeded")
			}
			assertNothingDropped(t, fixture, "persona_staged_h_")
			found := false
			for name, database := range fixture.backend.server(fixture.manager) {
				if strings.HasPrefix(name, "persona_previous_m_") && database.dump == "manager-two" {
					found = true
				}
				if database.dump == "manager-two" && !strings.HasPrefix(name, "persona_previous_m_") && name != fixture.manager.Database {
					errorf(t, "unexpected copy of the previous database under %s", name)
				}
			}
			if shape == "both" && !strings.Contains(err.Error(), "persona_staged_m_") {
				errorf(t, "the error does not name the staged database that may remain: %v", err)
			}
			if !found && fixture.backend.server(fixture.manager)[fixture.manager.Database] == nil {
				errorf(t, "the previous manager database is gone: %v", fixture.backend.server(fixture.manager))
			}
		})
	}
}

// The server stays unreachable after an install rename committed: nothing can
// be undone. Files must then stay consistent with the databases that are live,
// and the error must say where the previous databases and files are.
func TestUnreachableServerAfterCommitKeepsFilesAndNamesThePreviousState(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.down = true
	_, err := restoreWithFault(t, fixture, "manager:install")
	if err == nil {
		fatal(t, "restore succeeded")
	}
	raw, _ := os.ReadFile(filepath.Join(fixture.home, "data", "facts", "note.md"))
	if string(raw) != "original knowledge" {
		errorf(t, "files were rolled back under databases that stayed restored: %q", raw)
	}
	for _, part := range []Target{fixture.hub, fixture.manager} {
		for name, database := range fixture.backend.server(part) {
			if strings.HasPrefix(name, "persona_previous_") && !strings.Contains(err.Error(), name) {
				errorf(t, "the error does not name the previous database %s (%s): %v", name, database.dump, err)
			}
		}
	}
	for _, wanted := range []string{"persona_previous_h_", "persona_previous_m_", "previous runtime files", "restore"} {
		if !strings.Contains(err.Error(), wanted) {
			errorf(t, "the error lacks %q: %v", wanted, err)
		}
	}
	assertNothingDropped(t, fixture, "persona_staged_h_")
}

// An output inside a tree the backup collects (the knowledge base, runtime
// directories) would be copied into itself with its staging directory.
func TestCreateRefusesAnOutputInsideACollectedTree(t *testing.T) {
	for _, inside := range []string{"data/backups", "data", "managers/assistant/out", "extensions/out", "skills"} {
		t.Run(inside, func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			opts := fixture.options()
			opts.Output = filepath.Join(fixture.home, filepath.FromSlash(inside), "install.backup")
			_, err := fixture.operator().Create(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "inside") {
				fatalf(t, "create = %v", err)
			}
			if _, statErr := os.Stat(filepath.Dir(opts.Output)); statErr == nil && inside != "data" && inside != "managers/assistant/out" && inside != "skills" && inside != "data/backups" {
				errorf(t, "the output directory was created: %v", statErr)
			}
			entries, _ := filepath.Glob(filepath.Join(fixture.home, filepath.FromSlash(inside), ".persona-backup-*"))
			if len(entries) != 0 {
				errorf(t, "staging left behind: %v", entries)
			}
		})
	}
}

// Outside the collected trees (including a sibling that merely shares a prefix)
// the output is fine, even inside the runtime directory.
func TestCreateAllowsAnOutputBesideTheCollectedTrees(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	for _, dir := range []string{"backups", "data-old"} {
		opts := fixture.options()
		opts.Output = filepath.Join(fixture.home, dir, "install.backup")
		if _, err := fixture.operator().Create(context.Background(), opts); err != nil {
			errorf(t, "create into %s: %v", dir, err)
		}
	}
}

// A restore of a bundle with docker/db-password onto an install that already
// has one keeps the install's: the role's password is not part of the bundle,
// and a Compose cluster only accepts the one its own file names.
func TestRestoreKeepsTheExistingDockerPasswordFile(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	password := filepath.Join(fixture.home, "docker", "db-password")
	writeFixtureFile(t, password, []byte("old-cluster-password"))
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	writeFixtureFile(t, password, []byte("new-cluster-password"))
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	raw, _ := os.ReadFile(password)
	if string(raw) != "new-cluster-password" {
		fatalf(t, "docker/db-password after restore = %q", raw)
	}
	if !result.KeptDockerPassword {
		errorT(t, "the result does not report the kept password file")
	}
	if err := os.Remove(password); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	result, err = operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	raw, _ = os.ReadFile(password)
	if string(raw) != "old-cluster-password" {
		fatalf(t, "an absent docker/db-password was not restored from the bundle: %q", raw)
	}
	if result.KeptDockerPassword {
		errorT(t, "the result reports a kept password file, but the bundle's copy was installed")
	}
}

// Neither the bundle nor the install has the file: nothing was kept.
func TestRestoreWithoutAnyDockerPasswordFileReportsNothingKept(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	if result.KeptDockerPassword {
		errorT(t, "the result reports a kept password file")
	}
	if _, err := os.Stat(filepath.Join(fixture.home, "docker", "db-password")); !errors.Is(err, os.ErrNotExist) {
		errorf(t, "a password file appeared: %v", err)
	}
}

func readNote(t *testing.T, fixture *backupFixture) string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(fixture.home, "data", "facts", "note.md"))
	return string(raw)
}

func assertDatabases(t *testing.T, fixture *backupFixture, hub, manager string) {
	t.Helper()
	if got := fixture.backend.server(fixture.hub)[fixture.hub.Database]; got == nil || got.dump != hub {
		errorf(t, "hub database = %+v, want %s", got, hub)
	}
	if got := fixture.backend.server(fixture.manager)[fixture.manager.Database]; got == nil || got.dump != manager {
		errorf(t, "manager database = %+v, want %s", got, manager)
	}
}

// The server dies before the first rename took effect: no bundle database can
// be live, so the files go back, and nothing is claimed about a previous
// database that was never made.
func TestServerDownBeforeTheFirstRenameRollsFilesBack(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.preCommit, fixture.backend.preCommitDown = "hub:previous", true
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil || !strings.Contains(err.Error(), "injected failure before") {
		fatalf(t, "restore error = %v", err)
	}
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "files were kept under databases that are the install's own: %q", got)
	}
	if strings.Contains(err.Error(), "left as restored") || strings.Contains(err.Error(), "by hand") {
		errorf(t, "the error claims bundle databases may be live: %v", err)
	}
	assertDatabases(t, fixture, "hub-two", "manager-two")
}

// One existence check fails briefly while the server is back for the undo.
func TestBriefExistenceFailureDoesNotKeepBundleFiles(t *testing.T) {
	for _, fault := range []string{"previous", "install"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			fixture.backend.armExistsFailures = 1
			operator := fixture.operator()
			if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
				fatal(t, err)
			}
			fixture.mutate(t)
			fixture.backend.dropped = nil
			if fault == "previous" {
				fixture.backend.preCommit = "hub:previous"
			} else {
				fixture.backend.failInstallPart = "manager"
			}
			_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
			if err == nil || !strings.Contains(err.Error(), "injected") {
				fatalf(t, "restore error = %v", err)
			}
			if got := readNote(t, fixture); got != "changed knowledge" {
				errorf(t, "files were kept although no bundle database is live: %q", got)
			}
			if strings.Contains(err.Error(), "left as restored") {
				errorf(t, "the error claims bundle databases may be live: %v", err)
			}
			if fault == "previous" && strings.Contains(err.Error(), "persona_previous_") {
				errorf(t, "the error names a previous database that was never made: %v", err)
			}
			assertDatabases(t, fixture, "hub-two", "manager-two")
			assertNothingDropped(t, fixture, "persona_staged_")
			assertNoTemporaryDatabases(t, fixture.backend) // the stage that never got installed is discarded
		})
	}
}

// The undo of the manager works and the hub's does not: a bundle database is
// live (the hub's), so the files stay as restored, and only the previous
// database still parked is named.
func TestPartialUndoKeepsFilesAndNamesOnlyWhatIsStillParked(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.failUndoPart = "hub"
	_, err := restoreWithFault(t, fixture, "manager:install")
	if err == nil {
		fatal(t, "restore succeeded")
	}
	if got := readNote(t, fixture); got != "original knowledge" {
		errorf(t, "files were rolled back under a live bundle database: %q", got)
	}
	assertDatabases(t, fixture, "hub-one", "manager-two") // the hub is the bundle's; the manager was put back
	var hubPrevious, managerPrevious string
	for name := range fixture.backend.server(fixture.hub) {
		if strings.HasPrefix(name, "persona_previous_h_") {
			hubPrevious = name
		}
	}
	for name := range fixture.backend.server(fixture.manager) {
		if strings.HasPrefix(name, "persona_previous_m_") {
			managerPrevious = name
		}
	}
	if hubPrevious == "" || !strings.Contains(err.Error(), "Previous hub database: "+hubPrevious) {
		errorf(t, "the error does not name the hub's previous database %q: %v", hubPrevious, err)
	}
	if managerPrevious != "" {
		errorf(t, "the manager's previous database was not renamed back: %s", managerPrevious)
	}
	if !strings.Contains(err.Error(), "previous built-in manager database: (none)") {
		errorf(t, "the error names a manager database that was already put back: %v", err)
	}
}

// The destination's existence check failing is no evidence that the rename did
// not happen.
func TestDestinationCheckFailureIsNotTakenForNotCommitted(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.existsErrName = fixture.manager.Database
	_, err := restoreWithFault(t, fixture, "manager:install")
	if err == nil {
		fatal(t, "restore succeeded")
	}
	assertDatabases(t, fixture, "hub-two", "manager-two")
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "files = %q", got)
	}
}

// A restore onto an external knowledge base names the copy it keeps.
func TestRestoreReportsThePreviousExternalKnowledgeBase(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	external := filepath.Join(t.TempDir(), "knowledge")
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external original"))
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(`{"database_name":"persona_manager","kb_dir":`+strconvQuote(external)+`}`))
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external changed"))
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true, AllowExternalKB: true})
	if err != nil {
		fatal(t, err)
	}
	if result.PreviousExternalKB == "" {
		fatal(t, "the result does not name the previous external knowledge base")
	}
	if info, err := os.Stat(result.PreviousExternalKB); err != nil || !info.IsDir() {
		fatalf(t, "the named directory does not exist: %v", err)
	}
}

// The refusal sees through symlinks: an output reached through a link into the
// knowledge base, and a knowledge base or collected directory that is itself a
// link, are inside the collected tree.
func TestOutputRefusalResolvesSymlinks(t *testing.T) {
	for _, shape := range []string{"link-to-kb", "kb-is-link", "collected-dir-is-link"} {
		t.Run(shape, func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			outside := t.TempDir()
			var output string
			switch shape {
			case "link-to-kb":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(filepath.Join(fixture.home, "data"), link); err != nil {
					skip(t, "symlinks unavailable: ", err)
				}
				output = filepath.Join(link, "x.backup")
			case "kb-is-link":
				if err := os.RemoveAll(filepath.Join(fixture.home, "data")); err != nil {
					fatal(t, err)
				}
				if err := os.Symlink(outside, filepath.Join(fixture.home, "data")); err != nil {
					skip(t, "symlinks unavailable: ", err)
				}
				output = filepath.Join(outside, "x.backup")
			case "collected-dir-is-link":
				if err := os.Symlink(outside, filepath.Join(fixture.home, "skills")); err != nil {
					skip(t, "symlinks unavailable: ", err)
				}
				output = filepath.Join(outside, "x.backup")
			}
			opts := fixture.options()
			opts.Output = output
			_, err := fixture.operator().Create(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "inside") {
				fatalf(t, "create = %v", err)
			}
			if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(output), ".persona-backup-*")); len(entries) != 0 {
				errorf(t, "staging left behind: %v", entries)
			}
		})
	}
}

// The aside rename worked, the install rename failed without happening, the
// check of it failed and so did the re-check: the install is taken for
// committed. The previous database then renames back, which proves the target
// name was free, so no bundle database is live and the files must go back.
func TestSuccessfulPreviousUndoProvesNoBundleDatabaseIsLive(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	fixture.backend.armExistsFailures = 2 // the commit check and the re-check
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil || !strings.Contains(err.Error(), "injected") {
		fatalf(t, "restore error = %v", err)
	}
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "files were kept although both live databases are the install's own: %q", got)
	}
	if strings.Contains(err.Error(), "may be live") {
		errorf(t, "the error claims a bundle database may be live: %v", err)
	}
	assertDatabases(t, fixture, "hub-two", "manager-two")
	assertNoTemporaryDatabases(t, fixture.backend) // and the stage is not left behind
}

// A successfully undone install is no longer installed, even when another part
// of the undo failed. The hub has no previous database in the second case, so
// nothing but the undo of its install can clear it.
func TestUndoneInstallIsNotCountedAsLive(t *testing.T) {
	for _, hubExisted := range []bool{true, false} {
		t.Run(fmt.Sprintf("hub-existed=%v", hubExisted), func(t *testing.T) {
			fixture := newBackupFixture(t, "persona_manager")
			operator := fixture.operator()
			if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
				fatal(t, err)
			}
			fixture.mutate(t)
			if !hubExisted {
				delete(fixture.backend.server(fixture.hub), fixture.hub.Database)
			}
			// The manager's aside rename fails without happening and both of its
			// checks fail, so its undo cannot tell; the hub's install is undone fine.
			fixture.backend.preCommit, fixture.backend.armExistsFailures = "manager:previous", 2
			_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
			if err == nil || !strings.Contains(err.Error(), "injected") {
				fatalf(t, "restore error = %v", err)
			}
			if got := readNote(t, fixture); got != "changed knowledge" {
				errorf(t, "files were kept although no bundle database is live: %q", got)
			}
			if hubExisted {
				assertDatabases(t, fixture, "hub-two", "manager-two")
			} else if got := fixture.backend.server(fixture.manager)[fixture.manager.Database]; got == nil || got.dump != "manager-two" {
				errorf(t, "manager database = %+v", got)
			}
		})
	}
}

// A previous database whose existence cannot be checked is not taken for
// absent: it stays named, so the operator can find it.
func TestParkedDatabaseWhoseCheckFailsStaysNamed(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.failUndoPart = "manager"
	fixture.backend.existsErrPrefix = "persona_previous_m_"
	_, err := restoreWithFault(t, fixture, "manager:install")
	if err == nil {
		fatal(t, "restore succeeded")
	}
	parked := ""
	for name := range fixture.backend.server(fixture.manager) {
		if strings.HasPrefix(name, "persona_previous_m_") {
			parked = name
		}
	}
	if parked == "" {
		fatal(t, "no previous manager database is parked")
	}
	if !strings.Contains(err.Error(), "previous built-in manager database: "+parked) {
		errorf(t, "the parked database %s is not named: %v", parked, err)
	}
}

// What the swap knows only narrows on established facts: a check that fails,
// or a rename that reports an error, rules nothing out; a rename that works
// rules out every layout it could not have been done from.
func TestSwapKnowledgeNarrowsOnlyOnEstablishedFacts(t *testing.T) {
	installing := knowing(func(l layout) bool { return l == layout{atAside, atStage} })
	unknown := installing.renamed(atStage, atTarget, errors.New("connection lost"))
	if unknown != installing|knowing(func(l layout) bool { return l == layout{atAside, atTarget} }) {
		fatalf(t, "an install rename that reports an error leaves %b", unknown)
	}
	if got := installing.renamed(atStage, atTarget, nil); !got.surely(bundleAt(atTarget)) {
		errorf(t, "an install rename that works leaves %b", got)
	}
	// Putting the previous database back works only if the name was free: no
	// bundle database can be installed under it.
	if got := unknown.renamed(atAside, atTarget, nil); !got.surely(back) {
		errorf(t, "a previous database renamed back leaves %b", got)
	}
	// Renamed back with an error: then the staged copy existing proves the
	// install never happened, and the target existing that the original is back.
	maybe := unknown.renamed(atAside, atTarget, errors.New("connection lost"))
	if !maybe.may(bundleAt(atTarget)) || !maybe.may(back) {
		fatalf(t, "a rename back that reports an error leaves %b", maybe)
	}
	if got := maybe.observed(atStage, true).observed(atTarget, true); !got.surely(back) {
		errorf(t, "staged copy and target present leave %b", got)
	}
	if got := maybe.observed(atStage, true); !got.surely(bundleAt(atStage)) || !got.unsure(originalAt(atAside)) {
		errorf(t, "the staged copy present leaves %b", got)
	}
	// A failed check is not an observation: probe leaves the knowledge alone.
	fixture := newBackupFixture(t, "persona_manager")
	part := &swapPart{target: fixture.hub, label: "hub", stage: "persona_staged_h_x", aside: "persona_previous_h_x", known: maybe}
	fixture.backend.existsFailures = 3
	fixture.operator().probe(part, false)
	if part.known != maybe || part.confused {
		errorf(t, "failed checks changed the knowledge to %b", part.known)
	}
	// A result no layout explains is not trusted either.
	part.learn(maybe.observed(atStage, false).observed(atTarget, false).observed(atAside, false))
	if !part.confused || !part.known.may(bundleAt(atTarget)) || !part.known.may(originalAt(atAside)) {
		errorf(t, "a contradiction leaves %b (confused %v)", part.known, part.confused)
	}
}

// No external knowledge base existed before the restore: nothing was kept, so
// no empty directory is left and none is reported.
func TestRestoreLeavesNoEmptyPreviousExternalKnowledgeBase(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	parent := t.TempDir()
	external := filepath.Join(parent, "knowledge")
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external original"))
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(`{"database_name":"persona_manager","kb_dir":`+strconvQuote(external)+`}`))
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	if err := os.RemoveAll(external); err != nil {
		fatal(t, err)
	}
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true, AllowExternalKB: true})
	if err != nil {
		fatal(t, err)
	}
	if result.PreviousExternalKB != "" {
		errorf(t, "an empty previous external knowledge base is reported: %s", result.PreviousExternalKB)
	}
	if left, _ := filepath.Glob(filepath.Join(parent, ".*persona-backup-previous-*")); len(left) != 0 {
		errorf(t, "an empty previous-copy directory is left: %v", left)
	}
	if raw, _ := os.ReadFile(filepath.Join(external, "fact.md")); string(raw) != "external original" {
		errorf(t, "the knowledge base was not restored: %q", raw)
	}
}

// The previous database's rename back commits and then reports an error
// (or works), after the install rename failed without happening and every
// check of the part failed: the install is not known to have failed. For
// either part, the result must be settled from what is then checked, not from
// the failed undo of the install: no bundle database is live, so the files go
// back and the staged copy is dropped. If the checks keep failing it cannot be
// told, and then the files stay and everything that may hold something is
// named.
func TestUncertainPreviousUndoIsSettledForEitherPart(t *testing.T) {
	for _, part := range []string{"hub", "manager"} {
		for _, mode := range []string{"commits-then-errors", "works", "checks-keep-failing"} {
			t.Run(part+"/"+mode, func(t *testing.T) {
				fixture := newBackupFixture(t, "persona_manager")
				operator := fixture.operator()
				if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
					fatal(t, err)
				}
				fixture.mutate(t)
				target, role := fixture.hub, "h"
				if part == "manager" {
					target, role = fixture.manager, "m"
				}
				stagePrefix, asidePrefix := "persona_staged_"+role+"_", "persona_previous_"+role+"_"
				ours := func(name string) bool {
					return name == target.Database || strings.HasPrefix(name, stagePrefix) || strings.HasPrefix(name, asidePrefix)
				}
				blind, putBack := false, false
				backend := &scriptedBackend{fakeBackend: fixture.backend}
				backend.decide = func(op, from, to string) int {
					switch {
					case op == "rename" && strings.HasPrefix(from, stagePrefix) && to == target.Database:
						blind = true
						return callFails
					case op == "rename" && from == target.Database && strings.HasPrefix(to, stagePrefix):
						return callFails
					case op == "rename" && strings.HasPrefix(from, asidePrefix) && to == target.Database:
						putBack = mode != "checks-keep-failing"
						if mode == "commits-then-errors" || mode == "checks-keep-failing" {
							return callCommitsThen
						}
					case op == "exists" && blind && !putBack && ours(from):
						return callFails
					}
					return callWorks
				}
				_, err := (&Operator{backend: backend, now: time.Now, random: rand.Reader}).Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
				if err == nil || !strings.Contains(err.Error(), "scripted") {
					fatalf(t, "restore error = %v", err)
				}
				assertDatabases(t, fixture, "hub-two", "manager-two")
				if mode != "checks-keep-failing" {
					if got := readNote(t, fixture); got != "changed knowledge" {
						errorf(t, "files were kept although both live databases are the install's own: %q", got)
					}
					if strings.Contains(err.Error(), "may be") || strings.Contains(err.Error(), "left as restored") {
						errorf(t, "the error claims a bundle database may be live: %v", err)
					}
					assertNoTemporaryDatabases(t, fixture.backend)
					return
				}
				if got := readNote(t, fixture); got != "original knowledge" {
					errorf(t, "files were rolled back although it cannot be told whether the bundle's %s database is live: %q", part, got)
				}
				previousFiles, _ := filepath.Glob(filepath.Join(fixture.home, ".persona-backup-previous-*"))
				wanted := append([]string{"the " + map[string]string{"hub": "hub", "manager": "built-in manager"}[part] + " database " + target.Database + " may be the bundle's"}, previousFiles...)
				for name := range fixture.backend.server(target) {
					if strings.HasPrefix(name, stagePrefix) {
						wanted = append(wanted, name)
					}
				}
				if len(wanted) != 3 {
					fatalf(t, "expected the previous files and the staged copy, have %v", wanted)
				}
				for _, want := range wanted {
					if !strings.Contains(err.Error(), want) {
						errorf(t, "the error does not name %q: %v", want, err)
					}
				}
			})
		}
	}
}

// Fault scripts through the whole Restore, files included: the files are kept
// exactly when a bundle database is live or it cannot be told, the error then
// names the previous files and every possibly-live database, every original
// and every staged copy left is named, and a second restore converges.
func TestRestoreFaultScriptsKeepFilesOnlyWithTheBundle(t *testing.T) {
	random := mathrand.New(mathrand.NewSource(5951))
	runs, kept, unknowable := 0, 0, 0
	// One bundle and install serve every run: each run ends with a converged
	// restore, and the next starts from the install's own databases and files
	// again, without the previous run's leftovers.
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	for run := 0; run < 120; run++ {
		script := make([]int, 24)
		density := 0.1 + 0.7*random.Float64()
		for i := range script {
			if random.Float64() < density {
				script[i] = 1 + random.Intn(2)
			} else if random.Intn(15) == 0 {
				script[i] = callServerDown
			}
		}
		label := fmt.Sprint(script)
		for _, server := range fixture.backend.servers {
			for name := range server {
				delete(server, name)
			}
		}
		leftovers, _ := filepath.Glob(filepath.Join(fixture.home, ".persona-backup-previous-*"))
		for _, leftover := range leftovers {
			if err := os.RemoveAll(leftover); err != nil {
				fatal(t, err)
			}
		}
		fixture.mutate(t)
		// The script starts with the swap (its first call checks the hub's
		// name); staging works.
		backend := &scriptedBackend{fakeBackend: fixture.backend}
		backend.decide = func(op, from, to string) int {
			if backend.script == nil && op == "exists" && from == fixture.hub.Database {
				backend.script, backend.decide = script, nil
				return backend.outcome(op, from, to)
			}
			return callWorks
		}
		operator := &Operator{backend: backend, now: time.Now, random: rand.Reader}
		_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
		runs++
		hubLive := fixture.backend.server(fixture.hub)[fixture.hub.Database]
		managerLive := fixture.backend.server(fixture.manager)[fixture.manager.Database]
		live := (hubLive != nil && hubLive.dump == "hub-one") || (managerLive != nil && managerLive.dump == "manager-one")
		filesKept := readNote(t, fixture) == "original knowledge"
		if err == nil {
			if !filesKept || hubLive == nil || hubLive.dump != "hub-one" || managerLive == nil || managerLive.dump != "manager-one" {
				fatalf(t, "script %s: success with files kept %v and %+v, %+v", label, filesKept, hubLive, managerLive)
			}
			continue
		}
		message := err.Error()
		if live && !filesKept {
			fatalf(t, "script %s: a bundle database is live and the files were rolled back: %v", label, err)
		}
		if filesKept != strings.Contains(message, "left as restored") {
			fatalf(t, "script %s: files kept %v, but the error says otherwise: %v", label, filesKept, err)
		}
		if filesKept {
			kept++
			previousFiles, _ := filepath.Glob(filepath.Join(fixture.home, ".persona-backup-previous-*"))
			if len(previousFiles) != 1 || !strings.Contains(message, previousFiles[0]) {
				fatalf(t, "script %s: the previous files %v are not named: %v", label, previousFiles, err)
			}
			if !live {
				unknowable++
				if !strings.Contains(message, " may be the bundle's") {
					fatalf(t, "script %s: files kept, no bundle database live, and no possibly-live one named: %v", label, err)
				}
			}
		}
		for _, part := range []Target{fixture.hub, fixture.manager} {
			for name, database := range fixture.backend.server(part) {
				parked := strings.HasSuffix(database.dump, "-two") && name != fixture.hub.Database && name != fixture.manager.Database
				if (parked || strings.HasPrefix(name, "persona_staged_")) && !strings.Contains(message, name) {
					fatalf(t, "script %s: %s (%s) is left and not named: %v", label, name, database.dump, err)
				}
			}
		}
		if _, err := fixture.operator().Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err != nil {
			fatalf(t, "script %s: a second restore does not converge: %v", label, err)
		}
		assertDatabases(t, fixture, "hub-one", "manager-one")
		for _, dump := range []string{"hub-two", "manager-two"} {
			found := false
			for _, server := range fixture.backend.servers {
				for _, database := range server {
					found = found || database.dump == dump
				}
			}
			if !found {
				fatalf(t, "script %s: the original %s is lost", label, dump)
			}
		}
	}
	if kept < 5 || unknowable == 0 {
		fatalf(t, "scripts too tame: %d runs, %d kept the files, %d unknowable", runs, kept, unknowable)
	}
	logf(t, "%d restores, %d kept the files, %d of them while no bundle database was live (unknowable)", runs, kept, unknowable)
}

// A staged copy that cannot be dropped when the restore fails before the swap
// is named, so it can be dropped by hand.
func TestStagedCopyLeftBeforeTheSwapIsNamed(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	fixture.backend.restoreErr, fixture.backend.restorePart = errors.New("injected manager restore failure"), "manager"
	backend := &scriptedBackend{fakeBackend: fixture.backend}
	backend.decide = func(op, from, to string) int {
		if op == "drop" && strings.HasPrefix(from, "persona_staged_h_") {
			return callFails
		}
		return callWorks
	}
	_, err := (&Operator{backend: backend, now: time.Now, random: rand.Reader}).Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err == nil || !strings.Contains(err.Error(), "injected manager restore failure") {
		fatalf(t, "restore error = %v", err)
	}
	left := 0
	for name := range fixture.backend.server(fixture.hub) {
		if strings.HasPrefix(name, "persona_staged_") {
			left++
			if !strings.Contains(err.Error(), name) {
				errorf(t, "the staged copy %s is left and not named: %v", name, err)
			}
		}
	}
	if left != 1 {
		errorf(t, "%d staged copies are left, want the hub's only", left)
	}
	assertDatabases(t, fixture, "hub-two", "manager-two")
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "files = %q", got)
	}
}
