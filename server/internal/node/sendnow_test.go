package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A Claude Code screen with a message queued, as 2.1.319 draws it (#6088; the words made up).
const queuedScreen = `⏺ Bash(go test ./...)
  ⎿  Running…

✻ Waddling… (12s · ↓ 120 tokens)

❯ Also run the linter at the end.   ctrl+x ctrl+s to send now
────────────────────────────────────────
  Press up to edit queued messages · esc to interrupt`

// The same as 2.1.311 draws it in a herdr pane (#2467, read off a real one): the hint names ctrl+enter, on the line
// under the message.
const queuedScreenCtrlEnter = `  Running build.sh in the foreground · 10s
  ⎿  $ ./build.sh (10s)
     (ctrl+b to run in background)

❯ Also say the word pineapple at the end.
  ctrl+enter to send now

· Tinkering… (13s · ↓ 286 tokens)

─────────────────────────────────────────────────────────────────────────────────
❯ Press up to edit queued messages
─────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt`

// A Claude Code that labels the hint under its message "Queued ·" (2.1.326; testdata has one read off a herdr pane), as
// one drew it fullscreen in a 93×21 terminal (2026-11-17, from the report): the node refused it as unrecognized and
// pressed nothing.
const queuedScreenQueuedLabel = `❯ 为啥要重建 es?
  ⎿  Queued · ctrl+enter to send now
· Waddling… (20m 25s · ↓ 76.8k tokens)
────────
❯ Press up to edit queued messages
────────
  Opus 5.5 (xhigh) … $17.32
  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt`

// A Claude Code whose send-now names keys a terminal can't press (made up: cmd is the Mac's command key).
const queuedScreenOtherKeys = `❯ Also run the linter at the end.
  cmd+enter to send now

✻ Waddling… (12s · ↓ 120 tokens)
────────────────────────────────────────
❯ Press up to edit queued messages`

// claudeScreen is a Claude Code screen read off a real pane (testdata/claude-<version>-<what>.txt).
func claudeScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude-"+name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A message queued with no send-now hint beside it at all.
const queuedScreenNoHint = `❯ Also run the linter at the end.

✻ Waddling… (12s · ↓ 120 tokens)
────────────────────────────────────────
❯ Press up to edit queued messages`

const idleScreen = `⏺ All tests pass.

❯
────────────────────────────────────────
  ? for shortcuts`

// queueHost shows its scrolled screen, if any, until ctrl+end is pressed, then its queued screen (queuedScreen when
// none is given) until a send-now key is pressed (unless stuck), then the screen with nothing queued.
type queueHost struct {
	bareHost
	mu       sync.Mutex
	scrolled string
	atEnd    bool
	queued   string
	sent     bool
	stuck    bool
	first    string
}

func (h *queueHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.first != "" {
		return h.first, nil
	}
	if h.scrolled != "" && !h.atEnd {
		return h.scrolled, nil
	}
	if h.sent && !h.stuck {
		return idleScreen, nil
	}
	if h.queued != "" {
		return h.queued, nil
	}
	return queuedScreen, nil
}

func (h *queueHost) SendKeys(ctx context.Context, p host.PaneRef, keys ...string) error {
	h.mu.Lock()
	for _, k := range keys {
		if k == "ctrl+end" {
			h.atEnd = true
		} else {
			h.sent = true
		}
	}
	h.mu.Unlock()
	return h.bareHost.SendKeys(ctx, p, keys...)
}

func codeOf(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// Send now presses Claude Code's own send-now only while its screen offers it, the keys its hint names, and is done
// when the queue is gone.
func TestSendQueuedNow(t *testing.T) {
	defer func(d time.Duration) { sendNowWait = d }(sendNowWait)
	sendNowWait = 600 * time.Millisecond
	node := func(h *queueHost, agent string) *Node {
		return &Node{hosts: host.NewRegistry(h), agents: map[string]host.Agent{"s1": {Agent: agent, PaneID: "orca:t1:l1"}}, paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
	}

	for screen, want := range map[string]string{
		queuedScreen:                              "ctrl+x ctrl+s",
		queuedScreenCtrlEnter:                     "ctrl+enter",
		queuedScreenQueuedLabel:                   "ctrl+enter",
		claudeScreen(t, "2.1.321-send-now-herdr"): "ctrl+enter",
		claudeScreen(t, "2.1.321-send-now-tmux"):  "ctrl+x ctrl+s",
		claudeScreen(t, "2.1.326-send-now-herdr"): "ctrl+enter",
		// another binding, as the hint names it: pressed as named
		"❯ Also run the linter.\n  ctrl+x ctrl+q to send now\n❯ Press up to edit queued messages": "ctrl+x ctrl+q",
	} {
		h := &queueHost{bareHost: bareHost{kind: host.Orca}, queued: screen}
		if err := node(h, "claude").sendQueuedNow(context.Background(), "s1"); err != nil {
			t.Fatalf("send now with %q: %v", want, err)
		}
		if got := strings.Join(h.keys, " "); got != want {
			t.Fatalf("keys %q, want the hint's %q", got, want)
		}
	}

	// nothing queued on the screen (the queue was taken a moment ago): no key at all
	h := &queueHost{bareHost: bareHost{kind: host.Orca}, first: idleScreen}
	if err := node(h, "claude").sendQueuedNow(context.Background(), "s1"); codeOf(err) != "nothing_queued" || len(h.keys) != 0 {
		t.Fatalf("nothing queued: %v, keys %q", err, h.keys)
	}

	// not a Claude: refused before looking
	h = &queueHost{bareHost: bareHost{kind: host.Orca}}
	if err := node(h, "codex").sendQueuedNow(context.Background(), "s1"); codeOf(err) != "send_now_unsupported" || len(h.keys) != 0 {
		t.Fatalf("codex: %v, keys %q", err, h.keys)
	}

	// the key went in and the queue stays on the screen: in doubt, delivery unknown
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, stuck: true}
	err := node(h, "claude").sendQueuedNow(context.Background(), "s1")
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "send_now_in_doubt" || !strings.Contains(string(re.Data), `"delivered":"unknown"`) {
		t.Fatalf("stuck: %v", err)
	}
}

// A queue whose send-now hint Persona can't press is refused as unrecognized, with nothing pressed and no word of
// updating Claude Code: a hint naming keys a terminal can't send, or only a click, is quoted, and a queue the
// transcript has with no hint on the screen is refused the same way.
func TestSendQueuedNowUnrecognized(t *testing.T) {
	node := func(h *queueHost) *Node {
		return &Node{hosts: host.NewRegistry(h), agents: map[string]host.Agent{"s1": {Agent: "claude", PaneID: "orca:t1:l1"}}, paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
	}

	h := &queueHost{bareHost: bareHost{kind: host.Orca}, first: queuedScreenOtherKeys}
	err := node(h).sendQueuedNow(context.Background(), "s1")
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "send_now_unrecognized" || len(h.keys) != 0 {
		t.Fatalf("other keys: %v, keys %q", err, h.keys)
	}
	if !strings.Contains(re.Message, `"cmd+enter to send now"`) || strings.Contains(re.Message, "update") {
		t.Fatalf("message %q should quote the hint and not ask for an update", re.Message)
	}
	if !strings.Contains(string(re.Data), `"delivered":"none"`) {
		t.Fatalf("data %s", re.Data)
	}

	h = &queueHost{bareHost: bareHost{kind: host.Orca}, first: "❯ Also run the linter at the end.\n  click to send now"}
	err = node(h).sendQueuedNow(context.Background(), "s1")
	if !errors.As(err, &re) || re.Code != "send_now_unrecognized" || len(h.keys) != 0 || !strings.Contains(re.Message, `"click to send now"`) {
		t.Fatalf("a click: %v, keys %q", err, h.keys)
	}

	h = &queueHost{bareHost: bareHost{kind: host.Orca}, first: queuedScreenNoHint}
	n := node(h)
	p := filepath.Join(t.TempDir(), "s1.jsonl")
	writeLines(t, p, []string{`{"type":"queue-operation","operation":"enqueue","content":"Also run the linter at the end."}`})
	n.paths["s1"] = p
	err = n.sendQueuedNow(context.Background(), "s1")
	if !errors.As(err, &re) || re.Code != "send_now_unrecognized" || len(h.keys) != 0 || strings.Contains(re.Message, "update") {
		t.Fatalf("no hint, queued in the transcript: %v, keys %q", err, h.keys)
	}

	// the same screen with the transcript's queue empty: nothing queued, not unrecognized
	writeLines(t, p, []string{`{"type":"queue-operation","operation":"enqueue","content":"x"}`, `{"type":"queue-operation","operation":"remove","content":"x"}`})
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, first: queuedScreenNoHint}
	n = node(h)
	n.paths["s1"] = p
	if err := n.sendQueuedNow(context.Background(), "s1"); codeOf(err) != "nothing_queued" || len(h.keys) != 0 {
		t.Fatalf("no hint, nothing in the transcript: %v, keys %q", err, h.keys)
	}
}

// A fullscreen Claude Code scrolled up has its queued message and hint off screen (#4537, the screens read off
// 2.1.321): the node scrolls back to the end with Claude Code's own key, finds the hint there and presses its keys.
// When the end shows no hint either, nothing but the scroll key went in.
func TestSendQueuedNowScrolledUp(t *testing.T) {
	defer func(d, s time.Duration) { sendNowWait, sendNowScrollWait = d, s }(sendNowWait, sendNowScrollWait)
	sendNowWait, sendNowScrollWait = 600*time.Millisecond, 300*time.Millisecond
	node := func(h *queueHost) *Node {
		return &Node{hosts: host.NewRegistry(h), agents: map[string]host.Agent{"s1": {Agent: "claude", PaneID: "orca:t1:l1"}}, paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
	}
	scrolled := claudeScreen(t, "2.1.321-send-now-scrolled")
	if hint, _ := sendNowOffer(scrolled); hint != "" {
		t.Fatalf("the scrolled screen has no hint, got %q", hint)
	}

	h := &queueHost{bareHost: bareHost{kind: host.Orca}, scrolled: scrolled, queued: claudeScreen(t, "2.1.321-send-now-back-at-end")}
	if err := node(h).sendQueuedNow(context.Background(), "s1"); err != nil {
		t.Fatalf("scrolled up: %v", err)
	}
	if got := strings.Join(h.keys, " "); got != "ctrl+end ctrl+enter" {
		t.Fatalf("keys %q, want the scroll back, then the hint's ctrl+enter", got)
	}

	// macOS names no key in the pill: Claude Code's own ctrl+end all the same
	mac := strings.Replace(scrolled, "Jump to bottom (ctrl+End) ↓", "Jump to bottom (click) ↓", 1)
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, scrolled: mac, queued: claudeScreen(t, "2.1.321-send-now-back-at-end")}
	if err := node(h).sendQueuedNow(context.Background(), "s1"); err != nil || strings.Join(h.keys, " ") != "ctrl+end ctrl+enter" {
		t.Fatalf("macOS pill: %v, keys %q", err, h.keys)
	}

	// older output scrolled into view quotes a hint: not the queue's, the one at the end is pressed
	quoting := strings.Replace(scrolled, "  Ran 1 shell command", "  ⎿  ctrl+x ctrl+q to send now", 1)
	if hint, _ := sendNowOffer(quoting); hint != "ctrl+x ctrl+q to send now" {
		t.Fatalf("the quoting screen should hold a hint-shaped line, got %q", hint)
	}
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, scrolled: quoting, queued: claudeScreen(t, "2.1.321-send-now-back-at-end")}
	if err := node(h).sendQueuedNow(context.Background(), "s1"); err != nil || strings.Join(h.keys, " ") != "ctrl+end ctrl+enter" {
		t.Fatalf("a quoted hint above: %v, keys %q", err, h.keys)
	}

	// still no hint at the end, with the queue in the transcript: refused, only the scroll key pressed
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, first: scrolled}
	n := node(h)
	p := filepath.Join(t.TempDir(), "s1.jsonl")
	writeLines(t, p, []string{`{"type":"queue-operation","operation":"enqueue","content":"third queued probe: reply THIRD-OK"}`})
	n.paths["s1"] = p
	if err := n.sendQueuedNow(context.Background(), "s1"); codeOf(err) != "send_now_unrecognized" || strings.Join(h.keys, " ") != "ctrl+end" {
		t.Fatalf("no hint at the end: %v, keys %q", err, h.keys)
	}
}

