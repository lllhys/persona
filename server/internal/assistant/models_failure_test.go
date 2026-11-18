package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestProviderTestClassifiesSyntheticGatewayFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantCode, wantHead, wantDetail, notShown string
		status                                               int
	}{
		{
			name: "OpenAI nested 400 model code", status: http.StatusBadRequest,
			body:       `{"error":{"code":"model_not_found","message":"synthetic upstream detail ` + fakeKey + `"}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "synthetic upstream detail ***",
		},
		{
			name: "Anthropic nested 404 model code", status: http.StatusNotFound,
			body:       `{"type":"error","error":{"type":"not_found_error","code":"model_not_found","message":"synthetic model detail"}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "synthetic model detail",
		},
		{
			name: "deeper nested 404 model code", status: http.StatusNotFound,
			body:       `{"error":{"message":"outer synthetic message","error":{"code":"model_not_found","message":"actual deeper upstream message"}}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "actual deeper upstream message",
			notShown:   `{"error"`,
		},
		{
			name: "top-level 400 model code", status: http.StatusBadRequest,
			body:       `{"code":"model_not_found","message":"synthetic top-level detail"}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "synthetic top-level detail",
		},
		{
			name: "top-level 404 model code", status: http.StatusNotFound,
			body:       `{"code":"model_not_found","message":"synthetic missing model"}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "synthetic missing model",
		},
		{
			name: "OpenAI nested model wording", status: http.StatusBadRequest,
			body:       `{"error":{"message":"The model synthetic-model does not exist"}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "synthetic-model does not exist",
		},
		{
			name: "Anthropic nested model wording", status: http.StatusNotFound,
			body:       `{"type":"error","error":{"type":"not_found_error","message":"Synthetic service knows no model synthetic-model"}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "knows no model synthetic-model",
		},
		{
			name: "selected model unavailable to the API key", status: http.StatusNotFound,
			body:       `{"type":"error","error":{"type":"not_found_error","message":"The selected model is unavailable to this API key"}}`,
			wantCode:   "model_not_found",
			wantHead:   "The gateway is reachable",
			wantDetail: "The selected model is unavailable to this API key",
		},
		{
			name: "model service unavailable is not a missing model", status: http.StatusNotFound,
			body:       `{"type":"error","error":{"type":"api_error","message":"model service unavailable"}}`,
			wantCode:   "not_found",
			wantHead:   "Not found (404): check the Base URL, model and configured protocol",
			wantDetail: "model service unavailable",
		},
		{
			name: "ordinary unsupported endpoint", status: http.StatusNotFound,
			body:       `{"error":{"code":"unsupported_endpoint","message":"unsupported endpoint: POST /v1/messages"}}`,
			wantCode:   "not_found",
			wantHead:   "Not found (404): check the Base URL, model and configured protocol",
			wantDetail: "unsupported endpoint",
		},
		{
			name: "plain missing endpoint", status: http.StatusNotFound,
			body:       `404 page not found`,
			wantCode:   "not_found",
			wantHead:   "Not found (404): check the Base URL, model and configured protocol",
			wantDetail: "404 page not found",
		},
		{
			name: "402 budget exhausted", status: http.StatusPaymentRequired,
			body:       `{"error":{"code":"budget_exhausted","message":"synthetic budget exhausted"}}`,
			wantCode:   "quota_exhausted",
			wantHead:   "Budget or quota exhausted (402)",
			wantDetail: "synthetic budget exhausted",
		},
		{
			name: "402 nested quota exhausted", status: http.StatusPaymentRequired,
			body:       `{"type":"error","error":{"code":"account_quota_exhausted","message":"synthetic quota exhausted"}}`,
			wantCode:   "quota_exhausted",
			wantHead:   "Budget or quota exhausted (402)",
			wantDetail: "synthetic quota exhausted",
		},
		{
			name: "unknown code fallback", status: http.StatusBadRequest,
			body:       `{"error":{"code":"future_code","message":"synthetic request rejected"}}`,
			wantHead:   "The gateway returned 400",
			wantDetail: "synthetic request rejected",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(gw.Close)
			s, _ := launcherTestService(t)
			out := s.providerTest(i18n.With(context.Background(), i18n.En), probeInput{BaseURL: gw.URL, APIKey: fakeKey, Model: "synthetic-model"})
			code, _ := out["code"].(string)
			msg, _ := out["error"].(string)
			if out["ok"] != false || code != tc.wantCode || !strings.Contains(msg, tc.wantHead) || !strings.Contains(msg, tc.wantDetail) {
				t.Fatalf("providerTest() = %v, want code %q, head %q and detail %q", out, tc.wantCode, tc.wantHead, tc.wantDetail)
			}
			if strings.Contains(msg, fakeKey) {
				t.Fatalf("upstream detail leaked the key: %q", msg)
			}
			if tc.notShown != "" && strings.Contains(msg, tc.notShown) {
				t.Fatalf("error shows the raw envelope instead of its message: %q", msg)
			}
		})
	}
}

func TestProviderDiscoverKeepsMissingEndpointClassification(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"synthetic model missing"}}`))
	}))
	t.Cleanup(gw.Close)
	s, _ := launcherTestService(t)
	out := s.providerDiscover(i18n.With(context.Background(), i18n.En), probeInput{BaseURL: gw.URL, APIKey: fakeKey})
	msg, _ := out["error"].(string)
	if out["code"] != "not_found" || !strings.Contains(msg, "Model-list endpoint not found (404)") || strings.Contains(msg, "synthetic model missing") {
		t.Fatalf("providerDiscover() = %v, want the established missing model-list endpoint result", out)
	}
}

