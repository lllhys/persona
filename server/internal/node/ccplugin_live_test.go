//go:build cclive

package node

// Live end-to-end check of Persona's Claude Code plugin (integrations/claude-code-plugin): a real claude with the plugin
// loaded from this repository (--plugin-dir), this node's socket served under a scratch PERSONA_HOME, and one message
// handed to the plugin as the owner's. It checks the parts a fake cannot: that the plugin loads and polls, that the
// node tells the claude process at the other end of the socket on this system, that the prompt goes in as the person's
// words (the transcript's origin), and that the node sees it there.
//
// Never run by CI: it needs the cclive build tag and CCLIVE=1, and it runs one real turn on whatever this machine's
// Claude Code is set up for. The session is a throwaway one in a scratch directory (its transcript is removed after);
// no session of the user's is touched. Variables a parent Claude Code session exports are dropped, so it can run from
// inside one.
//
//	cd server && CCLIVE=1 [CCLIVE_CLAUDE=<claude executable>] go test -tags cclive -run TestCCPluginLive -v -count=1 ./internal/node

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/acceptloop"
)

func TestCCPluginLive(t *testing.T) {
	if os.Getenv("CCLIVE") != "1" {
		t.Skip("set CCLIVE=1 (and CCLIVE_CLAUDE to a claude executable when it is not on PATH) to run one real turn")
	}
	claude := os.Getenv("CCLIVE_CLAUDE")
	if claude == "" {
		var err error
		if claude, err = exec.LookPath("claude"); err != nil {
			t.Skip("no claude on PATH")
		}
	}
	plugin, err := filepath.Abs(filepath.Join("..", "..", "..", "integrations", "claude-code-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	n, _ := testNode(t)
	n.home = home // the machine's Claude Code directory: the claude's sessions/<pid>.json and its transcript
	oldServes := ccPluginServes
	ccPluginServes = func(*Node, claudeProc) bool { return true } // a stream-json claude names an SDK's entrypoint
	t.Cleanup(func() { ccPluginServes = oldServes })

	scratch, err := os.MkdirTemp("", "ccl") // short: a socket path stays under ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch) })
	work := filepath.Join(scratch, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(scratch, "node.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ln.Close() })
	go acceptloop.Run(ctx, ln, t.Logf, func(c net.Conn) { go n.serveSock(ctx, c) })

	cmd := exec.Command(claude, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--plugin-dir", plugin)
	cmd.Dir = work
	cmd.Env = append(childEnv(os.Environ()), "PERSONA_HOME="+scratch)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	replies := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var m struct {
				Type   string `json:"type"`
				Result string `json:"result"`
			}
			if json.Unmarshal(sc.Bytes(), &m) == nil && m.Type == "result" {
				replies <- m.Result
			}
		}
		close(replies)
	}()

	var sid string
	var pid int
	waitWithin(t, 60*time.Second, "the plugin's first poll", func() bool {
		n.ccPlugins.mu.Lock()
		defer n.ccPlugins.mu.Unlock()
		for id, s := range n.ccPlugins.sessions {
			if s.poll != nil {
				sid, pid = id, s.pid
				return true
			}
		}
		return false
	})
	c, ok := n.liveClaudeOf(pid)
	if !ok || c.SessionID != sid {
		t.Fatalf("the poll came from pid %d, which has %q open, not %s", pid, c.SessionID, sid)
	}
	t.Logf("claude %d (pid %d) polls for %s, plugin %s on Claude Code %s", cmd.Process.Pid, pid, sid,
		n.ccPlugins.sessions[sid].version, n.ccPlugins.sessions[sid].engine)
	t.Cleanup(func() {
		if p := n.transcriptPath(sid); p != "" {
			_ = os.Remove(p)
			_ = os.Remove(filepath.Dir(p)) // the scratch directory's project folder, when nothing else is in it
		}
	})
	c.Status = "idle" // a stream-json claude before its first prompt: at rest

	const text = "Reply with the single word PONG and nothing else."
	took, why := n.ccPluginSend(context.Background(), c, sid, text)
	if !took {
		t.Fatalf("the plugin did not take the message: %s", why)
	}
	select {
	case r := <-replies:
		if !strings.Contains(strings.ToUpper(r), "PONG") {
			t.Errorf("the turn answered %q", r)
		}
	case <-time.After(3 * time.Minute):
		t.Fatal("no answer to the message within 3 minutes")
	}
	path := n.transcriptPath(sid)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range strings.Split(string(b), "\n") {
		var e struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Origin map[string]any `json:"origin"`
		}
		if json.Unmarshal([]byte(l), &e) != nil || e.Type != "user" {
			continue
		}
		var said string
		_ = json.Unmarshal(e.Message.Content, &said)
		if said == text {
			found = true
			t.Logf("transcript line: origin %v, isMeta %v", e.Origin, e.IsMeta)
			if e.IsMeta || e.Origin["kind"] != "plugin" || e.Origin["name"] != "persona" || e.Origin["asUser"] != true {
				t.Errorf("the prompt went in as %v (isMeta %v), not as the person's words", e.Origin, e.IsMeta)
			}
		}
	}
	if !found {
		t.Errorf("no user line with the message in %s", path)
	}
}

// childEnv is env without what a parent Claude Code session exports to the commands it runs (its own session, its
// host's provider): the claude started here runs as one started from a plain terminal.
func childEnv(env []string) []string {
	hostProvider := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=") {
			hostProvider = true
		}
	}
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "CLAUDE_CODE_"), strings.HasPrefix(name, "CLAUDE_AGENT_SDK"), name == "CLAUDECODE",
			name == "CLAUDE_PID", name == "CLAUDE_EFFORT", hostProvider && strings.HasPrefix(name, "ANTHROPIC_"):
			continue
		}
		out = append(out, kv)
	}
	return out
}
