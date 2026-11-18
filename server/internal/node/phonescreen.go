package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Phone Screen (#9314, docs/extensions/computer-use.md "Phone Screen"): the live screen's third mode. Persona Input
// adds a portrait virtual display shaped like the phone's picture and puts the window the phone picks on it. The node
// starts the helper's session (`--socket … --token … --phone-screen`, its Accessibility being the helper's own),
// relays the phone's requests to it one at a time (JSON lines), and binds the session to the live screen's paired RFB
// stream, as video is bound: only a phone signed in to Screen Sharing can ask, and when that stream ends the session
// ends, so the helper puts every window back and lets the display go. One session per Mac.

// phoneScreenVersion is the session's request set this node speaks (PersonaInputKit's PhoneScreenPreflight.version).
const phoneScreenVersion = 1

// How long one request may take before the helper is taken as stuck and the session ends.
var phoneScreenTimeouts = map[string]time.Duration{
	"start": 30 * time.Second, "windows": 15 * time.Second, "place": 30 * time.Second, "stop": 30 * time.Second,
}

// parsePhoneScreenPreflight reads the helper's `phone_screen: <version> granted|missing` line: version 0 when the
// helper has no Phone Screen (one from before it prints no such line).
func parsePhoneScreenPreflight(out string) (int, string) {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "phone_screen:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 {
			return 0, ""
		}
		v, err := strconv.Atoi(f[0])
		if err != nil || v < 1 || (f[1] != "granted" && f[1] != "missing") {
			return 0, ""
		}
		return v, f[1]
	}
	return 0, ""
}

// phoneScreenStatus is phone_screen for the computer and browser statuses: nil off a Mac, without Persona Input, or
// with one that has no Phone Screen. It reads the same cached --preflight as screen_input.
func phoneScreenStatus(ctx context.Context) *proto.PhoneScreen {
	helper, _ := inputHelpers.current(ctx, func(string, ...any) {})
	if helper == "" {
		return nil
	}
	v, permission := parsePhoneScreenPreflight(inputPermissions.raw(ctx, helper))
	if v < 1 {
		return nil
	}
	return &proto.PhoneScreen{Version: min(v, phoneScreenVersion), Permission: permission}
}

// phoneRestoreFile is where the helper keeps where windows were and the text size it gave them (PhoneScreenRestore):
// there only while something is to be put back.
func phoneRestoreFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".persona", "phone-screen.json")
}

