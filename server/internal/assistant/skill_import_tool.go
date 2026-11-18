package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func skillImportArguments(raw json.RawMessage) (skillImportInput, error) {
	var in skillImportInput
	var fields map[string]json.RawMessage
	if len(raw) > 6<<20 || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return in, errors.New("skill_import_request")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return in, errors.New("skill_import_request")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || !validSkillImport(in) || len(in.Archive) > assistantskills.MaxImportZIP || len(in.Reference) > 256 || len(in.Environment) > 128 {
		return in, errors.New("skill_import_request")
	}
	if in.Op == "preview" {
		if len(in.Archive) == 0 || in.Reference != "" || in.Environment != "" {
			return in, errors.New("skill_import_request")
		}
	} else if in.Reference == "" || in.Environment == "" || (in.Op == "install") != (len(in.Archive) > 0) {
		return in, errors.New("skill_import_request")
	}
	return in, nil
}

func (s *Service) toolSkillImport(ctx context.Context, machine, self string, raw json.RawMessage) (string, error) {
	in, err := skillImportArguments(raw)
	if err != nil {
		return "", err
	}
	target, err := s.skillImportTarget(ctx)
	if err != nil {
		return "", err
	}
	if in.Op == "install" || in.Op == "undo" {
		// Read the node's exact receipt for the card. No archive bytes or skill
		// instructions enter the permission card, and no model-supplied label is trusted.
		preview, err := s.runSkillImport(ctx, target, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "status", Reference: in.Reference}, Environment: in.Environment})
		if err != nil {
			return "", skillImportToolError(err, false)
		}
		want := "previewed"
		if in.Op == "undo" {
			want = "installed"
		}
		if preview.Status != want {
			return "", errors.New("skill_import_changed: use status to inspect this receipt; nothing was changed")
		}
		shown, _ := json.Marshal(map[string]any{"name": preview.Name, "machine": preview.Machine, "files": preview.Files, "bytes": preview.Bytes})
		summary := i18n.T(s.lang(ctx), "assistant-skill_import."+in.Op, i18n.Args{"name": preview.Name, "machine": preview.Machine, "files": preview.Files})
		ok, text, err := s.confirmSkillImport(ctx, machine, self, "skill_import", summary, shown, preview.ExpiresAt)
		if err != nil || !ok {
			return text, err
		}
	}
	out, err := s.runSkillImport(ctx, target, in)
	if err != nil {
		return "", skillImportToolError(err, in.Op == "install" || in.Op == "undo")
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func skillImportToolError(err error, writing bool) error {
	code := "skill_import_unavailable"
	var coded *rpc.Error
	if errors.As(err, &coded) && coded.Code != "" {
		code = coded.Code
	}
	if writing {
		return fmt.Errorf("%s: the outcome may be unknown; use skill_import status with the same reference and environment before any retry. If the receipt expired or the computer restarted, inspect Settings > Skills; do not assume failure or remove an arbitrary directory", code)
	}
	return fmt.Errorf("%s: no installation or undo was requested; update Persona on the computer if this operation is unsupported", code)
}

func (s *Service) confirmSkillImport(ctx context.Context, machine, self, tool, summary string, shown json.RawMessage, expires time.Time) (bool, string, error) {
	if machine == "" || self == "" {
		return false, "", errors.New("a calling conversation is required")
	}
	ctx, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	now := time.Now().UTC()
	r, err := s.ask(ctx, proto.Interaction{ID: "ix_" + randomHex(6), MachineID: machine, SessionID: self,
		Kind: proto.IxPermission, ToolName: tool, Summary: summary, ToolInput: shown, SessionTitle: "Persona",
		CreatedAt: now, ExpiresAt: expires})
	if err != nil {
		return false, "", err
	}
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	if r.UserAllowed() {
		return true, "", nil
	}
	if r.UserDenied() {
		return false, "The user declined the change: nothing changed.", nil
	}
	return false, prompts.RenderSettingUnanswered(), nil
}
