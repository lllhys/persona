package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

const (
	nodeRefusalPrompt   = `{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T12:00:00Z","cwd":"/tmp/project","message":{"role":"user","content":"run the checks"}}`
	nodeRefusalFallback = `{"type":"system","subtype":"model_refusal_fallback","uuid":"f1","parentUuid":"u1","timestamp":"2026-01-01T12:00:01Z","trigger":"refusal","direction":"retry","scope":"session","originalModel":"primary-model","fallbackModel":"fallback-model","requestId":"synthetic-request","apiRefusalCategory":"synthetic-policy","apiRefusalExplanation":"Upstream refused the request","refusedUserMessageUuid":"u1","retractedMessageUuids":[]}`
	nodeRefusalError    = `{"type":"assistant","uuid":"e1","parentUuid":"f1","timestamp":"2026-01-01T12:00:02Z","isApiErrorMessage":true,"error":"model_not_found","apiErrorStatus":404,"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"API Error: 404 fallback-model is not available"}]}}`
	nodeRefusalSuccess  = `{"type":"assistant","uuid":"a1","timestamp":"2026-01-01T12:00:04Z","message":{"model":"fallback-model","role":"assistant","content":[{"type":"text","text":"Checks passed"}]}}`
)

func refusalMetaNode() *Node {
	return &Node{metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
}

func freshRefusalMeta(t *testing.T, n *Node, path string, lines []string) quickMeta {
	t.Helper()
	st := writeLines(t, path, lines)
	n.mu.Lock()
	m := n.metas[path]
	m.readAt = time.Time{}
	n.metas[path] = m
	n.mu.Unlock()
	return n.meta(path, st)
}

func TestModelRefusalMetaSurvivesTailAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	blob := `{"type":"api-request-blob","content":"` + strings.Repeat("x", quickTail+1) + `"}`
	lines := []string{nodeRefusalPrompt, nodeRefusalFallback, blob, nodeRefusalError}
	st := writeLines(t, path, lines)
	if m := readQuickMeta(path, st); m.modelFallback != nil || m.turnErr.code != "" {
		t.Fatal("the test must put the fallback outside the quick tail")
	}
	for range 2 { // A fresh Node represents a process restart: only the transcript supplies the evidence.
		n := refusalMetaNode()
		m := n.meta(path, st)
		if r := m.modelFallback; r == nil || r.OriginalModel != "primary-model" || r.FallbackModel != "fallback-model" ||
			r.Category != "synthetic-policy" || r.Scope != "session" || r.Raw != nodeRefusalFallback {
			t.Fatalf("fallback lost outside tail: %+v", r)
		}
		if m.model != "fallback-model" || m.turnErr.code != proto.TurnErrorModelFallbackUnavailable || m.turnErr.kind != proto.TurnErrorUser {
			t.Fatalf("fallback failure not classified: model=%q failure=%+v", m.model, m.turnErr)
		}
		var s proto.Session
		m.turnErr.set(&s)
		if s.TurnErrorCode != proto.TurnErrorModelFallbackUnavailable {
			t.Fatalf("session code lost: %+v", s)
		}
		turnFailure{}.set(&s)
		if s.TurnErrorCode != "" {
			t.Fatal("clearing a failure left its code")
		}
	}
}

