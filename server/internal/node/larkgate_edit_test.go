package node

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func newLarkEditRig(t *testing.T, answer func(proto.Interaction) proto.Resolution, user ...proto.LarkRule) *spaceRig {
	t.Helper()
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	api := newFakeAPI(t)
	hub := newScriptHub(t, func(_ context.Context, ix proto.Interaction) (any, error) {
		return answer(ix), nil
	}, notifyOK)
	n := hub.n
	n.mu.Lock()
	n.dir = t.TempDir()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL, user...)}}, time.Now()
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	return &spaceRig{t: t, n: n, api: api, hub: hub, state: filepath.Join(t.TempDir(), ".persona")}
}

// runLarkEditHook uses the real Claude Code PreToolUse Bash handler, including interactionID and askOnce. This is the
// only path that may declare editable_text; the shared larkCheck used by Codex and the runtimes is tested separately.
func runLarkEditHook(t *testing.T, r *spaceRig, cwd, command string, extra map[string]any) map[string]any {
	t.Helper()
	input := map[string]any{"command": command}
	for key, value := range extra {
		input[key] = value
	}
	raw, err := json.Marshal(hookInput{HookEventName: "PreToolUse", SessionID: testThread, ToolName: "Bash",
		ToolUseID: "toolu_lark_edit", Cwd: cwd, ToolInput: mustRaw(input)})
	if err != nil {
		t.Fatal(err)
	}
	r.n.mu.Lock()
	r.n.procs[testThread] = &assistantProc{id: testThread, agent: "claude", cwd: cwd}
	r.n.mu.Unlock()
	out, err := r.n.handleHook(context.Background(), sockRequest{Kind: "hook", Input: raw, Attempt: "edit-attempt"})
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("the Lark hook left an eligible send to the ordinary permission flow")
	}
	spec, _ := out.(map[string]any)["hookSpecificOutput"].(map[string]any)
	if spec == nil {
		t.Fatalf("hook output has no hookSpecificOutput: %#v", out)
	}
	return spec
}

func mustRaw(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func editedAllow(ix proto.Interaction, text string) proto.Resolution {
	res := proto.Resolution{Allow: boolp(true)}
	if ix.EditableText != "" {
		res.EditedText = text
	}
	return res
}

func TestLarkEditHookRetiresOriginalAndGrantsOnlyEditedCallOnce(t *testing.T) {
	const original = "original text"
	edited := "Edited $HOME `printf nope` $(printf nope) \\\" ' ;\n第二行 😀"
	r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, edited) })
	cwd := t.TempDir()
	base := r.processIn(cwd, testThread)
	const body = `{"text":"original text","mentions":[{"key":"@_user_1","id":"ou_example"}],"large":9007199254740993}`
	const query = "receive_id_type=open_id&tag=a%20b"
	command := `ex POST '/conversations/F1/messages?` + query + `' '` + body + `'`
	spec := runLarkEditHook(t, r, cwd, command, map[string]any{
		"description": "send the reviewed update", "timeout": 120000, "run_in_background": true,
	})
	if spec["permissionDecision"] != "allow" {
		t.Fatalf("edited Allow = %#v", spec)
	}
	card := r.hub.lastCreated(t)
	if card.EditableText != original || card.Kind != proto.IxPermission {
		t.Fatalf("editable card = %#v", card)
	}
	updated, _ := spec["updatedInput"].(map[string]any)
	if updated == nil || updated["description"] != "send the reviewed update" || updated["timeout"] != float64(120000) ||
		updated["run_in_background"] != true || len(updated) != 4 {
		t.Fatalf("updatedInput did not preserve the other tool inputs: %#v", spec["updatedInput"])
	}
	updatedCommand, _ := updated["command"].(string)
	v := classifyLark(updatedCommand, r.n.larkCfg.Gates, cwd, r.n.larkCLIRisk(r.n.larkCfg))
	if len(v.calls) != 1 || v.other || v.unparsed || v.unknown || v.unsure || v.deny != "" {
		t.Fatalf("updated command is not the same one standalone call: %q\n%+v", updatedCommand, v)
	}
	call := v.calls[0]
	if call.Method != http.MethodPost || call.Path != "/conversations/F1/messages" || call.Query != query || call.Gate != 0 || call.CLI {
		t.Fatalf("edited call identity changed: %+v", call)
	}
	oldFields, _, ok := editableTextFields(body)
	if !ok {
		t.Fatal("test body is not editable")
	}
	newFields, newText, ok := editableTextFields(call.Body)
	if !ok || newText != edited {
		t.Fatalf("edited body text = %q, %v; body %s", newText, ok, call.Body)
	}
	for _, key := range []string{"mentions", "large"} {
		if string(newFields[key]) != string(oldFields[key]) {
			t.Errorf("%s changed from %s to %s", key, oldFields[key], newFields[key])
		}
	}
	url := base + "/api/v1/conversations/F1/messages"
	if code, response := send(t, http.MethodPost, url+"?receive_id_type=user_id&tag=a%20b", call.Body); code != http.StatusForbidden {
		t.Errorf("edited body with a changed query = %d %s, want 403", code, response)
	}
	if code, response := send(t, http.MethodPost, url+"?"+query, body); code != http.StatusForbidden {
		t.Errorf("original body after edited Allow = %d %s, want its grant retired", code, response)
	}
	tampered := strings.Replace(call.Body, "ou_example", "ou_someone_else", 1)
	if code, response := send(t, http.MethodPost, url+"?"+query, tampered); code != http.StatusForbidden {
		t.Errorf("edited text with a changed mention = %d %s, want 403", code, response)
	}
	adjacentNumber := strings.Replace(call.Body, "9007199254740993", "9007199254740992", 1)
	if code, response := send(t, http.MethodPost, url+"?"+query, adjacentNumber); code != http.StatusForbidden {
		t.Errorf("a different large integer collapsed into the approved float64 body = %d %s, want 403", code, response)
	}
	expandedDollar := strings.Replace(call.Body, "$HOME", "/tmp", 1)
	if code, response := send(t, http.MethodPost, url+"?"+query, expandedDollar); code != http.StatusForbidden {
		t.Errorf("a '$' in edited text became a dynamic-body wildcard = %d %s, want 403", code, response)
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Fatalf("an unapproved request reached the API: %+v", got)
	}
	if code, response := send(t, http.MethodPost, url+"?"+query, call.Body); code != http.StatusOK {
		t.Fatalf("the one edited call = %d %s, want 200", code, response)
	}
	if code, _ := send(t, http.MethodPost, url+"?"+query, call.Body); code != http.StatusForbidden {
		t.Errorf("the edited call repeated = %d, want 403", code)
	}
	got := r.api.requests()
	if len(got) != 1 || got[0].Query != query || got[0].Body != call.Body {
		t.Fatalf("upstream requests = %+v, want exactly the edited query and body once", got)
	}
	waitFor(t, "the edited sent-as-user report", func() bool {
		r.hub.mu.Lock()
		defer r.hub.mu.Unlock()
		return len(r.hub.larkSent) == 1
	})
	r.hub.mu.Lock()
	report := r.hub.larkSent[0]
	r.hub.mu.Unlock()
	if report.Text != edited || report.LetThrough.EditedFrom != original || report.LetThrough.InteractionID != card.ID ||
		report.LetThrough.Kind != proto.LarkThroughCard {
		t.Fatalf("sent report = %#v; card id %q", report, card.ID)
	}
}

