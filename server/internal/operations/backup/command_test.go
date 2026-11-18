package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandErrorsRedactDiscoveredSecrets(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	fixture.backend.dumpErr = errors.New("dump " + fixture.hub.URL + " used hub-secret with hub-token-secret and usage-secret")
	fixture.backend.dumpPart = "hub"
	var stdout, stderr bytes.Buffer
	err := runWithOperator(context.Background(), fixture.operator(), []string{"create", "--home", fixture.home, "--output", fixture.output, "--writes-quiesced"}, &stdout, &stderr)
	if err == nil {
		fatal(t, "expected the injected dump failure")
	}
	output := err.Error() + stdout.String() + stderr.String()
	for _, secret := range []string{fixture.hub.URL, "hub-secret", "hub-token-secret", "usage-secret"} {
		if strings.Contains(output, secret) {
			errorf(t, "output contains %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "<redacted>") {
		fatalf(t, "error does not show redaction: %q", output)
	}
}

func TestCreateAndRestoreRequireExplicitQuiescence(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), Options{Home: fixture.home, Output: fixture.output}); err == nil || !strings.Contains(err.Error(), "--writes-quiesced") {
		fatalf(t, "create without quiescence = %v", err)
	}
	if _, err := fixture.operator().Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output}); err == nil || !strings.Contains(err.Error(), "--writes-quiesced") {
		fatalf(t, "restore without quiescence = %v", err)
	}
}

func TestCommandUsageIsTopLevel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runWithOperator(context.Background(), fixturelessOperator{}, nil, &stdout, &stderr)
	var usage string
	if err == nil || !strings.Contains(err.Error(), "personad backup create") {
		fatalf(t, "missing usage = %v", err)
	}
	usage = Usage
	for _, command := range []string{"create", "verify", "restore"} {
		if !strings.Contains(usage, "personad backup "+command) {
			errorf(t, "usage lacks %s", command)
		}
	}
}

type fixturelessOperator struct{}

func (fixturelessOperator) Create(context.Context, Options) (Result, error)  { return Result{}, nil }
func (fixturelessOperator) Verify(context.Context, Options) (Result, error)  { return Result{}, nil }
func (fixturelessOperator) Restore(context.Context, Options) (Result, error) { return Result{}, nil }

func TestBundleRoundTripAndSHAMismatch(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	staging := t.TempDir()
	if err := extractBundle(fixture.output, staging, manifest); err != nil {
		fatal(t, err)
	}
	for _, entry := range manifest.Files {
		info, err := os.Stat(filepath.Join(staging, filepath.FromSlash(entry.Path)))
		if err != nil || info.Mode().Perm() != os.FileMode(entry.Mode) {
			errorf(t, "extracted %s mode = %v, %v", entry.Path, info.Mode(), err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, hubDumpPath), []byte("tamper!"), 0o600); err != nil {
		fatal(t, err)
	}
	tampered := filepath.Join(t.TempDir(), "tampered.backup")
	if err := publishBundle(context.Background(), staging, tampered); err != nil {
		fatal(t, err)
	}
	tamperedManifest, _, err := readBundleManifest(tampered)
	if err != nil {
		fatal(t, err)
	}
	if err := extractBundle(tampered, t.TempDir(), tamperedManifest); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		fatalf(t, "tampered bundle verification = %v", err)
	}
}

func TestBundleRejectsTraversalIndependentManagersAndWrongMode(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	bad := *manifest
	bad.Files = append([]FileEntry(nil), manifest.Files...)
	bad.Files[0].Path = "../escape"
	if err := bad.validate(); err == nil {
		fatal(t, "path traversal was accepted")
	}
	independent := *manifest
	independent.Files = append(append([]FileEntry(nil), manifest.Files...), FileEntry{Path: "runtime/managers/m-abcdef/secret.key", Size: 1, SHA256: strings.Repeat("0", 64), Mode: 0o600})
	if err := independent.validate(); err == nil {
		fatal(t, "an independently hosted manager was accepted")
	}
	if err := os.Chmod(fixture.output, 0o644); err != nil {
		fatal(t, err)
	}
	if _, _, err := readBundleManifest(fixture.output); err == nil || !strings.Contains(err.Error(), "0600") {
		fatalf(t, "group-readable bundle was accepted: %v", err)
	}
}

func TestCanceledPublicationLeavesNoBundle(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	manifest, _, err := readBundleManifest(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	staging := t.TempDir()
	if err := extractBundle(fixture.output, staging, manifest); err != nil {
		fatal(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := filepath.Join(t.TempDir(), "interrupted.backup")
	if err := publishBundle(ctx, staging, output); !errors.Is(err, context.Canceled) {
		fatalf(t, "publication interruption = %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		fatalf(t, "interrupted publication left an output: %v", err)
	}
}

func TestArchiveContainsOnlyRegularOwnerOnlyFiles(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	if _, err := fixture.operator().Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	file, err := os.Open(fixture.output)
	if err != nil {
		fatal(t, err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fatal(t, err)
		}
		if header.Typeflag != tar.TypeReg || header.Mode&0o077 != 0 {
			errorf(t, "member %s type %d mode %#o", header.Name, header.Typeflag, header.Mode&0o777)
		}
		if header.Name == "runtime/hub.json" && header.Mode&0o777 != 0o600 {
			errorf(t, "secret configuration mode = %#o", header.Mode&0o777)
		}
		if strings.HasPrefix(header.Name, "runtime/managers/m-") {
			errorf(t, "independent manager entered bundle: %s", header.Name)
		}
	}
}

type resultOperator struct {
	fixturelessOperator
	result Result
}

func (o resultOperator) Restore(context.Context, Options) (Result, error) { return o.result, nil }

func TestRestoreOutputNamesWhatWasKept(t *testing.T) {
	for _, kept := range []bool{true, false} {
		var stdout, stderr bytes.Buffer
		operator := resultOperator{result: Result{Bundle: "b", Restored: true, KeptDockerPassword: kept, PreviousExternalKB: "/kb/.data.persona-backup-previous-1"}}
		if err := runWithOperator(context.Background(), operator, []string{"restore", "--bundle", "b", "--writes-quiesced"}, &stdout, &stderr); err != nil {
			fatal(t, err)
		}
		if got := strings.Contains(stdout.String(), "kept this install's docker/db-password"); got != kept {
			errorf(t, "kept=%v: output %q", kept, stdout.String())
		}
		if !strings.Contains(stdout.String(), "/kb/.data.persona-backup-previous-1") {
			errorf(t, "the previous external knowledge base is not named: %q", stdout.String())
		}
	}
}

func TestUsageStatesTheOutputRestriction(t *testing.T) {
	if !strings.Contains(Usage, "outside the knowledge base") {
		errorf(t, "usage does not say where --output may not be: %s", Usage)
	}
}
