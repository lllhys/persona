package node

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// What Paseo 0.15.4 titles an agent with from its first prompt, as its create-agent-title.js answers for each (a lone
// surrogate half, which JavaScript keeps when its cut splits a pair, reaches Go as U+FFFD).
func TestPaseoPromptTitle(t *testing.T) {
	a := strings.Repeat
	for _, c := range []struct{ prompt, want string }{
		{"fix the build", "fix the build"},
		{"  \n\n  fix  the\tbuild  \n second line", "fix the build"},
		{`<persona-context kind="context" title="Project · App &quot;1.3&quot; · Background" url="">` + "\nbody\n</persona-context>\n\nfix it",
			`<persona-context kind="context" title="Project · App &quot;1`},
		{a("a", 59) + " b", a("a", 59)},
		{a("a", 60) + " b", a("a", 60)},
		{a("a", 59) + "\U0001F600tail", a("a", 59) + "\uFFFD"},
		{a("a", 58) + " \U0001F600b", a("a", 58) + " \uFFFD"},
		{a("修", 70), a("修", 60)},
		{"x\u3000 y\u0085z", "x y\u0085z"}, // JavaScript's white space is Go's but U+0085, with U+FEFF
		{"\uFEFFhello", "hello"},
		{"\r\n\r\nwindows line\r\nnext", "windows line"},
		{"   ", ""},
		{"", ""},
	} {
		if got := paseoPromptTitle(c.prompt); got != c.want {
			t.Errorf("%q: %q, want %q", c.prompt, got, c.want)
		}
	}
}

// A Paseo session's title is Paseo's word (#8133): it counts as the session's first prompt (title_source "prompt",
// which Persona's naming may replace) only when it is what Paseo makes of that prompt, taken as it was sent or as the
// node reads it, or the folder name the bridge stands in with. A title given in Paseo (a rename, the title another agent
// created it with) stays of unknown provenance, which nothing replaces.
func TestRuntimeTitleIsThePromptOnlyWhenPaseoDerivedIt(t *testing.T) {
	n, _ := runtimeNode(t)
	t.Cleanup(freshOffline)
	transcript := func(id, prompt string) {
		content, _ := json.Marshal(prompt)
		writeTranscript(t, n.home, "/w/app", id, time.Now().Add(-time.Minute),
			`{"type":"user","uuid":"u1","timestamp":"2026-11-04T10:00:00Z","cwd":"/w/app","message":{"role":"user","content":`+string(content)+`}}`,
			`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T10:00:05Z","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"done"}]}}`,
			strings.Repeat(" ", 4096))
	}
	ids := map[string]string{"derived": "eeeeeeee-0000-4000-8000-000000000001", "context": "eeeeeeee-0000-4000-8000-000000000002",
		"given": "eeeeeeee-0000-4000-8000-000000000003", "folder": "eeeeeeee-0000-4000-8000-000000000004"}
	transcript(ids["derived"], "  fix  the\tlogin page\nand its tests")
	transcript(ids["context"], `<persona-context kind="context" title="Project">background</persona-context>`+"\n\nfix the login page")
	transcript(ids["given"], "fix the login page")
	transcript(ids["folder"], "fix the login page")
	titles := map[string]string{"derived": "fix the login page", "context": `<persona-context kind="context" title="Project">background</`,
		"given": "Login page fix", "folder": "app"}
	var rows []map[string]any
	for name, id := range ids {
		rows = append(rows, map[string]any{"session_id": "paseo-" + name, "transcript": id, "agent": "claude", "title": titles[name],
			"cwd": "/w/app", "status": "idle"})
	}
	host := dialRuntime(t, n, paseoHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": rows})
	want := map[string]proto.TitleSource{"derived": proto.TitleSourcePrompt, "context": proto.TitleSourcePrompt,
		"given": proto.TitleSourceUnknown, "folder": proto.TitleSourcePrompt}
	var differs string
	defer func() {
		if t.Failed() {
			t.Log(differs)
		}
	}()
	waitFor(t, "each row's title and where it came from", func() bool {
		freshOffline()
		got := listed(t, n)
		for name, source := range want {
			if s := got["paseo-"+name]; s.TitleSource != source || s.Title != titles[name] {
				differs = fmt.Sprintf("%s: %q from %q, want %q", name, s.Title, s.TitleSource, source)
				return false
			}
		}
		return true
	})
}
