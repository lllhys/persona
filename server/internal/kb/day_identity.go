package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"time"
)

// DayIdentity is archive metadata for a single coherent day read. It is internal
// matching evidence, not a claim that an unmarked message was written by the user.
type DayIdentity struct {
	Chat     *Chat
	Zone     *time.Location
	Messages map[int]MessageIdentity // source-file line number
}

type MessageIdentity struct {
	Ref       string
	At        time.Time
	Mine      bool
	Ambiguous bool // indistinguishable lines or receipts must not use text fallback
}

// DayWithIdentity reads a day and its receipt metadata under the archive locks.
// Legacy receipts only contain timestamps; those cannot establish a message ID.
func (e *Engine) DayWithIdentity(chatID, day string) (msgs []DayMessage, prev, next string, identity DayIdentity, err error) {
	s := e.Store
	s.lockArchive()
	defer s.unlockArchive()
	if err = s.pendingIngestLocked(chatID); err != nil {
		return
	}
	if _, err = s.settleChatLocked(chatID); err != nil {
		return
	}
	var c Chat
	var ok bool
	if ok, err = readJSON(s.entityPath(KindChat, chatID), &c); err != nil {
		return
	}
	if !ok {
		err = ErrNotFound
		return
	}
	zone := s.zoneLocked()
	raw, readErr := os.ReadFile(s.path(KindChat, chatID, "messages", zoneFile))
	if readErr != nil && !os.IsNotExist(readErr) {
		err = readErr
		return
	}
	if readErr == nil {
		if zone, err = loadZone(strings.TrimSpace(string(raw))); err != nil {
			return
		}
	}
	msgs, prev, next, err = e.Day(chatID, day)
	if err != nil {
		return
	}
	r, err := s.receipts(c.Source, c.SourceID, chatID)
	if err != nil {
		return
	}
	identity = DayIdentity{Chat: &c, Zone: zone, Messages: map[int]MessageIdentity{}}
	// The time remains separate from the body hash so re-zoning needs no receipt rewrite.
	byLine := map[string][]int{}
	for _, m := range msgs {
		if len(m.Text) < 6 || m.Text[5] != ' ' {
			continue
		}
		key := m.Text[:6] + receiptBodyHash(m.Text[6:])
		byLine[key] = append(byLine[key], m.N)
	}
	byReceipt := map[string][]MessageIdentity{}
	for ref, line := range r.Lines {
		at, exists := r.IDs[ref]
		if !exists || line.Hash == "" || at.In(zone).Format("2006-01-02") != day {
			continue
		}
		key := at.In(zone).Format("15:04 ") + line.Hash
		byReceipt[key] = append(byReceipt[key], MessageIdentity{Ref: ref, At: at, Mine: line.Mine})
	}
	for key, ns := range byLine {
		refs := byReceipt[key]
		if len(ns) > 1 || len(refs) > 1 {
			for _, n := range ns {
				identity.Messages[n] = MessageIdentity{Ambiguous: true}
			}
		} else if len(refs) == 1 {
			identity.Messages[ns[0]] = refs[0]
		}
	}
	return
}

func receiptBodyHash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func receiptLineHash(l Line) string {
	// Use the day reader's rendering, including its continuation-line whitespace.
	ms := messages(l.Format(time.UTC))
	return receiptBodyHash(ms[0].text[6:])
}
