package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

const kbMarkedDay = `{"day":"2026-01-02","messages":[{"n":1,"text":"20:00 me: assistant words","sent_by":"assistant","sent_ledger_id":7,"recalled_at":"2026-01-03T00:00:00Z"},{"n":2,"text":"20:01 me: my own words"}],"prev":"","next":"","sent_origin":"available"}`

func checkKBDayOrigin(t *testing.T, result assistantwire.KBDayResult, allowed bool) {
	t.Helper()
	want := "missing_scope"
	if allowed {
		want = "available"
	}
	if result.SentOrigin != want || len(result.Messages) != 2 {
		t.Fatalf("day response: %+v", result)
	}
	first, second := result.Messages[0], result.Messages[1]
	if allowed {
		if first.SentBy != "assistant" || first.SentLedgerID != 7 || first.RecalledAt == nil {
			t.Fatalf("matched row lost origin: %+v", first)
		}
	} else if first.SentBy != "" || first.SentLedgerID != 0 || first.RecalledAt != nil {
		t.Fatalf("origin disclosed without sent.read: %+v", first)
	}
	if second.SentBy != "" || second.SentLedgerID != 0 || second.RecalledAt != nil {
		t.Fatalf("user's row marked: %+v", second)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var serialized struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(raw, &serialized); err != nil {
		t.Fatal(err)
	}
	for i, row := range serialized.Messages {
		if allowed && i == 0 {
			continue
		}
		for _, field := range []string{"sent_by", "sent_ledger_id", "recalled_at"} {
			if _, ok := row[field]; ok {
				t.Fatalf("row %d must omit %s: %s", i, field, raw)
			}
		}
	}
}

func TestExtensionKBDayOriginPermissions(t *testing.T) {
	for _, tc := range []struct {
		id      string
		grant   extension.Permissions
		allowed bool
	}{
		{"reader", extension.Permissions{Hub: []string{extension.ScopeKBRead}}, false},
		{"curator", extension.Permissions{Hub: []string{extension.ScopeKBRead, extension.ScopeSentRead}}, true},
		{"legacy", extension.Permissions{HubAPI: true}, true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			h := newTestManager(t)
			spec := kbExtensionSpec(t, h, tc.id, tc.grant)
			params := json.RawMessage(`{"id":"example-chat","day":"2026-01-02"}`)
			h.opts.Assistant = func(_ context.Context, method string, raw json.RawMessage) (json.RawMessage, error) {
				want := assistantwire.MethodKBDay
				if tc.allowed {
					want = assistantwire.MethodKBExtensionDay
				}
				if method != want || string(raw) != string(params) {
					t.Errorf("assistant got %s %s; want %s %s", method, raw, want, params)
				}
				// Even a marked upstream response must be stripped without the grant.
				return json.RawMessage(kbMarkedDay), nil
			}
			out, err := spec.Handle(context.Background(), assistantwire.MethodKBDay, params)
			if err != nil {
				t.Fatal(err)
			}
			checkKBDayOrigin(t, out.(assistantwire.KBDayResult), tc.allowed)
		})
	}
	h := newTestManager(t)
	spec := kbExtensionSpec(t, h, "sent-only", extension.Permissions{Hub: []string{extension.ScopeSentRead}})
	_, err := spec.Handle(context.Background(), assistantwire.MethodKBDay, json.RawMessage(`{}`))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != extension.CodePermissionDenied {
		t.Fatalf("sent.read alone must not open the archive: %v", err)
	}
}

func TestExtensionKBDayOriginOlderAssistant(t *testing.T) {
	h := newTestManager(t)
	spec := kbExtensionSpec(t, h, "curator", extension.Permissions{Hub: []string{extension.ScopeKBRead, extension.ScopeSentRead}})
	var methods []string
	h.opts.Assistant = func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		methods = append(methods, method)
		if method == assistantwire.MethodKBExtensionDay {
			return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method"}
		}
		return json.RawMessage(`{"day":"2026-01-02","messages":[],"prev":"","next":""}`), nil
	}
	out, err := spec.Handle(context.Background(), assistantwire.MethodKBDay, json.RawMessage(`{"id":"example-chat","day":"2026-01-02"}`))
	if err != nil || out.(assistantwire.KBDayResult).SentOrigin != "unavailable" || len(methods) != 2 || methods[1] != assistantwire.MethodKBDay {
		t.Fatalf("older assistant: %+v %v; calls %v", out, err, methods)
	}
	h.opts.Assistant = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return nil, &rpc.Error{Code: "query_failed", Message: "ledger unavailable"}
	}
	_, err = spec.Handle(context.Background(), assistantwire.MethodKBDay, json.RawMessage(`{"id":"example-chat","day":"2026-01-02"}`))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "query_failed" {
		t.Fatalf("ledger failure must not look like no marks: %v", err)
	}
}

func TestExtensionKBDayOriginFromProcess(t *testing.T) {
	h, _ := extensionTestHub(t)
	a := withAssistant(h)
	for _, method := range []string{assistantwire.MethodKBDay, assistantwire.MethodKBExtensionDay} {
		a.on(method, func(raw json.RawMessage) (any, error) {
			var params assistantwire.KBDayParams
			if err := json.Unmarshal(raw, &params); err != nil || params.ID != "example-chat" || params.Day != "2026-01-02" {
				t.Errorf("day params: %s %v", raw, err)
			}
			return json.RawMessage(kbMarkedDay), nil
		})
	}
	for _, tc := range []struct {
		id, scopes string
		allowed    bool
	}{
		{"day-reader", `["kb.read"]`, false},
		{"day-curator", `["kb.read","sent.read"]`, true},
	} {
		startKBProcess(t, h, tc.id, tc.scopes)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var result assistantwire.KBDayResult
		err := h.extensions.get(tc.id).proc.Call(ctx, "kbtest.day", assistantwire.KBDayParams{ID: "example-chat", Day: "2026-01-02"}, &result)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		checkKBDayOrigin(t, result, tc.allowed)
	}
	if len(a.got(assistantwire.MethodKBExtensionDay)) != 1 || len(a.got(assistantwire.MethodKBDay)) != 1 {
		t.Fatal("each process must use the method its actual grant allows")
	}
}