func TestLarkEditAcceptsStaticShellQuoting(t *testing.T) {
	tests := []struct{ name, body, command string }{
		{name: "double quoted JSON", body: `{"text":"hello"}`, command: `ex POST /conversations/F1/messages "{\"text\":\"hello\"}"`},
		{name: "single quote and Unicode in the original", body: `{"text":"it's 雪😀"}`,
			command: `ex POST /conversations/F1/messages ` + shellQuote(`{"text":"it's 雪😀"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, "changed") })
			cwd := t.TempDir()
			spec := runLarkEditHook(t, r, cwd, tc.command, nil)
			if spec["permissionDecision"] != "allow" {
				t.Fatalf("static shell quoting was refused: %#v", spec)
			}
			updated, _ := spec["updatedInput"].(map[string]any)
			command, _ := updated["command"].(string)
			v := classifyLark(command, r.n.larkCfg.Gates, cwd, r.n.larkCLIRisk(r.n.larkCfg))
			if len(v.calls) != 1 {
				t.Fatalf("updated command %q parsed as %+v", command, v)
			}
			_, text, ok := editableTextFields(v.calls[0].Body)
			if !ok || text != "changed" {
				t.Fatalf("updated body = %q, %v", text, ok)
			}
		})
	}
}

func TestLarkEditRetiresTheGrantOfItsOwnCard(t *testing.T) {
	r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, "edited") })
	cwd := t.TempDir()
	r.processIn(cwd, testThread)
	call := larkCall{Gate: 0, Method: http.MethodPost, Path: "/conversations/F1/messages", Body: `{"text":"original"}`}
	r.n.gates().grant("ex", testThread, call, proto.LarkLetThrough{Kind: proto.LarkThroughCard, InteractionID: "ix_another_card"})
	spec := runLarkEditHook(t, r, cwd, `ex POST /conversations/F1/messages '{"text":"original"}'`, nil)
	if spec["permissionDecision"] != "allow" {
		t.Fatalf("edited Allow = %#v", spec)
	}
	card := r.hub.lastCreated(t)
	p := r.n.gates()
	p.mu.Lock()
	defer p.mu.Unlock()
	var ownOriginal, otherOriginal, edited bool
	for _, grant := range p.grants {
		switch grant.through.InteractionID {
		case card.ID:
			ownOriginal = ownOriginal || grant.through.EditedFrom == ""
			edited = edited || grant.through.EditedFrom == "original"
		case "ix_another_card":
			otherOriginal = true
		}
	}
	if ownOriginal || !otherOriginal || !edited {
		t.Fatalf("grants after edit: own original %v, other card %v, edited %v; grants %+v", ownOriginal, otherOriginal, edited, p.grants)
	}
}

func TestLarkEditHookUnchangedAndDenyAnswersKeepOrdinaryBehavior(t *testing.T) {
	t.Run("unchanged text uses the original grant without updatedInput", func(t *testing.T) {
		r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, "same") })
		cwd := t.TempDir()
		base := r.processIn(cwd, testThread)
		spec := runLarkEditHook(t, r, cwd, `ex POST /conversations/F1/messages '{"text":"same"}'`, nil)
		if spec["permissionDecision"] != "allow" || spec["updatedInput"] != nil {
			t.Fatalf("unchanged answer = %#v", spec)
		}
		url := base + "/api/v1/conversations/F1/messages"
		if code, _ := send(t, http.MethodPost, url, `{"text":"same"}`); code != http.StatusOK {
			t.Errorf("original confirmed call = %d, want 200", code)
		}
		if code, _ := send(t, http.MethodPost, url, `{"text":"same"}`); code != http.StatusForbidden {
			t.Errorf("original confirmed call repeated = %d, want 403", code)
		}
	})

	t.Run("deny note is the tool result and authorizes neither version", func(t *testing.T) {
		const note = "Use a concise status update instead"
		r := newLarkEditRig(t, func(proto.Interaction) proto.Resolution {
			return proto.Resolution{Allow: boolp(false), Message: note}
		})
		cwd := t.TempDir()
		base := r.processIn(cwd, testThread)
		spec := runLarkEditHook(t, r, cwd, `ex POST /conversations/F1/messages '{"text":"verbose"}'`, nil)
		if spec["permissionDecision"] != "deny" || spec["permissionDecisionReason"] != note || spec["updatedInput"] != nil {
			t.Fatalf("deny answer = %#v", spec)
		}
		for _, body := range []string{`{"text":"verbose"}`, `{"text":"concise"}`} {
			if code, _ := send(t, http.MethodPost, base+"/api/v1/conversations/F1/messages", body); code != http.StatusForbidden {
				t.Errorf("body %s after Deny = %d, want 403", body, code)
			}
		}
		if got := r.api.requests(); len(got) != 0 {
			t.Fatalf("a denied request reached the API: %+v", got)
		}
	})
}

func TestLarkEditEligibilityIsOnlyStaticAuthenticatedSendOrReply(t *testing.T) {
	r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, "changed") })
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "message.json"), []byte(`{"text":"from file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(cwd, "message.json")
	tests := []struct {
		name, command string
		editable      bool
	}{
		{name: "send", command: `ex POST /conversations/F1/messages '{"text":"hello"}'`, editable: true},
		{name: "reply", command: `ex POST /messages/om_1/replies '{"text":"hello","replyMode":"thread"}'`, editable: true},
		{name: "reaction", command: `ex POST /messages/om_1/reactions '{"text":"hello"}'`},
		{name: "multiple calls", command: `ex POST /conversations/F1/messages '{"text":"one"}' && ex POST /conversations/F1/messages '{"text":"two"}'`},
		{name: "dynamic body", command: `ex POST /conversations/F1/messages "$BODY"`},
		{name: "relative file", command: `ex POST /conversations/F1/messages @message.json`},
		{name: "absolute file", command: `ex POST /conversations/F1/messages @` + absolute},
		{name: "nested content only", command: `ex POST /conversations/F1/messages '{"content":{"text":"hello"}}'`},
		{name: "duplicate text key", command: `ex POST /conversations/F1/messages '{"text":"one","text":"two"}'`},
		{name: "empty text", command: `ex POST /conversations/F1/messages '{"text":"  "}'`},
		{name: "lark cli", command: `lark-cli im +messages-send --text hello`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, got := r.n.editableLarkCall(r.n.larkCfg, tc.command, cwd)
			if got != tc.editable {
				t.Errorf("editableLarkCall(%q) = %v, want %v", tc.command, got, tc.editable)
			}
		})
	}
	r.n.larkCfg.Gates[0] = exampleGate(r.api.URL)
	if _, _, _, got := r.n.editableLarkCall(r.n.larkCfg, `ex POST /conversations/F1/messages '{"text":"hello"}'`, cwd); got {
		t.Error("a direct command whose credentials are not held by the proxy became editable")
	}
}

func TestSharedLarkCheckDoesNotDeclareEditingForCodexOrRuntimes(t *testing.T) {
	r := newSpaceRig(t)
	cwd := t.TempDir()
	if got := r.check(cwd, testThread, `ex POST /conversations/F1/messages '{"text":"hello"}'`); got != "allow" {
		t.Fatalf("shared gate decision = %q", got)
	}
	if card := r.hub.lastCreated(t); card.EditableText != "" {
		t.Fatalf("shared Codex/runtime path declared editable_text %q", card.EditableText)
	}
}

func TestLarkEditDeniedByTheSameRuleAfterApproval(t *testing.T) {
	blocked := proto.LarkRule{Method: http.MethodPost, Path: "/conversations/*/messages", Body: map[string]any{"text": "blocked"}, Action: "deny"}
	r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, "blocked") }, blocked)
	cwd := t.TempDir()
	base := r.processIn(cwd, testThread)
	spec := runLarkEditHook(t, r, cwd, `ex POST /conversations/F1/messages '{"text":"allowed"}'`, nil)
	if spec["permissionDecision"] != "deny" || spec["updatedInput"] != nil {
		t.Fatalf("edit into a deny rule = %#v", spec)
	}
	if cards := r.cards(); cards != 1 {
		t.Fatalf("edit validation created %d cards, want only the original", cards)
	}
	p := r.n.gates()
	p.mu.Lock()
	grants := len(p.grants)
	p.mu.Unlock()
	if grants != 0 {
		t.Fatalf("%d grants survived a denied edit; the original must be retired and no edited grant made", grants)
	}
	if code, _ := send(t, http.MethodPost, base+"/api/v1/conversations/F1/messages", `{"text":"allowed"}`); code != http.StatusForbidden {
		t.Errorf("original text after a denied edit = %d, want 403", code)
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Fatalf("a denied edit reached the API: %+v", got)
	}
}