func syntheticModelCall(ctx context.Context, s *Service) error {
	raw, _ := json.Marshal(assistantwire.ExtensionComplete{Extension: "kb-curator", Params: json.RawMessage(`{"messages":[{"role":"user","content":"x"}]}`)})
	_, ok, err := s.modelsRequest(ctx, assistantwire.MethodModelsExtensionComplete, raw)
	if !ok {
		return errors.New("model call not answered")
	}
	return err
}

func TestManagerDirectCallClassifiesSyntheticFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantCode, wantExplanation, wantDetail string
		status                                            int
	}{
		{
			name: "model unavailable", status: http.StatusNotFound,
			body:            `{"error":{"code":"model_not_found","message":"synthetic direct model detail"}}`,
			wantCode:        assistantwire.CodeModelFailed,
			wantExplanation: "The gateway is reachable",
			wantDetail:      "synthetic direct model detail",
		},
		{
			name: "context too long", status: http.StatusBadRequest,
			body:            `{"type":"error","code":"context_length_exceeded","message":"synthetic direct context detail"}`,
			wantCode:        assistantwire.CodeModelFailed,
			wantExplanation: "The input exceeds this model’s context window",
			wantDetail:      "synthetic direct context detail",
		},
		{
			name: "unknown code remains model failed", status: http.StatusBadRequest,
			body:       `{"error":{"code":"future_code","message":"synthetic direct fallback detail"}}`,
			wantCode:   assistantwire.CodeModelFailed,
			wantDetail: "synthetic direct fallback detail",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := rawModelService(t, "", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := syntheticModelCall(i18n.With(context.Background(), i18n.En), s)
			var re *rpc.Error
			if !errors.As(err, &re) || re.Code != tc.wantCode || !strings.Contains(re.Message, tc.wantDetail) {
				t.Fatalf("model call error = %v, want code %q and detail %q", err, tc.wantCode, tc.wantDetail)
			}
			if tc.wantExplanation != "" && !strings.Contains(re.Message, tc.wantExplanation) {
				t.Fatalf("model call error = %q, want explanation %q", re.Message, tc.wantExplanation)
			}
			if tc.wantExplanation == "" && (strings.Contains(re.Message, "The gateway is reachable") || strings.Contains(re.Message, "context window. Shorten")) {
				t.Fatalf("unclassified error acquired a classified explanation: %q", re.Message)
			}
		})
	}
}

