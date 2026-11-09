package proto

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"math/bits"
	"sync"
)

// A machine's session list travels whole on every change: the node sends it to its manager (sessions.changed), the
// manager sends it on to the hub (fleet.sessions), and a busy machine lists 150 sessions of about 1 KB of which one
// changed. Decoding all of them again took about 1.3 ms and 900 KB each time, on each hop. ScanSessionList reads the
// list's elements as bytes, and a SessionListDecoder decodes only those whose bytes it has not seen in the last list:
// the others are the sessions it decoded then, copied.

// SessionListDecoder decodes the successive session lists of one machine. Decoding is a function of the bytes, so a
// session whose bytes were in the last list is that list's session again: each is kept as decoded, apart from what the
// caller was handed, and handed out as a copy (a struct copy: its slices and pointers are shared, and never changed in
// place by the callers, as for any kept list). The zero value is ready; safe for concurrent use.
type SessionListDecoder struct {
	mu   sync.Mutex
	gen  uint64
	memo map[string]*listedSession // the element's bytes → their session, for the elements of the last list
}

// listedSession is one element of the last list: its bytes (the memo's key) and their decoding.
type listedSession struct {
	raw string
	s   Session
	gen uint64 // the last Decode that listed it
}

// Decode decodes rows, the elements of a "sessions" array as ScanSessionList read them, into the list json.Unmarshal
// gives for that array (an empty list for no rows), with the same options and the same failures: an element that does
// not decode fails the whole list. raws[i] are the bytes list[i] was decoded from, the same string for the same
// bytes as long as they stay in the lists, so a caller can keep what it makes of a session and tell by them that the
// session is the same.
func (d *SessionListDecoder) Decode(rows []jsontext.Value) (list []Session, raws []string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.memo == nil {
		d.memo = make(map[string]*listedSession, len(rows))
	}
	d.gen++
	list, raws = make([]Session, len(rows)), make([]string, len(rows))
	for i, row := range rows {
		if e := d.memo[string(row)]; e != nil {
			list[i], raws[i], e.gen = e.s, e.raw, d.gen
			continue
		}
		// As the list's own decoder decodes each element: Session.UnmarshalJSONFrom with encoding/json's options.
		if err = jsonv2.Unmarshal(row, &list[i], json.DefaultOptionsV1()); err != nil {
			list, raws = nil, nil
			break
		}
		e := &listedSession{raw: string(row), s: list[i], gen: d.gen}
		d.memo[e.raw], raws[i] = e, e.raw
	}
	for k, e := range d.memo {
		if e.gen != d.gen {
			delete(d.memo, k) // not in this list: only the last list's sessions are kept, a failed one's too
		}
	}
	return list, raws, err
}

// DecodeChangedSessions turns ScanSessionList's and SessionListDecoder's use on (the hub's fleet.sessions, the
// manager's sessions.changed): false decodes and encodes every list whole, as before them.
const DecodeChangedSessions = true

// maxListMembers bounds the members ScanSessionList reads besides the list: events carry two or three.
const maxListMembers = 16

// emptyObject is the rest of an object that has only its list.
var emptyObject = []byte("{}")

// ScanSessionList reads data, an event's params, for the elements of its "sessions" array, each a slice of data, and
// the object's other members, in order, as an object of their own (rest). ok is false unless decoding rest with
// json.Unmarshal and the elements as a []Session gives what json.Unmarshal gives for data: data is one valid object;
// "sessions", written without escapes, is one of its members and an array; no member name is given twice, written
// with escapes, or read as "sessions" by json.Unmarshal's case-insensitive matching; and it has at most
// maxListMembers other members. When ok is false, decode data with json.Unmarshal: it then decodes it as it always
// has, a repeated or differently cased "sessions" included. data is read once, byte by byte (scanValue).
func ScanSessionList(data []byte) (rest []byte, rows []jsontext.Value, ok bool) {
	i := skipSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return nil, nil, false
	}
	var names [maxListMembers + 1][]byte
	n, listed := 0, false
	if i = skipSpace(data, i+1); i < len(data) && data[i] == '}' {
		i++
	} else {
		for {
			at := i
			if i = scanString(data, i); i < 0 || n == len(names) {
				return nil, nil, false
			}
			name := data[at:i]
			if bytes.IndexByte(name, '\\') >= 0 {
				return nil, nil, false
			}
			bare := name[1 : len(name)-1]
			for _, seen := range names[:n] {
				if bytes.Equal(seen[1:len(seen)-1], bare) {
					return nil, nil, false
				}
			}
			names[n] = name
			n++
			if i = skipSpace(data, i); i >= len(data) || data[i] != ':' {
				return nil, nil, false
			}
			i = skipSpace(data, i+1)
			if string(bare) == "sessions" {
				if rows, i = scanRows(data, i); i < 0 {
					return nil, nil, false
				}
				listed = true
			} else {
				if foldsToSessions(bare) {
					return nil, nil, false
				}
				at = i
				if i = scanValue(data, i, 1); i < 0 {
					return nil, nil, false
				}
				val := data[at:i]
				if rest == nil {
					rest = append(make([]byte, 0, len(name)+len(val)+16), '{')
				} else {
					rest = append(rest, ',')
				}
				rest = append(append(append(rest, name...), ':'), val...)
			}
			if i = skipSpace(data, i); i < len(data) && data[i] == '}' {
				i++
				break
			}
			if i >= len(data) || data[i] != ',' {
				return nil, nil, false
			}
			i = skipSpace(data, i+1)
		}
	}
	if skipSpace(data, i) != len(data) || !listed { // nothing after the object but space
		return nil, nil, false
	}
	if rest == nil {
		return emptyObject, rows, true
	}
	return append(rest, '}'), rows, true
}

