package rpc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// chunks reads data in the sizes it is told, in turn (0: an empty read, at most two in a row), and ends with end: with
// the last bytes, or in a read of its own.
type chunks struct {
	data    []byte
	sizes   []byte
	scale   int
	turn    int
	empties int
	end     error
	withEnd bool
}

func (c *chunks) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, c.end
	}
	n := len(p)
	if len(c.sizes) > 0 {
		n = int(c.sizes[c.turn%len(c.sizes)]) * c.scale
		c.turn++
	}
	if n == 0 && c.empties < 2 {
		c.empties++
		return 0, nil
	}
	c.empties = 0
	n = min(max(n, 1), len(p), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	if len(c.data) == 0 && c.withEnd {
		return n, c.end
	}
	return n, nil
}

var errTest = errors.New("the connection broke")

// scanned is the lines and the error of one way of reading r: a bufio.Scanner (as Serve had it) or a lineReader, which
// reads a line in pieces from a buffer of piece bytes on (0: linePiece, as Serve).
func scanned(r io.Reader, size, maximum, piece int, scanner bool) (lines []string, err error) {
	if scanner {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, size), maximum)
		for sc.Scan() {
			lines = append(lines, string(sc.Bytes()))
		}
		return lines, sc.Err()
	}
	shapes := &readShapes{r: r}
	sc := newLineReaderSize(shapes, size, maximum)
	if piece > 0 {
		sc.piece = piece
	}
	middles := 0
	sc.middle = func() { middles++ }
	for sc.Scan() {
		lines = append(lines, string(sc.Bytes()))
	}
	if sc.Bytes() != nil || sc.Owned() {
		return lines, errors.New("a line is left after the last")
	}
	if sc.held != 0 || sc.parts != nil {
		return lines, errors.New("pieces of a line are left after the last")
	}
	if middles != shapes.middle {
		return lines, fmt.Errorf("told of %d reads from the middle of a line, want %d", middles, shapes.middle)
	}
	return lines, sc.Err()
}

// readShapes counts, from the reads alone, what a lineReader's middle hook is told of: the reads that go on with a
// line begun in an earlier read, do not finish it, and have already grown past pingFrameMiddleBytes.
type readShapes struct {
	r       io.Reader
	middle  int
	open    bool // the reads so far end inside a line
	openLen int  // bytes of the line still open
}

func (s *readShapes) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && n <= len(p) { // a lineReader drops a bad count's bytes (bufio.ErrBadReadCount)
		chunk := p[:n]
		if s.open && bytes.IndexByte(chunk, '\n') < 0 {
			s.openLen += n
			if s.openLen > pingFrameMiddleBytes {
				s.middle++
			}
		}
		if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
			s.openLen = n - i - 1
		} else if !s.open {
			s.openLen = n
		}
		s.open = chunk[n-1] != '\n'
	}
	return n, err
}

func sameAsAScanner(t *testing.T, data, sizes []byte, scale, size, maximum, piece int, end error, withEnd bool) {
	t.Helper()
	read := func(scanner bool) ([]string, error) {
		return scanned(&chunks{data: data, sizes: sizes, scale: scale, end: end, withEnd: withEnd}, size, maximum, piece, scanner)
	}
	want, werr := read(true)
	got, err := read(false)
	if !slices.Equal(got, want) || err != werr {
		t.Fatalf("%d bytes in reads of %v times %d, buffer %d up to %d, pieces of %d, ending %v (with the last bytes: %v):\n"+
			"a lineReader read %d lines, %v\na bufio.Scanner %d lines, %v",
			len(data), sizes, scale, size, maximum, piece, end, withEnd, len(got), err, len(want), werr)
	}
}

