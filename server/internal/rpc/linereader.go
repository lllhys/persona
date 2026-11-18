package rpc

import (
	"bufio"
	"bytes"
	"io"
	"sync"
)

// lineStart is the buffer a lineReader starts with and goes back to, and all an idle connection keeps of it. Most
// lines are far shorter (answers, events); a longer one grows through grownBufs. Serve's bufio.Scanner started with
// 64 KiB, which every computer's connection kept.
const lineStart = 16 << 10

// pingFrameMiddleBytes bounds what counts as the middle of a frame for ping activity: an unfinished line below this
// size cannot be holding a pong behind it, however many raw reads its transport split it into, so it never clears a
// ping miss. It matches the reader's first buffer, so a line that outgrew it is by definition a large frame.
const pingFrameMiddleBytes = lineStart

// lineReader reads Serve's lines exactly as a bufio.Scanner with ScanLines did: the same limit, the same lines (the
// newline and one carriage return before it dropped, an unterminated last line given, empty lines kept) and the same
// errors (bufio.ErrTooLong, bufio.ErrBadReadCount, io.ErrNoProgress, the reader's own; nil at io.EOF). Only its memory
// differs: a Scanner kept the buffer its longest line grew for the connection's life (256 KiB per computer sending its
// session list, 32 MiB after one 21 MB request body), while a lineReader goes back to its first buffer once a long
// line is through, and hands the grown one to grownBufs for the next long line on any connection.
// FuzzLineReaderIsAScanner compares the two.
//
// A line that fills a buffer of linePiece is read on in pieces and put together once, in a buffer of its size, where a
// Scanner doubles its buffer and copies the line into each one (64 MiB of buffers for a 30 MB line, #3199). That
// buffer is the line's own (Owned): the reader does not write to it again.
type lineReader struct {
	r          io.Reader
	max        int
	piece      int      // linePiece; smaller in tests
	first      []byte   // the first buffer, kept
	buf        []byte   // first, a grown one while a long line needs it, or the piece being read
	grown      bool     // buf is not first
	start, end int      // the unread part of buf
	seen       int      // how much of the unread part has no newline: a line read in several reads is searched once
	parts      [][]byte // the full pieces of the line being read that came before buf
	held       int      // their bytes
	spent      bool     // buf was the last piece of a line of many: it goes to the GC, not back to grownBufs
	err        error
	token      []byte
	owned      bool
	// middle, when set, is told of each read that went on with a line begun before it, did not finish it and has
	// already grown past pingFrameMiddleBytes: the middle of a frame, which Serve counts as ping activity while the
	// frame is still arriving (PingActivity). Small lines split by the transport never reach it.
	middle func()
}

// linePiece is the buffer from which a line is read in pieces, and the size of a piece: the largest buffer grownBufs
// keeps. Up to it a line grows as before, by doubling through those pooled buffers (lines that long come often, and a
// copy of a few hundred KiB costs less than putting pieces together); past it each doubling would copy megabytes into
// a buffer the GC has to take back. A line of two pieces (a file's 1 MiB chunk in base64 is 1.4 MiB) takes both from
// grownBufs and gives both back, so the next one allocates only its line.
const linePiece = lineStart << 6

func newLineReader(r io.Reader, maximum int) *lineReader {
	return newLineReaderSize(r, min(lineStart, maximum), maximum)
}

func newLineReaderSize(r io.Reader, size, maximum int) *lineReader {
	b := make([]byte, size)
	return &lineReader{r: r, max: maximum, piece: linePiece, first: b, buf: b}
}

// Bytes is the line Scan found, valid until the next Scan unless it is Owned.
func (s *lineReader) Bytes() []byte { return s.token }

// Owned: the line Scan found is in a buffer of its own (it was read in pieces), which the reader does not write to
// again or keep: the caller may keep it, or parts of it, past the next Scan.
func (s *lineReader) Owned() bool { return s.owned }

// buffered is the input already read after the current token, valid until the next Scan. Ping activity uses it to
// find replies among the later lines, and an unfinished line that names a result, before a deadline reply wakes its
// caller (bufferedActivity).
func (s *lineReader) buffered() []byte { return s.buf[s.start:s.end] }

// Err is what ended the input, nil for io.EOF.
func (s *lineReader) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *lineReader) setErr(err error) {
	if s.err == nil || s.err == io.EOF {
		s.err = err
	}
}

