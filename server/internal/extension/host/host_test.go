package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/rpc"
)

// TestMain doubles as the plugin: HOST_TEST_PLUGIN=<mode> runs the test binary as one.
func TestMain(m *testing.M) {
	switch os.Getenv("HOST_TEST_PLUGIN") {
	case "":
		RestartFirst, RanAWhile, StopGrace, PingEvery, PingTimeout, HealthFirst = 50*time.Millisecond, time.Hour, time.Second, time.Hour, time.Second, 50*time.Millisecond
		os.Exit(m.Run())
	case "rpc":
		_ = serve.Serve(context.Background(), os.Stdin, os.Stdout, demo())
	case "badproto":
		peer := rpc.New("p", os.Stdout)
		peer.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			return extension.InitializeResult{Protocol: 9, ID: "demo", Version: "1.0.0"}, nil
		}
		_ = peer.Serve(context.Background(), os.Stdin)
	case "quit": // a program that ends by itself as soon as it starts
		os.Exit(3)
	case "lastwords": // a program that says why it is dying right before it exits, like a panic
		os.Stderr.WriteString("first\n")
		time.Sleep(100 * time.Millisecond)
		os.Stderr.WriteString("last words\n")
		os.Exit(1)
	case "httpquit": // a program that opens its socket and then dies without closing it
		if _, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET")); err != nil {
			os.Exit(1)
		}
		os.Exit(3)
	case "mute": // a program that runs but never listens, so /health never answers
		time.Sleep(time.Hour)
	case "http":
		ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
		if err != nil {
			os.Exit(1)
		}
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	case "hang": // answers /health twice, then hangs while the program stays alive
		if f, err := os.OpenFile(os.Getenv("HOST_TEST_PIDS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, os.Getpid())
			f.Close()
		}
		ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
		if err != nil {
			os.Exit(1)
		}
		var answered atomic.Int32
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if answered.Add(1) > 2 {
				select {}
			}
			w.WriteHeader(http.StatusOK)
		}))
	}
	os.Exit(0)
}

// demo is the test plugin: demo.echo, demo.env, demo.pid, demo.sleep, demo.exit, demo.ask (calls the hub back).
func demo() serve.Program {
	var init extension.InitializeParams
	var hub *serve.Hub
	return serve.Program{ID: "demo", Version: "1.0.0",
		Init: func(_ context.Context, p extension.InitializeParams, h *serve.Hub) (map[string]any, error) {
			init, hub = p, h
			return map[string]any{"connectors": []map[string]any{{"id": "demo"}}}, nil
		},
		Namespaces: map[string]serve.Handler{"demo": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "echo":
				return params, nil
			case "init":
				return init, nil
			case "env":
				return os.Getenv("DEMO_ENV"), nil
			case "leak":
				return os.Getenv("HOST_TEST_HUB_SECRET"), nil
			case "pid":
				return os.Getpid(), nil
			case "sleep":
				time.Sleep(10 * time.Second)
				return nil, nil
			case "exit":
				go func() { time.Sleep(10 * time.Millisecond); os.Exit(3) }()
				return nil, nil
			case "ask":
				var out string
				err := hub.Call(ctx, "demo.secret", map[string]any{}, &out)
				return out, err
			}
			return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: method}
		}},
	}
}

func spec(t *testing.T, mode string) Spec {
	self, _ := os.Executable()
	return Spec{ID: "demo", Name: "Demo", Command: self, RPC: true, Env: []string{"HOST_TEST_PLUGIN=" + mode, "DEMO_ENV=yes"},
		Init: func(context.Context) (extension.InitializeParams, error) {
			return extension.InitializeParams{Extension: extension.ExtensionInfo{ID: "demo", Version: "1.0.0"}, Config: json.RawMessage(`{"a":1}`),
				Secrets: map[string]string{"k": "v"}}, nil
		},
		Handle: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method == "demo.secret" {
				return "from the hub", nil
			}
			return nil, errors.New("no")
		},
		Logf: t.Logf}
}

func waitState(t *testing.T, p *Proc, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := p.Status(); s == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, e := p.Status()
	t.Fatalf("state %s (%s), want %s", s, e, want)
}

