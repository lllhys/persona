package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/nwverse/persona/server/internal/secretkey"
)

const maxManifestSize = 8 << 20

func validMemberName(name string) bool {
	return name != "" && name != "." && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name &&
		name != ".." && !strings.HasPrefix(name, "../")
}

func validFingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validPayloadPath(name string) bool {
	if name == hubDumpPath || name == managerDumpPath || strings.HasPrefix(name, "kb/") {
		return true
	}
	if !strings.HasPrefix(name, "runtime/") || strings.HasPrefix(name, "runtime/managers/m-") {
		return false
	}
	rel := strings.TrimPrefix(name, "runtime/")
	for _, prefix := range []string{"managers/assistant/", "extensions/", "skills/", "hub.machines_kept/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	if rel == "docker/db-password" {
		return true
	}
	if !strings.Contains(rel, "/") {
		if rel == "hub.json" || rel == "secret.key" || strings.HasPrefix(rel, "secret.key.next-") {
			return true
		}
		for _, optional := range optionalRootFiles {
			if rel == optional {
				return true
			}
		}
	}
	return false
}

func (m *Manifest) validate() error {
	if m.Format != formatVersion {
		return fmt.Errorf("unsupported backup format %d", m.Format)
	}
	if m.HubDatabase == "" || m.ManagerDatabase == "" {
		return errors.New("the manifest does not name both databases")
	}
	if len(m.Hub.Schema) == 0 || len(m.Manager.Schema) == 0 {
		return errors.New("the manifest has no database schema identity")
	}
	if !validFingerprint(m.HubKeyFingerprint) || !validFingerprint(m.ManagerKeyFingerprint) {
		return errors.New("the manifest has an invalid encryption-key fingerprint")
	}
	if m.Manager.SealingKey != "" && m.Manager.SealingKey != m.ManagerKeyFingerprint {
		return errors.New("the built-in manager database and current secret.key do not match")
	}
	if m.KnowledgeBase.Relative != "" && m.KnowledgeBase.External != "" {
		return errors.New("the knowledge-base location is ambiguous")
	}
	if m.KnowledgeBase.Relative != "" && !validMemberName(m.KnowledgeBase.Relative) {
		return errors.New("the knowledge-base relative path is invalid")
	}
	if m.KnowledgeBase.External != "" && (!filepath.IsAbs(m.KnowledgeBase.External) || filepath.Clean(m.KnowledgeBase.External) == string(filepath.Separator)) {
		return errors.New("the knowledge-base external path is invalid")
	}
	seen := make(map[string]bool, len(m.Files))
	for _, entry := range m.Files {
		if !m.KnowledgeBase.Present && strings.HasPrefix(entry.Path, "kb/") {
			return fmt.Errorf("the manifest records no knowledge base but lists the knowledge-base file %q", entry.Path)
		}
		if !validMemberName(entry.Path) || entry.Path == manifestPath || seen[entry.Path] || !validPayloadPath(entry.Path) {
			return fmt.Errorf("invalid or duplicate payload path %q", entry.Path)
		}
		seen[entry.Path] = true
		if entry.Size < 0 || !validSHA256(entry.SHA256) || (entry.Mode != 0o600 && entry.Mode != 0o700) {
			return fmt.Errorf("invalid payload identity for %s", entry.Path)
		}
	}
	for _, required := range []string{hubDumpPath, managerDumpPath, "runtime/hub.json", hubKeyPath, managerKeyPath, "runtime/managers/assistant/manager.json"} {
		if !seen[required] {
			return fmt.Errorf("the bundle lacks required member %s", required)
		}
		for _, entry := range m.Files {
			if entry.Path == required && entry.Mode != 0o600 {
				return fmt.Errorf("required member %s is not mode 0600", required)
			}
		}
	}
	for _, directory := range m.RuntimeDirectories {
		if !validMemberName(directory) || strings.HasPrefix(directory, "managers/m-") {
			return fmt.Errorf("invalid runtime directory %q", directory)
		}
	}
	return nil
}

