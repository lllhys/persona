package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A knowledge-base target that holds the runtime directory (home, its parent,
// a grandparent or higher, /) would move the whole install, the previous-files
// directory with it, into its own previous copy. One that is or holds a
// restored runtime item (hub.json, managers, managers/assistant,
// extensions...) would push the item just restored into the previous files.
// restoreItems refuses both before anything is changed. One inside a runtime
// directory (kb_dir "kb" is managers/assistant/kb, a documented layout) is
// accepted: TestKnowledgeBaseInsideARuntimeDirectoryRoundTrips.
func TestRestoreItemsRefuseAKnowledgeBaseOverlappingTheRuntime(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "srv", "persona", "runtime")
	if err := os.MkdirAll(home, 0o700); err != nil {
		fatal(t, err)
	}
	for _, external := range []string{
		filepath.Join(root, "srv"), // grandparent
		root,                       // higher
		filepath.Join(root, "srv", "persona"),
		home,
		string(filepath.Separator),
		filepath.Join(home, "hub.json"),
		filepath.Join(home, "managers"),
		filepath.Join(home, "managers", "assistant"),
		filepath.Join(home, "extensions"),
		filepath.Join(home, "hub.json", "kb"),
		filepath.Join(home, "docker"),
		filepath.Join(home, ".persona-backup-previous-x"),
		filepath.Join(home, ".persona-backup-previous-x", "data"),
	} {
		manifest := &Manifest{KnowledgeBase: KnowledgeBaseLocator{External: external, Present: true}}
		_, err := restoreItems(home, t.TempDir(), manifest, true)
		switch {
		case err == nil:
			errorf(t, "restoreItems accepted external knowledge base %s", external)
		case !strings.Contains(err.Error(), "knowledge base") || !strings.Contains(err.Error(), "Nothing was changed"):
			errorf(t, "unclear refusal for %s: %v", external, err)
		case pathWithin(external, home) && !strings.Contains(err.Error(), "holds the runtime directory"):
			errorf(t, "the refusal of %s does not say it holds the runtime directory: %v", external, err)
		}
	}
	for _, relative := range []string{"hub.json", "secret.key", "managers", "managers/assistant", "skills", "hub.machines_kept", "extensions", "docker", "apns.json", ".persona-backup-previous-1/data"} {
		manifest := &Manifest{KnowledgeBase: KnowledgeBaseLocator{Relative: relative, Present: true}}
		if _, err := restoreItems(home, t.TempDir(), manifest, true); err == nil {
			errorf(t, "restoreItems accepted relative knowledge base %q", relative)
		}
	}
	// Through a symbolic link as well: the runtime is reached through a link,
	// the knowledge base is named by its real path, and only the resolved paths
	// show that one holds the other.
	link := filepath.Join(t.TempDir(), "link-to-srv")
	if err := os.Symlink(filepath.Join(root, "srv"), link); err == nil {
		for _, external := range []string{filepath.Join(root, "srv"), filepath.Join(root, "srv", "persona", "runtime", "managers")} {
			manifest := &Manifest{KnowledgeBase: KnowledgeBaseLocator{External: external, Present: true}}
			if _, err := restoreItems(filepath.Join(link, "persona", "runtime"), t.TempDir(), manifest, true); err == nil {
				errorf(t, "restoreItems accepted %s for a runtime reached through a symbolic link", external)
			}
		}
	}
	// The ordinary layouts stay accepted.
	for _, manifest := range []*Manifest{
		{KnowledgeBase: KnowledgeBaseLocator{Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "data", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "kb/notes", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{External: filepath.Join(root, "knowledge"), Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{External: filepath.Join(root, "srv", "persona", "knowledge"), Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "managers/assistant/kb", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "managers/assistant/data/notes", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "extensions/kb", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "skills/notes", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "hub.machines_kept/kb", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{Relative: "managers/other", Present: true}},
		{KnowledgeBase: KnowledgeBaseLocator{External: filepath.Join(home, "extensions", "x"), Present: true}},
	} {
		if _, err := restoreItems(home, t.TempDir(), manifest, true); err != nil {
			errorf(t, "restoreItems refused knowledge base %+v: %v", manifest.KnowledgeBase, err)
		}
	}
}

// Through the operator: the refusal comes before any database is staged and
// before any file is moved, with a message that names the knowledge base.
func TestRestoreRefusesAKnowledgeBaseHoldingTheRuntimeUpFront(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	// home becomes <tmp>/srv/persona/runtime, the example of the review.
	deep := filepath.Join(t.TempDir(), "srv", "persona")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		fatal(t, err)
	}
	if err := os.Rename(fixture.home, filepath.Join(deep, "runtime")); err != nil {
		fatal(t, err)
	}
	fixture.home = filepath.Join(deep, "runtime")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	fixture.mutate(t)
	for _, external := range []string{filepath.Dir(deep), filepath.Join(fixture.home, "managers")} {
		tampered := rewriteManifest(t, fixture.output, func(m *Manifest) {
			m.KnowledgeBase = KnowledgeBaseLocator{External: external, Present: true}
		})
		created := len(fixture.backend.created)
		_, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: tampered, WritesQuiesced: true, AllowExternalKB: true})
		if err == nil || !strings.Contains(err.Error(), "knowledge base") || !strings.Contains(err.Error(), external) {
			fatalf(t, "restore with external knowledge base %s = %v", external, err)
		}
		if len(fixture.backend.created) != created {
			errorf(t, "databases were staged before the layout was refused: %v", fixture.backend.created[created:])
		}
		if got := readNote(t, fixture); got != "changed knowledge" {
			errorf(t, "the knowledge base changed: %q", got)
		}
		if left, _ := filepath.Glob(filepath.Join(fixture.home, ".persona-backup-previous-*")); len(left) != 0 {
			errorf(t, "a previous-files directory was made: %v", left)
		}
		if _, err := os.Stat(filepath.Join(fixture.home, "hub.json")); err != nil {
			errorf(t, "the runtime moved: %v", err)
		}
		assertDatabases(t, fixture, "hub-two", "manager-two")
	}
	// The normal external knowledge base beside the runtime is still restored.
	external := filepath.Join(deep, "knowledge")
	writeFixtureFile(t, filepath.Join(external, "fact.md"), []byte("external changed"))
	normal := rewriteManifest(t, fixture.output, func(m *Manifest) {
		m.KnowledgeBase = KnowledgeBaseLocator{External: external, Present: true}
	})
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: normal, WritesQuiesced: true, AllowExternalKB: true})
	if err != nil {
		fatal(t, err)
	}
	assertFile(t, filepath.Join(external, "facts", "note.md"), []byte("original knowledge"))
	if result.PreviousExternalKB == "" || filepath.Dir(result.PreviousExternalKB) != deep {
		errorf(t, "previous external knowledge base %q is not beside it in %s", result.PreviousExternalKB, deep)
	}
	kept, _ := filepath.Glob(filepath.Join(result.PreviousExternalKB, "*-knowledge", "fact.md"))
	if len(kept) != 1 {
		fatalf(t, "the previous external knowledge base is not kept in %s: %v", result.PreviousExternalKB, kept)
	}
	assertFile(t, kept[0], []byte("external changed"))
}

