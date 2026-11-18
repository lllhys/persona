package rpc

import (
	"context"
	"encoding/json"
	"io"
	"runtime/debug"
	"testing"
)

const (
	spentFirstLen  = 2*linePiece + linePiece/4
	spentSecondLen = linePiece + 400<<10
)

type spentReader struct{ data []byte }

func (r *spentReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), 32<<10)], r.data)
	r.data = r.data[n:]
	return n, nil
}

func spentLine(length int, fill byte) []byte {
	prefix, suffix := []byte(`{"event":"review","params":"`), []byte(`"}`)
	line := make([]byte, length)
	copy(line, prefix)
	for i := len(prefix); i < length-len(suffix); i++ {
		line[i] = fill
	}
	copy(line[length-len(suffix):], suffix)
	if !json.Valid(line) {
		panic("invalid review JSON")
	}
	return append(line, '\n')
}

func drainGrown() int {
	n := 0
	for grownPool(linePiece).Get() != nil {
		n++
	}
	return n
}

// A multi-piece line's tail belongs to the GC. The next two-piece line may pool its own tail, not that spent one.
func TestMultiPieceTailDoesNotEnterPoolOnNextLine(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	drainGrown()
	stream := append(spentLine(spentFirstLen, 'a'), spentLine(spentSecondLen, 'b')...)
	p := New("h", io.Discard)
	events := 0
	p.OnEvent = func(event string, params json.RawMessage) {
		events++
		if event != "review" || !json.Valid(params) {
			t.Errorf("invalid delivered event %q", event)
		}
	}
	if err := p.Serve(context.Background(), &spentReader{data: stream}); err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}
	if got := drainGrown(); events != 2 || got > 1 {
		t.Fatalf("got events=%d pooled=%d; want both events and at most the second line's own final piece (sync.Pool may drop it at any GC)", events, got)
	}
}