func pid(t *testing.T, p *Proc) int {
	t.Helper()
	var n int
	if err := p.Call(context.Background(), "demo.pid", struct{}{}, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func alive(pid int) bool { return osproc.Alive(pid) }

func TestStartedByCallsStopsWhenIdle(t *testing.T) {
	t.Setenv("HOST_TEST_HUB_SECRET", "the hub's own")
	s := spec(t, "rpc")
	s.OnCall, s.Idle, s.CleanEnv = true, 300*time.Millisecond, true
	p := Start(context.Background(), s)
	defer p.Stop()
	if st, _ := p.Status(); st != StateStopped {
		t.Fatal("nothing needs it yet:", st)
	}
	var got map[string]any
	if err := p.Call(context.Background(), "demo.echo", map[string]any{"x": 1}, &got); err != nil || got["x"] != float64(1) {
		t.Fatal(got, err)
	}
	var init extension.InitializeParams
	if err := p.Call(context.Background(), "demo.init", struct{}{}, &init); err != nil {
		t.Fatal(err)
	}
	if init.Protocol != extension.Protocol || string(init.Config) != `{"a":1}` || init.Secrets["k"] != "v" {
		t.Fatalf("%+v", init)
	}
	var env, asked string
	_ = p.Call(context.Background(), "demo.env", struct{}{}, &env)
	var leaked string
	_ = p.Call(context.Background(), "demo.leak", struct{}{}, &leaked)
	if leaked != "" {
		t.Fatal("a clean environment has none of the hub's own variables:", leaked)
	}
	_ = p.Call(context.Background(), "demo.ask", struct{}{}, &asked)
	if env != "yes" || asked != "from the hub" {
		t.Fatal(env, asked)
	}
	if res, ok := p.Result(); !ok || string(res.Contributes["connectors"]) != `[{"id":"demo"}]` {
		t.Fatal(res)
	}
	n := pid(t, p)
	waitState(t, p, StateStopped)
	time.Sleep(100 * time.Millisecond)
	if alive(n) {
		t.Fatal("the program is still there")
	}
	if pid(t, p) == n {
		t.Fatal("a new run")
	}
}

func TestHeldRunsWhileHeld(t *testing.T) {
	p := Start(context.Background(), spec(t, "rpc"))
	defer p.Stop()
	if err := p.Call(context.Background(), "demo.echo", struct{}{}, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatal("not started by calls:", err)
	}
	release := p.Hold()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	n := pid(t, p)
	release()
	waitState(t, p, StateStopped)
	time.Sleep(100 * time.Millisecond)
	if alive(n) {
		t.Fatal("released: it stops")
	}
}

func TestRestartsAfterExitAndTimeouts(t *testing.T) {
	s := spec(t, "rpc")
	s.Startup, s.CallTimeout = true, 200*time.Millisecond
	ups := 0
	var mu sync.Mutex
	s.OnUp = func() { mu.Lock(); ups++; mu.Unlock() }
	p := Start(context.Background(), s)
	defer p.Stop()
	if err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := pid(t, p)
	_ = p.Call(context.Background(), "demo.exit", struct{}{}, nil)
	waitState(t, p, StateError)
	waitState(t, p, StateRunning)
	second := pid(t, p)
	if second == first {
		t.Fatal("restarted")
	}
	for i := 0; i < StrikesToKill; i++ {
		err := p.Call(context.Background(), "demo.sleep", struct{}{}, nil)
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != extension.CodeTimeout {
			t.Fatal(err)
		}
	}
	waitState(t, p, StateError)
	waitState(t, p, StateRunning)
	if pid(t, p) == second {
		t.Fatal("restarted after the timeouts")
	}
	mu.Lock()
	defer mu.Unlock()
	if ups != 3 {
		t.Fatal("up three times:", ups)
	}
}

func TestRefusesWrongProtocolAndIdentity(t *testing.T) {
	s := spec(t, "badproto")
	s.Startup = true
	p := Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, "协议版本") {
		t.Fatal(why)
	}
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Init = func(context.Context) (extension.InitializeParams, error) {
		return extension.InitializeParams{Extension: extension.ExtensionInfo{ID: "other", Version: "1.0.0"}}, nil
	}
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, `"demo"`) {
		t.Fatal(why)
	}
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Accept = func(extension.InitializeResult) error { return errors.New("not what was installed") }
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Check = func(context.Context) error { return errors.New("another build") }
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); why != "another build" {
		t.Fatal(why)
	}
	p.Stop()
}