// scanRows reads the "sessions" member's value at data[i:]: an array, its elements as slices of data, and where it
// ends (-1: it is not one). A list is never empty for json.Unmarshal (an empty array is an empty list), so neither are
// rows.
func scanRows(data []byte, i int) ([]jsontext.Value, int) {
	if i >= len(data) || data[i] != '[' {
		return nil, -1 // null or another type: json.Unmarshal says what that is
	}
	rows := make([]jsontext.Value, 0, min(len(data)/512, 4096)+8) // sessions are rarely under 512 bytes
	if i = skipSpace(data, i+1); i < len(data) && data[i] == ']' {
		return rows, i + 1
	}
	for {
		at := i
		if i = scanValue(data, i, 2); i < 0 {
			return nil, -1
		}
		rows = append(rows, jsontext.Value(data[at:i:i]))
		if i = skipSpace(data, i); i < len(data) && data[i] == ']' {
			return rows, i + 1
		}
		if i >= len(data) || data[i] != ',' {
			return nil, -1
		}
		i = skipSpace(data, i+1)
	}
}

// The scan reads JSON as json.Unmarshal reads it, with encoding/json's options (json.DefaultOptionsV1): RFC 8259's
// grammar, any byte from 0x80 up in a string (invalid UTF-8 is let through), a name given twice in an object (not
// checked, apart from ScanSessionList's own), and at most maxNestingDepth arrays and objects one in another. It takes
// what jsontext's decoder takes, in less time: about 1 KB per session, every session, on every change of a machine's
// list, most of it the text of strings (scanString). Each function returns where what it read ends, -1 when it is
// not there.

// maxNestingDepth is the most arrays and objects jsontext reads one in another (its own maxNestingDepth).
const maxNestingDepth = 10000

// scanValue reads the value at b[i:] (no space before it), inside depth arrays and objects.
func scanValue(b []byte, i, depth int) int {
	var buf [64]byte
	open := buf[:0] // the arrays ('[') and objects ('{') open in it, the innermost last
	for {
		if i >= len(b) {
			return -1
		}
		switch c := b[i]; c {
		case '"':
			i = scanString(b, i)
		case '{', '[':
			if depth+len(open) >= maxNestingDepth {
				return -1
			}
			if i = skipSpace(b, i+1); i < len(b) && b[i] == c+2 { // '{'+2 is '}', '['+2 is ']'
				i++
				break
			}
			open = append(open, c)
			if c == '{' {
				if i = scanName(b, i); i < 0 {
					return -1
				}
			}
			continue // to its first value
		case 't':
			i = scanLiteral(b, i, "true")
		case 'f':
			i = scanLiteral(b, i, "false")
		case 'n':
			i = scanLiteral(b, i, "null")
		default:
			i = scanNumber(b, i)
		}
		// After a value: the ends of what it closes, to the next value (after a comma), or the end of the outermost.
		for {
			if i < 0 {
				return -1
			}
			if len(open) == 0 {
				return i
			}
			if i = skipSpace(b, i); i >= len(b) {
				return -1
			}
			c := open[len(open)-1]
			if b[i] == c+2 {
				i++
				open = open[:len(open)-1]
				continue
			}
			if b[i] != ',' {
				return -1
			}
			if i = skipSpace(b, i+1); c == '{' {
				i = scanName(b, i)
			}
			if i < 0 {
				return -1
			}
			break
		}
	}
}

