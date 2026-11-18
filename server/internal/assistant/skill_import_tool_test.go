package assistant

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func skillImportCall(ctx context.Context, s *Service, name, args string) proto.ToolResult {
	return s.Tool(ctx, ToolCall{Name: name, MachineID: "server", SessionID: "c1", Arguments: json.RawMessage(args)})
}

func mcpSkillFixture(t *testing.T) (*Service, []byte, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PERSONA_SKILLS_DIR", filepath.Join(root, "skills"))
	t.Setenv("PERSONA_PLUGIN_SKILLS_DIR", filepath.Join(root, "plugins"))
	f := newFakeHub().withMachine()
	f.hello.Assistant.Machines = []string{"server"}
	f.devices = []Device{{ID: "server", Home: root, Name: "Example computer"}}
	i := &assistantskills.Importer{}
	f.nodes = map[string]nodeAnswer{"skills.import": func(_ string, raw json.RawMessage) (any, error) {
		var in struct {
			assistantskills.ImportRequest
			Dir, Scope string
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		out, err := i.Import(context.Background(), assistantskills.ImportTarget{Scope: in.Scope, Cwd: filepath.Join(root, in.Dir)}, in.ImportRequest)
		var coded *assistantskills.ImportError
		if errors.As(err, &coded) {
			return nil, &rpc.Error{Code: coded.Code, Message: "private filesystem details"}
		}
		return out, err
	}}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("---\nname: example-skill\ndescription: A synthetic skill.\n---\n# Example\n"))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return newTestService(t, f), b.Bytes(), filepath.Join(root, "skills", "example-skill")
}

func skillToolResult(t *testing.T, s *Service, in skillImportInput) skillImportOutput {
	t.Helper()
	raw, _ := json.Marshal(in)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := skillImportCall(ctx, s, "skill_import", string(raw))
	var out skillImportOutput
	if result.IsError || json.Unmarshal([]byte(result.Content[0].Text), &out) != nil {
		t.Fatalf("skill import %s: %+v", in.Op, result)
	}
	return out
}

func answerSkillImportCard(t *testing.T, s *Service, answer proto.Resolution, inspect func(proto.Interaction)) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		card, ok := awaitCard(s)
		if !ok {
			t.Error("no import confirmation card")
			return
		}
		inspect(card)
		raw, _ := json.Marshal(answer)
		if _, err := s.Action(context.Background(), Action{Op: "answer", InteractionID: card.ID, Answer: raw}); err != nil {
			t.Error(err)
		}
	}()
	return done
}

func TestSkillImportToolPreviewApproveStatusAndUndo(t *testing.T) {
	s, archive, installedPath := mcpSkillFixture(t)
	preview := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "preview", Archive: archive}})
	if preview.Status != "previewed" || preview.Name != "example-skill" || preview.Environment == "" || len(s.pendingQuestions()) != 0 {
		t.Fatalf("preview: %+v", preview)
	}
	allow := true
	done := answerSkillImportCard(t, s, proto.Resolution{Allow: &allow}, func(card proto.Interaction) {
		if card.ToolName != "skill_import" || !strings.Contains(card.Summary, "Example computer") || !strings.Contains(card.Summary, "example-skill") || bytes.Contains(card.ToolInput, []byte("archive")) {
			t.Errorf("unsafe or incomplete install card: %+v", card)
		}
		if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
			t.Error("skill installed before approval")
		}
	})
	installed := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "install", Archive: archive, Reference: preview.Reference}, Environment: preview.Environment})
	<-done
	if installed.Status != "installed" {
		t.Fatalf("install: %+v", installed)
	}
	status := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "status", Reference: preview.Reference}, Environment: preview.Environment})
	if status.Reference != installed.Reference || status.Status != "installed" {
		t.Fatalf("recovery from original preview: %+v", status)
	}
	done = answerSkillImportCard(t, s, proto.Resolution{Allow: &allow}, func(proto.Interaction) {})
	undone := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "undo", Reference: installed.Reference}, Environment: installed.Environment})
	<-done
	if undone.Status != "undone" || undone.Recovery == "" {
		t.Fatalf("undo: %+v", undone)
	}
	if _, err := os.Stat(filepath.Join(undone.Recovery, "SKILL.md")); err != nil {
		t.Fatal("undo lost recovery data", err)
	}
}

