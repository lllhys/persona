package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func controlFile(t *testing.T, f *cliFixture, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func controlStart(t *testing.T) (*cliFixture, string) {
	t.Helper()
	f := cliTest(t)
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "default", "medium", cliExec())
	if err != nil {
		t.Fatal(err)
	}
	f.idle(t, id)
	controlTranscript(t, f, id)
	return f, id
}

func controlTranscript(t *testing.T, f *cliFixture, id string) {
	t.Helper()
	// The fake claude emits events without writing a transcript. Give session listing the same
	// transcript presence it has in production; saved settings must override even old transcript metadata.
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.n.mu.Lock()
	f.n.paths[id] = path
	f.n.mu.Unlock()
}

func checkControlState(t *testing.T, f *cliFixture, id, mode, effort string) {
	t.Helper()
	if s := f.session(t, id); s.PermissionMode != mode || s.Effort != effort {
		t.Fatalf("shown settings: mode %q effort %q, want %q %q", s.PermissionMode, s.Effort, mode, effort)
	}
	st, ok := f.n.state.get(id)
	if !ok || st.AppMode != mode || st.AppEffort != effort || st.ModelEffort != effort {
		t.Fatalf("saved settings: mode %q effort %q model effort %q, want %q %q", st.AppMode, st.AppEffort, st.ModelEffort, mode, effort)
	}
	p := f.proc(id)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode != mode || p.effort != effort || p.model.Effort != effort {
		t.Fatalf("process settings: mode %q effort %q model effort %q, want %q %q", p.mode, p.effort, p.model.Effort, mode, effort)
	}
	if len(p.controls) != 0 {
		t.Fatalf("completed control requests retained: %d", len(p.controls))
	}
}

func checkControlRefusal(t *testing.T, err error, reason string) {
	t.Helper()
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "assistant_control_failed" || !strings.Contains(re.Message, reason) {
		t.Fatalf("control refusal = %v, want assistant_control_failed with %q", err, reason)
	}
}

// A refused field remains the last acknowledged value in the process, session list, and restart state.
func TestAssistantControlRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, mode, effort, errorFile, reason string
	}{
		{"permission mode", "plan", "", "control-mode-error.json", "Permission mode is disabled by policy"},
		{"effort override", "", "high", "control-effort-error.json", "CLAUDE_CODE_EFFORT_LEVEL=medium overrides this session"},
		{"auto effort", "", "auto", "control-effort-error.json", "This model does not support the requested effort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, id := controlStart(t)
			reason, _ := json.Marshal(tc.reason)
			controlFile(t, f, tc.errorFile, string(reason))
			_, err := nodeCall(t, f.n, "session.modes", map[string]any{"session_id": id, "permission_mode": tc.mode, "effort": tc.effort})
			checkControlRefusal(t, err, tc.reason)
			checkControlState(t, f, id, "default", "medium")
			if tc.effort == "auto" && !strings.Contains(f.file("stdin.jsonl"), `"effortLevel":null`) {
				t.Fatal("auto did not clear the session effort")
			}
			if err := os.Remove(filepath.Join(f.dir, tc.errorFile)); err != nil {
				t.Fatal(err)
			}
			if err := f.n.setModes(context.Background(), id, tc.mode, tc.effort); err != nil {
				t.Fatalf("retry: %v", err)
			}
			mode, effort := tc.mode, tc.effort
			if mode == "" {
				mode = "default"
			}
			if effort == "" {
				effort = "medium"
			}
			checkControlState(t, f, id, mode, effort)
		})
	}
}

func TestAssistantControlPartialSuccess(t *testing.T) {
	f, id := controlStart(t)
	controlFile(t, f, "control-effort-error.json", `"Invalid effort level"`)
	err := f.n.setModes(context.Background(), id, "plan", "high")
	checkControlRefusal(t, err, "Invalid effort level")
	checkControlState(t, f, id, "plan", "medium")
}

// An unrelated success is not an ACK, and a pending request is not an applied setting.
func TestAssistantControlWaitsForMatchingResponse(t *testing.T) {
	f, id := controlStart(t)
	controlFile(t, f, "control-hold", "")
	controlFile(t, f, "control-extra.jsonl", `{"type":"control_response","response":{"subtype":"success","request_id":"unrelated"}}`+"\n")
	done := make(chan error, 1)
	go func() { done <- f.n.setModes(context.Background(), id, "plan", "") }()
	cliWait(t, "the held control request", func() bool { return f.file("control-waiting") != "" })
	select {
	case err := <-done:
		t.Fatalf("returned before the matching response: %v", err)
	default:
	}
	if s := f.session(t, id); s.PermissionMode != "default" {
		t.Fatalf("unconfirmed mode shown: %q", s.PermissionMode)
	}
	if st, _ := f.n.state.get(id); st.AppMode != "default" {
		t.Fatalf("unconfirmed mode persisted: %q", st.AppMode)
	}
	if err := os.Remove(filepath.Join(f.dir, "control-hold")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("matching response did not complete the request")
	}
	checkControlState(t, f, id, "plan", "medium")
}

