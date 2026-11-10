package mdns

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// A small DNS wire codec. golang.org/x/net/dns/dnsmessage is not used because it rejects labels that contain a dot,
// which DNS-SD instance names may ("Hub v1.2"): names here are kept as raw labels end to end.

const (
	typeA    uint16 = 1
	typePTR  uint16 = 12
	typeTXT  uint16 = 16
	typeAAAA uint16 = 28
	typeSRV  uint16 = 33
	typeANY  uint16 = 255

	classIN   uint16 = 1
	classANY  uint16 = 255
	classMask uint16 = 0x7fff
	// topBit is the cache-flush bit on a record's class and the unicast-response (QU) bit on a question's class.
	topBit uint16 = 0x8000

	flagResponse uint16 = 0x8000
	flagAA       uint16 = 0x0400
	opcodeMask   uint16 = 0x7800

	maxPacket   = 9000 // RFC 6762 §17
	maxLabel    = 63
	maxNameWire = 255
)

var (
	errMalformed = errors.New("mdns: malformed message")
	errTooBig    = errors.New("mdns: message too big")
	// errNameBudget ends the parse of a message whose names read more labels and follow more compression pointers in
	// total than the message has bytes (readName); it is errMalformed too.
	errNameBudget = fmt.Errorf("%w: names read too many labels and compression pointers", errMalformed)
)

// name is a domain name as raw labels, most specific first, without the root. A label may hold any bytes,
// including dots and spaces; nothing is escaped.
type name []string

func (n name) equal(m name) bool {
	if len(n) != len(m) {
		return false
	}
	for i := range n {
		if !asciiEqualFold(n[i], m[i]) {
			return false
		}
	}
	return true
}

func (n name) wireLen() int {
	l := 1
	for _, s := range n {
		l += 1 + len(s)
	}
	return l
}

func (n name) validate() error {
	for _, s := range n {
		if s == "" || len(s) > maxLabel {
			return errors.New("mdns: bad label length in " + strconv.Quote(n.String()))
		}
	}
	if n.wireLen() > maxNameWire {
		return errors.New("mdns: name too long: " + strconv.Quote(n.String()))
	}
	return nil
}

// String renders the name in presentation format with a trailing dot: "." and "\" in a label are backslash
// escaped and control bytes, space and DEL become \DDD (a space is \032). Bytes >= 0x80 (UTF-8) are kept.
func (n name) String() string {
	if len(n) == 0 {
		return "."
	}
	var b strings.Builder
	for _, s := range n {
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '.' || c == '\\':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c <= ' ' || c == 0x7f:
				b.WriteByte('\\')
				b.WriteByte('0' + c/100)
				b.WriteByte('0' + c/10%10)
				b.WriteByte('0' + c%10)
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('.')
	}
	return b.String()
}

// asciiLower lowercases ASCII letters only, as DNS name comparison does (RFC 6762 §16).
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

type question struct {
	name  name
	typ   uint16
	class uint16 // with the QU bit
}

// record is one resource record; which rdata fields matter depends on typ.
type record struct {
	name  name
	typ   uint16
	class uint16 // with the cache-flush bit
	ttl   uint32

	target name       // PTR, SRV
	port   uint16     // SRV
	txt    []string   // TXT
	addr   netip.Addr // A, AAAA
}

type message struct {
	id          uint16
	flags       uint16
	questions   []question
	answers     []record
	authorities []record
	additionals []record
}

func (m *message) isResponse() bool { return m.flags&flagResponse != 0 }

// builder packs a message, compressing repeated name suffixes.
type builder struct {
	buf  []byte
	comp map[string]int // exact wire form of a suffix -> its offset
}

func (b *builder) u16(v uint16) { b.buf = binary.BigEndian.AppendUint16(b.buf, v) }
func (b *builder) u32(v uint32) { b.buf = binary.BigEndian.AppendUint32(b.buf, v) }

func (b *builder) name(n name) error {
	if err := n.validate(); err != nil {
		return err
	}
	for i := range n {
		key := suffixKey(n[i:])
		if off, ok := b.comp[key]; ok {
			b.u16(0xC000 | uint16(off))
			return nil
		}
		if len(b.buf) <= 0x3fff {
			b.comp[key] = len(b.buf)
		}
		b.buf = append(b.buf, byte(len(n[i])))
		b.buf = append(b.buf, n[i]...)
	}
	b.buf = append(b.buf, 0)
	return nil
}