func TestSkillImportToolRefusesUnapprovedChangedAndForgedRequests(t *testing.T) {
	s, archive, installedPath := mcpSkillFixture(t)
	preview := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "preview", Archive: archive}})
	for _, raw := range []string{`null`, `{"op":"preview","archive":null}`, `{"op":"preview","path":"/arbitrary"}`, `{"op":"preview","machine_id":"other"}`, `{"op":"undo","reference":"fake"}`, `{"op":"preview","archive":"UEs="} {}`} {
		if out := skillImportCall(context.Background(), s, "skill_import", raw); !out.IsError || len(s.pendingQuestions()) != 0 {
			t.Fatalf("accepted forged request: %s: %+v", raw, out)
		}
	}
	request := skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "install", Archive: archive, Reference: preview.Reference}, Environment: preview.Environment}
	allow := true
	for _, answer := range []proto.Resolution{{Allow: &allow, AnsweredBy: "assistant"}, {Allow: new(bool)}, {Dismiss: true}} {
		done := answerSkillImportCard(t, s, answer, func(proto.Interaction) {})
		raw, _ := json.Marshal(request)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out := skillImportCall(ctx, s, "skill_import", string(raw))
		<-done
		cancel()
		if out.IsError {
			t.Fatalf("decline: %+v", out)
		}
		if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
			t.Fatal("unapproved install")
		}
	}
	request.Environment = "another environment"
	raw, _ := json.Marshal(request)
	if out := skillImportCall(context.Background(), s, "skill_import", string(raw)); !out.IsError || len(s.pendingQuestions()) != 0 || !strings.Contains(out.Content[0].Text, "skill_environment_changed") {
		t.Fatalf("stale environment: %+v", out)
	}
	request.Environment = preview.Environment
	request.Archive = append(archive, 0)
	done := answerSkillImportCard(t, s, proto.Resolution{Allow: &allow}, func(proto.Interaction) {})
	raw, _ = json.Marshal(request)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := skillImportCall(ctx, s, "skill_import", string(raw))
	<-done
	if !out.IsError || !strings.Contains(out.Content[0].Text, "skill_import_changed") || strings.Contains(out.Content[0].Text, "private filesystem") {
		t.Fatalf("changed archive: %+v", out)
	}
}

func TestSkillImportToolRechecksTargetAfterApproval(t *testing.T) {
	s, archive, installedPath := mcpSkillFixture(t)
	preview := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "preview", Archive: archive}})
	allow := true
	done := answerSkillImportCard(t, s, proto.Resolution{Allow: &allow}, func(proto.Interaction) {
		// A changed home changes the execution directory without changing the machine id.
		f := s.hub.(*fakeHub)
		f.mu.Lock()
		f.devices[0].Home += "/changed"
		f.mu.Unlock()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, _ := json.Marshal(skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "install", Archive: archive, Reference: preview.Reference}, Environment: preview.Environment})
	out := skillImportCall(ctx, s, "skill_import", string(raw))
	<-done
	if !out.IsError || !strings.Contains(out.Content[0].Text, "skill_environment_changed") {
		t.Fatalf("changed target: %+v", out)
	}
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Fatal("changed target installed a skill")
	}
}

func TestSkillImportToolLostResponseIsRecoverableWithoutReplay(t *testing.T) {
	s, archive, _ := mcpSkillFixture(t)
	preview := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "preview", Archive: archive}})
	f := s.hub.(*fakeHub)
	call := f.nodes["skills.import"]
	f.nodes["skills.import"] = func(device string, raw json.RawMessage) (any, error) {
		out, err := call(device, raw)
		var in assistantskills.ImportRequest
		_ = json.Unmarshal(raw, &in)
		if err == nil && in.Op == "install" {
			return nil, errors.New("private transport details after publication")
		}
		return out, err
	}
	allow := true
	done := answerSkillImportCard(t, s, proto.Resolution{Allow: &allow}, func(proto.Interaction) {})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, _ := json.Marshal(skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "install", Archive: archive, Reference: preview.Reference}, Environment: preview.Environment})
	out := skillImportCall(ctx, s, "skill_import", string(raw))
	<-done
	if !out.IsError || !strings.Contains(out.Content[0].Text, "outcome may be unknown") || strings.Contains(out.Content[0].Text, "private transport") {
		t.Fatalf("uncertain result: %+v", out)
	}
	status := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "status", Reference: preview.Reference}, Environment: preview.Environment})
	if status.Status != "installed" || status.Reference == preview.Reference {
		t.Fatalf("lost install receipt was not recovered: %+v", status)
	}
	if len(s.pendingQuestions()) != 0 {
		t.Fatal("receipt query requested approval or replayed the install")
	}
}
