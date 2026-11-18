package ai

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// panicking is a transport whose next request panics once armed, in the caller's goroutine: a bug in the exchange
// itself (the transport, the reading of the answer).
type panicking struct {
	armed atomic.Bool
	next  http.RoundTripper
}

func (p *panicking) RoundTrip(r *http.Request) (*http.Response, error) {
	if p.armed.CompareAndSwap(true, false) {
		panic("the exchange broke")
	}
	return p.next.RoundTrip(r)
}

// recovering calls f as a caller that outlives a panic does (the knowledge base loop's kbRun, its job pool) and says
// whether it panicked.
func recovering(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

// callWithin is call under a deadline: a request the gate never lets out ends as put off instead of hanging the test.
func callWithin(c *Client, d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var out struct{ OK bool }
	err := c.JSON(ctx, "sys", "user", &out)
	if err == nil && !out.OK {
		return errors.New("no answer")
	}
	return err
}

// gateState is what the gate counts, read under its lock.
func gateState(g *gate) (held, probing bool, inflight, out, cap int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held, g.probing, g.inflight, len(g.out), g.cap
}

// A panic that unwinds through a request gives its slot back like any other end. Its caller may recover and carry on
// (the knowledge base loop does), and the request then stayed counted as out for good: the next busy answer capped
// the connection at what was "in flight", the cap was full with nothing out, and every later request was put off as
// busy, none sent, until the process restarted (#6698).
func TestAPanicGivesItsSlotBack(t *testing.T) {
	clk := fakeClock(t)
	u := newUpstream(t, ok)
	tr := &panicking{next: http.DefaultTransport}
	c := NewWith(Config{BaseURL: u.srv.URL, APIKey: "panic-slot", Model: "m"}, tr)
	g := gateFor(u.srv.URL, "panic-slot")

	tr.armed.Store(true)
	if !recovering(func() { _ = call(c) }) {
		t.Fatal("the request did not panic")
	}
	if _, _, inflight, out, _ := gateState(g); inflight != 0 || out != 0 {
		t.Fatalf("after the panic: %d counted in flight (%d with a start time), want none", inflight, out)
	}

	// One busy answer, then the upstream is fine for good.
	u.set(answer(503, busyBody))
	if err := call(c); !IsPaused(err) {
		t.Fatalf("the busy answer: %v", err)
	}
	u.set(ok)
	clk.add(busyFirst + time.Second)
	sent := u.sent.Load()
	for i := range 3 {
		if err := callWithin(c, 5*time.Second); err != nil {
			held, probing, inflight, _, limit := gateState(g)
			t.Fatalf("request %d after the pause: %v (held %v, finding out %v, %d in flight under a cap of %d)", i, err, held, probing, inflight, limit)
		}
	}
	if got := u.sent.Load() - sent; got != 3 {
		t.Fatalf("%d request(s) reached the upstream after the pause, want 3", got)
	}
	if p := Held(c.Config()); p != nil {
		t.Fatalf("still held: %+v", p)
	}
}

// The same of the request finding out whether a pause is over: when a panic unwinds through it, the next request
// finds out in its place. It stayed "finding out" for good, so every later request waited for an answer that could
// not come and was put off when its own time ran out.
func TestAPanicInTheRequestFindingOutLetsTheNextFindOut(t *testing.T) {
	clk := fakeClock(t)
	u := newUpstream(t, answer(503, busyBody))
	tr := &panicking{next: http.DefaultTransport}
	c := NewWith(Config{BaseURL: u.srv.URL, APIKey: "panic-probe", Model: "m"}, tr)
	g := gateFor(u.srv.URL, "panic-probe")
	heldGate(t, clk, c)
	u.set(ok)

	tr.armed.Store(true)
	if !recovering(func() { _ = call(c) }) {
		t.Fatal("the request finding out did not panic")
	}
	if held, probing, inflight, _, _ := gateState(g); !held || probing || inflight != 0 {
		t.Fatalf("after the panic: held %v, finding out %v, %d in flight; want held, nobody finding out, none in flight", held, probing, inflight)
	}
	sent := u.sent.Load()
	for i := range 3 {
		clk.add(time.Minute)
		if err := callWithin(c, 5*time.Second); err != nil {
			t.Fatalf("request %d after the panic: %v", i, err)
		}
	}
	if got := u.sent.Load() - sent; got != 3 {
		t.Fatalf("%d request(s) reached the upstream after the panic, want 3", got)
	}
	if p := Held(c.Config()); p != nil {
		t.Fatalf("still held: %+v", p)
	}
	if logs := strings.Join(clk.lines(), "\n"); !strings.Contains(logs, "answers again after") {
		t.Fatalf("the log does not say the connection answers again:\n%s", logs)
	}
}

// Requests put off for want of a turn are in the log: how many, how many requests are out under what cap, and for
// how long the longest has been, at most one line every noTurnEvery. A request put off for want of a slot used to
// leave no line at all, so a connection that put everything off looked the same whether its upstream was at its
// limit or a slot had been lost (#6698).
func TestRequestsPutOffForWantOfATurnAreInTheLog(t *testing.T) {
	clk := fakeClock(t)
	var n atomic.Int32
	streaming := make(chan struct{}) // the long request's 200 and first events are on the wire
	finish := make(chan struct{})    // lets its stream end
	var once sync.Once
	end := func() { once.Do(func() { close(finish) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { // a port probe from elsewhere on the machine, not this test's client
			http.NotFound(w, r)
			return
		}
		switch n.Add(1) {
		case 1: // one busy answer: the connection is capped at one request
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, busyBody)
		case 2: // a long answer, which keeps the one slot
			w.Header().Set("content-type", "text/event-stream")
			_, _ = io.WriteString(w, sse(evStart, evBlock(0, "text"), evText(0, `{"ok":`)))
			w.(http.Flusher).Flush()
			close(streaming)
			<-finish
			_, _ = io.WriteString(w, sse(evText(0, `true}`), evDelta("end_turn", 5), evStop))
		default:
			w.Header().Set("content-type", "text/event-stream")
			_, _ = io.WriteString(w, sse(evStart, evBlock(0, "text"), evText(0, `{"ok":true}`), evDelta("end_turn", 5), evStop))
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(end) // before the server's Close, so a failed test does not hang it
	c := New(Config{BaseURL: srv.URL, APIKey: "no-turn", Model: "m"})
	heldGate(t, clk, c)
	g := gateFor(srv.URL, "no-turn")

	long := make(chan error, 1)
	go func() { long <- callWithin(c, 3*time.Minute) }()
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the long request never reached the upstream")
	}
	gateUntil(t, g, "the gate let go at the long request's 200", func() bool { return !g.held })

	putOff := func(what string) {
		t.Helper()
		if err := callWithin(c, 30*time.Millisecond); !IsPaused(err) {
			t.Fatalf("%s: %v, want put off for want of the slot", what, err)
		}
	}
	lines := func() []string {
		var out []string
		for _, l := range clk.lines() {
			if strings.Contains(l, "for want of a turn") {
				out = append(out, l)
			}
		}
		return out
	}
	clk.add(3 * time.Minute)
	putOff("the first request beside the long one")
	if got := lines(); len(got) != 1 || !strings.HasSuffix(got[0], ": 1 request(s) put off for want of a turn since 19:46:06: 1 out under a cap of 1, the longest for 3m0s") {
		t.Fatalf("the first request put off: log = %q", got)
	}
	// More of them inside noTurnEvery are counted, not logged.
	clk.add(4 * time.Minute)
	putOff("the second")
	clk.add(time.Minute)
	putOff("the third")
	if got := lines(); len(got) != 1 {
		t.Fatalf("within %s of the first line: log = %q", noTurnEvery, got)
	}
	// The next one after it says how many there were, and how long the request holding the slot has been out.
	clk.add(noTurnEvery)
	putOff("the fourth")
	if got := lines(); len(got) != 2 || !strings.HasSuffix(got[1], ": 3 request(s) put off for want of a turn since 19:50:06: 1 out under a cap of 1, the longest for 18m0s") {
		t.Fatalf("after %s: log = %q", noTurnEvery, got)
	}

	end()
	if err := <-long; err != nil {
		t.Fatalf("the long request: %v", err)
	}
	if err := callWithin(c, 5*time.Second); err != nil {
		t.Fatalf("with the slot free again: %v", err)
	}
	if _, _, inflight, out, _ := gateState(g); inflight != 0 || out != 0 {
		t.Fatalf("at the end: %d counted in flight (%d with a start time), want none", inflight, out)
	}
}

// With several requests out, the line tells how long the one out longest has been, among those still out: the ones
// that ended are not counted, whichever of them ended first.
func TestTheLineTellsTheLongestStillOut(t *testing.T) {
	clk := fakeClock(t) // 19:43:00
	gone, cancel := context.WithCancel(context.Background())
	cancel() // a request that would have to wait is put off at once instead
	g := &gate{host: "upstream", wake: make(chan struct{})}
	var out []turn
	for range 4 { // one a minute, from 19:43:00
		next, err := g.admit(gone)
		if err != nil {
			t.Fatalf("request %d: %v", len(out), err)
		}
		out = append(out, next)
		clk.add(time.Minute)
	}
	// The last comes back busy with three others out: the connection is held, under a cap of two. Then the first
	// ends: the two in the middle are out, and they fill the cap.
	if err := g.done(out[3], false, false, &httpError{status: 503, body: busyBody}); !IsPaused(err) {
		t.Fatalf("the busy answer: %v", err)
	}
	_ = g.done(out[0], false, false, io.ErrUnexpectedEOF)
	clk.add(busyFirst + time.Second) // the pause is over
	if _, err := g.admit(gone); !IsPaused(err) {
		t.Fatalf("a request with the cap full: %v, want put off", err)
	}
	longest := 3*time.Minute + busyFirst + time.Second // the second request went out at 19:44:00
	want := ": 1 request(s) put off for want of a turn since 19:47:06: 2 out under a cap of 2, the longest for " + longest.String()
	if logs := clk.lines(); len(logs) != 2 || !strings.HasSuffix(logs[1], want) {
		t.Fatalf("logs = %q, want the second to end %q", logs, want)
	}
}

// A pause that runs past today names its day in the log: "until 15:43:00" for a Retry-After of twenty hours read as
// a pause that was already over.
func TestAHoldPastTodayNamesItsDay(t *testing.T) {
	clk := fakeClock(t) // 19:43:00 on 2026-11-02
	u := newUpstream(t, answer(503, busyBody, "Retry-After", "72000"))
	if err := call(u.client("past-today")); !IsPaused(err) {
		t.Fatalf("the busy answer: %v", err)
	}
	if logs := clk.lines(); len(logs) != 1 || !strings.HasSuffix(logs[0], "requests are put off until 2026-11-03 15:43:00") {
		t.Fatalf("logs = %q", logs)
	}
}

// The gate counts right whatever comes in whatever order: random runs of requests coming, ending every way a request
// ends (answered, a limit before or after the 200, another failure before or after it, a panic unwinding through it
// before or after it) and time passing.
// After every step the requests counted as out are the ones that are, at most one of them is finding out, and with
// nothing out and no pause running a request goes out at once: no order of events strands the connection (#6698).
func TestTheGateCountsRightInAnyOrder(t *testing.T) {
	clk := fakeClock(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel() // a request that would have to wait is put off at once instead
	ends := []struct {
		name     string
		answered bool // the upstream said 200
		err      error
	}{
		{"answered", true, nil},
		{"answered", true, nil},
		{"busy after the 200", true, &streamLimit{reason: ReasonBusy, event: "error", code: "overloaded_error", msg: "Overloaded"}},
		{"busy", false, &httpError{status: 503, body: busyBody}},
		{"busy, retry after 90s", false, &httpError{status: 503, body: busyBody, retryAfter: 90 * time.Second}},
		{"out of quota", false, &httpError{status: 429, body: quotaBody}},
		{"not found", false, &httpError{status: 404, body: "no such model"}},
		{"bad gateway", false, &httpError{status: 502, body: "bad gateway"}},
		{"dropped", false, io.ErrUnexpectedEOF},
		{"cut off after the 200", true, io.ErrUnexpectedEOF},
		{"panicked", false, errUnwound},
		{"panicked after the 200", true, errUnwound},
	}
	waits := []time.Duration{time.Second, 6 * time.Second, time.Minute, 6 * time.Minute, 31 * time.Minute}
	type request struct {
		turn turn
		let  bool // the gate went at its 200: it is an ordinary request from then on
	}
	for seed := int64(1); seed <= 3000; seed++ {
		rnd := rand.New(rand.NewSource(seed))
		g := &gate{host: "walk", wake: make(chan struct{})}
		var out []*request
		var trace []string
		check := func() {
			t.Helper()
			held, probing, inflight, timed, limit := gateState(g)
			finding := 0
			for _, r := range out {
				if r.turn.probe && !r.let {
					finding++
				}
			}
			var problem string
			switch {
			case inflight != len(out) || timed != len(out):
				problem = "the requests counted as out are not the ones that are"
			case finding > 1 || probing != (finding == 1):
				problem = "who is finding out does not match the requests out"
			case limit < 0:
				problem = "a negative cap"
			}
			if problem == "" && len(out) == 0 && (!held || !now().Before(g.pause.Until)) {
				// Nothing out and no pause running: a request goes out at once. It ends as a dropped connection, which
				// changes nothing but who finds out next.
				next, err := g.admit(gone)
				if err != nil {
					problem = "nothing is out, no pause is running, and a request is put off: " + err.Error()
				} else {
					_ = g.done(next, next.probe, false, io.ErrUnexpectedEOF)
				}
			}
			if problem != "" {
				t.Fatalf("seed %d: %s\nevents: %s\ngate: held %v, finding out %v, %d in flight (%d timed), cap %d; %d out by the test's count",
					seed, problem, strings.Join(trace, ", "), held, probing, inflight, timed, limit, len(out))
			}
		}
		for range 60 {
			switch k := rnd.Intn(10); {
			case k < 4: // a request comes
				next, err := g.admit(gone)
				if err != nil {
					if !IsPaused(err) {
						t.Fatalf("seed %d: a request not let out: %v", seed, err)
					}
					trace = append(trace, "put off")
					break
				}
				trace = append(trace, map[bool]string{true: "one finds out", false: "one goes out"}[next.probe])
				out = append(out, &request{turn: next})
			case k < 8 && len(out) > 0: // one of those out ends
				i := rnd.Intn(len(out))
				r := out[i]
				out = append(out[:i], out[i+1:]...)
				end := ends[rnd.Intn(len(ends))]
				if end.answered && r.turn.probe && g.answers() { // its 200 is seen before its answer is read
					r.let = true
				}
				// A limit the stream reports after the 200 is no answer to the gate (messageFailure, respond), and
				// neither is a request that did not return (Chat's defer).
				var limit *streamLimit
				told := end.answered && !errors.As(end.err, &limit) && end.err != errUnwound
				_ = g.done(r.turn, r.turn.probe && !r.let, told, end.err)
				trace = append(trace, end.name)
			default:
				d := waits[rnd.Intn(len(waits))]
				clk.add(d)
				trace = append(trace, "+"+d.String())
			}
			check()
		}
	}
}
