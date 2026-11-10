package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

type cindyPendingInteraction struct {
	RequestID string          `json:"requestId"`
	Request   json.RawMessage `json:"request"`
}

func (b *cindyBridge) syncPrompts(ctx context.Context, cindyID, personaID string, pending []cindyPendingInteraction) {
	for _, p := range pending {
		if p.RequestID == "" {
			continue
		}
		key := cindyID + "/" + p.RequestID
		b.mu.Lock()
		a := b.asks[key]
		due := a == nil || (!a.retryAt.IsZero() && time.Now().After(a.retryAt))
		if due {
			b.asks[key] = &cindyAsk{cindyID: cindyID, reqID: p.RequestID}
		}
		b.mu.Unlock()
		if due {
			b.askInteraction(ctx, key, cindyID, personaID, p)
		}
	}
}

func (b *cindyBridge) prunePrompts(ctx context.Context, waiting map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, a := range b.asks {
		if _, ok := waiting[a.cindyID]; ok {
			continue
		}
		if a.cancel != nil {
			a.cancel()
		}
		delete(b.asks, key)
	}
}

func (b *cindyBridge) askInteraction(ctx context.Context, key, cindyID, personaID string, p cindyPendingInteraction) {
	ix := mapCindyInteraction(personaID, p)
	if ix == nil {
		return
	}
	sum := sha256.Sum256([]byte(key))
	ix.ID = "cindy_" + hex.EncodeToString(sum[:8])
	askCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	if a := b.asks[key]; a != nil {
		a.cancel, a.retryAt = cancel, time.Time{}
	}
	b.mu.Unlock()
	go func() {
		defer cancel()
		var res proto.Resolution
		err := b.peer.Call(askCtx, "interaction.create", map[string]any{"interaction": ix}, &res)
		if askCtx.Err() != nil {
			if ctx.Err() == nil {
				_ = b.peer.Notify("interaction.cancel", map[string]any{"id": ix.ID})
			}
			return
		}
		if err != nil {
			b.mu.Lock()
			if a := b.asks[key]; a != nil {
				a.retryAt = time.Now().Add(cindyAskRetry)
			}
			b.mu.Unlock()
			return
		}
		if res.Dismiss {
			return
		}
		decision := mapCindyDecision(ix.Kind, res)
		_ = b.postJSON(ctx, "/api/interactions/respond", map[string]any{
			"sessionId": cindyID,
			"requestId": p.RequestID,
			"decision":  decision,
		}, nil)
	}()
}

func mapCindyInteraction(personaID string, p cindyPendingInteraction) *proto.Interaction {
	var raw struct {
		Kind        string          `json:"kind"`
		ToolName    string          `json:"toolName"`
		ToolInput   json.RawMessage `json:"toolInput"`
		Description string          `json:"description"`
		Reason      string          `json:"reason"`
		Plan        string          `json:"plan"`
		Questions   []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(p.Request, &raw) != nil {
		return nil
	}
	ix := &proto.Interaction{SessionID: personaID}
	switch raw.Kind {
	case "plan":
		ix.Kind = proto.IxPlan
		ix.Plan = firstOf(raw.Plan, raw.Description)
		return ix
	case "question":
		for _, q := range raw.Questions {
			pq := proto.Question{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
			for _, o := range q.Options {
				pq.Options = append(pq.Options, proto.QuestionOption{Label: o.Label, Description: o.Description})
			}
			ix.Questions = append(ix.Questions, pq)
		}
		if len(ix.Questions) == 0 {
			return nil
		}
		ix.Kind = proto.IxQuestion
		return ix
	default:
		ix.Kind = proto.IxPermission
		ix.ToolName = firstOf(raw.ToolName, "Tool")
		if len(raw.ToolInput) > 0 && string(raw.ToolInput) != "null" {
			ix.ToolInput = raw.ToolInput
			ix.Summary = toolSummary(ix.ToolName, raw.ToolInput)
		}
		if ix.Summary == "" {
			ix.Summary = firstOf(raw.Description, ix.ToolName)
		}
		ix.Reason = raw.Reason
		return ix
	}
}

func mapCindyDecision(kind string, res proto.Resolution) map[string]any {
	switch kind {
	case proto.IxQuestion:
		return map[string]any{"kind": "question", "answers": res.Answers}
	case proto.IxPlan:
		allow := res.Approve != nil && *res.Approve
		if !allow {
			return map[string]any{"kind": "permission", "behavior": "deny", "reason": firstOf(res.Feedback, res.Message, "denied")}
		}
		return map[string]any{"kind": "permission", "behavior": "allow"}
	default:
		allow := res.Allow != nil && *res.Allow
		if !allow {
			return map[string]any{"kind": "permission", "behavior": "deny", "reason": firstOf(res.Message, res.Feedback, "denied")}
		}
		return map[string]any{"kind": "permission", "behavior": "allow", "always": res.Always}
	}
}