func TestHTTPOnly(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := spec(t, "http")
	sock := filepath.Join(dir, "demo.sock")
	s.RPC, s.HTTP, s.Socket, s.Startup = false, true, sock, true
	s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+sock)
	_ = os.WriteFile(sock, nil, 0o600) // a stale one is removed before the start
	p := Start(context.Background(), s)
	waitState(t, p, StateRunning)
	if !p.Healthy(context.Background()) || !p.Ready() {
		t.Fatal("healthy")
	}
	if err := p.Call(context.Background(), "x.y", nil, nil); err == nil {
		t.Fatal("no protocol")
	}
	end, err := p.BeginHTTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	end()
	p.Stop()
	if st, _ := p.Status(); st != StateStopped {
		t.Fatal(st)
	}
}

// An HTTP-only program the hub runs that stops answering /health while it stays alive is restarted after
// StrikesToKill failed checks in a row, like a protocol one that misses its pings; it used to stay in error for good.
func TestHTTPOnlyThatStopsAnsweringIsRestarted(t *testing.T) {
	every, timeout := HealthEvery, HealthTimeout
	HealthEvery, HealthTimeout = 50*time.Millisecond, 100*time.Millisecond
	defer func() { HealthEvery, HealthTimeout = every, timeout }()
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	o := &observed{}
	s := spec(t, "hang")
	sock, pids := filepath.Join(dir, "demo.sock"), filepath.Join(dir, "pids")
	s.RPC, s.HTTP, s.Socket, s.Startup, s.Observe = false, true, sock, true, o
	s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+sock, "HOST_TEST_PIDS="+pids)
	p := Start(context.Background(), s)
	defer p.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, runs, _ := o.snapshot(); len(runs) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, runs, _ := o.snapshot()
	if len(runs) < 3 || runs[0] != RunStart || !strings.HasPrefix(runs[1], RunExit+" ") || !strings.Contains(runs[1], "health") ||
		runs[2] != RunStart {
		st, why := p.Status()
		t.Fatalf("runs %q (state %s: %s)", runs, st, why)
	}
	b, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	first, err := strconv.Atoi(strings.Fields(string(b))[0])
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); alive(first) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(first) {
		t.Fatal("the hung program is still there")
	}
}

// A started HTTP-only program that never answers /health is an error after ReadyWithin and is started again, and
// Stop still returns. It used to stay starting for good, with no error and no restart.
func TestHTTPNeverReadyIsAnErrorAndRestarted(t *testing.T) {
	old := ReadyWithin
	ReadyWithin = 300 * time.Millisecond
	t.Cleanup(func() { ReadyWithin = old })
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	o := &observed{}
	s := spec(t, "mute")
	s.RPC, s.HTTP, s.Socket, s.Startup, s.Observe = false, true, filepath.Join(dir, "demo.sock"), true, o
	p := Start(context.Background(), s)
	defer func() { // on its own goroutine, so that a Stop that hangs fails this test instead of the whole run
		stopped := make(chan struct{})
		go func() { p.Stop(); close(stopped) }()
		select {
		case <-stopped:
			if st, _ := p.Status(); st != StateStopped {
				t.Errorf("state %s after Stop", st)
			}
		case <-time.After(10 * time.Second):
			t.Error("Stop did not return")
		}
	}()
	want := RunExit + " not answering /health within " + ReadyWithin.String()
	startedAgain := func() bool {
		_, runs, _ := o.snapshot()
		i := slices.Index(runs, want)
		return i >= 0 && slices.Contains(runs[i:], RunStart)
	}
	for deadline := time.Now().Add(10 * time.Second); !startedAgain(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			_, runs, _ := o.snapshot()
			st, why := p.Status()
			t.Fatalf("runs %q, state %s (%s): want %q and then a new start", runs, st, why, want)
		}
	}
}

