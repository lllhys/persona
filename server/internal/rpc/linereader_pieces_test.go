package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// Around the size from which a line is read in pieces and around the limit, a lineReader with Serve's sizes reads what
// a bufio.Scanner reads: a line one byte under the limit is a line, one at it is too long (bufio.ErrTooLong, with the
// lines before it), whether the reads split it anywhere or hand over everything at once, and whatever ends the input.
func TestLineReaderInPiecesIsAScanner(t *testing.T) {
	const limit = 3<<20 + 4097 // not a multiple of a piece: the last piece is a short one
	line := func(n int, b byte) string { return strings.Repeat(string(b), n) }
	var cases []string
	for _, n := range []int{linePiece - 1, linePiece, linePiece + 1, 2*linePiece - 1, 2 * linePiece, 2*linePiece + 1,
		limit - 2, limit - 1, limit, limit + 1} {
		cases = append(cases,
			"before\n"+line(n, 'a')+"\nafter\n",
			line(n, 'b'),          // the last line, without a newline
			line(n-1, 'c')+"\r\n", // the carriage return is the line's last byte, as it may be a piece's
			line(n, 'd')+"\r\nafter",
		)
	}
	cases = append(cases,
		// A second long line starts in what is left of the first one's last piece.
		line(linePiece+5, 'e')+"\n"+line(40<<10, 'f')+"\n"+line(2*linePiece+9, 'g')+"\r\n"+line(100, 'h')+"\n",
		// Many short lines behind a long one, then a long one again.
		line(2*linePiece, 'i')+"\n"+strings.Repeat("short line\n", 5000)+line(linePiece+linePiece/2, 'j')+"\n\n\nend",
		line(linePiece, '\r')+"\n"+line(linePiece, '\n')+line(linePiece+1, '\r'),
	)
	for i, data := range cases {
		for _, sizes := range [][]byte{nil, {32}, {1, 7, 0, 200}, {255, 0, 0, 3}} {
			for _, end := range []error{io.EOF, errTest} {
				t.Run(fmt.Sprint(i, sizes, end), func(t *testing.T) {
					sameAsAScanner(t, []byte(data), sizes, 1024, lineStart, limit, 0, end, i%2 == 0)
				})
			}
		}
	}
}

// The limit is the line's length, not a count of buffers: with Serve's own (64 MiB) a line of that many bytes is too
// long once exactly that many are read, as with a bufio.Scanner, and one byte fewer is a line.
func TestLineReaderLimitIsServes(t *testing.T) {
	const limit = 64 << 20
	for _, c := range []struct {
		length int
		err    error
	}{{limit - 1, nil}, {limit, bufio.ErrTooLong}} {
		read := 0
		r := readerFunc(func(p []byte) (int, error) {
			if read == c.length+1 {
				return 0, io.EOF
			}
			n := min(len(p), 64<<10, c.length+1-read)
			for i := range p[:n] {
				p[i] = 'x'
			}
			read += n
			if read == c.length+1 {
				p[n-1] = '\n'
			}
			return n, nil
		})
		sc := newLineReader(r, limit)
		lines, longest := 0, 0
		for sc.Scan() {
			lines++
			longest = max(longest, len(sc.Bytes()))
		}
		wantLines, wantRead := 1, c.length+1
		if c.err != nil {
			wantLines, wantRead, longest = 0, limit, c.length
		}
		if sc.Err() != c.err || lines != wantLines || longest != c.length || read != wantRead {
			t.Errorf("a line of %d bytes: %d lines (the longest %d bytes), %d bytes read, %v; want %d lines, %d bytes read, %v",
				c.length, lines, longest, read, sc.Err(), wantLines, wantRead, c.err)
		}
	}
}