func TestLarkEditShellPayloadsStayInsideTheTextField(t *testing.T) {
	payloads := []string{
		`"; ex PUT /messages/om_1/replies '{"text":"other"}' #`,
		`'; ex POST /conversations/OTHER/messages '{"text":"other"}' #`,
		`$(ex POST /conversations/OTHER/messages '{"text":"other"}')`,
		"`ex POST /conversations/OTHER/messages '{\"text\":\"other\"}'`",
		"line one\nex DELETE /messages/om_1/recall",
		`","mentions":[{"id":"ou_other"}],"ignored":"x`,
		`$HOME \\ "quoted" 'single' 😀`,
	}
	for _, edited := range payloads {
		t.Run(edited, func(t *testing.T) {
			r := newLarkEditRig(t, func(ix proto.Interaction) proto.Resolution { return editedAllow(ix, edited) })
			cwd := t.TempDir()
			spec := runLarkEditHook(t, r, cwd, `ex POST '/conversations/F1/messages?a=1' '{"text":"original","mentions":[]}'`, nil)
			if spec["permissionDecision"] != "allow" {
				t.Fatalf("valid text was not editable: %#v", spec)
			}
			updated, _ := spec["updatedInput"].(map[string]any)
			command, _ := updated["command"].(string)
			v := classifyLark(command, r.n.larkCfg.Gates, cwd, r.n.larkCLIRisk(r.n.larkCfg))
			if len(v.calls) != 1 || v.other || v.unparsed || v.unknown || v.unsure || v.deny != "" {
				t.Fatalf("text escaped into shell syntax: %q\n%+v", command, v)
			}
			call := v.calls[0]
			if call.Method != http.MethodPost || call.Path != "/conversations/F1/messages" || call.Query != "a=1" {
				t.Fatalf("payload changed call identity: %+v", call)
			}
			fields, text, ok := editableTextFields(call.Body)
			if !ok || text != edited || string(fields["mentions"]) != "[]" || len(fields) != 2 {
				t.Fatalf("payload changed another JSON field: %s", call.Body)
			}
		})
	}
}

func TestEditableTextFieldsRejectsAmbiguousJSON(t *testing.T) {
	valid := `{"text":"hello","large":9007199254740993,"mention":{"id":"ou_example"}}`
	fields, text, ok := editableTextFields(valid)
	if !ok || text != "hello" || string(fields["large"]) != "9007199254740993" {
		t.Fatalf("valid body = %#v, %q, %v", fields, text, ok)
	}
	for _, body := range []string{
		`{"text":"one","text":"two"}`,
		`{"text":"one","mentions":[],"mentions":[]}`,
		`["text"]`,
		`{"text":"one"} {"text":"two"}`,
		`{"text":null}`,
		`{"text":" "}`,
		`{"content":{"text":"nested"}}`,
		`{"text":"unterminated}`,
	} {
		if _, _, ok := editableTextFields(body); ok {
			t.Errorf("ambiguous or non-text body accepted: %s", body)
		}
	}
}
