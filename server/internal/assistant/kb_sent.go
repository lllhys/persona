package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/kb"
)

// Archive clocks have minute precision. Allow two minutes of send/collection clock
// skew on either side of that minute, without prefix or fuzzy text matching.
const kbSentSlack = 2 * time.Minute

type kbSentStore interface {
	SentAsUsersForArchive(context.Context, string, time.Time, time.Time) ([]db.SentAsUser, error)
}

type kbSentLine struct {
	message  *kb.DayMessage
	identity kb.MessageIdentity
	minute   time.Time
	body     string
	mine     bool
}

func (s *Service) kbDaySent(ctx context.Context, e *kb.Engine, p assistantwire.KBDayParams) (assistantwire.KBDayResult, error) {
	messages, prev, next, identity, err := e.DayWithIdentity(p.ID, p.Day)
	out := assistantwire.KBDayResult{Day: p.Day, Messages: messages, Prev: prev, Next: next, SentOrigin: "unavailable"}
	if out.Messages == nil {
		out.Messages = []kb.DayMessage{}
	}
	if err != nil {
		return out, err
	}
	st, ok := s.store().(kbSentStore)
	if !ok || identity.Chat == nil {
		return out, nil
	}
	connector := identity.Chat.Connector
	if connector == "" {
		connector = identity.Chat.Source
	}
	if connector == "" {
		return out, nil
	}
	day, err := time.ParseInLocation("2006-01-02 15:04", p.Day+" 12:00", identity.Zone)
	if err != nil {
		return out, err
	}
	var lines []kbSentLine
	lines = append(lines, kbSentLines(messages, identity, p.Day)...)
	// Include adjacent days in ambiguity checks: one send near midnight must not
	// mark a line on each of two separately requested day pages.
	for _, d := range []time.Time{day.AddDate(0, 0, -1), day.AddDate(0, 0, 1)} {
		date := d.Format("2006-01-02")
		ms, _, _, ids, err := e.DayWithIdentity(p.ID, date)
		if errors.Is(err, kb.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		lines = append(lines, kbSentLines(ms, ids, date)...)
	}
	rows, err := st.SentAsUsersForArchive(ctx, connector, dayStart(day).Add(-kbSentSlack), dayStart(day.AddDate(0, 0, 1)).Add(kbSentSlack))
	if err != nil {
		return out, err
	}
	kbMarkSent(lines, identity.Chat, rows)
	out.SentOrigin = "available"
	return out, nil
}

func kbSentLines(messages []kb.DayMessage, identity kb.DayIdentity, day string) []kbSentLine {
	out := make([]kbSentLine, 0, len(messages))
	for i := range messages {
		m := &messages[i]
		if len(m.Text) < 7 {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02 15:04", day+" "+m.Text[:5], identity.Zone)
		if err != nil || m.Text[5] != ' ' || at.In(identity.Zone).Format("2006-01-02 15:04") != day+" "+m.Text[:5] {
			continue
		}
		// Repeated DST wall-clock minutes do not identify one instant. Leave them
		// unmarked unless a receipt supplies its original absolute timestamp.
		sender, body, ok := strings.Cut(m.Text[6:], ": ")
		if !ok {
			continue
		}
		id, known := identity.Messages[m.N]
		mine := sender == "我" // i18n-ignore: the archive's canonical owner label
		if known {
			mine = id.Mine && !id.Ambiguous
		}
		if !id.At.IsZero() {
			at = id.At.Truncate(time.Minute)
		} else if kbSentRepeatedMinute(at, identity.Zone) {
			mine = false
			id.Ambiguous = true
		}
		out = append(out, kbSentLine{message: m, identity: id, minute: at, body: kbSentText(body), mine: mine})
	}
	return out
}

// Like echo matching, ignore whitespace changes; retain case, punctuation and the
// entire text. A clipped ledger prefix must never claim a longer human message.
func kbSentText(s string) string { return strings.Join(strings.Fields(s), "") }

func kbMarkSent(lines []kbSentLine, chat *kb.Chat, rows []db.SentAsUser) {
	byRef := map[string][]int{}
	byText := map[string][]int{}
	ambiguousText := map[string]bool{}
	for i, l := range lines {
		if l.identity.Ambiguous {
			ambiguousText[l.body] = true
		}
		if !l.mine && !l.identity.Ambiguous {
			continue
		}
		if l.identity.Ref != "" {
			byRef[l.identity.Ref] = append(byRef[l.identity.Ref], i)
		}
		if l.body != "" {
			byText[l.body] = append(byText[l.body], i)
		}
	}
	for _, ids := range byText {
		sort.Slice(ids, func(i, j int) bool { return lines[ids[i]].minute.Before(lines[ids[j]].minute) })
	}
	usedRows, usedLines := map[int]bool{}, map[int]bool{}
	match := func(candidates map[int][]int) {
		counts := map[int]int{}
		for _, ids := range candidates {
			for _, i := range ids {
				counts[i]++
			}
		}
		for r, ids := range candidates {
			if len(ids) != 1 || counts[ids[0]] != 1 || usedLines[ids[0]] || !lines[ids[0]].mine || lines[ids[0]].identity.Ambiguous {
				continue
			}
			i := ids[0]
			m := lines[i].message
			m.SentBy, m.SentLedgerID, m.RecalledAt = "assistant", rows[r].ID, rows[r].RecalledAt
			usedRows[r], usedLines[i] = true, true
		}
	}
	exact := map[int][]int{}
	for r, row := range rows {
		if !kbSentExactTarget(chat, row) {
			continue
		}
		if ref := kbSentRef(row); ref != "" {
			exact[r] = byRef[ref]
		}
	}
	match(exact)
	fallback := map[int][]int{}
	for r, row := range rows {
		if usedRows[r] || !kbSentTarget(chat, row) {
			continue
		}
		// A nonunique exact reference cannot become a content guess.
		if len(exact[r]) > 0 {
			continue
		}
		text := kbSentText(row.Text)
		if ambiguousText[text] {
			continue
		}
		ids := byText[text]
		start := sort.Search(len(ids), func(i int) bool { return lines[ids[i]].minute.Add(time.Minute + kbSentSlack).After(row.At) })
		for _, i := range ids[start:] {
			l := lines[i]
			if l.minute.Add(-kbSentSlack).After(row.At) {
				break
			}
			if l.identity.Ref != "" && row.MessageRef != "" && !usedLines[i] {
				continue
			}
			fallback[r] = append(fallback[r], i)
		}
	}
	match(fallback)
}

// Generic destinations must equal the archive source ID, without lossy ID or
// display-name conversions. Unknown target shapes deliberately do not match.
func kbSentTarget(chat *kb.Chat, row db.SentAsUser) bool {
	connector := chat.Connector
	if connector == "" {
		connector = chat.Source
	}
	if row.Connector != connector || chat.SourceID == "" {
		return false
	}
	var target map[string]json.RawMessage
	if json.Unmarshal(row.Target, &target) != nil {
		return false
	}
	for _, key := range []string{"chat", "thread", "channel", "conversation", "recipient", "to", "ref"} {
		var value string
		if json.Unmarshal(target[key], &value) != nil || value == "" {
			continue
		}
		if value == chat.SourceID {
			return true
		}
	}
	return false
}

func kbSentRef(row db.SentAsUser) string {
	if row.MessageRef == "" {
		return ""
	}
	if row.Connector == "github" {
		switch row.Action {
		case "github_comment":
			return "c" + row.MessageRef
		case "github_review":
			return "r" + row.MessageRef
		default:
			return ""
		}
	}
	return row.MessageRef
}

// Inspect both adjacent zone offsets, including half-hour DST rollbacks.
func kbSentRepeatedMinute(at time.Time, zone *time.Location) bool {
	local := at.In(zone)
	start, end := local.ZoneBounds()
	_, offset := local.Zone()
	for _, edge := range []time.Time{start, end} {
		if edge.IsZero() {
			continue
		}
		for _, near := range []time.Time{edge.Add(-time.Second), edge} {
			_, other := near.In(zone).Zone()
			if other == offset {
				continue
			}
			shifted := at.Add(time.Duration(offset-other) * time.Second)
			if shifted.In(zone).Format("2006-01-02 15:04") == local.Format("2006-01-02 15:04") {
				return true
			}
		}
	}
	return false
}

// These providers issue globally unique message IDs (GitHub's resource kind is
// included by kbSentRef). Other connectors must also establish the destination.
func kbSentExactTarget(chat *kb.Chat, row db.SentAsUser) bool {
	connector := chat.Connector
	if connector == "" {
		connector = chat.Source
	}
	if connector != row.Connector {
		return false
	}
	switch connector {
	case "lark", "github":
		return chat.Source == connector || kbSentTarget(chat, row)
	default:
		return kbSentTarget(chat, row)
	}
}