// A line read in pieces is in a buffer of its own, of its size, and stays what it was while the reader goes on; a
// line that fits a pooled buffer is the reader's and is not said to be owned.
func TestLineReaderOwnsALineReadInPieces(t *testing.T) {
	sizes := []int{10, 3 << 20, 300 << 10, linePiece + linePiece/2, 0, 60 << 10, 2*linePiece + 1, 900 << 10, 5}
	var data []byte
	for i, n := range sizes {
		data = append(append(data, bytes.Repeat([]byte{byte('a' + i)}, n)...), "\r\n"...)
	}
	for _, reads := range [][]byte{nil, {32}, {3, 0, 250}} {
		sc := newLineReader(&chunks{data: bytes.Clone(data), sizes: reads, scale: 1024, end: io.EOF}, 8<<20)
		var owned [][]byte
		var ownedOf []int
		for i := 0; sc.Scan(); i++ {
			if i >= len(sizes) {
				t.Fatalf("reads %v: more than %d lines", reads, len(sizes))
			}
			line := sc.Bytes()
			if want := bytes.Repeat([]byte{byte('a' + i)}, sizes[i]); !bytes.Equal(line, want) {
				t.Fatalf("reads %v: line %d is %d bytes, want %d of %q", reads, i, len(line), sizes[i], want[:min(1, len(want))])
			}
			if long := sizes[i] > linePiece; sc.Owned() != long {
				t.Errorf("reads %v: line %d of %d bytes owned: %v, want %v", reads, i, sizes[i], sc.Owned(), long)
			}
			if sc.Owned() {
				// Its buffer is the line and the carriage return dropped from it: nothing grown past it.
				if spare := cap(line) - len(line); spare != 1 {
					t.Errorf("reads %v: line %d of %d bytes is in a buffer %d bytes larger", reads, i, sizes[i], spare)
				}
				owned, ownedOf = append(owned, line), append(ownedOf, i)
			}
		}
		if err := sc.Err(); err != nil || len(owned) != 3 {
			t.Fatalf("reads %v: %d owned lines, %v; want 3, no error", reads, len(owned), err)
		}
		for k, line := range owned {
			i := ownedOf[k]
			if !bytes.Equal(line, bytes.Repeat([]byte{byte('a' + i)}, sizes[i])) {
				t.Errorf("reads %v: line %d changed after the reader went on", reads, i)
			}
		}
	}
}

// A long line costs its pieces and the buffer they are put together in: twice its size, where doubling and copying
// cost more than three times it (64 MiB of buffers for a 21 MB line and its copies on the way).
func TestLineReaderReadsALongLineInItsSize(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's instrumentation allocates on its own")
	}
	const length = 21 << 20
	data := append(bytes.Repeat([]byte("x"), length), '\n')
	read := func() {
		sc := newLineReader(&socketReads{data: data}, 64<<20)
		if !sc.Scan() || len(sc.Bytes()) != length || !sc.Owned() || sc.Scan() || sc.Err() != nil {
			t.Fatalf("a %d-byte line was not read as one owned line: %v", length, sc.Err())
		}
	}
	read()
	var before, after runtime.MemStats
	least := ^uint64(0)
	func() {
		defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no GC empties grownBufs in the middle of a run
		for range 3 {
			runtime.ReadMemStats(&before)
			read()
			runtime.ReadMemStats(&after)
			least = min(least, after.TotalAlloc-before.TotalAlloc)
		}
	}()
	// The pieces (a short last one rounded up to a whole piece), the line, and the first buffers it grew through.
	if limit := uint64(2*length + 3*linePiece); least > limit {
		t.Errorf("reading a %d-byte line allocated %d bytes, over %d (its pieces and the line)", length, least, limit)
	}
}

// What ends the input in the middle of a long line gives the line so far, then its error, as a bufio.Scanner does;
// nothing of the line stays with the reader.
func TestLineReaderLongLineCutShort(t *testing.T) {
	data := bytes.Repeat([]byte("y"), 2*linePiece+17)
	sc := newLineReader(&chunks{data: bytes.Clone(data), sizes: []byte{64}, scale: 1024, end: errTest}, 8<<20)
	if !sc.Scan() || !bytes.Equal(sc.Bytes(), data) || !sc.Owned() {
		t.Fatalf("the cut line: %d bytes (owned: %v), want %d", len(sc.Bytes()), sc.Owned(), len(data))
	}
	if sc.Scan() || !errors.Is(sc.Err(), errTest) || sc.Bytes() != nil || sc.Owned() || sc.parts != nil || sc.held != 0 || sc.grown {
		t.Fatalf("after the cut line: err %v, line %d bytes, %d bytes of pieces held, a grown buffer kept: %v", sc.Err(),
			len(sc.Bytes()), sc.held, sc.grown)
	}
}