func TestAssistantControlExit(t *testing.T) {
	f, id := controlStart(t)
	p := f.proc(id)
	controlFile(t, f, "control-exit", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := f.n.setModes(ctx, id, "plan", "")
	checkControlRefusal(t, err, "")
	if ctx.Err() != nil {
		t.Fatal("waited for the caller's deadline after claude exited")
	}
	if st, _ := f.n.state.get(id); st.AppMode != "default" {
		t.Fatalf("mode after exit: %q", st.AppMode)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode != "default" || len(p.controls) != 0 {
		t.Fatalf("process after exit: mode %q, pending %d", p.mode, len(p.controls))
	}
}

func TestAssistantControlCancellationStopsProcess(t *testing.T) {
	f, id := controlStart(t)
	p := f.proc(id)
	controlFile(t, f, "control-hold", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.n.setModes(ctx, id, "", "high") }()
	cliWait(t, "the held control request", func() bool { return f.file("control-waiting") != "" })
	cancel()
	select {
	case err := <-done:
		checkControlRefusal(t, err, context.Canceled.Error())
	case <-time.After(5 * time.Second):
		t.Fatal("canceled control request did not return")
	}
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the unconfirmed process could still apply a late setting")
	}
	if st, _ := f.n.state.get(id); st.AppEffort != "medium" || st.ModelEffort != "medium" {
		t.Fatalf("saved effort after cancellation: %q, model %q", st.AppEffort, st.ModelEffort)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.effort != "medium" || len(p.controls) != 0 {
		t.Fatalf("process after cancellation: effort %q, pending %d", p.effort, len(p.controls))
	}
}

func TestAssistantControlPendingModeThenBypass(t *testing.T) {
	f, id := controlStart(t)
	p := f.proc(id)
	p.mu.Lock()
	p.busy = true // A restart must wait until this turn ends.
	p.mu.Unlock()
	controlFile(t, f, "control-hold", "")
	planDone := make(chan error, 1)
	go func() { planDone <- f.n.setModes(context.Background(), id, "plan", "") }()
	cliWait(t, "the held plan request", func() bool { return f.file("control-waiting") != "" })
	bypassDone := make(chan error, 1)
	go func() { bypassDone <- f.n.setModes(context.Background(), id, proto.ModeBypass, "") }()
	select {
	case err := <-bypassDone:
		t.Fatalf("bypass overtook an unconfirmed mode: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if st, _ := f.n.state.get(id); st.AppMode != "default" {
		t.Fatalf("a queued mode was recorded before the current request finished: %q", st.AppMode)
	}
	if err := os.Remove(filepath.Join(f.dir, "control-hold")); err != nil {
		t.Fatal(err)
	}
	for _, done := range []chan error{planDone, bypassDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("serialized mode request did not finish")
		}
	}
	if mode, _ := f.n.modesOf(id); mode != proto.ModeBypass {
		t.Fatalf("old ACK replaced the later selection: %q", mode)
	}
	if st, _ := f.n.state.get(id); st.AppMode != proto.ModeBypass {
		t.Fatalf("later bypass selection not saved: %q", st.AppMode)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode != "plan" || !p.alive() {
		t.Fatalf("busy process should keep its acknowledged mode until restart: %q", p.mode)
	}
	if strings.Contains(f.file("stdin.jsonl"), `"mode":"bypassPermissions"`) {
		t.Fatal("bypass was sent as an unsupported live control request")
	}
}

func TestAssistantControlStartupRefusal(t *testing.T) {
	for _, field := range []string{"mode", "effort"} {
		t.Run(field, func(t *testing.T) {
			f := cliTest(t)
			controlFile(t, f, "control-"+field+"-error.json", `"Setting refused"`)
			_, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "acceptEdits", "high", cliExec())
			checkControlRefusal(t, err, "Setting refused")
			if strings.Contains(f.file("stdin.jsonl"), `"type":"user"`) {
				t.Fatal("sent a user message after refusing its settings")
			}
			f.n.mu.Lock()
			var id string
			for key := range f.n.procs {
				id = key
			}
			f.n.mu.Unlock()
			if id == "" {
				t.Fatal("the process disappeared after an explicit refusal")
			}
			controlTranscript(t, f, id)
			mode := "acceptEdits"
			if field == "mode" {
				mode = "default"
			}
			checkControlState(t, f, id, mode, "auto")
		})
	}
}

// A failed first start restores shown state, but a genuine missing-command retry must still carry the request.
func TestAssistantControlStartRetryKeepsRequestedSettings(t *testing.T) {
	f := cliTest(t)
	shortStartRetries(t, 10*time.Millisecond)
	script := `if [ ! -e "$CLI_FAKE_DIR/retry-ready" ]; then
  : > "$CLI_FAKE_DIR/retry-ready"
  echo 'zsh: command not found: claude' >&2
  exit 127
fi
` + fakeCLI
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), "bin", "claude"), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "acceptEdits", "high")
	if err != nil {
		t.Fatalf("retry with settings: %v", err)
	}
	f.idle(t, id)
	controlTranscript(t, f, id)
	checkControlState(t, f, id, "acceptEdits", "high")
	if input := f.file("stdin.jsonl"); !strings.Contains(input, `"mode":"acceptEdits"`) || !strings.Contains(input, `"effortLevel":"high"`) {
		t.Fatalf("retry dropped requested settings: %s", input)
	}
}

