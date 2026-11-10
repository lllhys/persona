package node

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/acceptloop"
	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/proto"
)

// standbyAway is what standby says it cannot do.
const standbyAway = "the manager is not connected to this computer"

// standbyHold is how long after the manager went away hooks that wait on the phone are told to retry.
var standbyHold = 10 * time.Minute

// holdsForPhone: the hook would wait on the phone if the manager were there (asksPhone), so in standby it is told to
// retry. A Bash PreToolUse waits only for the assistant's gated commands, which standby cannot tell: it goes on.
func holdsForPhone(in hookInput, req sockRequest) bool {
	if req.ReportOnly || req.Attempt == "" || !asksPhone(in) {
		return false
	}
	return !(in.HookEventName == "PreToolUse" && in.ToolName == "Bash")
}

// spooled says whether standby keeps a hook's event: what the node reports to the manager (notifyHookEvent). A
// resumed hook was reported by the node it first reached; a global report-only one only for the events it reports.
func spooled(in hookInput, req sockRequest) bool {
	if req.Resume || !validID(in.SessionID) {
		return false
	}
	return !req.ReportOnly || reportedEvent(in)
}

// standby answers node.sock while no manager connection is served (Attach). It listens only where nothing else
// answers: a `node --stdio` that serves by itself (no resident node reached in time) keeps the socket, and standby
// takes it once that one is gone.
type standby struct {
	sp   *spool
	logf func(string, ...any)
	now  func() time.Time
	// retry is how often standby looks whether it can (or still does) own node.sock while it is wanted.
	retry time.Duration
	// afterAsk, in tests only, runs between asking whether anything answers on node.sock and taking it.
	afterAsk func()

	mu      sync.Mutex
	wanted  bool
	ln      net.Listener
	lnFile  os.FileInfo // the socket file ln listens on: another one at the path means someone took it
	looping bool
	conns   sync.WaitGroup
	healthy time.Time // the manager was last there for a while (or the resident node started)
}

func newStandby(sp *spool, logf func(string, ...any)) *standby {
	if sp != nil && sp.logf == nil {
		sp.logf = logf
	}
	return &standby{sp: sp, logf: logf, now: time.Now, retry: 5 * time.Second}
}

// want makes standby answer node.sock from now on (as soon as nothing else does).
func (s *standby) want() {
	s.mu.Lock()
	s.wanted = true
	if s.healthy.IsZero() {
		s.healthy = s.now() // the resident node starting: its manager is likely on its way
	}
	if !s.looping {
		s.looping = true
		go s.loop()
	}
	s.mu.Unlock()
	s.tryListen()
}

// managerSeen: a manager connection was served for a while, until at; the hold counts from there.
func (s *standby) managerSeen(at time.Time) {
	s.mu.Lock()
	s.healthy = at
	s.mu.Unlock()
}

// unwant stops standby (a manager connection's Node takes node.sock) and waits for the requests it took.
func (s *standby) unwant() {
	s.mu.Lock()
	s.wanted = false
	s.closeLocked()
	s.mu.Unlock()
	s.conns.Wait()
}

func (s *standby) loop() {
	t := time.NewTicker(s.retry)
	defer t.Stop()
	for range t.C {
		if !s.check() {
			return
		}
	}
}