// A line of many pieces leaves none of them in grownBufs: the one collection that follows a large body (memtrim)
// gives all of it back, where a pool would keep what it was given until the second.
func TestLineReaderLeavesNoPiecePooled(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's sync.Pool drops buffers at random")
	}
	const length = 21 << 20
	data := append(append(bytes.Repeat([]byte("x"), length), '\n'), "next"...)
	read := func() {
		sc := newLineReader(&socketReads{data: data}, 64<<20)
		if !sc.Scan() || len(sc.Bytes()) != length || !sc.Scan() || string(sc.Bytes()) != "next" || sc.Scan() || sc.Err() != nil {
			t.Fatalf("a %d-byte line and a short one were not read: %v", length, sc.Err())
		}
	}
	read()
	before := liveHeap()
	read()
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	// At most the small buffers the line grew through on its way to the first piece, if no collection came since.
	if kept := int64(after.HeapAlloc) - int64(before); kept > linePiece {
		t.Errorf("one collection after a %d-byte line left %d bytes, want under %d", length, kept, linePiece)
	}
	// And not one piece: the pool of that size is as empty after the line as before it.
	pooled := func() (n int) {
		for grownPool(linePiece).Get() != nil {
			n++
		}
		return n
	}
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // a GC would empty the pool by itself
	pooled()
	read()
	if n := pooled(); n != 0 {
		t.Errorf("a %d-byte line left %d of its pieces in the pool", length, n)
	}
}

// Lines of two pieces, one after another (a file's chunks), take their two buffers from grownBufs and give them back:
// each costs its own buffer and no more.
func TestLineReaderReusesTwoPieces(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's sync.Pool drops buffers at random")
	}
	const length, lines = linePiece + 400<<10, 8
	data := bytes.Repeat(append(bytes.Repeat([]byte("x"), length), '\n'), lines)
	read := func() {
		sc := newLineReader(&socketReads{data: data}, 64<<20)
		n := 0
		for sc.Scan() {
			if n++; len(sc.Bytes()) != length || !sc.Owned() {
				t.Fatalf("line %d: %d bytes (owned: %v), want %d", n, len(sc.Bytes()), sc.Owned(), length)
			}
		}
		if n != lines || sc.Err() != nil {
			t.Fatalf("%d lines, %v; want %d", n, sc.Err(), lines)
		}
	}
	var before, after runtime.MemStats
	least := ^uint64(0)
	func() {
		defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no GC empties grownBufs in the middle of a run
		read()
		for range 3 {
			runtime.ReadMemStats(&before)
			read()
			runtime.ReadMemStats(&after)
			least = min(least, after.TotalAlloc-before.TotalAlloc)
		}
	}()
	if limit := uint64(lines*length + linePiece); least > limit {
		t.Errorf("reading %d lines of %d bytes allocated %d bytes, over %d (the lines)", lines, length, least, limit)
	}
}

// The middle hook is told only of a read that continues a line already bigger than pingFrameMiddleBytes without
// finishing it: small lines cut at any read boundary never count (#8771), and neither does the read that begins a line
// or one that finishes it. The scanned harness checks the same against readShapes for every read pattern.
func TestLineReaderMiddleHook(t *testing.T) {
	reads := []string{"a", "b", "c\nd", "e\n", "fg", "h", "\n"}
	sc := newLineReaderSize(readerFunc(func(p []byte) (int, error) {
		if len(reads) == 0 {
			return 0, io.EOF
		}
		n := copy(p, reads[0])
		reads = reads[1:]
		return n, nil
	}), 32, 8<<20)
	var told []string
	var lines []string
	sc.middle = func() { told = append(told, string(sc.buf[sc.start:sc.end])) }
	for sc.Scan() {
		lines = append(lines, string(sc.Bytes()))
	}
	if sc.Err() != nil || !slices.Equal(lines, []string{"abc", "de", "fgh"}) {
		t.Fatalf("lines %q, %v", lines, sc.Err())
	}
	// All lines stay tiny: a split small line is transport framing, not a frame holding a pong, so the hook stays silent.
	if len(told) != 0 {
		t.Fatalf("small split lines fired the middle hook: %q", told)
	}

	// A line that outgrows the threshold fires the hook once it is past it, never on its first read. A buffer big
	// enough to keep each piece in one raw read makes the firing point deterministic.
	big := strings.Repeat("z", pingFrameMiddleBytes+64)
	reads = []string{"xy\n", big[:100], big[100 : pingFrameMiddleBytes+10], big[pingFrameMiddleBytes+10:], "\n"}
	sc = newLineReaderSize(readerFunc(func(p []byte) (int, error) {
		if len(reads) == 0 {
			return 0, io.EOF
		}
		n := copy(p, reads[0])
		reads = reads[1:]
		return n, nil
	}), 32<<10, 8<<20)
	told = nil
	sc.middle = func() { told = append(told, "x") }
	for sc.Scan() {
	}
	if sc.Err() != nil {
		t.Fatal(sc.Err())
	}
	// The 100-byte start is below the threshold; a piece that crosses it fires, and the hook never fires for a
	// first read or the read that completes the line.
	if len(told) == 0 {
		t.Fatal("a frame over the middle threshold never fired the hook")
	}
}