type controlWriteError struct{}

func (controlWriteError) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (controlWriteError) Close() error              { return nil }

func TestAssistantControlWriteFailure(t *testing.T) {
	p := &assistantProc{stdin: controlWriteError{}, exited: make(chan struct{})}
	err := p.control(context.Background(), map[string]any{"subtype": "set_permission_mode", "mode": "plan"})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v", err)
	}
	if len(p.controls) != 0 {
		t.Fatalf("failed write retained %d pending requests", len(p.controls))
	}
}

type blockedControlWriter struct {
	*io.PipeWriter
	entered, finished chan struct{}
}

func (w *blockedControlWriter) Write(data []byte) (int, error) {
	close(w.entered)
	defer close(w.finished)
	return w.PipeWriter.Write(data)
}

func TestAssistantControlCancellationWhileStdinBlocked(t *testing.T) {
	reader, writer := io.Pipe()
	sink := &blockedControlWriter{PipeWriter: writer, entered: make(chan struct{}), finished: make(chan struct{})}
	t.Cleanup(func() {
		_ = writer.Close()
		_ = reader.Close()
	})
	p := &assistantProc{stdin: sink, exited: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.control(ctx, map[string]any{"subtype": "set_permission_mode", "mode": "plan"}) }()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the control request never reached stdin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocked write cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked stdin write prevented cancellation")
	}
	p.mu.Lock()
	pending := len(p.controls)
	p.mu.Unlock()
	if pending != 0 {
		t.Fatalf("blocked write left %d pending requests", pending)
	}
	// The production caller retires the process after an unconfirmed control, closing its stdin.
	_ = sink.Close()
	select {
	case <-sink.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("closing stdin did not release the writer")
	}
}

func controlRequestIDs(t *testing.T, sink *lineSink, count int) []string {
	t.Helper()
	var ids []string
	cliWait(t, "control request writes", func() bool {
		ids = nil
		for _, line := range strings.Split(strings.TrimSpace(sink.String()), "\n") {
			var req struct {
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal([]byte(line), &req) == nil && req.RequestID != "" {
				ids = append(ids, req.RequestID)
			}
		}
		return len(ids) == count
	})
	return ids
}

// Cancellation removes the pending request. Its late response and a duplicate response must not complete a new one.
func TestAssistantControlCancellationAndLateResponses(t *testing.T) {
	sink := &lineSink{}
	p := &assistantProc{stdin: sink, exited: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	request := map[string]any{"subtype": "set_permission_mode", "mode": "plan"}
	go func() { done <- p.control(ctx, request) }()
	first := controlRequestIDs(t, sink, 1)[0]
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	p.mu.Lock()
	pending := len(p.controls)
	p.mu.Unlock()
	if pending != 0 {
		t.Fatalf("cancellation retained %d pending requests", pending)
	}
	go func() { done <- p.control(context.Background(), request) }()
	second := controlRequestIDs(t, sink, 2)[1]
	if first == second {
		t.Fatal("request ID reused")
	}
	p.controlResponse(assistantControlResponse{RequestID: first, Subtype: "error", Error: "late refusal"})
	p.controlResponse(assistantControlResponse{RequestID: "unrelated", Subtype: "success"})
	select {
	case err := <-done:
		t.Fatalf("stale response completed the new request: %v", err)
	default:
	}
	p.controlResponse(assistantControlResponse{RequestID: second, Subtype: "success"})
	p.controlResponse(assistantControlResponse{RequestID: second, Subtype: "error", Error: "duplicate"})
	if err := <-done; err != nil {
		t.Fatalf("matching success: %v", err)
	}
}
