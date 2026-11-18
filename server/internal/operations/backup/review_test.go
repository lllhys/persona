package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/secretkey"
)

// A password given as a URL query parameter is what pgx authenticates with; the
// command-line clients must receive the same password, still only through the
// environment.
func TestClientEnvironmentUsesQueryStringPassword(t *testing.T) {
	isolateLibpqEnvironment(t)
	target := Target{URL: "postgres://persona@db.example:5432/persona?password=query-secret&sslmode=disable"}
	for _, docker := range []bool{false, true} {
		environment := clientEnvironment(target, "persona", docker)
		values := environmentMap(t, environment)
		if values["PGPASSWORD"] != "query-secret" {
			fatalf(t, "docker=%v client environment = %s", docker, shownEnvironment(environment))
		}
		for _, variable := range environment {
			if strings.Contains(variable, "query-secret") && !strings.HasPrefix(variable, "PGPASSWORD=") {
				fatalf(t, "password outside PGPASSWORD: %q", variable)
			}
		}
	}
}

// User files in the knowledge base and skills are content, not runtime state:
// names that look like logs or lock files must survive a create/restore round
// trip. Runtime artifacts in the manager's own directory are still skipped.
func TestUserFilesWithRuntimeLookingNamesAreBackedUp(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	userFiles := []string{
		filepath.Join("data", "meeting.log"),
		filepath.Join("data", "project", "yarn.lock"),
		filepath.Join("data", "notes.pid"),
		filepath.Join("data", "draft.tmp-1"),
		filepath.Join("skills", "example", "uv.lock"),
		filepath.Join("extensions", "example", "Cargo.lock"),
	}
	for _, name := range userFiles {
		writeFixtureFile(t, filepath.Join(fixture.home, name), []byte("user content "+name))
	}
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.lock"), []byte("runtime lock"))
	if runtime.GOOS != "windows" {
		t.Chdir(filepath.Join(fixture.home, "extensions", "example"))
		listener, err := net.Listen("unix", "run.sock")
		if err != nil {
			fatal(t, err)
		}
		defer listener.Close()
	}
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	for _, entry := range manifest.Files {
		if strings.HasSuffix(entry.Path, "manager.lock") || strings.HasSuffix(entry.Path, "run.sock") {
			errorf(t, "runtime artifact %s was backed up", entry.Path)
		}
	}
	for _, name := range userFiles {
		if err := os.Remove(filepath.Join(fixture.home, name)); err != nil {
			fatal(t, err)
		}
	}
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err != nil {
		fatal(t, err)
	}
	for _, name := range userFiles {
		raw, err := os.ReadFile(filepath.Join(fixture.home, name))
		if err != nil || string(raw) != "user content "+name {
			errorf(t, "%s after restore = %q, %v", name, raw, err)
		}
	}
}

// rewriteManifest republishes an existing bundle with a changed manifest, as a
// tampered or hand-edited bundle would arrive.
func rewriteManifest(t *testing.T, bundle string, change func(*Manifest)) string {
	t.Helper()
	manifest, _, err := readBundleManifest(bundle)
	if err != nil {
		fatal(t, err)
	}
	staging := t.TempDir()
	if err := extractBundle(bundle, staging, manifest); err != nil {
		fatal(t, err)
	}
	change(manifest)
	raw, err := json.Marshal(manifest)
	if err != nil {
		fatal(t, err)
	}
	if err := os.WriteFile(filepath.Join(staging, manifestPath), raw, 0o600); err != nil {
		fatal(t, err)
	}
	// Written by hand: publishBundle would refuse an invalid manifest.
	output := filepath.Join(t.TempDir(), "tampered.backup")
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fatal(t, err)
	}
	defer file.Close()
	writer := tar.NewWriter(file)
	members := []string{manifestPath}
	for _, entry := range manifest.Files {
		members = append(members, entry.Path)
	}
	for _, member := range members {
		if err := appendTarMember(context.Background(), writer, staging, member); err != nil {
			fatal(t, err)
		}
	}
	if err := writer.Close(); err != nil {
		fatal(t, err)
	}
	return output
}

func TestRelativeKnowledgeBaseCannotEscapeHome(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	victim := filepath.Join(filepath.Dir(fixture.home), "victim")
	writeFixtureFile(t, filepath.Join(victim, "keep.txt"), []byte("outside the runtime directory"))
	tampered := rewriteManifest(t, fixture.output, func(m *Manifest) { m.KnowledgeBase.Relative = "../victim" })
	_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: tampered, WritesQuiesced: true})
	if err == nil {
		fatal(t, "a relative knowledge-base path outside the runtime directory was restored without --allow-external-kb")
	}
	raw, readErr := os.ReadFile(filepath.Join(victim, "keep.txt"))
	if readErr != nil || string(raw) != "outside the runtime directory" {
		fatalf(t, "directory outside the runtime was replaced: %q, %v", raw, readErr)
	}
	for _, relative := range []string{"..", "../victim", "../../etc", "data/../../victim"} {
		manifest := &Manifest{KnowledgeBase: KnowledgeBaseLocator{Relative: relative, Present: true}}
		if _, err := restoreItems(fixture.home, t.TempDir(), manifest, true); err == nil {
			errorf(t, "restoreItems accepted relative knowledge base %q", relative)
		}
	}
}

// Before its first key rotation the built-in manager's database has no
// sealing_key record. Verification must then prove the bundled key opens the
// sealed values instead of treating the missing record as a wildcard.
func TestUnrecordedManagerKeyIsCheckedAgainstSealedValues(t *testing.T) {
	sealWith := func(t *testing.T, key []byte) string {
		t.Helper()
		sealed, err := secretkey.SealWith(key, "provider token")
		if err != nil {
			fatal(t, err)
		}
		return sealed
	}
	unrecorded := func(fixture *backupFixture, values ...string) {
		identity := fixture.backend.identities["manager-one"]
		identity.SealingKey = ""
		identity.SealedValues = values
		fixture.backend.identities["manager-one"] = identity
		fixture.backend.add(fixture.manager, fixture.manager.Database, "manager-one")
	}

	t.Run("mismatched key is refused", func(t *testing.T) {
		fixture := newBackupFixture(t, "persona_manager")
		unrecorded(fixture, sealWith(t, bytes.Repeat([]byte{9}, secretkey.Size)))
		_, err := fixture.operator().Create(context.Background(), fixture.options())
		if err == nil || !strings.Contains(err.Error(), "secret.key") {
			fatalf(t, "create with a key that opens no sealed value = %v", err)
		}
		if _, statErr := os.Stat(fixture.output); statErr == nil {
			fatal(t, "an unverifiable bundle was published")
		}
		assertNoTemporaryDatabases(t, fixture.backend)
	})

	t.Run("current or kept key is accepted", func(t *testing.T) {
		fixture := newBackupFixture(t, "persona_manager")
		previous := bytes.Repeat([]byte{7}, secretkey.Size)
		writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "secret.key.prev"), previous)
		unrecorded(fixture, sealWith(t, fixture.managerKey), sealWith(t, previous))
		operator := fixture.operator()
		if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
			fatal(t, err)
		}
		if _, err := operator.Verify(context.Background(), Options{Home: fixture.home, Bundle: fixture.output}); err != nil {
			fatal(t, err)
		}
		if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err != nil {
			fatal(t, err)
		}
	})

	t.Run("no sealed values needs no key proof", func(t *testing.T) {
		fixture := newBackupFixture(t, "persona_manager")
		unrecorded(fixture)
		if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
			fatal(t, err)
		}
	})
}