// Scan finds the next line as bufio.Scanner.Scan with ScanLines does, step for step but for the buffers a long line
// is read into.
func (s *lineReader) Scan() bool {
	// The last line goes now, as a Scanner's does when the next split finds none: kept while Scan waits for input, it
	// would keep the buffer a long line grew (Bytes is valid only until the next Scan).
	s.token, s.owned = nil, false
	for {
		if s.end > s.start || s.err != nil {
			data := s.buf[s.start:s.end]
			if i := bytes.IndexByte(data[s.seen:], '\n'); i >= 0 {
				s.token = s.line(data[:s.seen+i])
				s.start += s.seen + i + 1
				s.seen = 0
				return true
			}
			// The bytes not searched yet came in the latest read, those searched or held before it in earlier ones.
			// Only a frame already bigger than pingFrameMiddleBytes can hold a pong behind it: transport framing splits
			// small lines across reads too (streaming decompression, lost segments), and those splits must not keep a
			// dead link alive (#8771).
			if s.middle != nil && len(data) > s.seen && (s.seen > 0 || s.held > 0) && s.held+len(data) > pingFrameMiddleBytes {
				s.middle()
			}
			s.seen = len(data)
			if s.err != nil && (len(data) > 0 || s.held > 0) {
				s.token = s.line(data)
				s.start, s.seen = s.end, 0
				return true
			}
		}
		if s.err != nil {
			s.token, s.start, s.end, s.seen = nil, 0, 0, 0
			s.dropParts()
			s.shrink()
			return false
		}
		// A long line is through once what is left fits the first buffer again.
		if s.grown && s.held == 0 && s.end-s.start < len(s.first) {
			s.shrink()
		}
		if s.start > 0 && (s.end == len(s.buf) || s.start > len(s.buf)/2) {
			copy(s.buf, s.buf[s.start:s.end])
			s.end -= s.start
			s.start = 0
		}
		if s.end == len(s.buf) { // full of one line's start (s.start is 0): the line needs more room
			const maxInt = int(^uint(0) >> 1)
			// The line so far is what a Scanner's one buffer would hold: too long at the same length.
			if size := s.held + len(s.buf); size >= s.max || size > maxInt/2 {
				s.token = nil
				s.dropParts()
				s.setErr(bufio.ErrTooLong)
				return false
			}
			if s.held > 0 || (s.grown && len(s.buf) >= s.piece) {
				// In pieces from here: this buffer is kept as it is and the line goes on in the next, never past the
				// limit. Nothing is copied until the line is whole (line).
				s.parts = append(s.parts, s.buf)
				s.held += len(s.buf)
				s.buf = takeGrown(min(s.piece, s.max-s.held))
				s.start, s.end, s.seen = 0, 0, 0
			} else {
				// Out of the first buffer straight to four times it (64 KiB: a session list of a few dozen rows fits),
				// then doubling as a Scanner does. Lines just past the first buffer come often: doubling from 16 KiB
				// read a replay of node traffic 10% slower than the 64 KiB start did; this, with seen, reads it as fast.
				size := len(s.buf) * 2
				if !s.grown && len(s.buf) <= maxInt/4 {
					size *= 2
				}
				grown := takeGrown(min(size, s.max))
				copy(grown, s.buf[s.start:s.end])
				if s.grown {
					s.release()
				}
				s.buf, s.grown = grown, true
				s.end -= s.start
				s.start = 0
			}
		}
		for loop := 0; ; {
			n, err := s.r.Read(s.buf[s.end:len(s.buf)])
			if n < 0 || len(s.buf)-s.end < n {
				s.setErr(bufio.ErrBadReadCount)
				break
			}
			s.end += n
			if err != nil {
				s.setErr(err)
				break
			}
			if n > 0 {
				break
			}
			loop++
			if loop > 100 { // bufio's maxConsecutiveEmptyReads
				s.setErr(io.ErrNoProgress)
				break
			}
		}
	}
}

// line is the line that ends with tail, the last of it in buf: tail itself, or, for a line read in pieces, the
// pieces and tail put together in one buffer of the line's size, which is the line's own. bytes.Join makes that buffer
// without clearing it first.
func (s *lineReader) line(tail []byte) []byte {
	if s.held == 0 {
		return dropCR(tail)
	}
	whole := bytes.Join(append(s.parts, tail), nil)
	s.dropParts()
	s.owned = true
	return dropCR(whole)
}

// dropParts lets the pieces before buf go. A line of two pieces gives its first back to grownBufs here and its second
// when the reader is through with it (release), for the next such line. A longer line's pieces all go to the GC, the
// last one too (spent), as every buffer over linePiece did before: a pool keeps what it is given until the second
// collection, so megabytes of a line that is over would stay through the one collection that follows a large body
// (memtrim).
func (s *lineReader) dropParts() {
	switch len(s.parts) {
	case 0:
	case 1:
		if !s.spent {
			giveGrown(s.parts[0])
		}
		s.spent = false // the next buffer is this line's tail, not the spent first piece it replaced
	default:
		s.spent = true
	}
	s.parts, s.held = nil, 0
}

// release lets a grown buf go, before another takes its place: back to grownBufs, unless it is spent.
func (s *lineReader) release() {
	if !s.spent {
		giveGrown(s.buf)
	}
	s.spent = false
}

// shrink moves the unread part back into the first buffer and lets the grown one go.
func (s *lineReader) shrink() {
	if !s.grown {
		return
	}
	n := copy(s.first, s.buf[s.start:s.end])
	s.release()
	s.buf, s.grown, s.start, s.end = s.first, false, 0, n
}

func dropCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

// grownBufs keeps the buffers long lines grew to, 64 KiB to 1 MiB (a pool per size), for the next long line on any
// connection: lines that long come often (a computer's session list), and only a few are being read at once. A buffer
// of any other size, and the pieces of a line of more than two (dropParts), are left to the GC, as a pool's are at
// every other GC.
var grownBufs [5]sync.Pool

func grownPool(size int) *sync.Pool {
	for i := range grownBufs {
		if size == lineStart<<(i+2) {
			return &grownBufs[i]
		}
	}
	return nil
}

func takeGrown(size int) []byte {
	if p := grownPool(size); p != nil {
		if b, ok := p.Get().(*[]byte); ok {
			return *b
		}
	}
	return make([]byte, size)
}

func giveGrown(b []byte) {
	if p := grownPool(len(b)); p != nil {
		p.Put(&b)
	}
}