func TestQuickMetaRewindKeepsPreTailModelFallback(t *testing.T) {
	visibleReply := strings.Replace(nodeRefusalSuccess, `"uuid":"a1"`, `"uuid":"a1","parentUuid":"f1"`, 1)
	hiddenPrompt := strings.Replace(strings.Replace(nodeRefusalPrompt, `"uuid":"u1"`, `"uuid":"u2"`, 1), `"parentUuid":null`, `"parentUuid":"d1"`, 1)
	hiddenFallback := strings.Replace(strings.Replace(strings.Replace(nodeRefusalFallback,
		`"uuid":"f1"`, `"uuid":"f2"`, 1), `"parentUuid":"u1"`, `"parentUuid":"u2"`, 1), `"fallbackModel":"fallback-model"`, `"fallbackModel":"hidden-fallback"`, 1)
	for _, tc := range []struct {
		name       string
		hiddenTurn []string
	}{
		{"a hidden reply clears the fallback", []string{
			hiddenPrompt,
			`{"type":"assistant","uuid":"b1","parentUuid":"u2","timestamp":"2026-01-01T12:01:01Z","message":{"model":"primary-model","role":"assistant","content":[{"type":"text","text":"B succeeded"}]}}`,
			`{"type":"system","subtype":"turn_duration","uuid":"d2","parentUuid":"b1","timestamp":"2026-01-01T12:01:02Z","durationMs":10}`,
		}},
		{"a hidden turn replaces the fallback", []string{
			hiddenPrompt, hiddenFallback,
			`{"type":"assistant","uuid":"b1","parentUuid":"f2","timestamp":"2026-01-01T12:01:01Z","message":{"model":"hidden-fallback","role":"assistant","content":[{"type":"text","text":"B succeeded"}]}}`,
			`{"type":"system","subtype":"turn_duration","uuid":"d2","parentUuid":"b1","timestamp":"2026-01-01T12:01:02Z","durationMs":10}`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			lines := []string{nodeRefusalPrompt, nodeRefusalFallback, visibleReply,
				`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","timestamp":"2026-01-01T12:00:05Z","durationMs":10}`,
				`{"type":"api-request-blob","content":"` + strings.Repeat("x", quickTail+1) + `"}`}
			st := writeLines(t, path, lines)
			prev := readQuickMeta(path, st)
			prev.modelFallback = &proto.ModelRefusal{OriginalModel: "primary-model", FallbackModel: "fallback-model", Scope: "session"}
			lines = append(lines, tc.hiddenTurn...)
			lines = append(lines, `{"type":"system","subtype":"away_summary","uuid":"s1","parentUuid":"d1","timestamp":"2026-01-01T12:02:00Z","content":"recap"}`)
			m := readQuickMetaSince(path, writeLines(t, path, lines), prev)
			if got := m.modelFallback; got == nil || got.FallbackModel != "fallback-model" {
				t.Fatalf("hidden turn changed the pre-tail fallback: %+v", got)
			}
		})
	}
}

func TestModelRefusalMetaFollowsSessionAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	n := refusalMetaNode()
	lines := []string{nodeRefusalPrompt, nodeRefusalFallback, nodeRefusalError}
	if m := freshRefusalMeta(t, n, path, lines); m.turnErr.code != proto.TurnErrorModelFallbackUnavailable {
		t.Fatalf("initial failure: %+v", m.turnErr)
	}
	// Another session has no fallback, even when the model error is identical.
	other := filepath.Join(t.TempDir(), "other.jsonl")
	if m := freshRefusalMeta(t, n, other, []string{nodeRefusalPrompt, nodeRefusalError}); m.modelFallback != nil || m.turnErr.code != "" {
		t.Fatalf("fallback crossed sessions: %+v", m)
	}
	// A new prompt clears the error while retaining a session-scoped fallback for later turns.
	lines = append(lines, strings.Replace(nodeRefusalPrompt, `"uuid":"u1"`, `"uuid":"u2"`, 1))
	m := freshRefusalMeta(t, n, path, lines)
	if m.modelFallback == nil || m.turnErr.code != "" || m.turnErr.text != "" {
		t.Fatalf("new turn: fallback=%+v failure=%+v", m.modelFallback, m.turnErr)
	}
	lines = append(lines, nodeRefusalError)
	if m := freshRefusalMeta(t, n, path, lines); m.turnErr.code != proto.TurnErrorModelFallbackUnavailable {
		t.Fatalf("later failed turn: %+v", m.turnErr)
	}
	lines = append(lines, nodeRefusalSuccess)
	if m := freshRefusalMeta(t, n, path, lines); m.modelFallback == nil || m.turnErr.code != "" || m.turnErr.text != "" {
		t.Fatalf("successful fallback reply: %+v", m)
	}
	lines = append(lines, `{"type":"user","uuid":"local1","message":{"role":"user","content":"<local-command-stdout>Set model to primary-model</local-command-stdout>"}}`, nodeRefusalError)
	if m := freshRefusalMeta(t, n, path, lines); m.modelFallback != nil || m.turnErr.code != "" {
		t.Fatalf("model switch left stale cause: %+v", m)
	}
	// A rewritten transcript cannot inherit the old scan's fallback.
	if m := freshRefusalMeta(t, n, path, []string{nodeRefusalPrompt, nodeRefusalError}); m.modelFallback != nil || m.turnErr.code != "" {
		t.Fatalf("rewritten transcript inherited fallback: %+v", m)
	}
}

