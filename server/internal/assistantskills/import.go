package assistantskills

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ImportTarget comes only from the authorized manager's current execution context, never from browser paths.
type ImportTarget struct{ Scope, Cwd string }

type ImportRequest struct {
	Op        string `json:"op"`
	Archive   []byte `json:"archive,omitempty"`
	Reference string `json:"reference,omitempty"`
}

type ImportResult struct {
	Status      string    `json:"status,omitempty"`
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Files       int       `json:"files,omitempty"`
	Bytes       int       `json:"bytes,omitempty"`
	Reference   string    `json:"reference,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Recovery    string    `json:"recovery,omitempty"`
}

type importReference struct {
	target    ImportTarget
	root      string
	rootInfo  os.FileInfo
	installed os.FileInfo
	digest    [32]byte
	tree      string
	expires   time.Time
	result    ImportResult
}

// Importer holds only bounded, expiring metadata receipts, not uploaded bytes or staged files.
// A node restart invalidates receipts explicitly; no deferred install survives cancellation or restart.
type Importer struct {
	mu            sync.Mutex
	refs          map[string]*importReference
	beforePublish func() // deterministic filesystem/cancellation races in tests
}

const importReceiptTTL = 10 * time.Minute

func (i *Importer) Import(ctx context.Context, target ImportTarget, in ImportRequest) (ImportResult, error) {
	var empty ImportResult
	if !i.mu.TryLock() {
		return empty, importErr("skill_import_busy")
	}
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if target.Scope == "" || target.Cwd == "" {
		return empty, importErr("skill_environment_changed")
	}
	if i.refs == nil {
		i.refs = map[string]*importReference{}
	}
	for key, ref := range i.refs {
		if time.Now().After(ref.expires) {
			delete(i.refs, key)
		}
	}
	switch in.Op {
	case "preview", "install", "cancel", "undo", "status":
	default:
		return empty, importErr("skill_import_request")
	}
	var ref *importReference
	if in.Op != "preview" {
		ref = i.refs[in.Reference]
		if ref == nil {
			return empty, importErr("skill_import_expired")
		}
		if ref.target != target {
			return empty, importErr("skill_environment_changed")
		}
		if in.Op == "cancel" {
			if ref.result.Status != "previewed" {
				return empty, importErr("skill_import_request")
			}
			delete(i.refs, in.Reference)
			return empty, nil
		}
		if !sameImportRoot(ref) {
			return empty, importErr("skill_environment_changed")
		}
		if in.Op == "status" {
			return ref.result, nil // receipts record outcomes; this does not re-run an effect
		}
	}
	if in.Op == "undo" {
		return i.undo(ctx, in.Reference, ref)
	}
	if in.Op == "install" && (ref.result.Status != "previewed" || ref.digest != sha256.Sum256(in.Archive)) {
		return empty, importErr("skill_import_changed")
	}
	if in.Op == "preview" {
		// An installed operation has two lookup keys but only one metadata receipt.
		operations := map[*importReference]struct{}{}
		for _, ref := range i.refs {
			operations[ref] = struct{}{}
		}
		if len(operations) >= 32 {
			return empty, importErr("skill_import_busy")
		}
	}
	p, err := inspectArchive(ctx, in.Archive)
	if err != nil {
		return empty, err
	}
	if err := importConflict(p.name, target.Cwd); err != nil {
		return empty, err
	}
	root, err := filepath.Abs(UserDir())
	if err != nil {
		return empty, importErr("skill_import_io")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return empty, importErr("skill_import_io")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return empty, importErr("skill_import_io")
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return empty, importErr("skill_import_io")
	}
	if ref != nil && (ref.root != root || !os.SameFile(ref.rootInfo, rootInfo)) {
		return empty, importErr("skill_environment_changed")
	}
	stage, err := os.MkdirTemp(root, ".import-")
	if err != nil {
		return empty, importErr("skill_import_io")
	}
	defer os.RemoveAll(stage)
	if err := p.extract(ctx, stage); err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, importErr("skill_import_io")
	}
	result := ImportResult{Status: "previewed", Name: p.name, Description: p.description, Files: len(p.files), Bytes: p.bytes,
		Destination: filepath.Join(root, p.name), ExpiresAt: time.Now().Add(importReceiptTTL)}
	tree, err := importTree(ctx, stage)
	if err != nil {
		return empty, importErr("skill_import_io")
	}
	if in.Op == "preview" {
		result.Reference = importID()
		i.refs[result.Reference] = &importReference{target: target, root: root, rootInfo: rootInfo,
			digest: sha256.Sum256(in.Archive), tree: tree, expires: result.ExpiresAt, result: result}
		return result, nil
	}
	// Check again after staging: both another importer and plugin provisioning may have claimed the name.
	if i.beforePublish != nil {
		i.beforePublish()
	}
	if err := importConflict(p.name, target.Cwd); err != nil {
		return empty, err
	}
	if !sameImportRoot(ref) || ctx.Err() != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, importErr("skill_environment_changed")
	}
	info, err := os.Lstat(stage)
	if err != nil {
		return empty, importErr("skill_import_io")
	}
	if err := importRename(stage, result.Destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return empty, importErr("skill_conflict")
		}
		return empty, importErr("skill_import_io")
	}
	result.Status = "installed"
	result.Reference = importID()
	installed := &importReference{target: target, root: root, rootInfo: rootInfo,
		installed: info, tree: tree, expires: result.ExpiresAt, result: result}
	// Retain the preview key too: a caller that lost the install response can recover
	// its outcome and undo receipt without repeating the installation.
	i.refs[in.Reference], i.refs[result.Reference] = installed, installed
	// Publication is the commit point. A disconnect after it cannot turn a completed install into a partial one.
	return result, nil
}

func importID() string { return hex.EncodeToString([]byte(rand.Text())) }

func sameImportRoot(ref *importReference) bool {
	at, err := filepath.Abs(UserDir())
	if err != nil {
		return false
	}
	at, err = filepath.EvalSymlinks(at)
	if err != nil || at != ref.root {
		return false
	}
	info, err := os.Stat(at)
	return err == nil && os.SameFile(ref.rootInfo, info)
}

func importConflict(name, cwd string) error {
	for _, n := range slices.Concat(builtinNames(), pluginNames(PluginDir())) {
		if strings.EqualFold(n, name) {
			return importErr("skill_conflict")
		}
	}
	for _, dir := range []string{UserDir(), filepath.Join(cwd, ".claude", "skills"), filepath.Join(cwd, ".agents", "skills")} {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return importErr("skill_import_io")
		}
		for _, e := range entries {
			if strings.EqualFold(e.Name(), name) {
				return importErr("skill_conflict")
			}
		}
	}
	return nil
}

func importTree(ctx context.Context, dir string) (string, error) {
	h := sha256.New()
	count, total := 0, int64(0)
	err := filepath.WalkDir(dir, func(at string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > MaxImportEntries*MaxImportDepth+1 {
			return importErr("skill_undo_changed")
		}
		rel, _ := filepath.Rel(dir, at)
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Mode())
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > MaxImportFile {
			return importErr("skill_undo_changed")
		}
		f, err := os.Open(at)
		if err != nil {
			return err
		}
		n, readErr := io.Copy(h, io.LimitReader(&importContextReader{ctx, f}, MaxImportFile+1))
		closeErr := f.Close()
		total += n
		if readErr != nil || closeErr != nil || n > MaxImportFile || total > MaxImportExpanded {
			return importErr("skill_undo_changed")
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// Undo detaches only the exact, unchanged new directory. Recovery is retained, not deleted: a local writer holding
// an open file across the rename cannot lose its changes. No arbitrary name/path removal endpoint is exposed.
func (i *Importer) undo(ctx context.Context, key string, ref *importReference) (ImportResult, error) {
	var empty ImportResult
	if ref.installed == nil || ref.result.Status != "installed" {
		return empty, importErr("skill_import_request")
	}
	at := ref.result.Destination
	info, err := os.Lstat(at)
	if err != nil || !info.IsDir() || !os.SameFile(info, ref.installed) {
		return empty, importErr("skill_undo_changed")
	}
	tree, err := importTree(ctx, at)
	if err != nil || tree != ref.tree {
		return empty, importErr("skill_undo_changed")
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	recovery := filepath.Join(ref.root, ".undone-"+importID())
	if err := importRename(at, recovery); err != nil {
		return empty, importErr("skill_import_io")
	}
	after, err := importTree(ctx, recovery)
	moved, statErr := os.Lstat(recovery)
	if err != nil || statErr != nil || !os.SameFile(moved, ref.installed) || after != ref.tree {
		// Restore without overwriting. If another install took the name meanwhile, retain all data in recovery.
		_ = importRename(recovery, at)
		return empty, importErr("skill_undo_changed")
	}
	ref.result = ImportResult{Status: "undone", Name: ref.result.Name, Recovery: recovery,
		Reference: key, ExpiresAt: ref.expires}
	return ref.result, nil
}
