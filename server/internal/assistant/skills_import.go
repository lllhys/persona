package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/rpc"
)

type skillImportInput struct {
	assistantskills.ImportRequest
	Environment string `json:"environment,omitempty"`
}

type skillImportOutput struct {
	assistantskills.ImportResult
	Environment string `json:"environment"`
	Machine     string `json:"machine"`
}

type skillImportTarget struct {
	machine, name, dir, scope, environment string
}

func (s *Service) skillImportTarget(ctx context.Context) (skillImportTarget, error) {
	machine := s.settings(ctx).MachineID
	d, ok := s.device(ctx, machine)
	if !ok || machine == "" || d.Home == "" || s.isWorkMachine(machine) {
		return skillImportTarget{}, s.missingAssistantMachine(ctx)
	}
	id := s.id
	if id == "" {
		id = assistantwire.AssistantID
	}
	scope := id + "\x00" + machine
	sum := sha256.Sum256([]byte(scope + "\x00" + s.assistantCwd(d)))
	return skillImportTarget{machine, d.Name, s.assistantRel(), scope, hex.EncodeToString(sum[:])}, nil
}

func (s *Service) skillImportRoutes() {
	s.handle("POST /api/v1/skills/import", s.importSkill)
}

func (s *Service) importSkill(w http.ResponseWriter, r *http.Request) {
	if !fullRole(r) {
		apierr.Write(w, http.StatusForbidden, errors.New(http.StatusText(http.StatusForbidden)))
		return
	}
	before, err := s.skillImportTarget(r.Context())
	if err != nil {
		apierr.Call(w, err)
		return
	}
	var in skillImportInput
	// Base64 plus JSON framing fits below 6 MiB. Unknown fields reject browser-selected machines or paths.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		status := http.StatusBadRequest
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = http.StatusRequestEntityTooLarge
		}
		apierr.WriteCode(w, status, "skill_import_request", errors.New("skill_import_request"))
		return
	}
	if decoder.Decode(new(any)) != io.EOF || len(in.Archive) > assistantskills.MaxImportZIP ||
		!validSkillImport(in) {
		apierr.WriteCode(w, http.StatusBadRequest, "skill_import_request", errors.New("skill_import_request"))
		return
	}
	out, err := s.runSkillImport(r.Context(), before, in)
	if err != nil {
		writeSkillImportError(w, err)
		return
	}
	apierr.WriteJSON(w, out)
}

func validSkillImport(in skillImportInput) bool {
	switch in.Op {
	case "preview", "install", "cancel", "undo", "status":
		return true
	}
	return false
}

// The HTTP and MCP entry points share target locking, node calls and error handling.
// The lock is never held while waiting for a person's confirmation.
func (s *Service) runSkillImport(ctx context.Context, before skillImportTarget, in skillImportInput) (skillImportOutput, error) {
	var empty skillImportOutput
	s.prefs.change.Lock()
	defer s.prefs.change.Unlock()
	target, err := s.skillImportTarget(ctx)
	if err != nil {
		return empty, err
	}
	if target != before || (in.Op != "preview" && in.Environment != target.environment) {
		return empty, &rpc.Error{Code: "skill_environment_changed", Message: "skill_environment_changed"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out assistantskills.ImportResult
	params := map[string]any{"dir": target.dir, "scope": target.scope, "op": in.Op, "archive": in.Archive, "reference": in.Reference}
	if err := s.call(ctx, target.machine, "skills.import", params, &out); err != nil {
		return empty, cleanSkillImportError(err)
	}
	return skillImportOutput{out, target.environment, target.name}, nil
}

func cleanSkillImportError(err error) error {
	var coded *rpc.Error
	isRPC := errors.As(err, &coded)
	if err.Error() == "unknown method skills.import" || (isRPC && (coded.Code == "unknown_method" || coded.Message == "unknown method skills.import")) {
		return &rpc.Error{Code: "node_outdated", Message: "node_outdated"}
	}
	if isRPC && strings.HasPrefix(coded.Code, "skill_") {
		return &rpc.Error{Code: coded.Code, Message: coded.Code}
	}
	if errors.Is(err, apierr.ErrOffline) || isRPC && coded.Code != "" {
		return err
	}
	return &rpc.Error{Code: apierr.NodeCode(err), Message: apierr.NodeCode(err)}
}

func writeSkillImportError(w http.ResponseWriter, err error) {
	var coded *rpc.Error
	if errors.As(err, &coded) && coded.Code == "node_outdated" {
		apierr.WriteCode(w, http.StatusConflict, coded.Code, errors.New(coded.Code))
	} else if errors.As(err, &coded) && strings.HasPrefix(coded.Code, "skill_") {
		status := http.StatusUnprocessableEntity
		switch coded.Code {
		case "skill_conflict", "skill_import_expired", "skill_import_changed", "skill_environment_changed", "skill_undo_changed":
			status = http.StatusConflict
		case "skill_import_busy":
			status = http.StatusTooManyRequests
		case "skill_import_io":
			status = http.StatusInternalServerError
		case "skill_import_request":
			status = http.StatusBadRequest
		}
		apierr.WriteCode(w, status, coded.Code, errors.New(coded.Code))
	} else {
		apierr.Call(w, err)
	}
}
