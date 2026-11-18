package archiverecovery

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/managerconf"
)

const (
	archiveRecoveryVersion = 1
	Dir                    = "archive-recovery"
	archiveRecoveryMost    = 10000
)

type Stage string

const (
	Begin  Stage = "begin"
	Settle Stage = "settle"
	End    Stage = "end"
)

type Chat struct {
	Source string `json:"source"`
	Chat   string `json:"chat"`
}

type Work struct {
	Chats []Chat
}

func (w *Work) Add(source, chat string) {
	for _, pair := range w.Chats {
		if pair.Source == source && pair.Chat == chat {
			return
		}
	}
	w.Chats = append(w.Chats, Chat{Source: source, Chat: chat})
}

// Record is the durable form of one source-manager reservation.
// Its stage records how far the assistant got; recovery never replays write.
type Record struct {
	Version    int    `json:"version"`
	Stage      Stage  `json:"stage"`
	ID         string `json:"id"`
	Generation string `json:"generation"`
	Token      string `json:"token"`
	Chats      []Chat `json:"chats"`
}

func NewRecord(req assistantwire.ArchiveFence, stage Stage, work Work) Record {
	chats := append([]Chat{}, work.Chats...)
	return Record{Version: archiveRecoveryVersion, Stage: stage, ID: req.ID,
		Generation: req.Generation, Token: req.Token, Chats: chats}
}

func Path(dir, token string) string {
	return filepath.Join(dir, Dir, token+".json")
}

func validArchiveRecoveryHex(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateArchiveRecovery(record Record) error {
	if record.Version != archiveRecoveryVersion || record.ID == "" ||
		!validArchiveRecoveryHex(record.Generation) || !validArchiveRecoveryHex(record.Token) {
		return errors.New("invalid archive recovery identity")
	}
	switch record.Stage {
	case Begin, Settle, End:
	default:
		return errors.New("invalid archive recovery stage")
	}
	if record.Chats == nil || len(record.Chats) > archiveRecoveryMost {
		return errors.New("invalid archive recovery chats")
	}
	seen := make(map[Chat]struct{}, len(record.Chats))
	for _, pair := range record.Chats {
		if pair.Source == "" || pair.Chat == "" {
			return errors.New("invalid archive recovery chat")
		}
		if _, ok := seen[pair]; ok {
			return errors.New("duplicate archive recovery chat")
		}
		seen[pair] = struct{}{}
	}
	return nil
}

func Write(dir string, record Record) error {
	if dir == "" {
		return errors.New("archive recovery directory unavailable")
	}
	if err := validateArchiveRecovery(record); err != nil {
		return err
	}
	path := Path(dir, record.Token)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return managerconf.WriteFileAtomic(path, raw, 0o600)
}

func Remove(dir, token string) error {
	if dir == "" || !validArchiveRecoveryHex(token) {
		return errors.New("invalid archive recovery cleanup")
	}
	err := os.Remove(Path(dir, token))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func Read(dir string) ([]Record, error) {
	if dir == "" {
		return nil, errors.New("archive recovery directory unavailable")
	}
	entries, err := os.ReadDir(filepath.Join(dir, Dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > archiveRecoveryMost {
		return nil, errors.New("too many archive recovery records")
	}
	var records []Record
	for _, entry := range entries {
		name := entry.Name()
		// A crash can leave the temporary half of an atomic replacement. The
		// previous complete record remains authoritative.
		if strings.Contains(name, ".tmp-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return nil, errors.New("invalid archive recovery entry")
		}
		raw, err := os.ReadFile(filepath.Join(dir, Dir, name))
		if err != nil {
			return nil, err
		}
		var record Record
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("decode archive recovery journal: %w", err)
		}
		if err := validateArchiveRecovery(record); err != nil {
			return nil, err
		}
		if name != record.Token+".json" {
			return nil, errors.New("archive recovery token does not match its journal")
		}
		records = append(records, record)
	}
	return records, nil
}
