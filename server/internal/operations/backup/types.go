// Package backup implements verified, whole-install backup and recovery as an
// operations command. It deliberately does not extend the hub's business API.
package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	formatVersion   = 1
	manifestPath    = "manifest.json"
	hubDumpPath     = "db/hub.dump"
	managerDumpPath = "db/manager.dump"
	managerKeyPath  = "runtime/managers/assistant/secret.key"
	hubKeyPath      = "runtime/secret.key"
)

// Target is one PostgreSQL database and the client used to dump it. URL is
// never serialized into a manifest or included in an error.
type Target struct {
	Part             string
	URL              string
	Database         string
	Container        string
	DefaultContainer string
}

// Migration is the stable schema identity recorded in each database.
type Migration struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Part    string `json:"part"`
}

// DatabaseIdentity is the restorable schema and encryption-key identity. The
// server version is informational; verification compares schema and key data.
type DatabaseIdentity struct {
	ServerVersion string      `json:"server_version"`
	Schema        []Migration `json:"schema"`
	SealingKey    string      `json:"sealing_key_fingerprint,omitempty"`
	// SealedValues are the manager's sealed secrets, read only when no
	// sealing_key record names its key: verification then proves the bundled
	// key opens them. Never serialized.
	SealedValues []string `json:"-"`
}

// FileEntry authenticates one payload member. Payload is regular and
// owner-only: secrets and dumps are 0600; executables may be 0700.
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

// KnowledgeBaseLocator describes where its files belong without putting a
// credential-bearing database URL in the manifest.
type KnowledgeBaseLocator struct {
	Relative string `json:"relative,omitempty"`
	External string `json:"external,omitempty"`
	// Present records whether the install had a knowledge base. A manifest
	// must state it (see UnmarshalJSON).
	Present bool `json:"present"`
}

// UnmarshalJSON requires present: a manifest without it cannot say whether
// the bundle has no knowledge base or was written without the record.
func (l *KnowledgeBaseLocator) UnmarshalJSON(data []byte) error {
	var raw struct {
		Relative string `json:"relative"`
		External string `json:"external"`
		Present  *bool  `json:"present"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if raw.Present == nil {
		return errors.New("the manifest's knowledge_base does not say whether the bundle has a knowledge base (present); this bundle was not written by personad backup create, or by a version before the format was final")
	}
	*l = KnowledgeBaseLocator{Relative: raw.Relative, External: raw.External, Present: *raw.Present}
	return nil
}

// Manifest is the versioned bundle identity. Key fingerprints are HMACs, not
// key material; the key material itself is hashed as ordinary payload content.
type Manifest struct {
	Format                int                  `json:"format"`
	CreatedAt             time.Time            `json:"created_at"`
	Hub                   DatabaseIdentity     `json:"hub"`
	Manager               DatabaseIdentity     `json:"manager"`
	HubDatabase           string               `json:"hub_database"`
	ManagerDatabase       string               `json:"manager_database"`
	HubKeyFingerprint     string               `json:"hub_key_fingerprint"`
	ManagerKeyFingerprint string               `json:"manager_key_fingerprint"`
	KnowledgeBase         KnowledgeBaseLocator `json:"knowledge_base"`
	RuntimeDirectories    []string             `json:"runtime_directories,omitempty"`
	Files                 []FileEntry          `json:"files"`
}

// Backend isolates PostgreSQL administration and client programs so failure,
// interruption, and rollback behavior is tested without a production server.
type Backend interface {
	Identity(context.Context, Target, string, string) (DatabaseIdentity, error)
	Quiesced(context.Context, Target, string, bool) error
	Dump(context.Context, Target, string) error
	Create(context.Context, Target, string) error
	Restore(context.Context, Target, string, string) error
	Drop(context.Context, Target, string) error
	Rename(context.Context, Target, string, string) error
	Exists(context.Context, Target, string) (bool, error)
}

// Options controls one operation. DatabaseURL overrides are useful when the
// runtime configuration itself is lost; secrets are accepted but never logged.
type Options struct {
	Home                   string
	Bundle                 string
	Output                 string
	WritesQuiesced         bool
	AllowExternalKB        bool
	DatabaseURL            string
	ManagerDatabaseURL     string
	HubDockerContainer     string
	ManagerDockerContainer string
	Manifest               *Manifest
	BundleHome             string
}

// Result is safe operational output: no URLs, passwords, or key material.
type Result struct {
	Bundle            string
	Verified          bool
	Restored          bool
	PreviousHubDB     string
	PreviousManagerDB string
	PreviousFiles     string
	// KeptDockerPassword: the bundle has a docker/db-password, but the install's
	// own was kept (see restoreItems).
	KeptDockerPassword bool
	// PreviousExternalKB: where an approved external knowledge base's previous
	// copy was kept (its own directory, beside the knowledge base).
	PreviousExternalKB string
	// Notes are printed after the result: for create, a knowledge base that
	// was not there to collect; for restore, what the bundle records as absent
	// or empty and restore left in place (see leftInPlaceNotes).
	Notes []string
}