// check is one look of loop: false once standby is no longer wanted (the loop ends).
func (s *standby) check() bool {
	s.mu.Lock()
	if !s.wanted {
		s.looping = false
		s.mu.Unlock()
		return false
	}
	if s.ln != nil && !s.ownsPathLocked() {
		// A node serving by itself took the path, or is taking it (it removes the file, then listens): let it be for
		// this round, and take it back once nothing answers there. Listening again now could beat that node's listen.
		s.closeLocked()
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	s.tryListen()
	return true
}

func (s *standby) closeLocked() {
	if s.ln != nil {
		s.ln.Close()
		s.ln, s.lnFile = nil, nil
	}
}

func (s *standby) ownsPathLocked() bool {
	fi, err := os.Stat(nodeclient.SocketPath())
	return err == nil && s.lnFile != nil && os.SameFile(fi, s.lnFile)
}

// tryListen listens on node.sock unless standby already does or something else answers there. It asks outside mu (a
// ping takes up to 1.3 s, and holding, managerSeen and unwant wait for mu) and looks again under it. It takes only
// what it asked about: the file that did not answer, or a path with nothing there. A node serving by itself may take
// the path meanwhile (remove, then listen); standby removing that node's new file kept the path for good (#1556).
func (s *standby) tryListen() {
	s.mu.Lock()
	skip := !s.wanted || s.ln != nil
	s.mu.Unlock()
	if skip {
		return
	}
	path := nodeclient.SocketPath()
	asked, _ := os.Lstat(path) // nil: nothing there
	if answers(path) {
		return // a live node serves it
	}
	if s.afterAsk != nil {
		s.afterAsk()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.wanted || s.ln != nil {
		return // unwanted meanwhile (a connection's Node takes the socket), or another try listened first
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.logf("standby: %v", err)
		return
	}
	if now, _ := os.Lstat(path); !sameEntry(asked, now) {
		return // a node is taking the path: ask again next time
	} else if now != nil {
		_ = os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		s.logf("standby: %v", err)
		return
	}
	// Never unlink on close: the next Node listens on the same path, maybe before this one is closed.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = os.Chmod(path, 0o600)
	fi, err := os.Stat(path)
	if err != nil {
		ln.Close()
		s.logf("standby: %v", err)
		return
	}
	s.ln, s.lnFile = ln, fi
	// Ends when closeLocked closes ln; a transient Accept error does not end it (acceptloop.Run).
	go acceptloop.Run(context.Background(), ln, s.logf, func(c net.Conn) {
		s.mu.Lock()
		current := s.ln == ln
		if current {
			s.conns.Add(1) // under mu: unwant's Wait sees every request taken before it closed ln
		}
		s.mu.Unlock()
		if !current {
			c.Close() // ln was closed: the next Accept ends the loop
			return
		}
		go func() {
			defer s.conns.Done()
			s.serve(c)
		}()
	})
}

// sameEntry says whether a and b (from os.Lstat; nil: nothing there) are the same file, or both nothing.
func sameEntry(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b)
}

// answers says whether a node serves path: it answers a ping (a listener that is closing, or a stopped process whose
// backlog still accepts, does not).
func answers(path string) bool {
	c, err := net.DialTimeout("unix", path, 300*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write([]byte(`{"kind":"ping"}` + "\n")); err != nil {
		return false
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	return err == nil && len(line) > 1
}

func (s *standby) listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ln != nil
}

// holding: the manager was there less than standbyHold ago, so a hook that would wait on the phone is told to retry
// (each one for at most nodeclient.ResumeWait). A manager that only flaps (connections shorter than a minute) does not
// renew it.
func (s *standby) holding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.healthy) < standbyHold
}

func (s *standby) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second)) // a request is one short line; unwant waits for this
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return
	}
	var req sockRequest
	if json.Unmarshal(line, &req) != nil {
		return
	}
	b, _ := json.Marshal(s.answer(req))
	_, _ = c.Write(append(b, '\n'))
}

// answer is standby's reply to req (one line on the socket, what a Node would answer when it could).
func (s *standby) answer(req sockRequest) any {
	switch req.Kind {
	case "hook":
		var in hookInput
		if json.Unmarshal(req.Input, &in) != nil {
			return map[string]any{"output": nil}
		}
		if spooled(in, req) {
			s.keep("hook", req)
		}
		if holdsForPhone(in, req) && s.holding() {
			return map[string]any{"retry": true} // the hook asks again; the manager is likely back soon
		}
		return map[string]any{"output": nil} // the terminal decides, as with no manager at all
	case "hook.decided":
		return map[string]any{"ok": true, "standby": true}
	case "ping": // which program and build answer, as a Node's does: persona-link node switch waits for it
		return map[string]any{"ok": true, "standby": true, "program": residentProgram.Load(), "version": residentVersion.Load()}
	case "ask":
		if req.Attempt != "" && s.holding() {
			return nodeclient.AskReply{Retry: true}
		}
		return nodeclient.AskReply{Status: nodeclient.AskUnavailable, Error: standbyAway}
	case "notify":
		var nr nodeclient.NotifyRequest
		if json.Unmarshal(req.Input, &nr) != nil {
			return nodeclient.NotifyReply{Error: "bad request"}
		}
		if strings.TrimSpace(nr.Title) == "" && strings.TrimSpace(nr.Body) == "" {
			return nodeclient.NotifyReply{Error: "the notification is empty"}
		}
		if err := s.keep("notify", req); err != nil {
			return nodeclient.NotifyReply{Error: "could not keep it for the manager: " + err.Error()}
		}
		return nodeclient.NotifyReply{Queued: true}
	case "mcp.tools": // an older `personad mcp` (no away_ok): what a node says when it cannot reach its manager
		if !req.AwayOK {
			return map[string]any{"tools": []proto.Tool{}, "error": standbyAway}
		}
	case "mcp.call":
		if !req.AwayOK {
			return proto.TextResult("Persona hub error: "+standbyAway+"; the call was not run", true)
		}
	}
	return map[string]any{"away": true} // the client takes it for no node (nodeclient.dial)
}

func (s *standby) keep(kind string, req sockRequest) error {
	dropped, err := s.sp.add(spoolEntry{ID: newUUID(), At: s.now().UTC(), Kind: kind, Request: req})
	if err != nil {
		s.logf("standby: keep %s: %v", kind, err)
	} else if dropped > 0 {
		s.logf("standby: spool full, dropped the %d oldest", dropped)
	}
	return err
}
