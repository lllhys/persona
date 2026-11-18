package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/rpc"
)

func (n *Node) importSkill(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		assistantskills.ImportRequest
		Dir   string `json:"dir"`
		Scope string `json:"scope"`
	}
	if len(raw) > 6<<20 {
		return nil, &rpc.Error{Code: "skill_zip_limits", Message: "skill_zip_limits"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil || d.Decode(new(any)) != io.EOF {
		return nil, &rpc.Error{Code: "skill_import_request", Message: "skill_import_request"}
	}
	// This is a manager-to-node RPC, not an assistant-facing tool. Its scope and directory must agree.
	id, machine, ok := strings.Cut(in.Scope, "\x00")
	if !ok || id == "" || machine == "" || strings.ContainsRune(machine, '\x00') || strings.ContainsAny(id, `/\.`) ||
		in.Dir != path.Join(compat.StateDir, identity.AssistantDirName(id)) {
		return nil, &rpc.Error{Code: "skill_environment_changed", Message: "skill_environment_changed"}
	}
	cwd, err := n.preparedDir(in.Dir)
	if err != nil {
		return nil, &rpc.Error{Code: "skill_environment_changed", Message: "skill_environment_changed"}
	}
	out, err := n.skillImports.Import(ctx, assistantskills.ImportTarget{Scope: in.Scope, Cwd: cwd}, in.ImportRequest)
	var e *assistantskills.ImportError
	if errors.As(err, &e) {
		return nil, &rpc.Error{Code: e.Code, Message: e.Code}
	}
	return out, err
}