// A program that ends by itself is an error and is started again, and Stop still returns (#2277). The run used to
// take the program's exit and then wait for it a second time while ending the run, so the host hung for good.
func TestAProgramThatQuitsIsStartedAgainAndStops(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	o := &observed{}
	s := spec(t, "quit")
	s.RPC, s.HTTP, s.Socket, s.Startup, s.Observe = false, true, filepath.Join(dir, "demo.sock"), true, o
	p := Start(context.Background(), s)
	defer func() { // on its own goroutine, so that a Stop that hangs fails this test instead of the whole run
		stopped := make(chan struct{})
		go func() { p.Stop(); close(stopped) }()
		select {
		case <-stopped:
			if st, _ := p.Status(); st != StateStopped {
				t.Errorf("state %s after Stop", st)
			}
		case <-time.After(10 * time.Second):
			t.Error("Stop did not return")
		}
	}()
	startedAgain := func() bool {
		_, runs, _ := o.snapshot()
		i := slices.Index(runs, "exit exit status 3")
		return i >= 0 && slices.Contains(runs[i:], RunStart)
	}
	for deadline := time.Now().Add(10 * time.Second); !startedAgain(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			_, runs, _ := o.snapshot()
			t.Fatalf("runs %q: the program's exit was not seen, or it was not started again", runs)
		}
	}
}

// A hub-run program's HTTP socket goes when its run ends: a program stopped by the hub (SIGTERM) leaves the file on
// disk, and it stayed there after the extension was switched off, stopped when idle, uninstalled or the hub stopped.
func TestSocketRemovedWhenRunEnds(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := spec(t, "http")
	sock := filepath.Join(dir, "demo.sock")
	s.RPC, s.HTTP, s.Socket, s.Startup = false, true, sock, true
	s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+sock)
	p := Start(context.Background(), s)
	waitState(t, p, StateRunning)
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the socket while running: %v", err)
	}
	p.Stop()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("the socket after Stop: %v, want it gone", err)
	}
}

// socketAtExit says, each time a run ends in error, whether the socket was still on disk then: the run is over by
// the time the Observer hears of it, and the next start has not removed it yet.
type socketAtExit struct {
	observed
	sock string
	mu   sync.Mutex
	left []bool
}

func (o *socketAtExit) Run(event, detail string) {
	o.observed.Run(event, detail)
	if event != RunExit {
		return
	}
	_, err := os.Stat(o.sock)
	o.mu.Lock()
	o.left = append(o.left, err == nil)
	o.mu.Unlock()
}

// A program that dies by itself leaves its socket file; the run that saw it go removes it.
func TestSocketRemovedWhenTheProgramExits(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "demo.sock")
	o := &socketAtExit{sock: sock}
	s := spec(t, "httpquit")
	s.RPC, s.HTTP, s.Socket, s.Startup, s.Observe = false, true, sock, true, o
	s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+sock)
	p := Start(context.Background(), s)
	exits := func() []bool {
		o.mu.Lock()
		defer o.mu.Unlock()
		return slices.Clone(o.left)
	}
	for deadline := time.Now().Add(10 * time.Second); len(exits()) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			p.Stop()
			_, runs, _ := o.snapshot()
			t.Fatalf("runs %q: the program's exit was not seen twice", runs)
		}
	}
	p.Stop()
	if left := exits(); slices.Contains(left, true) {
		t.Fatalf("socket left on disk after a run ended: %v", left)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("the socket after Stop: %v, want it gone", err)
	}
}