func TestManagerDirectCallUsesManagerLanguageWithoutRequestLanguage(t *testing.T) {
	s := rawModelService(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"synthetic language detail"}}`))
	})
	s.hub.(*fakeHub).hello.Locale = string(i18n.Ja)
	err := syntheticModelCall(context.Background(), s)
	var re *rpc.Error
	if !errors.As(err, &re) || !strings.Contains(re.Message, "ゲートウェイには接続できますが") || !strings.Contains(re.Message, "synthetic language detail") {
		t.Fatalf("model call error = %v, want the manager's Japanese explanation and upstream detail", err)
	}
}

func TestManagerDirectCallKeepsPauseAndRefusalMappings(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantCode string
		status               int
	}{
		{
			name: "pause", status: http.StatusTooManyRequests,
			body:     `{"error":{"code":"account_quota_exhausted","message":"synthetic quota exhausted"}}`,
			wantCode: assistantwire.CodeModelPaused,
		},
		{
			name: "refusal", status: http.StatusOK,
			body:     `{"model":"kb-model","content":[],"stop_reason":"refusal","usage":{"input_tokens":1,"output_tokens":1}}`,
			wantCode: assistantwire.CodeModelRefused,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := rawModelService(t, "", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := syntheticModelCall(i18n.With(context.Background(), i18n.En), s)
			var re *rpc.Error
			if !errors.As(err, &re) || re.Code != tc.wantCode {
				t.Fatalf("model call error = %v, want unchanged code %q", err, tc.wantCode)
			}
			if strings.Contains(re.Message, "The gateway is reachable") || strings.Contains(re.Message, "The input exceeds") {
				t.Fatalf("existing mapping acquired a new explanation: %q", re.Message)
			}
		})
	}
}

func TestModelCallErrorKeepsNotConfiguredAndTimeoutMappings(t *testing.T) {
	_, notConfigured := ai.New(ai.Config{}).Chat(context.Background(), ai.Request{})
	err := modelCallError(context.Background(), i18n.En, notConfigured)
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != assistantwire.CodeModelUnavailable || re.Message != "model.complete: "+notConfigured.Error() {
		t.Fatalf("not-configured error = %v, want its established code and text", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err = modelCallError(ctx, i18n.En, errors.New("synthetic upstream failure"))
	if !errors.As(err, &re) || re.Code != assistantwire.CodeModelTimeout || re.Message != "model.complete: timed out" {
		t.Fatalf("timeout error = %v, want its established code and text", err)
	}
}

func TestSavedDirectAPICheckExplainsClassifiedFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantExplanation string
		status                      int
	}{
		{
			name: "model unavailable", status: http.StatusNotFound,
			body:            `{"error":{"code":"model_not_found","message":"synthetic saved model detail"}}`,
			wantExplanation: "The gateway is reachable",
		},
		{
			name: "context too long", status: http.StatusBadRequest,
			body:            `{"error":{"code":"context_length_exceeded","message":"synthetic saved context detail"}}`,
			wantExplanation: "The input exceeds this model’s context window",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(gw.Close)
			s, mux := launcherTestService(t)
			s.hub.(*fakeHub).hello.Locale = string(i18n.En)
			create := fmt.Sprintf(`{"name":"direct","kind":"api","base_url":%q,"model":"synthetic-model","api_key":%q}`, gw.URL, fakeKey)
			if code, raw := launcherCall(t, mux, http.MethodPost, "/api/v1/launchers", create); code != http.StatusOK {
				t.Fatalf("create direct provider: %d %s", code, raw)
			}
			code, raw := launcherCall(t, mux, http.MethodPost, "/api/v1/launchers/direct/check", "")
			if code != http.StatusOK {
				t.Fatalf("check direct provider: %d %s", code, raw)
			}
			var out struct {
				Check proto.LauncherCheck `json:"check"`
			}
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}
			if out.Check.OK || !strings.Contains(out.Check.Error, tc.wantExplanation) || !strings.Contains(out.Check.Error, "synthetic saved") {
				t.Fatalf("check = %+v, want explanation %q and upstream detail", out.Check, tc.wantExplanation)
			}
		})
	}
}

func TestChatProbeTokenCompatibilityRequestAllowedWithNoRetryHeader(t *testing.T) {
	calls := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls == 1 {
			if body["max_tokens"] != float64(256) || body["max_completion_tokens"] != nil {
				t.Errorf("initial payload: %v", body)
			}
			w.Header().Set("X-Should-Retry", "false")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"max_tokens is not supported; use max_completion_tokens"}}`))
			return
		}
		if body["max_completion_tokens"] != float64(256) || body["max_tokens"] != nil {
			t.Errorf("corrected payload: %v", body)
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	t.Cleanup(gw.Close)
	s, _ := launcherTestService(t)
	out := s.providerTest(i18n.With(context.Background(), i18n.En), probeInput{BaseURL: gw.URL, Model: "synthetic-model", API: "chat"})
	if calls != 2 || out["ok"] != true || out["error"] != nil {
		t.Fatalf("calls = %d, providerTest() = %v, want the corrected second request to succeed", calls, out)
	}
}