// Create refuses a knowledge-base layout restore would refuse, before it
// stages or dumps anything.
func TestCreateRefusesAnOverlappingKnowledgeBaseBeforeDumping(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(`{"database_name":"persona_manager","kb_dir":".."}`))
	fixture.backend.dumpErr = errors.New("a database was dumped before the layout was checked")
	_, err := fixture.operator().Create(context.Background(), fixture.options())
	if err == nil || !strings.Contains(err.Error(), "knowledge base") || !strings.Contains(err.Error(), "Nothing was changed") {
		fatalf(t, "create with kb_dir holding managers/assistant = %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(fixture.output), ".persona-backup-stage-*")); len(left) != 0 {
		errorf(t, "a staging directory was made: %v", left)
	}
	if _, statErr := os.Stat(fixture.output); !errors.Is(statErr, os.ErrNotExist) {
		errorf(t, "a bundle was written: %v", statErr)
	}
}

// A knowledge base inside a runtime directory restore replaces (kb_dir "kb"
// is managers/assistant/kb, documented as relative to the manager's
// directory; or one inside extensions or skills) is collected once, as the
// knowledge base, and restored after its directory: a full restore installs
// both, a failed one puts both back, and the previous files hold each
// original exactly once.
func TestKnowledgeBaseInsideARuntimeDirectoryRoundTrips(t *testing.T) {
	for _, layout := range []struct{ kbDir, relative, directory string }{
		{"kb", "managers/assistant/kb", "assistant"},
		{"../../extensions/kb", "extensions/kb", "extensions"},
		{"../../skills/notes", "skills/notes", "skills"},
	} {
		t.Run(layout.directory, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				fixture := newBackupFixture(t, "persona_manager")
				kb := filepath.Join(fixture.home, filepath.FromSlash(layout.relative))
				config := `{"database_name":"persona_manager","kb_dir":"` + layout.kbDir + `"}`
				writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(config))
				writeFixtureFile(t, filepath.Join(kb, "facts", "note.md"), []byte("original nested knowledge"))
				writeFixtureFile(t, filepath.Join(kb, "meeting.log"), []byte("a knowledge file named like a log"))
				writeFixtureFile(t, filepath.Join(fixture.home, "skills", "s", "SKILL.md"), []byte("original skill"))
				operator := fixture.operator()
				if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
					fatal(t, err)
				}
				manifest, _, err := readBundleManifest(fixture.output)
				if err != nil {
					fatal(t, err)
				}
				if manifest.KnowledgeBase.Relative != layout.relative || !manifest.KnowledgeBase.Present {
					fatalf(t, "manifest knowledge base = %+v", manifest.KnowledgeBase)
				}
				for _, entry := range manifest.Files {
					if strings.HasPrefix(entry.Path, "runtime/"+layout.relative+"/") {
						errorf(t, "the knowledge base is collected twice, also as %s", entry.Path)
					}
				}
				if !hasMember(manifest, "kb/meeting.log") || !hasMember(manifest, "kb/facts/note.md") {
					errorf(t, "the knowledge base is not collected in full: %v", manifest.Files)
				}
				fixture.mutate(t)
				writeFixtureFile(t, filepath.Join(kb, "facts", "note.md"), []byte("changed nested knowledge"))
				writeFixtureFile(t, filepath.Join(kb, "later.md"), []byte("written after the backup"))
				writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "later.json"), []byte("manager file written after the backup"))
				if fail {
					fixture.backend.failInstallPart = "manager"
				}
				result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
				if fail {
					if err == nil {
						fatal(t, "restore succeeded")
					}
					assertFile(t, filepath.Join(kb, "facts", "note.md"), []byte("changed nested knowledge"))
					assertFile(t, filepath.Join(kb, "later.md"), []byte("written after the backup"))
					assertFile(t, filepath.Join(fixture.home, "managers", "assistant", "later.json"), []byte("manager file written after the backup"))
					assertFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(config))
					if dirs := previousFilesDirectories(t, fixture.home); len(dirs) != 0 {
						errorf(t, "previous files remain after the rollback: %v", dirs)
					}
					assertDatabases(t, fixture, "hub-two", "manager-two")
					continue
				}
				if err != nil {
					fatal(t, err)
				}
				assertFile(t, filepath.Join(kb, "facts", "note.md"), []byte("original nested knowledge"))
				assertFile(t, filepath.Join(kb, "meeting.log"), []byte("a knowledge file named like a log"))
				if _, err := os.Stat(filepath.Join(kb, "later.md")); !errors.Is(err, os.ErrNotExist) {
					errorf(t, "the knowledge base was merged, not replaced: %v", err)
				}
				assertFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), []byte(config))
				assertFile(t, filepath.Join(fixture.home, "skills", "s", "SKILL.md"), []byte("original skill"))
				// The install's knowledge base went into the previous files once,
				// inside its directory; nothing else holds a copy of it.
				holders := previousFilesHolding(t, fixture.home, layout.directory)
				if len(holders) != 1 || !strings.HasPrefix(holders[0], result.PreviousFiles) {
					fatalf(t, "previous %s directory %v not under the named previous files %q", layout.directory, holders, result.PreviousFiles)
				}
				inside := strings.TrimPrefix(layout.relative, map[string]string{"assistant": "managers/assistant", "extensions": "extensions", "skills": "skills"}[layout.directory])
				assertFile(t, filepath.Join(holders[0], filepath.FromSlash(inside), "later.md"), []byte("written after the backup"))
				if kbCopies := previousFilesHolding(t, fixture.home, filepath.Base(kb)); len(kbCopies) != 0 {
					errorf(t, "the previous files hold a second copy of the knowledge base: %v", kbCopies)
				}
			}
		})
	}
}