func manifestFiles(staging string) ([]FileEntry, error) {
	var files []FileEntry
	err := filepath.WalkDir(staging, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup staging contains a non-regular file: %s", name)
		}
		rel, err := filepath.Rel(staging, name)
		if err != nil {
			return err
		}
		member := filepath.ToSlash(rel)
		if member == manifestPath {
			return nil
		}
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files = append(files, FileEntry{Path: member, Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)), Mode: uint32(info.Mode().Perm())})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func writeManifest(staging string, manifest *Manifest) error {
	files, err := manifestFiles(staging)
	if err != nil {
		return err
	}
	manifest.Files = files
	if err := manifest.validate(); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(staging, manifestPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func publishBundle(ctx context.Context, staging, output string) error {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".persona-backup-publish-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	tarWriter := tar.NewWriter(file)
	manifest, err := readManifestPath(filepath.Join(staging, manifestPath))
	if err != nil {
		file.Close()
		return err
	}
	members := append([]string{manifestPath}, make([]string, 0, len(manifest.Files))...)
	for _, entry := range manifest.Files {
		members = append(members, entry.Path)
	}
	for _, member := range members {
		if err := appendTarMember(ctx, tarWriter, staging, member); err != nil {
			tarWriter.Close()
			file.Close()
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A hard link publishes the already-synced inode atomically and never
	// overwrites an existing backup, even if another creator races us.
	if err := os.Link(temporary, output); err != nil {
		return fmt.Errorf("publish the complete backup without overwriting: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return err
	}
	return secretkey.SyncDir(parent)
}

func appendTarMember(ctx context.Context, writer *tar.Writer, staging, member string) error {
	if !validMemberName(member) {
		return fmt.Errorf("invalid archive member %q", member)
	}
	name := filepath.Join(staging, filepath.FromSlash(member))
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("archive member is not a regular file: %s", member)
	}
	header := &tar.Header{Name: member, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime().UTC(), Format: tar.FormatPAX}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.CopyN(writer, contextReader{ctx: ctx, reader: file}, info.Size())
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func readManifestPath(name string) (*Manifest, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return decodeManifest(io.LimitReader(file, maxManifestSize+1))
}

func decodeManifest(reader io.Reader) (*Manifest, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("read the backup manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("the backup manifest has trailing data")
	}
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func readBundleManifest(bundle string) (*Manifest, int64, error) {
	info, err := os.Lstat(bundle)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("the bundle is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, 0, errors.New("the bundle must be mode 0600 before it is read")
	}
	file, err := os.Open(bundle)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, 0, errors.New("the bundle has no manifest")
		}
		if err != nil {
			return nil, 0, err
		}
		if header.Name != manifestPath {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size > maxManifestSize {
			return nil, 0, errors.New("the bundle manifest is invalid")
		}
		manifest, err := decodeManifest(io.LimitReader(reader, maxManifestSize+1))
		return manifest, info.Size(), err
	}
}

func extractBundle(bundle, destination string, manifest *Manifest) error {
	file, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer file.Close()
	expected := make(map[string]FileEntry, len(manifest.Files))
	for _, entry := range manifest.Files {
		expected[entry.Path] = entry
	}
	seen := make(map[string]bool, len(expected))
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name == manifestPath {
			if seen[manifestPath] {
				return errors.New("duplicate bundle manifest")
			}
			seen[manifestPath] = true
			if err := extractValidatedManifest(reader, destination, header.Size, manifest); err != nil {
				return err
			}
			continue
		}
		entry, ok := expected[header.Name]
		if !ok {
			return fmt.Errorf("unlisted bundle member %q", header.Name)
		}
		if seen[header.Name] || header.Typeflag != tar.TypeReg || header.Size != entry.Size || uint32(header.Mode&0o777) != entry.Mode {
			return fmt.Errorf("invalid bundle member %s", header.Name)
		}
		seen[header.Name] = true
		if err := extractMember(reader, destination, entry); err != nil {
			return err
		}
	}
	for name := range expected {
		if !seen[name] {
			return fmt.Errorf("bundle member %s is missing", name)
		}
	}
	if !seen[manifestPath] {
		return errors.New("the bundle manifest is missing")
	}
	return nil
}

func extractValidatedManifest(reader io.Reader, destination string, size int64, expected *Manifest) error {
	if size > maxManifestSize {
		return errors.New("the bundle manifest is too large")
	}
	name := filepath.Join(destination, manifestPath)
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	manifest, decodeErr := decodeManifest(io.TeeReader(io.LimitReader(reader, size), file))
	if decodeErr == nil && !reflect.DeepEqual(manifest, expected) {
		decodeErr = errors.New("the bundle manifest changed between validation passes")
	}
	if syncErr := file.Sync(); decodeErr == nil {
		decodeErr = syncErr
	}
	if closeErr := file.Close(); decodeErr == nil {
		decodeErr = closeErr
	}
	return decodeErr
}

func extractMember(reader io.Reader, destination string, entry FileEntry) error {
	name := filepath.Join(destination, filepath.FromSlash(entry.Path))
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(entry.Mode))
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.CopyN(io.MultiWriter(file, hash), reader, entry.Size)
	if copyErr == nil && written != entry.Size {
		copyErr = io.ErrUnexpectedEOF
	}
	if copyErr == nil && hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		copyErr = fmt.Errorf("SHA-256 mismatch for %s", entry.Path)
	}
	if syncErr := file.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	return copyErr
}