func TestChatProbeReasoningCompatibilityRequestAllowedWithNoRetryHeader(t *testing.T) {
	calls := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls == 1 {
			if body["reasoning_effort"] != "high" {
				t.Errorf("initial payload: %v", body)
			}
			w.Header().Set("X-Should-Retry", "false")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'reasoning_effort' is not supported with this model."}}`))
			return
		}
		if _, present := body["reasoning_effort"]; present {
			t.Errorf("corrected payload kept the rejected field: %v", body)
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	t.Cleanup(gw.Close)
	s, _ := launcherTestService(t)
	effort := "high"
	out := s.providerTest(i18n.With(context.Background(), i18n.En), probeInput{BaseURL: gw.URL, Model: "synthetic-model", API: "chat", Effort: &effort})
	if calls != 2 || out["ok"] != true || out["error"] != nil || out["effort"] != "" {
		t.Fatalf("calls = %d, providerTest() = %v, want a successful degraded second request", calls, out)
	}
}

func TestGatewayMessagePrecedenceAndBoundedFallback(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{
			name: "deeper message wins",
			raw:  `{"message":"top-level","error":{"message":"nested","error":{"message":"deeper"}}}`,
			want: "deeper",
		},
		{
			name: "deeper string error wins",
			raw:  `{"message":"top-level","error":{"code":"model_not_found","message":"nested","error":"model missing"}}`,
			want: "model missing",
		},
		{
			name: "nested message beats top-level",
			raw:  `{"message":"top-level","error":{"message":"nested"}}`,
			want: "nested",
		},
		{
			name: "string error beats top-level",
			raw:  `{"message":"top-level","error":"string error"}`,
			want: "string error",
		},
		{name: "top-level message", raw: `{"message":"top-level"}`, want: "top-level"},
		{name: "non-JSON fallback", raw: `<html>synthetic failure</html>`, want: `<html>synthetic failure</html>`},
		{
			name: "recognized JSON without message falls back",
			raw:  `{"error":{"code":"future_code"}}`,
			want: `{"error":{"code":"future_code"}}`,
		},
		{
			name: "nesting is bounded",
			raw:  `{"error":{"error":{"error":{"message":"too deep"}}}}`,
			want: `{"error":{"error":{"error":{"message":"too deep"}}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatewayMessage([]byte(tc.raw)); got != tc.want {
				t.Fatalf("gatewayMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}
