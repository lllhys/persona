package extmgr

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// extensionKBCall exposes only the public knowledge-base methods. Internal kb.* calls (notably kb.forget and
// kb.posts_*) never pass this allowlist, even with the old full-hub grant.
func (ps *extensions) extensionKBCall(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	var scope string
	switch method {
	case assistantwire.MethodKBList, assistantwire.MethodKBGet, assistantwire.MethodKBDay, assistantwire.MethodKBSearch, assistantwire.MethodKBOwner, assistantwire.MethodKBRecent:
		scope = extension.ScopeKBRead
	case assistantwire.MethodKBUpdate:
		scope = extension.ScopeKBWrite
	default:
		return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method " + method}
	}
	if ps.reg == nil || ps.reg.GetExact(id) == nil || !ps.reg.HubScopeAllowed(id, scope) {
		data, _ := json.Marshal(map[string]string{"scope": scope})
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: method + ": requires " + scope, Data: data}
	}
	if ps.h == nil {
		return nil, &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: errAssistantOffline.Error()}
	}
	var out json.RawMessage
	var ask any = params
	day := method == assistantwire.MethodKBDay
	sentRead := day && ps.reg.HubScopeAllowed(id, extension.ScopeSentRead)
	if sentRead {
		method = assistantwire.MethodKBExtensionDay
	}
	if method == assistantwire.MethodKBUpdate { // the write is the extension's: say whose (#6100)
		storageID, err := ps.assistantStorageID(ctx, ps.reg.SnapshotExact(id))
		if err != nil {
			return nil, err
		}
		method, ask = assistantwire.MethodKBExtensionUpdate, assistantwire.KBExtensionUpdate{Extension: id, StorageID: storageID, Params: params}
	}
	err := ps.h.kbAsk(ctx, method, ask, &out)
	oldAssistant := false
	if sentRead {
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == extension.CodeUnknownMethod {
			// An older assistant can still supply the archive, but cannot attest to its origin.
			oldAssistant = true
			err = ps.h.kbAsk(ctx, assistantwire.MethodKBDay, ask, &out)
		}
	}
	if err != nil {
		return nil, err
	}
	if day {
		var result assistantwire.KBDayResult
		if err := json.Unmarshal(out, &result); err != nil {
			return nil, err
		}
		if !sentRead {
			result.SentOrigin = "missing_scope"
		} else if oldAssistant || result.SentOrigin == "" {
			result.SentOrigin = "unavailable"
		}
		if result.SentOrigin != "available" {
			for i := range result.Messages {
				result.Messages[i].SentBy = ""
				result.Messages[i].SentLedgerID = 0
				result.Messages[i].RecalledAt = nil
			}
		}
		return result, nil
	}
	return out, nil
}