func TestScrolledUp(t *testing.T) {
	for screen, want := range map[string]string{
		"✻ Brewed for 7s                Jump to bottom (ctrl+End) ↓": "ctrl+end",
		"✻ Brewed for 7s                Jump to bottom (click) ↓":    "ctrl+end",
		"✻ Brewed for 7s     Jump to bottom: fn+↓ to scroll":         "ctrl+end",
		"✻ Brewed for 7s     3 new messages (ctrl+End) ↓":            "ctrl+end",
		"✻ Brewed for 7s     Jump to bottom (ctrl+g) ↓":              "ctrl+g", // a rebound scroll:bottom
	} {
		if keys, ok := scrolledUp(screen); !ok || strings.Join(keys, " ") != want {
			t.Errorf("%q: %q %v, want %q", screen, keys, ok, want)
		}
	}
	for _, screen := range []string{idleScreen, queuedScreenCtrlEnter, "● Scroll down to jump to bottom."} {
		if keys, ok := scrolledUp(screen); ok {
			t.Errorf("not scrolled up: %q gave %q", screen, keys)
		}
	}
}

func TestSendNowOffer(t *testing.T) {
	for screen, want := range map[string]string{
		queuedScreen:          "ctrl+x ctrl+s",
		queuedScreenCtrlEnter: "ctrl+enter",
		"❯ x\n   ctrl+x   ctrl+s  to send   now": "ctrl+x ctrl+s", // spacing as the terminal lays it out
		"❯ x\n  ctrl+enter\n  to send now":       "ctrl+enter",
		// the same keys, worded or cased otherwise
		"❯ x\n  Ctrl+Enter to send now":                 "ctrl+enter",
		"❯ x\n  (ctrl+enter to send now)":               "ctrl+enter",
		"❯ x\n  ctrl+enter · send now":                  "ctrl+enter",
		"❯ x\n  ctrl+enter: send now":                   "ctrl+enter",
		"❯ x\n  ctrl+enter send now":                    "ctrl+enter",
		"❯ x\n  ⌃⏎ to send now":                         "ctrl+enter",
		"❯ x\n  ^X ^S to send now":                      "ctrl+x ctrl+s",
		"❯ x\n  opt+enter to send now":                  "alt+enter",
		"❯ x\n  ctrl+shift+s to send now":               "ctrl+shift+s",
		"❯ x\n  ctrl+enter to send now · esc to cancel": "ctrl+enter",
		// the message ends in key names: only the hint's own keys
		"❯ stop it with ctrl+c   ctrl+enter to send now":  "ctrl+enter",
		"❯ stop it with ctrl+c\n  ctrl+enter to send now": "ctrl+enter",
		// output above quotes an older hint: the lowest is the one beside the queue
		"  ⎿  ctrl+x ctrl+s to send now\n❯ x\n  ctrl+enter to send now": "ctrl+enter",
		// a label before the keys (2.1.326), whatever it says or binds
		queuedScreenQueuedLabel:                                  "ctrl+enter",
		"❯ x\n  ⎿  Queued · ctrl+x ctrl+s to send now":           "ctrl+x ctrl+s",
		"❯ x\n  ⎿  Queued · Ctrl+Enter to send now":              "ctrl+enter",
		"❯ x\n  ⎿  Queued • ctrl+enter to send now":              "ctrl+enter",
		"❯ x\n  ⎿  Waiting · ⌃⏎ to send now":                     "ctrl+enter",
		"❯ x\n∷ Queued · ctrl+enter to send now":                 "ctrl+enter",
		"❯ x\n  ⎿  Queued · ctrl+enter to send now  ":            "ctrl+enter",
		"❯ fix the ctrl+c\n  ⎿  Queued · ctrl+enter to send now": "ctrl+enter",
	} {
		if _, keys := sendNowOffer(screen); strings.Join(keys, " ") != want {
			t.Errorf("keys %q, want %q in\n%s", keys, want, screen)
		}
	}
	for screen, want := range map[string]string{
		queuedScreenOtherKeys:                      "cmd+enter to send now",
		"❯ x\n  click to send now":                 "click to send now",
		"❯ x\n  fn+enter to send now":              "fn+enter to send now",
		"❯ x\n  ctrl+x cmd+s to send now":          "ctrl+x cmd+s to send now",
		"❯ x\n  ⎿  Queued · click to send now":     "click to send now",
		"❯ x\n  ⎿  Queued · cmd+enter to send now": "cmd+enter to send now",
	} {
		if hint, keys := sendNowOffer(screen); hint != want || keys != nil {
			t.Errorf("unpressable: %q %q, want %q and no keys", hint, keys, want)
		}
	}
	for _, screen := range []string{idleScreen, queuedScreenNoHint, "⏺ I am going to send now.",
		"● Press ctrl+enter to send now", "● Use ctrl+enter to send now.", "❯ x\n  to send now", "❯ x\n  the end to send now",
		// a label with no keys after it, or words between the separator and the keys
		"❯ x\n  ⎿  Queued · to send now", "● Done · press ctrl+enter to send now"} {
		if hint, keys := sendNowOffer(screen); hint != "" || keys != nil {
			t.Errorf("no hint in %q: %q %q", screen, hint, keys)
		}
	}
	if !offersSendNow(queuedScreen) || !offersSendNow(queuedScreenCtrlEnter) || !offersSendNow(queuedScreenQueuedLabel) ||
		offersSendNow(idleScreen) || offersSendNow(queuedScreenOtherKeys) {
		t.Fatal("the hint is what tells a queued message Persona can send")
	}
}