func hasMember(manifest *Manifest, name string) bool {
	for _, entry := range manifest.Files {
		if entry.Path == name {
			return true
		}
	}
	return false
}

// A manifest must say whether the bundle has a knowledge base, and one that
// says it has none lists no knowledge-base files.
func TestManifestKnowledgeBaseRecordIsStrict(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	if !manifest.KnowledgeBase.Present || !hasMember(manifest, "kb/facts/note.md") {
		fatalf(t, "fixture manifest knowledge base %+v", manifest.KnowledgeBase)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		fatal(t, err)
	}
	if _, err := decodeManifest(bytes.NewReader(raw)); err != nil {
		fatalf(t, "the manifest as written does not read back: %v", err)
	}
	for _, edit := range []struct{ from, to, want string }{
		{`"present":true`, `"present":null`, "present"},
		{`,"present":true`, ``, "present"},
		{`"present":true`, `"present":true,"extra":1`, "unknown field"},
		{`"present":true`, `"present":false`, `"kb/`},
	} {
		changed := strings.Replace(string(raw), edit.from, edit.to, 1)
		if changed == string(raw) {
			fatalf(t, "the manifest has no %s", edit.from)
		}
		if _, err := decodeManifest(strings.NewReader(changed)); err == nil || !strings.Contains(err.Error(), edit.want) {
			errorf(t, "manifest with %s instead of %s: %v", edit.to, edit.from, err)
		}
	}
	// Through restore, before anything is staged.
	tampered := rewriteManifest(t, fixture.output, func(m *Manifest) { m.KnowledgeBase.Present = false })
	created := len(fixture.backend.created)
	if _, err := fixture.operator().Restore(context.Background(), Options{Home: fixture.home, Bundle: tampered, WritesQuiesced: true}); err == nil || !strings.Contains(err.Error(), "records no knowledge base") {
		errorf(t, "restore of a bundle with present false and knowledge-base files = %v", err)
	}
	if len(fixture.backend.created) != created {
		errorf(t, "databases were staged: %v", fixture.backend.created[created:])
	}
}

// A knowledge base the bundle records as absent that lies inside a runtime
// directory the bundle replaces goes into the previous files with that
// directory, and the note says so instead of "left in place".
func TestAbsentKnowledgeBaseInsideAReplacedDirectoryIsNamedAsKept(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	config := []byte(`{"database_name":"persona_manager","kb_dir":"kb"}`)
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "manager.json"), config)
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", "kb", "later.md"), []byte("knowledge written after the backup"))
	fixture.mutate(t)
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	if len(result.Notes) != 1 || !strings.Contains(result.Notes[0], "lies inside managers/assistant, which restore replaced, so it is kept with that directory in the previous files") {
		errorf(t, "notes = %q", result.Notes)
	}
	holders := previousFilesHolding(t, fixture.home, "assistant")
	if len(holders) != 1 {
		fatalf(t, "previous managers/assistant: %v", holders)
	}
	assertFile(t, filepath.Join(holders[0], "kb", "later.md"), []byte("knowledge written after the backup"))
}
