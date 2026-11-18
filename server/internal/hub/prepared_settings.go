package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/nwverse/persona/server/internal/approval"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// preparedSettings retains executable hub operations in the hub. The public
// interaction and the assistant's journal contain only the random handle ID.
type preparedSettings struct {
	mu   sync.Mutex
	byID map[string]preparedSetting
}

type preparedSetting struct {
	manager string
	apply   func(context.Context, proto.Resolution) (string, error)
}

func (h *Hub) retainPreparedSetting(manager, id string, apply func(context.Context, proto.Resolution) (string, error)) error {
	if manager == "" || id == "" || apply == nil {
		return errors.New("a prepared setting needs its manager, interaction and apply path")
	}
	p := &h.prepared
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byID == nil {
		p.byID = map[string]preparedSetting{}
	}
	if _, exists := p.byID[id]; exists {
		return errors.New("the prepared setting handle already exists")
	}
	p.byID[id] = preparedSetting{manager: manager, apply: apply}
	return nil
}

func (h *Hub) dropPreparedSetting(manager, id string) {
	p := &h.prepared
	p.mu.Lock()
	if old, ok := p.byID[id]; ok && old.manager == manager {
		delete(p.byID, id)
	}
	p.mu.Unlock()
}

// applyPreparedSetting takes the handle before executing, so a duplicate bus
// call can never apply twice even if the assistant's own claim is retried.
func (h *Hub) applyPreparedSetting(ctx context.Context, manager, id string, r proto.Resolution) (string, error) {
	p := &h.prepared
	p.mu.Lock()
	old, ok := p.byID[id]
	if ok && old.manager == manager {
		delete(p.byID, id)
	} else {
		ok = false
	}
	p.mu.Unlock()
	if !ok {
		return "", errors.New("the prepared setting is no longer pending")
	}
	return old.apply(ctx, r)
}

// waitPreparedSetting asks the assistant to coordinate the card while the hub
// retains apply. Only Clarifying leaves the handle retained after this call;
// every other return cleans it up (a successful apply has already taken it).
func (h *Hub) waitPreparedSetting(ctx context.Context, manager string, ix proto.Interaction, replacement string,
	apply func(context.Context, proto.Resolution) (string, error), report func(proto.MCPWaitKind)) (approval.Outcome, error) {
	if err := h.retainPreparedSetting(manager, ix.ID, apply); err != nil {
		return approval.Outcome{}, err
	}
	keep := false
	defer func() {
		if !keep {
			h.dropPreparedSetting(manager, ix.ID)
		}
	}()
	if report != nil {
		report(proto.MCPWaitApproval)
		defer report(proto.MCPWaitComputer)
	}
	if h.askVia != nil { // the existing settings tests play the manager
		r, err := h.askVia(ctx, ix)
		if err != nil {
			return approval.Outcome{}, err
		}
		switch {
		case r.UserAllowed():
			text, err := h.applyPreparedSetting(ctx, manager, ix.ID, r)
			return approval.Outcome{Text: text}, err
		case r.UserDenied():
			return approval.Outcome{Text: "The user declined the change: nothing changed."}, nil
		default:
			return approval.Outcome{Text: prompts.RenderSettingUnanswered()}, nil
		}
	}
	if h.machineOwner(ix.MachineID) != manager {
		return approval.Outcome{}, errors.New("the prepared setting's manager no longer owns the machine")
	}
	var out approval.Outcome
	err := h.asstOf(manager).callAsIs(ctx, assistantwire.MethodFleetAskPrepared,
		map[string]any{"interaction": ix, "replacement": replacement}, &out)
	if err != nil {
		return approval.Outcome{}, err
	}
	keep = out.Clarifying
	return out, nil
}

func init() {
	handleBus(assistantwire.MethodFleetApprovalApply, (*Hub).busPreparedSettingApply)
	handleBus(assistantwire.MethodFleetApprovalDiscard, (*Hub).busPreparedSettingDiscard)
}

func (h *Hub) busPreparedSettingApply(ctx context.Context, c *busConn, params json.RawMessage) (any, error) {
	var p assistantwire.FleetApprovalApply
	if err := json.Unmarshal(params, &p); err != nil || p.InteractionID == "" {
		return nil, fmt.Errorf("invalid prepared setting apply: %w", err)
	}
	text, err := h.applyPreparedSetting(ctx, c.ident.ID, p.InteractionID, p.Resolution)
	if errors.Is(err, approval.ErrStale) {
		return assistantwire.FleetApprovalApplyResult{Stale: true}, nil
	}
	if err != nil {
		return nil, err
	}
	return assistantwire.FleetApprovalApplyResult{Text: text}, nil
}

func (h *Hub) busPreparedSettingDiscard(_ context.Context, c *busConn, params json.RawMessage) (any, error) {
	var p assistantwire.FleetApprovalDiscard
	if err := json.Unmarshal(params, &p); err != nil || p.InteractionID == "" {
		return nil, fmt.Errorf("invalid prepared setting discard: %w", err)
	}
	h.dropPreparedSetting(c.ident.ID, p.InteractionID)
	return map[string]any{"ok": true}, nil
}