func TestModelRefusalScanObservesLargeRows(t *testing.T) {
	big := strings.Repeat("x", quickTail+1)
	for _, tc := range []struct {
		name, scope, row string
		keep             bool
	}{
		{"other model after large content", "session", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + big + `"}],"model":"primary-model"}}`, false},
		{"same model after large content", "session", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + big + `"}],"model":"fallback-model"}}`, true},
		{"large new prompt", "turn", `{"type":"user","message":{"role":"user","content":"` + big + `"}}`, false},
		{"large tool result is not a turn", "turn", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool1","content":"` + big + `"}]}}`, true},
		{"sidechain cannot switch parent", "session", `{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"` + big + `"}],"model":"primary-model"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			fallback := strings.Replace(nodeRefusalFallback, `"scope":"session"`, `"scope":"`+tc.scope+`"`, 1)
			n := refusalMetaNode()
			m := freshRefusalMeta(t, n, path, []string{nodeRefusalPrompt, fallback, tc.row, nodeRefusalError})
			if (m.modelFallback != nil) != tc.keep || (m.turnErr.code == proto.TurnErrorModelFallbackUnavailable) != tc.keep {
				t.Fatalf("large row ignored: fallback=%+v failure=%+v", m.modelFallback, m.turnErr)
			}
		})
	}
}

func TestModelRefusalScanWaitsForCompleteLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	n := refusalMetaNode()
	writeLines(t, path, []string{nodeRefusalPrompt, nodeRefusalFallback})
	n.backgroundTasks(path)
	row := strings.Replace(nodeRefusalSuccess, "fallback-model", "primary-model", 1)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(row[:len(row)/2]); err != nil {
		t.Fatal(err)
	}
	n.backgroundTasks(path)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m := quickMeta{size: st.Size()}
	n.mergeModelFallback(path, &m)
	if m.modelFallback == nil {
		t.Fatal("incomplete line cleared fallback")
	}
	if _, err := file.WriteString(row[len(row)/2:] + "\n"); err != nil {
		t.Fatal(err)
	}
	n.backgroundTasks(path)
	st, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m.size = st.Size()
	n.mergeModelFallback(path, &m)
	if m.modelFallback != nil {
		t.Fatal("completed model switch retained fallback")
	}
}

func TestModelRefusalSessionRow(t *testing.T) {
	const id = "0d000000-0000-4000-8000-000000002203"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	lines := []string{nodeRefusalPrompt, nodeRefusalFallback, nodeRefusalError}
	writeLines(t, path, lines)
	n := rewindNode(t, newClaudeSim(simPrompt{"run the checks", noCode}), "claude", proto.StatusIdle)
	n.agents = map[string]host.Agent{id: n.agents["s1"]}
	n.paths, n.metas, n.bgScans = map[string]string{id: path}, map[string]quickMeta{}, map[string]*bgScan{}
	n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
	row := func() proto.Session {
		t.Helper()
		for _, s := range n.sessionList(context.Background(), false) {
			if s.SessionID == id {
				return s
			}
		}
		t.Fatal("session row missing")
		return proto.Session{}
	}
	if s := row(); s.ModelFallback == nil || s.Model != "fallback-model" || s.TurnErrorCode != proto.TurnErrorModelFallbackUnavailable {
		t.Fatalf("session lost fallback evidence: %+v", s)
	}
	// The throttle must not advertise a stale failed turn after the file changes, even before the next scan.
	lines = append(lines, nodeRefusalSuccess)
	writeLines(t, path, lines)
	if s := row(); s.TurnErrorCode != "" || s.TurnError != "" {
		t.Fatalf("stale failed turn survived new output: %+v", s)
	}
}

func TestModelRefusalListKey(t *testing.T) {
	s := proto.Session{SessionID: "session", Model: "fallback-model"}
	before := listKey([]proto.Session{s})
	s.ModelFallback = &proto.ModelRefusal{OriginalModel: "primary-model", FallbackModel: "fallback-model", Scope: "session"}
	fallback := listKey([]proto.Session{s})
	if fallback == before {
		t.Fatal("discovering fallback evidence did not publish the session")
	}
	s.TurnErrorCode = proto.TurnErrorModelFallbackUnavailable
	if listKey([]proto.Session{s}) == fallback {
		t.Fatal("classifying the failed fallback did not publish the session")
	}
}

func TestModelRefusalScanDoesNotReclassifyOlderSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	n := refusalMetaNode()
	st := writeLines(t, path, []string{nodeRefusalPrompt, nodeRefusalError})
	m := readQuickMeta(path, st)
	writeLines(t, path, []string{nodeRefusalPrompt, nodeRefusalError, nodeRefusalFallback, nodeRefusalError})
	n.backgroundTasks(path)
	n.mergeModelFallback(path, &m)
	if m.modelFallback != nil || m.turnErr.code != "" {
		t.Fatalf("a later fallback reclassified an earlier failure: %+v", m)
	}
}

// A refusal record's fields no one reviewed never leave the node (#2528): not in the session list (a terminal
// session, a Persona conversation's row), its active fallback, nor the items an app pages through; the record's own
// fields and words do, and the node still tells one record from another (Raw, kept in memory only).
func TestModelRefusalNodeOutputsLeaveNothingHidden(t *testing.T) {
	const marker = "HIDDEN-MARKER-2528-node"
	refusal := strings.Replace(nodeRefusalFallback, `"retractedMessageUuids":[]`,
		`"retractedMessageUuids":[],"content":"Synthetic refusal words","syntheticHidden":{"nested":{"deeper":"`+marker+`"}}`, 1)
	if !strings.Contains(refusal, marker) {
		t.Fatal("fixture")
	}
	const id = "0d000000-0000-4000-8000-000000002528"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	writeLines(t, path, []string{nodeRefusalPrompt, refusal, nodeRefusalError})
	n := rewindNode(t, newClaudeSim(simPrompt{"run the checks", noCode}), "claude", proto.StatusIdle)
	n.agents = map[string]host.Agent{id: n.agents["s1"]}
	n.paths, n.metas, n.bgScans = map[string]string{id: path}, map[string]quickMeta{}, map[string]*bgScan{}
	n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
	clean := func(what string, v any) []byte {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), marker) || strings.Contains(string(b), "syntheticHidden") || strings.Contains(string(b), `"raw"`) {
			t.Fatalf("%s carries the record's other fields: %s", what, b)
		}
		return b
	}
	var row *proto.Session
	for _, s := range n.sessionList(context.Background(), false) {
		if s.SessionID == id {
			row = &s
		}
	}
	if row == nil || row.ModelFallback == nil {
		t.Fatalf("no fallback row: %+v", row)
	}
	b := clean("session list", []proto.Session{*row})
	for _, want := range []string{`"original_model":"primary-model"`, `"fallback_model":"fallback-model"`, `"category":"synthetic-policy"`,
		`"scope":"session"`, `"explanation":"Upstream refused the request"`, `"text":"Synthetic refusal words"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("session list lost %s: %s", want, b)
		}
	}
	if row.ModelFallback.Raw != refusal {
		t.Fatal("the node lost the record it tells records apart by")
	}
	out, err := n.handle(context.Background(), "session.items", json.RawMessage(`{"session_id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	b = clean("session.items", out)
	if !strings.Contains(string(b), `"text":"Synthetic refusal words"`) || !strings.Contains(string(b), `"model_refusal":{"subtype":"model_refusal_fallback"`) {
		t.Fatalf("items lost the refusal: %s", b)
	}
}