// scanName reads an object member's name and colon, and the space after them, to its value.
func scanName(b []byte, i int) int {
	if i = scanString(b, i); i < 0 {
		return -1
	}
	if i = skipSpace(b, i); i >= len(b) || b[i] != ':' {
		return -1
	}
	return skipSpace(b, i+1)
}

// skipSpace skips JSON's whitespace.
func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	return i
}

// scanLiteral reads lit.
func scanLiteral(b []byte, i int, lit string) int {
	if len(b)-i < len(lit) || string(b[i:i+len(lit)]) != lit {
		return -1
	}
	return i + len(lit)
}

// scanNumber reads a number: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?. What follows it is the caller's to
// read: a digit after a leading 0 is not a comma, and fails there, as in jsontext.
func scanNumber(b []byte, i int) int {
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i < len(b) && b[i] == '0' {
		i++
	} else if i = scanDigits(b, i); i < 0 {
		return -1
	}
	if i < len(b) && b[i] == '.' {
		if i = scanDigits(b, i+1); i < 0 {
			return -1
		}
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		if i++; i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		return scanDigits(b, i)
	}
	return i
}

// scanDigits reads one digit or more.
func scanDigits(b []byte, i int) int {
	at := i
	for i < len(b) && '0' <= b[i] && b[i] <= '9' {
		i++
	}
	if i == at {
		return -1
	}
	return i
}

// scanString reads a string as jsontext reads it with invalid UTF-8 allowed: between quotes, any byte but a control
// character, '"' and '\\', and the escapes \" \\ \/ \b \f \n \r \t and \u with four hex digits (a lone surrogate
// included). Most of a session's bytes are the text of its strings: they are passed over eight at a time, to the
// first that is a quote, a backslash or a control character (specials).
func scanString(b []byte, i int) int {
	if i >= len(b) || b[i] != '"' {
		return -1
	}
	for i++; ; {
		for i+8 <= len(b) {
			if m := specials(binary.LittleEndian.Uint64(b[i : i+8])); m != 0 {
				i += bits.TrailingZeros64(m) / 8
				break
			}
			i += 8
		}
		if i >= len(b) {
			return -1
		}
		switch c := b[i]; {
		case c == '"':
			return i + 1
		case c == '\\':
			if i+1 >= len(b) {
				return -1
			}
			switch b[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
			case 'u':
				if len(b)-i < 6 || !isHex(b[i+2]) || !isHex(b[i+3]) || !isHex(b[i+4]) || !isHex(b[i+5]) {
					return -1
				}
				i += 6
			default:
				return -1
			}
		case c < ' ':
			return -1
		default:
			i++
		}
	}
}

const (
	lowBits  = 0x0101010101010101
	highBits = 0x8080808080808080
)

// specials has the high bit set of each byte of w (eight bytes of a string, the first in the low byte) that is '"',
// '\\' or under ' ', and maybe of bytes after the first of those, never of one before it: a byte borrows from the
// next only when it is one of them itself. Bytes from 0x80 up are never set but by a borrow.
func specials(w uint64) uint64 {
	q := w ^ ('"' * lowBits)
	e := w ^ ('\\' * lowBits)
	return ((q-lowBits)&^q | (e-lowBits)&^e | (w-' '*lowBits)&^w) & highBits
}

// isHex: c is a hex digit, in either case.
func isHex(c byte) bool { return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }

// foldsToSessions: json.Unmarshal could read a member of this name (other than "sessions" itself) into a field named
// "sessions": the same letters in another case, Unicode folding included, with or without dashes and underscores.
func foldsToSessions(name []byte) bool {
	var buf [64]byte
	kept := buf[:0]
	for _, c := range name {
		if c != '-' && c != '_' {
			if len(kept) == len(buf) {
				return false // far longer than "sessions" in any folding
			}
			kept = append(kept, c)
		}
	}
	return bytes.EqualFold(kept, []byte("sessions"))
}

// AppendSession appends to b the bytes json.Marshal gives for s (appendSession): what the hub writes into a list's one
// buffer itself. False when s holds a value it leaves to encoding/json (a time outside the years 0-9999, a nested
// value that does not encode, Session's fields changed since session_wire.go was written): encode s with json then.
func AppendSession(b []byte, s *Session) ([]byte, bool) { return appendSession(b, s) }