// suffixKey is the uncompressed wire form of n, which identifies it exactly (case included).
func suffixKey(n name) string {
	var sb strings.Builder
	for _, s := range n {
		sb.WriteByte(byte(len(s)))
		sb.WriteString(s)
	}
	return sb.String()
}

func (b *builder) record(r *record) error {
	if err := b.name(r.name); err != nil {
		return err
	}
	b.u16(r.typ)
	b.u16(r.class)
	b.u32(r.ttl)
	at := len(b.buf)
	b.u16(0) // rdlength, patched below
	switch r.typ {
	case typeA:
		a := r.addr.Unmap()
		if !a.Is4() {
			return errors.New("mdns: A record needs an IPv4 address")
		}
		v := a.As4()
		b.buf = append(b.buf, v[:]...)
	case typeAAAA:
		if !r.addr.Is6() || r.addr.Is4In6() {
			return errors.New("mdns: AAAA record needs an IPv6 address")
		}
		v := r.addr.As16()
		b.buf = append(b.buf, v[:]...)
	case typePTR:
		if err := b.name(r.target); err != nil {
			return err
		}
	case typeSRV:
		b.u16(0) // priority
		b.u16(0) // weight
		b.u16(r.port)
		if err := b.name(r.target); err != nil {
			return err
		}
	case typeTXT:
		if len(r.txt) == 0 {
			b.buf = append(b.buf, 0) // RFC 6763 §6.1: an empty TXT record holds one empty string
		}
		for _, s := range r.txt {
			if len(s) > 255 {
				return errors.New("mdns: TXT string longer than 255 bytes")
			}
			b.buf = append(b.buf, byte(len(s)))
			b.buf = append(b.buf, s...)
		}
	default:
		return errors.New("mdns: cannot pack record type " + strconv.Itoa(int(r.typ)))
	}
	rdlen := len(b.buf) - at - 2
	if rdlen > 0xffff {
		return errTooBig
	}
	binary.BigEndian.PutUint16(b.buf[at:], uint16(rdlen))
	return nil
}

func packMessage(m *message) ([]byte, error) {
	b := builder{buf: make([]byte, 0, 512), comp: map[string]int{}}
	b.u16(m.id)
	b.u16(m.flags)
	for _, n := range []int{len(m.questions), len(m.answers), len(m.authorities), len(m.additionals)} {
		if n > 0xffff {
			return nil, errTooBig
		}
		b.u16(uint16(n))
	}
	for _, q := range m.questions {
		if err := b.name(q.name); err != nil {
			return nil, err
		}
		b.u16(q.typ)
		b.u16(q.class)
	}
	for _, sec := range [][]record{m.answers, m.authorities, m.additionals} {
		for i := range sec {
			if err := b.record(&sec[i]); err != nil {
				return nil, err
			}
		}
	}
	if len(b.buf) > maxPacket {
		return nil, errTooBig
	}
	return b.buf, nil
}

// readName decodes the name at off and returns it with the offset just past it. Compression pointers must point
// strictly backwards from where the current run of labels started, which rules out loops.
//
// budget is shared by all the names of one message: each label read and each pointer followed takes one, and going
// past it is errNameBudget. Backwards-only pointers alone still let every name walk a chain through all the names
// before it, or let every 2-byte pointer read a long name again, so a crafted packet would cost work far beyond its
// size; a budget for the whole message keeps it linear (each label is at most maxLabel bytes), while a deep name read
// once still fits, since each of its labels and pointers takes at least two bytes of the message. A message with a
// record at every level of a deep name (an ip6.arpa name compressed label by label) reads the levels again for each
// record and runs out (TestNameBudgetLabelByLabel).
func readName(msg []byte, off int, budget *int) (name, int, error) {
	var n name
	next := -1
	limit := off
	pos := off
	total := 1
	for {
		if pos >= len(msg) {
			return nil, 0, errMalformed
		}
		c := msg[pos]
		switch c & 0xC0 {
		case 0x00:
			if c == 0 {
				pos++
				if next < 0 {
					next = pos
				}
				return n, next, nil
			}
			l := int(c)
			if pos+1+l > len(msg) {
				return nil, 0, errMalformed
			}
			total += 1 + l
			if total > maxNameWire {
				return nil, 0, errMalformed
			}
			if *budget--; *budget < 0 {
				return nil, 0, errNameBudget
			}
			n = append(n, string(msg[pos+1:pos+1+l]))
			pos += 1 + l
		case 0xC0:
			if pos+1 >= len(msg) {
				return nil, 0, errMalformed
			}
			ptr := int(c&0x3f)<<8 | int(msg[pos+1])
			if next < 0 {
				next = pos + 2
			}
			if ptr >= limit {
				return nil, 0, errMalformed
			}
			if *budget--; *budget < 0 {
				return nil, 0, errNameBudget
			}
			limit = ptr
			pos = ptr
		default: // 0x40 and 0x80 are reserved label types
			return nil, 0, errMalformed
		}
	}
}