// A lineReader reads what a bufio.Scanner with ScanLines does, whatever the reads, the buffer sizes and the size from
// which a line is read in pieces: the same lines, then the same error (too long, the reader's own, none at EOF).
func FuzzLineReaderIsAScanner(f *testing.F) {
	f.Add([]byte("a\nb\r\n\n\r\nlast"), []byte{1, 0, 3}, uint8(1), uint16(0), uint8(0), uint8(0))
	f.Add([]byte("\r\r\n\n\nx\r"), []byte{0, 0, 1}, uint8(2), uint16(3), uint8(1), uint8(1))
	f.Add([]byte(strings.Repeat("long line ", 40)+"\nshort\n"+strings.Repeat("y", 90)+"\n"), []byte{7, 50, 1}, uint8(3),
		uint16(500), uint8(2), uint8(15))
	f.Add([]byte(strings.Repeat("z", 64)+"\n"+strings.Repeat("z", 65)+"\n"), []byte{9}, uint8(4), uint16(61), uint8(3), uint8(4))
	f.Add([]byte("over the limit\nok\n"), []byte{}, uint8(0), uint16(4), uint8(0), uint8(2))
	f.Add([]byte(strings.Repeat("p", 31)+"\r\n"+strings.Repeat("q", 32)+"\n\n"+strings.Repeat("r", 200)), []byte{5, 0, 64}, uint8(7),
		uint16(180), uint8(1), uint8(7))
	f.Fuzz(func(t *testing.T, data, sizes []byte, size uint8, extra uint16, end, piece uint8) {
		n := int(size)%16 + 1
		ends := []error{io.EOF, errTest}
		sameAsAScanner(t, data, sizes, 1, n, n+int(extra)%400, int(piece)%96+1, ends[end%2], end&2 != 0)
	})
}

// With Serve's buffer and limit, lines that grow the buffer (a computer's session list), CRLF, empty and unterminated
// lines and lines over the limit read as a bufio.Scanner reads them.
func TestLineReaderAtServesSizes(t *testing.T) {
	line := func(n int, b byte) string { return strings.Repeat(string(b), n) }
	for i, data := range []string{
		line(300<<10, 'a') + "\r\n" + line(10, 'b') + "\n\n" + line(70<<10, 'c') + "\n" + line(1<<20, 'd') + "\n" + "end",
		line(lineStart-1, 'e') + "\n" + line(lineStart, 'f') + "\n" + line(lineStart+1, 'g') + "\r\n",
		line(3<<20, 'h') + "\n" + line(5, 'i') + "\n",
		"ok\n" + line(2<<20, 'j'),
	} {
		for _, sizes := range [][]byte{nil, {1, 7, 0, 200}} {
			t.Run(fmt.Sprint(i, sizes), func(t *testing.T) {
				sameAsAScanner(t, []byte(data), sizes, 1000, lineStart, 2<<20, 0, io.EOF, false)
			})
		}
	}
	// A reader that says it read more than it was given.
	bad := readerFunc(func(p []byte) (int, error) { return len(p) + 1, nil })
	for _, scanner := range []bool{true, false} {
		if lines, err := scanned(bad, lineStart, 2<<20, 0, scanner); len(lines) != 0 || err != bufio.ErrBadReadCount {
			t.Errorf("a bad read count (scanner %v): %q, %v", scanner, lines, err)
		}
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func liveHeap() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// Once a long line is through, Serve keeps only its first buffer, also while it waits for the rest of the next line:
// a bufio.Scanner kept what the line grew for the connection's life (512 KiB per computer after one 256 KiB session
// list, 32 MiB after a 21 MB body). That first buffer is small: an idle connection keeps 17.7 KiB (66.5 KiB with a
// 64 KiB start).
func TestServeKeepsNoLongLine(t *testing.T) {
	for _, c := range []struct{ conns, line, each int }{{1, 21 << 20, lineStart}, {16, 256 << 10, lineStart},
		{256, 100, 18 << 10}} {
		t.Run(fmt.Sprintf("%dx%d", c.conns, c.line), func(t *testing.T) {
			before := liveHeap()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var ws []*io.PipeWriter
			served := make(chan error, c.conns)
			for range c.conns {
				r, w := io.Pipe()
				ws = append(ws, w)
				go func() { served <- New("t", io.Discard).Serve(ctx, r) }()
			}
			long := append(bytes.Repeat([]byte("x"), c.line), '\n') // not JSON: Serve reads it and goes on
			for _, w := range ws {
				if _, err := w.Write(long); err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("part of the next")); err != nil { // read once Serve is back for a line
					t.Fatal(err)
				}
			}
			long = nil
			kept := int64(liveHeap()) - int64(before)
			for _, w := range ws {
				_ = w.Close()
			}
			for range c.conns {
				<-served
			}
			if budget := int64(c.conns*c.each) + 1<<20; kept > budget {
				t.Errorf("%d connections serving kept %d bytes after a %d-byte line each, want at most %d", c.conns, kept,
					c.line, budget)
			}
		})
	}
}