// phoneRestoreRun runs the helper's `--phone-screen-restore` through LaunchServices (its Accessibility is its own) and
// waits for it to finish.
func phoneRestoreRun(ctx context.Context, helper string) error {
	out, err := exec.CommandContext(ctx, "/usr/bin/open", "-W", "-g", "-n", helper, "--args", "--phone-screen-restore").CombinedOutput()
	if err != nil {
		return fmt.Errorf("open: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// phoneRestoreAfter is how long after a session ends the node looks for records it left (a helper that died, #9500).
const phoneRestoreAfter = 5 * time.Second

// phoneStart starts the helper's session and returns it once it said the token and whether it may move windows.
// A variable for tests.
var phoneStart = func(ctx context.Context, helper string) (net.Conn, bool, error) {
	c, err := dialScreenHelper(ctx, helper, "--phone-screen")
	if err != nil {
		return nil, false, err
	}
	_ = c.SetReadDeadline(time.Now().Add(screenHelperWait))
	line, err := readTokenLine(c)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		_ = c.Close()
		return nil, false, fmt.Errorf("the helper's second line: %w", err)
	}
	return c, line == "granted", nil
}

// phoneScreens holds this Mac's one session.
type phoneScreens struct {
	logf   func(string, ...any)
	signed func(pair string) bool              // the pair's RFB stream is open and signed in (screenLogins.signedIn)
	attach func(pair string, c io.Closer) bool // ends c with that stream (screenLogins.attach)

	// What a helper that died left (#9500): its file, how to run --phone-screen-restore, how long after a session ends to
	// look. Zero: phoneRestoreFile, phoneRestoreRun, phoneRestoreAfter (tests set their own).
	restoreFile  func() string
	restoreRun   func(ctx context.Context, helper string) error
	restoreAfter time.Duration

	starting sync.Mutex // one start at a time; never held by a request
	mu       sync.Mutex
	cur      *phoneSession
}

type phoneSession struct {
	owner *phoneScreens
	pair  string
	conn  net.Conn
	rd    *bufio.Reader
	calls sync.Mutex // one request and its answer at a time
	once  sync.Once
}

// Close ends the session: the helper puts everything back as its socket closes. Safe to call more than once. A helper
// that died first left its records: a moment later the node has them put back (restoreLeftovers).
func (s *phoneSession) Close() error {
	s.once.Do(func() {
		_ = s.conn.Close()
		s.owner.mu.Lock()
		if s.owner.cur == s {
			s.owner.cur = nil
		}
		s.owner.mu.Unlock()
		after := s.owner.restoreAfter
		if after == 0 {
			after = phoneRestoreAfter
		}
		time.AfterFunc(after, func() { s.owner.restoreLeftovers(context.Background()) })
	})
	return nil
}

// restoreLeftovers puts back what a helper that died left (#9500): windows on a display that is gone and, for terminals
// and editors, the text size it raised, so the user's terminal never stays big. Only while no session runs, and a
// session starting waits for it (both hold starting): a window is never put back, and its text made smaller, twice. It
// runs when the node starts and after every session ends; with no records it does nothing.
func (p *phoneScreens) restoreLeftovers(ctx context.Context) {
	file, run := p.restoreFile, p.restoreRun
	if file == nil {
		file = phoneRestoreFile
	}
	if run == nil {
		run = phoneRestoreRun
	}
	if _, err := os.Stat(file()); err != nil {
		return
	}
	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	busy := p.cur != nil
	p.mu.Unlock()
	if busy {
		return // that session puts them back as it starts
	}
	helper, _ := inputHelpers.current(ctx, p.logf)
	if helper == "" {
		return
	}
	if v, permission := parsePhoneScreenPreflight(inputPermissions.raw(ctx, helper)); v < 1 || permission != "granted" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	err := run(ctx, helper)
	_, left := os.Stat(file())
	switch {
	case left != nil && err != nil:
		// `open -W` can't wait on a helper that has already finished (its records were for windows that are gone): the
		// records are gone, so it did its work. Not a failure.
		p.logf("phone screen: put back what a session left (nothing to wait for)")
	case err != nil:
		p.logf("phone screen: putting back what a session left: %v", err)
	case left == nil:
		p.logf("phone screen: some of what a session left is kept for later (an app that didn't answer)")
	default:
		p.logf("phone screen: put back what a session left")
	}
}

// errPhoneHelper is a helper that stopped answering or answered nonsense: the session is over.
var errPhoneHelper = errors.New("phone screen: the helper stopped answering")

// call sends one request and returns the helper's answer; an answer with ok false becomes an rpc.Error with the
// helper's code.
func (s *phoneSession) call(req map[string]any) (map[string]any, error) {
	s.calls.Lock()
	defer s.calls.Unlock()
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	op, _ := req["op"].(string)
	wait := phoneScreenTimeouts[op]
	if wait == 0 {
		wait = 15 * time.Second
	}
	_ = s.conn.SetDeadline(time.Now().Add(wait))
	defer func() { _ = s.conn.SetDeadline(time.Time{}) }()
	if _, err := s.conn.Write(append(line, '\n')); err != nil {
		_ = s.Close()
		return nil, errPhoneHelper
	}
	answer, err := s.rd.ReadBytes('\n')
	if err != nil {
		_ = s.Close()
		return nil, errPhoneHelper
	}
	var out map[string]any
	if json.Unmarshal(answer, &out) != nil {
		_ = s.Close()
		return nil, errPhoneHelper
	}
	if ok, _ := out["ok"].(bool); !ok {
		code, _ := out["error"].(string)
		msg, _ := out["message"].(string)
		if code == "" {
			code = "failed"
		}
		return nil, &rpc.Error{Code: code, Message: msg}
	}
	delete(out, "ok")
	return out, nil
}

// session is the pair's live session, nil when there is none.
func (p *phoneScreens) session(pair string) *phoneSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil && p.cur.pair == pair {
		return p.cur
	}
	return nil
}

// open is the pair's session, started if there is none: busy while another pair holds this Mac's.
func (p *phoneScreens) open(ctx context.Context, pair string) (*phoneSession, error) {
	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	cur := p.cur
	p.mu.Unlock()
	if cur != nil {
		if cur.pair == pair {
			return cur, nil
		}
		return nil, &rpc.Error{Code: "busy", Message: "another device shows this Mac as a phone screen"}
	}
	if !p.signed(pair) {
		return nil, &rpc.Error{Code: "not_signed_in", Message: "the paired screen-sharing stream has not signed in"}
	}
	helper, _ := inputHelpers.current(ctx, p.logf)
	if helper == "" {
		return nil, &rpc.Error{Code: "unavailable", Message: "Persona Input is not installed on this Mac"}
	}
	switch v, permission := parsePhoneScreenPreflight(inputPermissions.raw(ctx, helper)); {
	case v < 1:
		return nil, &rpc.Error{Code: "unavailable", Message: "this Mac's Persona Input has no phone screen; update Persona Link"}
	case permission != "granted":
		return nil, &rpc.Error{Code: "permission", Message: "Persona Input is not allowed to control the computer (Accessibility)"}
	}
	conn, granted, err := phoneStart(ctx, helper)
	if err != nil {
		p.logf("phone screen: %v", err)
		return nil, &rpc.Error{Code: "unavailable", Message: "Persona Input did not start"}
	}
	if !granted {
		_ = conn.Close()
		inputPermissions.forget()
		return nil, &rpc.Error{Code: "permission", Message: "Persona Input is not allowed to control the computer (Accessibility)"}
	}
	s := &phoneSession{owner: p, pair: pair, conn: conn, rd: bufio.NewReader(conn)}
	p.mu.Lock()
	p.cur = s
	p.mu.Unlock()
	if !p.attach(pair, s) { // the stream ended meanwhile
		_ = s.Close()
		return nil, &rpc.Error{Code: "not_signed_in", Message: "the paired screen-sharing stream has ended"}
	}
	return s, nil
}

// screenPhone answers screen.phone {pair, op, width, height, pid, window} with the helper's answer (display,
// displays, windows, placed). Errors: unavailable (no helper that can, or not a Mac), permission (no Accessibility),
// not_signed_in, busy (another pair's session), no_session (windows / place before start), and the helper's own
// (display, window_gone, not_responding, no_display, bad_request).
func (n *Node) screenPhone(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Pair   string  `json:"pair"`
		Op     string  `json:"op"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		PID    int32   `json:"pid"`
		Window uint32  `json:"window"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Pair == "" {
		return nil, &rpc.Error{Code: "bad_request", Message: "screen.phone: need pair and op"}
	}
	if n.phones == nil {
		return nil, &rpc.Error{Code: "unavailable", Message: "phone screen needs a Mac"}
	}
	switch p.Op {
	case "start":
		if p.Width <= 0 || p.Height <= 0 {
			return nil, &rpc.Error{Code: "bad_request", Message: "screen.phone start: need width and height"}
		}
		s, err := n.phones.open(ctx, p.Pair)
		if err != nil {
			return nil, err
		}
		out, err := s.call(map[string]any{"op": "start", "width": p.Width, "height": p.Height})
		if err != nil {
			_ = s.Close() // no display (or not the new shape): the helper puts back what it did and ends
		}
		return out, err
	case "windows", "place":
		s := n.phones.session(p.Pair)
		if s == nil {
			return nil, &rpc.Error{Code: "no_session", Message: "the phone screen is not on"}
		}
		req := map[string]any{"op": p.Op}
		if p.Op == "place" {
			if p.PID <= 0 || p.Window == 0 {
				return nil, &rpc.Error{Code: "bad_request", Message: "screen.phone place: need pid and window"}
			}
			req["pid"], req["window"] = p.PID, p.Window
		}
		return s.call(req)
	case "stop":
		if s := n.phones.session(p.Pair); s != nil {
			_, err := s.call(map[string]any{"op": "stop"})
			_ = s.Close()
			if err != nil && !errors.Is(err, errPhoneHelper) {
				n.logger.Printf("phone screen: stop: %v", err)
			}
		}
		return map[string]any{}, nil
	}
	return nil, &rpc.Error{Code: "bad_request", Message: "screen.phone: unknown op " + p.Op}
}