// readRecord decodes the record at off. ok is false for records this package does not use (other types and
// classes, or malformed rdata); they are skipped, the rest of the message still parses, unless a name in the rdata
// used up the message's name budget (readName).
func readRecord(msg []byte, off int, budget *int) (r record, ok bool, next int, err error) {
	n, off, err := readName(msg, off, budget)
	if err != nil {
		return record{}, false, 0, err
	}
	if off+10 > len(msg) {
		return record{}, false, 0, errMalformed
	}
	r = record{
		name:  n,
		typ:   binary.BigEndian.Uint16(msg[off:]),
		class: binary.BigEndian.Uint16(msg[off+2:]),
		ttl:   binary.BigEndian.Uint32(msg[off+4:]),
	}
	rd := off + 10
	end := rd + int(binary.BigEndian.Uint16(msg[off+8:]))
	if end > len(msg) {
		return record{}, false, 0, errMalformed
	}
	if r.class&classMask != classIN {
		return r, false, end, nil
	}
	switch r.typ {
	case typeA:
		if end-rd != 4 {
			return r, false, end, nil
		}
		r.addr = netip.AddrFrom4([4]byte(msg[rd:end]))
	case typeAAAA:
		if end-rd != 16 {
			return r, false, end, nil
		}
		r.addr = netip.AddrFrom16([16]byte(msg[rd:end]))
	case typePTR:
		t, e, err := readName(msg[:end], rd, budget)
		if errors.Is(err, errNameBudget) {
			return record{}, false, 0, err
		}
		if err != nil || e != end {
			return r, false, end, nil
		}
		r.target = t
	case typeSRV:
		if end-rd < 7 {
			return r, false, end, nil
		}
		r.port = binary.BigEndian.Uint16(msg[rd+4:])
		t, e, err := readName(msg[:end], rd+6, budget)
		if errors.Is(err, errNameBudget) {
			return record{}, false, 0, err
		}
		if err != nil || e != end {
			return r, false, end, nil
		}
		r.target = t
	case typeTXT:
		for p := rd; p < end; {
			l := int(msg[p])
			p++
			if p+l > end {
				return r, false, end, nil
			}
			r.txt = append(r.txt, string(msg[p:p+l]))
			p += l
		}
	default:
		return r, false, end, nil
	}
	return r, true, end, nil
}

// unpackMessage decodes a whole message. Counts in the header are not trusted for allocation: records are
// appended as they parse, and running out of bytes is an error. Its names may read at most len(msg) labels and
// compression pointers in total (readName).
func unpackMessage(msg []byte) (*message, error) {
	if len(msg) < 12 || len(msg) > maxPacket {
		return nil, errMalformed
	}
	m := &message{
		id:    binary.BigEndian.Uint16(msg[0:]),
		flags: binary.BigEndian.Uint16(msg[2:]),
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	counts := [3]int{
		int(binary.BigEndian.Uint16(msg[6:])),
		int(binary.BigEndian.Uint16(msg[8:])),
		int(binary.BigEndian.Uint16(msg[10:])),
	}
	off := 12
	budget := len(msg)
	for range qd {
		n, next, err := readName(msg, off, &budget)
		if err != nil {
			return nil, err
		}
		if next+4 > len(msg) {
			return nil, errMalformed
		}
		m.questions = append(m.questions, question{
			name:  n,
			typ:   binary.BigEndian.Uint16(msg[next:]),
			class: binary.BigEndian.Uint16(msg[next+2:]),
		})
		off = next + 4
	}
	secs := [3]*[]record{&m.answers, &m.authorities, &m.additionals}
	for i, sec := range secs {
		for range counts[i] {
			r, ok, next, err := readRecord(msg, off, &budget)
			if err != nil {
				return nil, err
			}
			if ok {
				*sec = append(*sec, r)
			}
			off = next
		}
	}
	return m, nil
}