// The socket of a plugin the hub does not run is not the hub's: stopping it leaves the socket alone.
func TestRemoteSocketLeftAloneOnStop(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := spec(t, "")
	s.Command, s.RPC, s.HTTP, s.Socket, s.Startup = "", false, true, filepath.Join(dir, "far.sock"), true
	hl, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer hl.Close()
	go http.Serve(hl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	far := Start(context.Background(), s)
	waitState(t, far, StateRunning)
	far.Stop()
	if _, err := os.Stat(s.Socket); err != nil {
		t.Fatalf("the remote socket after Stop: %v, want it kept", err)
	}
}

// What a program writes right before it exits (a panic, the reason it dies) reaches the log. The pipes came from
// cmd.StderrPipe/StdoutPipe, which Wait closes once the program exits, so lines still in the pipe were dropped.
func TestLogsKeepWhatAProgramWritesBeforeItExits(t *testing.T) {
	for _, rpc := range []bool{true, false} {
		dir, err := os.MkdirTemp("", "hst")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		var mu sync.Mutex
		var lines []string
		got := make(chan struct{})
		s := spec(t, "lastwords")
		if !rpc {
			s.RPC, s.HTTP, s.Socket = false, true, filepath.Join(dir, "demo.sock")
		}
		s.Startup = true
		s.Logf = func(format string, args ...any) {
			line := fmt.Sprintf(format, args...)
			if strings.HasSuffix(line, ": first") {
				time.Sleep(500 * time.Millisecond) // a slow log: the program has exited and been waited for by now
			}
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, line)
			if strings.HasSuffix(line, ": last words") && !slices.ContainsFunc(lines[:len(lines)-1], func(l string) bool { return strings.HasSuffix(l, ": last words") }) {
				close(got)
			}
		}
		p := Start(context.Background(), s)
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			mu.Lock()
			t.Errorf("rpc=%v: the program's last line was not logged: %q", rpc, lines)
			mu.Unlock()
		}
		p.Stop()
	}
}

func TestElsewhere(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "rpc.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _ = serve.Serve(context.Background(), c, c, demo()) }()
		}
	}()
	s := spec(t, "")
	s.Command, s.RPCSocket, s.OnCall = "", sock, true
	p := Start(context.Background(), s)
	defer p.Stop()
	var n int
	if err := p.Call(context.Background(), "demo.pid", struct{}{}, &n); err != nil || n != os.Getpid() {
		t.Fatal(n, err)
	}

	s = spec(t, "")
	s.Command, s.RPCSocket, s.Startup = "", filepath.Join(dir, "none.sock"), true
	q := Start(context.Background(), s)
	defer q.Stop()
	waitState(t, q, StateError)

	s = spec(t, "")
	s.Command, s.RPC, s.HTTP, s.Socket, s.Startup = "", false, true, filepath.Join(dir, "far.sock"), true
	far := Start(context.Background(), s)
	defer far.Stop()
	waitState(t, far, StateError)
	hl, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer hl.Close()
	go http.Serve(hl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	waitState(t, far, StateRunning)
}

// observed records what an Observer is told.
type observed struct {
	mu    sync.Mutex
	calls []string // method code
	runs  []string // event detail
	logs  []string // source: line
}

func (o *observed) Call(method string, took time.Duration, code string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, strings.TrimSpace(method+" "+code))
}

func (o *observed) Run(event, detail string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runs = append(o.runs, strings.TrimSpace(event+" "+detail))
}

func (o *observed) Log(source, line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, source+": "+line)
}

func (o *observed) snapshot() (calls, runs, logs []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...), append([]string(nil), o.runs...), append([]string(nil), o.logs...)
}

