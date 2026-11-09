package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/transcript"
)

// usageLine is an assistant reply of a Claude Code transcript with the given usage.
func usageLine(t *testing.T, id string, input int, sidechain bool, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": "assistant", "isSidechain": sidechain, "uuid": id,
		"message": map[string]any{"id": "msg_" + id, "role": "assistant", "model": "claude-opus-5-5",
			"content": []map[string]any{{"type": "text", "text": text}},
			"usage": map[string]any{"input_tokens": input, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 25,
				"iterations": []map[string]any{{"input_tokens": input}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// A transcript the way a gateway left it (#3300): zeros on the turn's replies, the turn's sum on the last one, then
// Claude Code's own refusals (synthetic, usage 0).
func gatewayTranscript(t *testing.T) string {
	user := `{"type":"user","message":{"role":"user","content":"整理一下这周的事"}}` + "\n"
	refusal := `{"type":"assistant","isApiErrorMessage":true,"message":{"model":"<synthetic>","role":"assistant","usage":{"input_tokens":0,"output_tokens":0},"content":[{"type":"text","text":"Prompt is too long · automatic compaction failed: no assistant message in summarization response"}]}}` + "\n"
	return user +
		usageLine(t, "a1", 0, false, "先查一下") +
		usageLine(t, "s1", 1_706_464, true, "a subagent's own") + // not the conversation's context: left alone
		usageLine(t, "a2", 1_706_464, false, `quoted: \"input_tokens\": 9999999`) +
		user + refusal
}

func TestRepairUsageZeroesTheImpossibleInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	orig := gatewayTranscript(t)
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := repairUsage(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.lines != 1 || r.largest != 1_706_464 || r.end != int64(len(orig)) {
		t.Fatalf("repair = %+v", r)
	}
	if b, _ := os.ReadFile(r.backup); string(b) != orig {
		t.Fatalf("the backup is not the file as it was")
	}
	if st, _ := os.Stat(r.backup); st.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v", st.Mode().Perm())
	}
	got, _ := os.ReadFile(path)
	if len(got) != len(orig) {
		t.Fatalf("size changed: %d -> %d", len(orig), len(got))
	}
	ol, gl := strings.Split(orig, "\n"), strings.Split(string(got), "\n")
	for i := range ol {
		if i != 3 && ol[i] != gl[i] {
			t.Errorf("line %d changed:\n%s\n%s", i+1, ol[i], gl[i])
		}
	}
	var e struct {
		Message struct {
			Content []struct{ Text string } `json:"content"`
			Usage   struct {
				Input      int `json:"input_tokens"`
				Iterations []struct {
					Input int `json:"input_tokens"`
				} `json:"iterations"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(gl[3]), &e); err != nil {
		t.Fatalf("repaired line is not JSON: %v\n%s", err, gl[3])
	}
	if e.Message.Usage.Input != 0 || e.Message.Usage.Iterations[0].Input != 0 {
		t.Errorf("usage left: %+v", e.Message.Usage)
	}
	if e.Message.Content[0].Text != `quoted: \"input_tokens\": 9999999` { // text that looks like usage stays as it was
		t.Errorf("text changed: %q", e.Message.Content[0].Text)
	}
	if r, err := repairUsage(path, 0); err != nil || r.lines != 0 || r.backup != "" {
		t.Errorf("second repair = %+v, %v", r, err)
	}
}

// A check starts where the last one ended; a line being written is left for the next one.
func TestRepairUsageGoesOnFromTheLastCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	first := usageLine(t, "a1", 1_706_464, false, "x")
	_ = os.WriteFile(path, []byte(first), 0o600)
	r, _ := repairUsage(path, 0)
	if r.lines != 1 {
		t.Fatalf("first = %+v", r)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	half := usageLine(t, "a3", 2_000_000, false, "z")
	_, _ = f.WriteString(usageLine(t, "a2", 3_000_000, false, "y") + half[:20])
	f.Close()
	r2, err := repairUsage(path, r.end)
	if err != nil || r2.lines != 1 || r2.largest != 3_000_000 {
		t.Fatalf("second = %+v, %v", r2, err)
	}
	if st, _ := os.Stat(path); r2.end != st.Size()-20 {
		t.Errorf("end %d: the half line should be read again", r2.end)
	}
	if r3, _ := repairUsage(path, 7); r3.lines != 0 || r3.end != r2.end { // not a line start: from the top, nothing left
		t.Errorf("third = %+v", r3)
	}
}

// What a conversation can really hold stays: under the 1M window, and no more than its transcript plus what a
// request brings along.
func TestPlausibleUsageIsKept(t *testing.T) {
	big := strings.Repeat("字", 300_000) // 900 KB of text
	data := usageLine(t, "a1", 950_000, false, big) + usageLine(t, "a2", 180_000, false, "short")
	if fx, _, _ := implausibleUsage(strings.NewReader(data), 0, int64(len(data))); len(fx) != 0 {
		t.Errorf("flagged %d plausible replies", len(fx))
	}
	small := usageLine(t, "a1", 600_000, false, "short") // 600k tokens from a 400-byte transcript
	if fx, _, _ := implausibleUsage(strings.NewReader(small), 0, int64(len(small))); len(fx) != 1 {
		t.Errorf("a context larger than its transcript was kept")
	}
}

// replyLine is an assistant reply whose usage has one count, input tokens, so a long text puts it where a test wants.
func replyLine(t *testing.T, id string, input int, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": "assistant", "uuid": id, "message": map[string]any{"role": "assistant",
		"content": []map[string]any{{"type": "text", "text": text}}, "usage": map[string]any{"input_tokens": input}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// A line longer than the read buffer is read in pieces: a usage split between two of them is found and fixed in
// place, a long line without one is passed over, and a long line still being written is left for the next check.
func TestRepairUsageReadsLinesLongerThanTheBuffer(t *testing.T) {
	head := usageLine(t, "a0", 0, false, "short")
	split := replyLine(t, "a1", 1_706_464, "")
	at := usageHintRe.FindIndex([]byte(split))[0]
	split = replyLine(t, "a1", 1_706_464, strings.Repeat("x", usageReadSize-10-at)) // the first read ends inside the usage
	if i := usageHintRe.FindIndex([]byte(split)); i[0] != usageReadSize-10 || i[1] <= usageReadSize {
		t.Fatalf("usage at %v, want across %d", i, usageReadSize)
	}
	plain := replyLine(t, "a2", 120, strings.Repeat("y", 3*usageReadSize))
	tail := replyLine(t, "a3", 2_000_000, "z")
	partial := replyLine(t, "a4", 3_000_000, strings.Repeat("w", usageReadSize))[:usageReadSize+100]
	orig := head + split + plain + tail + partial
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := repairUsage(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(len(orig) - len(partial)); r.lines != 2 || r.largest != 2_000_000 || r.end != want {
		t.Fatalf("repair = %+v, want 2 lines up to 2000000 and end %d", r, want)
	}
	got, _ := os.ReadFile(path)
	if len(got) != len(orig) {
		t.Fatalf("size changed: %d -> %d", len(orig), len(got))
	}
	lines := strings.SplitAfter(string(got), "\n")
	if lines[0] != head || lines[2] != plain || lines[4] != partial {
		t.Error("a line without an impossible usage changed")
	}
	for _, i := range []int{1, 3} {
		var e struct {
			Message struct {
				Usage struct {
					Input int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &e); err != nil || e.Message.Usage.Input != 0 {
			t.Errorf("line %d: usage %d, %v", i+1, e.Message.Usage.Input, err)
		}
	}
}

// A copy that fails is removed: it is no backup, and on a full disk it holds the space.
func TestBackupCopyRemovesAFailedCopy(t *testing.T) {
	dir := t.TempDir()
	src, err := os.OpenFile(filepath.Join(dir, "s.jsonl"), os.O_CREATE|os.O_WRONLY, 0o600) // write-only: reads fail
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.WriteString(usageLine(t, "a1", 1_706_464, false, "x")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "s.jsonl.usage-backup-1")
	if err := backupCopy(src, dst, 100); err == nil {
		t.Fatal("copied from a file that cannot be read")
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the failed copy is left: %v", err)
	}
}

// longTranscript is a long conversation's transcript of about size bytes: tool results, and replies whose usage has six
// digits but is plausible, so each of them is decoded and none is fixed.
func longTranscript(tb testing.TB, size int) string {
	tb.Helper()
	result := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"` +
		strings.Repeat("x", 4000) + `"}]}}` + "\n"
	reply := `{"type":"assistant","isSidechain":false,"message":{"id":"msg_1","role":"assistant","content":[{"type":"tool_use",` +
		`"id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":3,"cache_creation_input_tokens":120,` +
		`"cache_read_input_tokens":150000,"output_tokens":25}}}` + "\n"
	path := filepath.Join(tb.TempDir(), "s.jsonl")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for n := 0; n < size; n += len(result) + len(reply) {
		_, _ = w.WriteString(result)
		_, _ = w.WriteString(reply)
	}
	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}
	return path
}

// usageRaceEnabled: built with -race, whose sync.Pool drops a quarter of what is put back, so a line's regexp match
// can allocate a matcher of its own (assistant_usage_race_test.go).
var usageRaceEnabled bool

// A process's first check reads the whole transcript a buffer at a time: a long conversation's 32 MB costs about the
// buffer, where it held all of it before (as much heap as the file is large, on each conversation's restart).
func TestRepairUsageStreamsTheTranscript(t *testing.T) {
	if usageRaceEnabled {
		t.Skip("-race allocates a regexp matcher per line on its own")
	}
	path := longTranscript(t, 32<<20)
	per := bytesPerRun(1, func() {
		if r, err := repairUsage(path, 0); err != nil || r.lines != 0 {
			t.Errorf("repair = %+v, %v", r, err)
		}
	})
	if per > 1<<20 {
		t.Errorf("checking a 32 MB transcript allocates %.0f bytes, want under %d", per, 1<<20)
	}
}

// BenchmarkRepairUsage is a process's first check of a long conversation's transcript, 64 MB.
func BenchmarkRepairUsage(b *testing.B) {
	path := longTranscript(b, 64<<20)
	b.ReportAllocs()
	for b.Loop() {
		if r, err := repairUsage(path, 0); err != nil || r.lines != 0 {
			b.Fatalf("repair = %+v, %v", r, err)
		}
	}
}

// The end of a turn refused as too long because of a gateway's number: the transcript is repaired, the process let go
// and the message sent again — once, however often it happens.
func TestCheckTurnUsageRetriesARefusedMessageOnce(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-000000000002"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	_ = os.WriteFile(path, []byte(gatewayTranscript(t)), 0o600)
	n.paths[id] = path
	var sent []string
	usageResend = func(_ context.Context, _ *Node, sid, cwd, launcher, text string, _ modelExecution) error {
		sent = append(sent, sid+" "+cwd+" "+launcher+" "+text)
		return nil
	}
	t.Cleanup(func() { usageResend = nil })
	proc := func() *assistantProc {
		exited := make(chan struct{})
		close(exited)
		p := &assistantProc{id: id, agent: "claude", launcher: "opus", cwd: "/home/u/.persona/assistant", exited: exited,
			stdin: nopCloser{io.Discard}, cmd: &exec.Cmd{}}
		n.mu.Lock()
		n.procs[id] = p
		n.mu.Unlock()
		return p
	}

	p := proc()
	n.checkTurnUsage(p, true)
	if len(sent) != 1 || !strings.HasPrefix(sent[0], id+" /home/u/.persona/assistant opus ") {
		t.Fatalf("sent %q", sent)
	}
	n.mu.Lock()
	_, running := n.procs[id]
	n.mu.Unlock()
	if running {
		t.Error("the process holding the number was not let go")
	}
	goldenNode(t, "usage_retry", strings.TrimPrefix(sent[0], id+" /home/u/.persona/assistant opus "))

	// The gateway does it again at once: repaired again, not sent again.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(usageLine(t, "a9", 1_500_000, false, "again"))
	f.Close()
	n.checkTurnUsage(proc(), true)
	if len(sent) != 1 {
		t.Errorf("sent again within %s: %q", usageRetryEvery, sent)
	}
	if b, _ := os.ReadFile(path); bytes.Contains(b, []byte("1500000")) {
		t.Error("the second number was not repaired")
	}

	// Nothing impossible in it: a conversation that is really too long keeps its error.
	n.usageRetried = nil
	p = proc()
	n.checkTurnUsage(p, true)
	if len(sent) != 1 {
		t.Errorf("sent without a repair: %q", sent)
	}
	n.mu.Lock()
	_, running = n.procs[id]
	n.mu.Unlock()
	if !running {
		t.Error("let go without a repair")
	}
}

// A turn that ended fine with an impossible number: repaired and the process let go, so the next message does not
// compact for nothing; with background tasks running in it, the process stays.
func TestCheckTurnUsageAfterAFinishedTurn(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-000000000003"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	n.paths[id] = path
	usageResend = func(context.Context, *Node, string, string, string, string, modelExecution) error {
		t.Error("a finished turn is not sent again")
		return nil
	}
	t.Cleanup(func() { usageResend = nil })
	for _, tasks := range [][]string{nil, {"tail -f deploy.log"}} {
		_ = os.WriteFile(path, []byte(usageLine(t, "a1", 1_176_124, false, "done")), 0o600)
		exited := make(chan struct{})
		close(exited)
		p := &assistantProc{id: id, agent: "claude", exited: exited, stdin: nopCloser{io.Discard}, cmd: &exec.Cmd{}, tasks: tasks}
		n.procs[id] = p
		n.checkTurnUsage(p, false)
		if b, _ := os.ReadFile(path); bytes.Contains(b, []byte("1176124")) {
			t.Errorf("tasks %v: not repaired", tasks)
		}
		if _, running := n.procs[id]; running != (tasks != nil) {
			t.Errorf("tasks %v: running %v", tasks, running)
		}
	}
}

// A message sent while the check ran is being answered: its process is not killed under it.
func TestCheckTurnUsageLeavesABusyProcess(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-000000000004"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	_ = os.WriteFile(path, []byte(gatewayTranscript(t)), 0o600)
	n.paths[id] = path
	usageResend = func(context.Context, *Node, string, string, string, string, modelExecution) error {
		t.Error("sent again over a turn being answered")
		return nil
	}
	t.Cleanup(func() { usageResend = nil })
	p := &assistantProc{id: id, agent: "claude", exited: make(chan struct{}), stdin: nopCloser{io.Discard}, cmd: &exec.Cmd{}, busy: true}
	n.procs[id] = p
	n.checkTurnUsage(p, true)
	if _, running := n.procs[id]; !running {
		t.Error("the busy process was let go")
	}
	if b, _ := os.ReadFile(path); bytes.Count(b, []byte("1706464")) != 2 { // the subagent's line keeps its own
		t.Error("not repaired")
	}
}

// The message sent again shows no bubble in the apps: it is only a <persona-context> block.
func TestUsageRetryShowsNoBubble(t *testing.T) {
	line, _ := json.Marshal(map[string]any{"type": "user", "uuid": "u1",
		"message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": prompts.RenderUsageRetry()}}}})
	var meta transcript.Meta
	if items := transcript.ParseLine(1, line, &meta); len(items) != 0 {
		t.Errorf("items %+v", items)
	}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
