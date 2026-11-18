package assistant

import (
	"errors"

	"github.com/nwverse/persona/server/internal/assistant/archiverecovery"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
)

// restoreArchiveReservations reconstructs cleanup before any new archive read.
// Every journal is loaded and validated before one is changed or retried.
func (s *Service) restoreArchiveReservations(e *kb.Engine) error {
	if e == nil || e.Store == nil {
		return errors.New("archive recovery store unavailable")
	}
	records, err := archiverecovery.Read(e.Store.Dir)
	if err != nil {
		return err
	}
	s.kb.archiveCleanupMu.Lock()
	defer s.kb.archiveCleanupMu.Unlock()
	if s.kb.archiveCleanup == nil && len(records) > 0 {
		s.kb.archiveCleanup = make(map[string]archiveReservation)
	}
	for _, record := range records {
		if _, exists := s.kb.archiveCleanup[record.Token]; exists {
			continue
		}
		pending := archiveReservation{dir: e.Store.Dir,
			req: assistantwire.ArchiveFence{ID: record.ID, Generation: record.Generation, Token: record.Token}}
		switch record.Stage {
		case archiverecovery.Begin:
			pending.method = assistantwire.ConnectorsArchiveAbort
		case archiverecovery.Settle, archiverecovery.End:
			pending.method = assistantwire.ConnectorsArchiveEnd
		}
		if record.Stage == archiverecovery.Settle {
			pairs := append([]archiverecovery.Chat{}, record.Chats...)
			pending.settle = func() error {
				for _, pair := range pairs {
					if err := e.Store.SettleIngest(pair.Source, pair.Chat); err != nil {
						return err
					}
				}
				return nil
			}
		}
		s.kb.archiveCleanup[record.Token] = pending
	}
	return nil
}