// The host tells the Observer each call with its code, each run and why it ended, and the host's lines about it.
func TestObserver(t *testing.T) {
	o := &observed{}
	s := spec(t, "rpc")
	s.OnCall, s.Observe = true, o
	p := Start(context.Background(), s)
	defer p.Stop()
	if err := p.Call(context.Background(), "demo.echo", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	_ = p.Call(context.Background(), "demo.nothing", map[string]any{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = p.Call(ctx, "demo.echo", map[string]any{}, nil)
	_ = p.Call(context.Background(), "demo.exit", map[string]any{}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, runs, _ := o.snapshot(); len(runs) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	calls, runs, logs := o.snapshot()
	want := []string{"demo.echo", "demo.nothing " + extension.CodeUnknownMethod, "demo.echo " + CodeCanceled, "demo.exit"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q, want %q", calls, want)
	}
	if len(runs) < 2 || runs[0] != RunStart || !strings.HasPrefix(runs[1], RunExit+" ") {
		t.Fatalf("runs %q", runs)
	}
	found := false
	for _, l := range logs {
		if strings.HasPrefix(l, LogHub+": ") && strings.Contains(l, "again in") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host's line about the exit: %q", logs)
	}
}

// A line longer than any buffer is cut and the rest of it dropped; the host keeps reading, so the program never
// blocks writing its log.
func TestLogLinesKeepsReadingPastALongLine(t *testing.T) {
	o := &observed{}
	var mu sync.Mutex
	var logged []string
	p := &Proc{spec: Spec{ID: "demo", Observe: o, Logf: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(format, args...))
	}}}
	pr, pw := io.Pipe()
	read := make(chan struct{})
	go func() {
		p.logLines(pr)
		close(read)
	}()
	wrote := make(chan error, 1)
	go func() {
		_, err := pw.Write([]byte(strings.Repeat("x", 2<<20)))
		if err == nil {
			_, err = pw.Write([]byte("\nafter\n"))
		}
		_ = pw.Close()
		wrote <- err
	}()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		_ = pr.Close()
		t.Fatal("the program is stuck writing its log")
	}
	<-read
	_, _, logs := o.snapshot()
	want := []string{LogProgram + ": " + strings.Repeat("x", LogLineMost) + "…", LogProgram + ": after"}
	if !slices.Equal(logs, want) {
		t.Fatalf("observed %d lines %.80q", len(logs), logs)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 2 || logged[1] != "plugin demo: after" {
		t.Fatalf("logged %.80q", logged)
	}
}

// openConns counts the connections a listener accepted that are still open.
type openConns struct {
	net.Listener
	mu   sync.Mutex
	open int
}

func (l *openConns) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.open++
	l.mu.Unlock()
	return &countedConn{Conn: c, l: l}, nil
}

func (l *openConns) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open
}

type countedConn struct {
	net.Conn
	l    *openConns
	once sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() {
		c.l.mu.Lock()
		c.l.open--
		c.l.mu.Unlock()
	})
	return c.Conn.Close()
}

// A plugin served from elsewhere outlives its Proc: Stop must not leave the health check's keep-alive connection open.
func TestStopClosesIdleHTTPConnections(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "far.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	cl := &openConns{Listener: ln}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })}
	go srv.Serve(cl)
	defer srv.Close()

	s := spec(t, "")
	s.Command, s.RPC, s.HTTP, s.Socket, s.Startup = "", false, true, sock, true
	p := Start(context.Background(), s)
	waitState(t, p, StateRunning)
	if n := cl.count(); n == 0 {
		t.Fatal("no connection to the plugin after its health check")
	}
	p.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for cl.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d connection(s) to the plugin still open after Stop", cl.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A long line in Chinese is cut on a whole character: it takes three bytes a character, and a cut at LogLineMost bytes
// fell inside one, which the log then showed as a broken character before the ellipsis.
func TestLogLineCutsOnWholeCharacters(t *testing.T) {
	o := &observed{}
	p := &Proc{spec: Spec{ID: "demo", Observe: o, Logf: func(string, ...any) {}}}
	const unit = "无法连接服务"
	long := strings.Repeat(unit, LogLineMost/len(unit)+2)
	for _, extra := range []string{"", "a", "ab"} { // moves the cut to each byte of a character
		p.logLine([]byte(extra+long), false)
	}
	short := strings.Repeat(unit, 100)
	p.logLine([]byte(short), false)
	_, _, logs := o.snapshot()
	if len(logs) != 4 {
		t.Fatalf("observed %d lines", len(logs))
	}
	for i, l := range logs[:3] {
		line, ok := strings.CutPrefix(l, LogProgram+": ")
		text, cut := strings.CutSuffix(line, "…")
		if !ok || !cut || !utf8.ValidString(line) || len(text) > LogLineMost || len(text) < LogLineMost-3 {
			t.Errorf("line %d: valid UTF-8 %v, %d bytes before the ellipsis (want %d to %d)", i, utf8.ValidString(line), len(text), LogLineMost-3, LogLineMost)
		}
	}
	if logs[3] != LogProgram+": "+short {
		t.Errorf("a short line was changed: %.40q", logs[3])
	}
}
