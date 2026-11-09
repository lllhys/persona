package extmgr

import (
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Log keeps a line (host.Observer), and hands it to whoever follows the log.
func (s *extensionStats) Log(source, line string) {
	if s == nil {
		return
	}
	text := s.redactLine(cut(line, logLineMost)) // before the lock: Call, Run, Count and the readers wait on it
	s.mu.Lock()
	defer s.mu.Unlock()
	l := LogLine{Seq: s.next, At: s.now(), Source: source, Text: text}
	s.next++
	if len(s.logs) < logLinesKept {
		s.logs = append(s.logs, l)
	} else {
		s.logs[l.Seq%logLinesKept] = l
	}
	for ch := range s.subs {
		select {
		case ch <- l:
		default: // a follower that does not keep up misses lines rather than hold the extension up
		}
	}
}

// redactLine is line without the hub's own credentials and what looks like one. It needs no lock: s.redact is set when
// s is made, and what it reads is set before the extensions start.
func (s *extensionStats) redactLine(line string) string {
	if s.redact != nil {
		line = s.redact(line)
	}
	return redactSecrets(line)
}

func cut(s string, most int) string {
	if len(s) <= most {
		return s
	}
	i := most
	for i > 0 && !utf8RuneStart(s[i]) {
		i--
	}
	return s[:i] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Lines are the kept lines after seq since (all when since < 0), oldest first, those with q in them (case
// insensitive), at most limit of the latest.
func (s *extensionStats) Lines(since int64, q string, limit int) (lines []LogLine, next int64) {
	if s == nil {
		return []LogLine{}, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(q)
	lines = []LogLine{}
	first := s.next - int64(len(s.logs))
	for seq := max(first, since+1); seq < s.next; seq++ {
		l := s.logs[seq%logLinesKept] // while not full, a line's seq is its index
		if q != "" && !strings.Contains(strings.ToLower(l.Text), q) && !strings.Contains(l.Source, q) {
			continue
		}
		lines = append(lines, l)
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, s.next
}

// follow hands each new line to ch until stop.
func (s *extensionStats) follow(ch chan LogLine) (stop func()) {
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[chan LogLine]struct{}{}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// The words secretPatterns need, a bit each: every match of a pattern holds one of its words, in any case.
const (
	wordAuthorization uint8 = 1 << iota
	wordCookie
	wordBearer
	wordToken
	wordSecret
	wordPassw
	wordKey
	wordSig

	allSecretWords uint8 = 1<<iota - 1
)

// secretPatterns find what looks like a credential in a line: it is replaced before the line is kept. words is what a
// line needs for the pattern to match it at all (redactSecrets skips the pattern when the line has none of them).
var secretPatterns = []struct {
	re    *lazyre.Regexp
	words uint8
}{
	{lazyre.New(`(?i)((?:authorization|cookie)["']?\s*[:=]\s*["']?)(?:(?:bearer|basic|token)\s+)?[^\s"',;&]{6,}`), wordAuthorization | wordCookie},
	{lazyre.New(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`), wordBearer},
	{lazyre.New(`(?i)((?:token|secret|password|passwd|api[_-]?key|access[_-]?key)["']?\s*[:=]\s*["']?)[^\s"',;&]{6,}`), wordToken | wordSecret | wordPassw | wordKey},
	{lazyre.New(`(?i)([?&](?:token|key|secret|access_token|sig)=)[^\s&"']+`), wordToken | wordKey | wordSecret | wordSig},
}

// redactSecrets is line with what looks like a credential in it replaced by "***". Most lines hold none of the words
// the patterns need and are given back as they are, without running the patterns (about 0.3 µs a byte between them).
// The words are looked for once, in line as it comes: a replacement only takes text out and puts "***" in, which
// makes no word, so a pattern whose words line lacks cannot match what the patterns before it leave either.
func redactSecrets(line string) string {
	words := secretWordsIn(line)
	if words == 0 {
		return line
	}
	for _, p := range secretPatterns {
		if words&p.words != 0 {
			line = p.re.ReplaceAllString(line, "${1}***")
		}
	}
	return line
}

// secretWordsIn is the words of secretPatterns in line, in any case, as (?i) matches them; it copies nothing. (?i) folds
// Unicode simply, and the only other runes that fold into ASCII letters are U+017F (s) and U+212A (k): a line holding
// either has every word, so that every pattern looks at it.
func secretWordsIn(line string) (words uint8) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c >= utf8.RuneSelf {
			if (c == 0xC5 || c == 0xE2) && (strings.HasPrefix(line[i:], "\u017f") || strings.HasPrefix(line[i:], "\u212a")) {
				return allSecretWords
			}
			continue
		}
		// c|0x20 is a lower-case letter only when c is that letter in either case.
		switch c | 0x20 {
		case 'a':
			words |= wordIf(line[i:], "authorization", wordAuthorization)
		case 'b':
			words |= wordIf(line[i:], "bearer", wordBearer)
		case 'c':
			words |= wordIf(line[i:], "cookie", wordCookie)
		case 'k':
			words |= wordIf(line[i:], "key", wordKey)
		case 'p':
			words |= wordIf(line[i:], "passw", wordPassw)
		case 's':
			words |= wordIf(line[i:], "secret", wordSecret) | wordIf(line[i:], "sig", wordSig)
		case 't':
			words |= wordIf(line[i:], "token", wordToken)
		}
	}
	return words
}

// wordIf is bit when s starts with word (lower-case ASCII letters) in any case, else 0.
func wordIf(s, word string, bit uint8) uint8 {
	if len(s) < len(word) {
		return 0
	}
	for j := 0; j < len(word); j++ {
		if s[j]|0x20 != word[j] {
			return 0
		}
	}
	return bit
}

// hostLogf is the log the host writes to: the hub's only (journal), the extension's own through Observe.
func (ps *extensions) hostLogf() func(string, ...any) {
	if ps.journal != nil {
		return ps.journal
	}
	return ps.logf
}

// redactOwnSecrets is line without the hub's own token in it.
func (h *Manager) redactOwnSecrets(line string) string {
	if h.cfg != nil && len(h.cfg.Token) >= 8 {
		line = strings.ReplaceAll(line, h.cfg.Token, "***")
	}
	return line
}
