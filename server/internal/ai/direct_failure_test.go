package ai

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
)

// directAPIError sends one synthetic request through the requested wire protocol and returns its error and request
// count. The response is deliberately identical on every request so an unexpected compatibility retry is observable.
func directAPIError(t *testing.T, api string, status int, body string, header ...string) (error, int) {
	t.Helper()
	u := newUpstream(t, answer(status, body, header...))
	client := New(Config{API: api, BaseURL: u.srv.URL, APIKey: "example-key", Model: "example-model"})
	_, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "synthetic input"}}, MaxTokens: 32})
	return err, int(u.sent.Load())
}

func requireDirectPause(t *testing.T, err error, reason string) *PausedError {
	t.Helper()
	var paused *PausedError
	if !errors.As(err, &paused) || paused.Reason != reason {
		t.Fatalf("error = %v, want a %s pause", err, reason)
	}
	if Transient(err) {
		t.Fatalf("pause is transient: %v", err)
	}
	return paused
}

func TestDirectHTTPFailureKindsAcrossProtocols(t *testing.T) {
	apis := []struct{ name, api string }{{"messages", ""}, {"responses", "responses"}, {"chat", "chat"}}
	cases := []struct {
		name      string
		status    int
		body      string
		want      FailureKind
		transient bool
	}{
		{"nested OpenAI model code", 404, `{"error":{"code":"model_not_found","message":"request rejected"}}`, FailureModelUnavailable, false},
		{"nested Anthropic context code", 400, `{"type":"error","error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"request rejected"}}`, FailureContextTooLong, false},
		{"native Anthropic missing model", 404, `{"type":"error","error":{"type":"not_found_error","message":"model: example-model"}}`, FailureModelUnavailable, false},
		{"native numeric code retains type and message", 404, `{"type":"error","error":{"type":"not_found_error","code":40401,"message":"model: example-model"}}`, FailureModelUnavailable, false},
		{"native endpoint 404 stays endpoint-only", 404, `{"type":"error","error":{"type":"not_found_error","message":"unknown endpoint"}}`, FailureOther, false},
		{"upstream service unavailable is not a missing model", 404, `{"error":{"type":"api_error","message":"upstream model service unavailable"}}`, FailureOther, false},
		{"selected model unavailable to this key", 404, `{"error":{"message":"The selected model is unavailable to this API key"}}`, FailureModelUnavailable, false},
		{"model not available to this key", 404, `{"error":{"message":"model is not available to this API key"}}`, FailureModelUnavailable, false},
		{"model service unavailable is not key inaccessibility", 404, `{"error":{"type":"api_error","message":"model service is unavailable"}}`, FailureOther, false},
		{"deeper nested context code", 400, `{"error":{"error":{"code":"context_length_exceeded","message":"request rejected"}}}`, FailureContextTooLong, false},
		{"top-level model code", 404, `{"type":"error","code":"model_not_found","message":"request rejected"}`, FailureModelUnavailable, false},
		{"unsupported endpoint beats model wording", 404, `{"error":{"code":"unsupported_endpoint","message":"model example-model does not exist on this endpoint"}}`, FailureOther, false},
		{"unknown code keeps model wording", 404, `{"error":{"code":"future_code","message":"The model example-model does not exist"}}`, FailureModelUnavailable, false},
		{"unknown code keeps context wording", 400, `{"error":{"code":"future_code","message":"prompt is too long: synthetic input"}}`, FailureContextTooLong, false},
		{"unknown code generic message", 400, `{"error":{"code":"future_code","message":"request rejected"}}`, FailureOther, false},
		{"string error wording", 404, `{"error":"unknown model example-model"}`, FailureModelUnavailable, false},
		{"explicit model code beyond ordinary statuses", 500, `{"error":{"code":"model_not_found","message":"request rejected"}}`, FailureModelUnavailable, false},
	}
	for _, protocol := range apis {
		for _, tc := range cases {
			t.Run(protocol.name+"/"+tc.name, func(t *testing.T) {
				err, requests := directAPIError(t, protocol.api, tc.status, tc.body)
				if requests != 1 {
					t.Fatalf("requests = %d, want 1", requests)
				}
				if want := fmt.Sprintf("ai: HTTP %d: %s", tc.status, tc.body); err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
				if got := FailureKindOf(err); got != tc.want || IsModelUnavailable(err) != (tc.want == FailureModelUnavailable) || IsContextTooLong(err) != (tc.want == FailureContextTooLong) {
					t.Fatalf("failure kind = %v, want %v (%v)", got, tc.want, err)
				}
				if got := Transient(err); got != tc.transient {
					t.Fatalf("Transient(%v) = %v, want %v", err, got, tc.transient)
				}
			})
		}
	}
}

func TestDirectHTTPErrorRetainsSemanticsPastClippedText(t *testing.T) {
	body := `{"padding":"` + strings.Repeat("x", 360) + `","error":{"code":"model_not_found","message":"request rejected"}}`
	err, requests := directAPIError(t, "", 404, body)
	if requests != 1 || !IsModelUnavailable(err) {
		t.Fatalf("requests = %d, error = %v; want one typed model failure", requests, err)
	}
	if want := "ai: HTTP 404: " + strings.TrimSpace(body[:300]); err.Error() != want {
		t.Fatalf("clipped error text changed:\n got %q\nwant %q", err, want)
	}
	contextBody := `{"padding":"` + strings.Repeat("x", 360) + `","error":{"code":"future_code","message":"prompt is too long: synthetic input"}}`
	err, requests = directAPIError(t, "responses", 400, contextBody)
	if requests != 1 || !IsContextTooLong(err) {
		t.Fatalf("requests = %d, error = %v; want the full message's context classification", requests, err)
	}
	unknownBody := `{"padding":"` + strings.Repeat("x", 360) + `","error":{"code":"future_code","message":"quota exhausted"}}`
	err, requests = directAPIError(t, "chat", 429, unknownBody)
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	// Before full envelopes were parsed, wording beyond the clipped 300-byte body could not turn a plain 429 into quota.
	requireDirectPause(t, err, ReasonBusy)
}

func TestDirectExplicitNoRetryAcrossProtocols(t *testing.T) {
	for _, protocol := range []struct{ name, api string }{{"messages", ""}, {"responses", "responses"}, {"chat", "chat"}} {
		for _, status := range []int{http.StatusInternalServerError, http.StatusRequestTimeout} {
			t.Run(fmt.Sprintf("%s/%d", protocol.name, status), func(t *testing.T) {
				err, requests := directAPIError(t, protocol.api, status, `{"error":{"message":"temporary failure"}}`, "X-Should-Retry", " FaLsE ")
				if requests != 1 || err == nil || IsPaused(err) || Transient(err) {
					t.Fatalf("requests = %d, error = %v, transient = %v; want one non-transient failure", requests, err, Transient(err))
				}
			})
		}
	}
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusRequestTimeout} {
		resp := &http.Response{StatusCode: status, Header: http.Header{"X-Should-Retry": []string{"false"}}}
		if err := newHTTPError(resp, []byte(`{"error":{"message":"temporary failure"}}`)); Transient(err) {
			t.Errorf("status %d with explicit no-retry is transient: %v", status, err)
		}
	}
	for _, value := range []string{"", "true", "0", "falsehood"} {
		resp := &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{}}
		if value != "" {
			resp.Header.Set("X-Should-Retry", value)
		}
		if err := newHTTPError(resp, []byte(`{"error":{"message":"temporary failure"}}`)); !Transient(err) {
			t.Errorf("X-Should-Retry %q made an ordinary 500 non-transient", value)
		}
	}
}

func TestDirectNoRetryRateLimitUsesRetryAfterAsQuota(t *testing.T) {
	fakeClock(t)
	for _, protocol := range []struct{ name, api string }{{"messages", ""}, {"responses", "responses"}, {"chat", "chat"}} {
		t.Run(protocol.name, func(t *testing.T) {
			body := `{"error":{"code":"rpm_limit","message":"requests per minute"}}`
			err, requests := directAPIError(t, protocol.api, 429, body, "X-Should-Retry", "false", "Retry-After", "1")
			paused := requireDirectPause(t, err, ReasonQuota)
			if requests != 1 || !paused.Until.Equal(now().Add(time.Second)) {
				t.Fatalf("requests = %d, pause = %+v; want one request and a 1-second quota pause", requests, paused)
			}
		})
	}
	t.Run("zero cooldown restores busy backoff", func(t *testing.T) {
		err, requests := directAPIError(t, "chat", 429, `{"error":{"code":"rpm_limit","message":"requests per minute"}}`,
			"X-Should-Retry", "false", "Retry-After", "0")
		paused := requireDirectPause(t, err, ReasonBusy)
		if requests != 1 || !paused.Until.Equal(now().Add(busyFirst)) {
			t.Fatalf("requests = %d, pause = %+v; want one request and ordinary busy backoff", requests, paused)
		}
	})
	t.Run("malformed cooldown leaves rpm limit busy", func(t *testing.T) {
		err, requests := directAPIError(t, "chat", 429, `{"error":{"code":"rpm_limit","message":"requests per minute"}}`,
			"X-Should-Retry", "false", "Retry-After", "soon")
		paused := requireDirectPause(t, err, ReasonBusy)
		if requests != 1 || !paused.Until.Equal(now().Add(busyFirst)) {
			t.Fatalf("requests = %d, pause = %+v; want one request and ordinary busy backoff", requests, paused)
		}
	})
	t.Run("large cooldown caps without overflowing", func(t *testing.T) {
		err, requests := directAPIError(t, "chat", 429, `{"error":{"code":"future_code","message":"request rejected"}}`,
			"X-Should-Retry", "false", "Retry-After", "10000000000")
		paused := requireDirectPause(t, err, ReasonQuota)
		if requests != 1 || !paused.Until.Equal(now().Add(afterMost)) {
			t.Fatalf("requests = %d, pause = %+v; want one request and the maximum pause", requests, paused)
		}
	})
}

func TestDirectPaymentRequiredPausesQuotaAcrossProtocols(t *testing.T) {
	fakeClock(t)
	for _, protocol := range []struct{ name, api string }{{"messages", ""}, {"responses", "responses"}, {"chat", "chat"}} {
		t.Run(protocol.name, func(t *testing.T) {
			err, requests := directAPIError(t, protocol.api, http.StatusPaymentRequired, `{"error":{"message":"payment required"}}`)
			paused := requireDirectPause(t, err, ReasonQuota)
			if requests != 1 || !paused.Until.Equal(now().Add(quotaPause)) {
				t.Fatalf("requests = %d, pause = %+v; want one request and the default quota pause", requests, paused)
			}
		})
	}
}

func TestDirectLimitCodesBeatMisleadingWording(t *testing.T) {
	fakeClock(t)
	apis := []string{"", "responses", "chat"}
	quotaCodes := []string{
		"insufficient_quota", "credit_balance_exhausted", "organization_spend_limit_exceeded",
		"project_spend_limit_exceeded", "account_quota_exhausted", "budget_exhausted",
		"weekly_budget_exhausted", "api_key_model_limit_exhausted", "quota_exhausted",
	}
	for i, code := range quotaCodes {
		t.Run("quota/"+code, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":%q,"message":"concurrency full; request rejected"}}`, code)
			err, requests := directAPIError(t, apis[i%len(apis)], 429, body)
			requireDirectPause(t, err, ReasonQuota)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
	for i, code := range []string{"rpm_limit", "tpm_limit", "concurrency_limit"} {
		t.Run("busy/"+code, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":%q,"message":"quota exhausted; credit billing"}}`, code)
			err, requests := directAPIError(t, apis[i%len(apis)], 429, body)
			requireDirectPause(t, err, ReasonBusy)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
	for _, status := range []int{400, 422} {
		t.Run(fmt.Sprintf("quota code on %d", status), func(t *testing.T) {
			err, requests := directAPIError(t, "responses", status, `{"error":{"code":"budget_exhausted","message":"request rejected"}}`)
			requireDirectPause(t, err, ReasonQuota)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
	for _, tc := range []struct {
		status    int
		transient bool
	}{{400, false}, {500, true}} {
		t.Run(fmt.Sprintf("busy code on unrelated status %d", tc.status), func(t *testing.T) {
			err, requests := directAPIError(t, "chat", tc.status, `{"error":{"code":"rpm_limit","message":"request rejected"}}`)
			if requests != 1 || IsPaused(err) || Transient(err) != tc.transient {
				t.Fatalf("requests = %d, error = %v, transient = %v; want unchanged status behavior", requests, err, Transient(err))
			}
		})
	}
	t.Run("unknown code keeps quota wording", func(t *testing.T) {
		err, requests := directAPIError(t, "", 429, `{"error":{"code":"future_code","message":"quota exhausted"}}`)
		requireDirectPause(t, err, ReasonQuota)
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})
	t.Run("unknown code does not change other statuses", func(t *testing.T) {
		err, requests := directAPIError(t, "responses", 500, `{"error":{"code":"future_code","message":"quota exhausted"}}`)
		if requests != 1 || IsPaused(err) || !Transient(err) {
			t.Fatalf("requests = %d, error = %v; want one ordinary transient 500", requests, err)
		}
	})
}

func TestDirectChatNoRetryAllowsModifiedCompatibilityRequests(t *testing.T) {
	cases := []struct {
		name, field string
		status      int
		body        string
	}{
		{"max_tokens", "max_tokens", 400, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`},
		{"stream_options", "stream_options", 400, `{"error":{"code":"future_code","message":"Unrecognized request argument supplied: stream_options"}}`},
		{"include_usage", "stream_options", 422, `{"detail":[{"loc":["body","include_usage"],"msg":"Extra inputs are not permitted"}]}`},
		{"reasoning_effort", "reasoning_effort", 400, `{"error":{"message":"Unsupported parameter: 'reasoning_effort' is not supported with this model."}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests []map[string]any
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests = append(requests, body)
				if len(requests) == 1 {
					w.Header().Set("X-Should-Retry", " FALSE ")
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			cfg := Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "example-model"}
			if tc.field == "reasoning_effort" {
				cfg.ReasoningEffort = "high"
			}
			reply, err := New(cfg).Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "synthetic input"}}, MaxTokens: 32})
			if err != nil || reply.Text != "ok" || len(requests) != 2 {
				t.Fatalf("reply = %+v, error = %v, requests = %v; want the modified second request to succeed", reply, err, requests)
			}
			first, second := requests[0], requests[1]
			switch tc.field {
			case "max_tokens":
				if first["max_tokens"] != float64(32) || second["max_completion_tokens"] != float64(32) {
					t.Fatalf("token field was not corrected: first = %v, second = %v", first, second)
				}
				if _, present := second["max_tokens"]; present {
					t.Fatalf("modified request retained max_tokens: %v", second)
				}
			case "stream_options":
				options, _ := first["stream_options"].(map[string]any)
				if options["include_usage"] != true {
					t.Fatalf("first request did not ask for usage: %v", first)
				}
				if _, present := second["stream_options"]; present {
					t.Fatalf("modified request retained stream_options: %v", second)
				}
			case "reasoning_effort":
				if first["reasoning_effort"] != "high" {
					t.Fatalf("first request omitted reasoning_effort: %v", first)
				}
				if _, present := second["reasoning_effort"]; present {
					t.Fatalf("modified request retained reasoning_effort: %v", second)
				}
			}
		})
	}
}

func TestDirectChatNoRetryCompatibilityChangeIsBounded(t *testing.T) {
	body := `{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`
	err, requests := directAPIError(t, "chat", http.StatusBadRequest, body, "X-Should-Retry", "false")
	if requests != 2 {
		t.Fatalf("requests = %d, want one rejection and one modified request", requests)
	}
	if want := "ai: HTTP 400: " + body; err == nil || err.Error() != want || Transient(err) {
		t.Fatalf("error = %v, want %q, non-transient", err, want)
	}
}

func TestDirectChatRecognizedCodesSkipCompatibilityRequests(t *testing.T) {
	t.Run("budget code beats stream_options wording", func(t *testing.T) {
		fakeClock(t)
		body := `{"error":{"code":"budget_exhausted","message":"Unrecognized request argument supplied: stream_options"}}`
		err, requests := directAPIError(t, "chat", http.StatusBadRequest, body)
		requireDirectPause(t, err, ReasonQuota)
		if requests != 1 {
			t.Fatalf("requests = %d, want the recognized quota code to stop at 1", requests)
		}
	})
	for _, tc := range []struct {
		code string
		want FailureKind
	}{
		{"model_not_found", FailureModelUnavailable},
		{"context_length_exceeded", FailureContextTooLong},
		{"unsupported_endpoint", FailureOther},
	} {
		t.Run(tc.code, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":%q,"message":"Unrecognized request argument supplied: stream_options"}}`, tc.code)
			err, requests := directAPIError(t, "chat", http.StatusBadRequest, body)
			if requests != 1 || IsPaused(err) || FailureKindOf(err) != tc.want {
				t.Fatalf("requests = %d, error = %v, kind = %v; want one raw request with kind %v", requests, err, FailureKindOf(err), tc.want)
			}
			if want := "ai: HTTP 400: " + body; err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestDirectStreamFailureKinds(t *testing.T) {
	cases := []struct {
		name      string
		api       string
		body      string
		want      FailureKind
		text      string
		transient bool
	}{
		{"messages nested model code", "", `data: {"type":"error","error":{"type":"not_found_error","code":"model_not_found","message":"request rejected"}}` + "\n\n", FailureModelUnavailable, "ai: error: not_found_error: request rejected", false},
		{"messages native missing model", "", `data: {"type":"error","error":{"type":"not_found_error","message":"model: example-model"}}` + "\n\n", FailureModelUnavailable, "ai: error: not_found_error: model: example-model", false},
		{"messages nested numeric code", "", `data: {"type":"error","error":{"type":"invalid_request_error","code":40004,"message":"prompt is too long"}}` + "\n\n", FailureContextTooLong, "ai: error: invalid_request_error: prompt is too long", false},
		{"messages top-level context code", "", `data: {"type":"error","code":"context_length_exceeded","message":"request rejected"}` + "\n\n", FailureContextTooLong, "ai: error: context_length_exceeded: request rejected", false},
		{"messages top-level numeric code", "", `data: {"type":"error","code":50003,"message":"upstream failed exactly","error":{"type":"api_error"}}` + "\n\n", FailureOther, "ai: error: api_error: upstream failed exactly", true},
		{"messages whole JSON model code", "", `{"type":"error","error":{"type":"not_found_error","code":"model_not_found","message":"request rejected"}}`, FailureModelUnavailable, "ai: error: not_found_error: request rejected", false},
		{"messages whole JSON top-level numeric code", "", `{"type":"error","code":40402,"message":"model: example-model","error":{"type":"not_found_error"}}`, FailureModelUnavailable, "ai: error: not_found_error: model: example-model", false},
		{"messages string error model wording", "", `{"error":"unknown model example-model"}`, FailureModelUnavailable, "ai: error: error: unknown model example-model", false},
		{"messages upstream service unavailable stays transient", "", `data: {"type":"error","error":{"type":"api_error","message":"upstream model service unavailable"}}` + "\n\n", FailureOther, "ai: error: api_error: upstream model service unavailable", true},
		{"messages model service unavailable stays transient", "", `data: {"type":"error","error":{"type":"api_error","message":"model service is unavailable"}}` + "\n\n", FailureOther, "ai: error: api_error: model service is unavailable", true},
		{"messages unknown code context wording", "", `data: {"type":"error","error":{"type":"invalid_request_error","code":"future_code","message":"prompt is too long"}}` + "\n\n", FailureContextTooLong, "ai: error: invalid_request_error: prompt is too long", false},
		{"messages unknown code server status unchanged", "", `data: {"type":"error","error":{"type":"api_error","code":"future_code","message":"prompt is too long"}}` + "\n\n", FailureOther, "ai: error: api_error: prompt is too long", true},
		{"messages unsupported endpoint", "", `data: {"type":"error","error":{"type":"not_found_error","code":"unsupported_endpoint","message":"model example-model does not exist"}}` + "\n\n", FailureOther, "ai: error: not_found_error: model example-model does not exist", false},
		{"responses nested model code", "responses", `data: {"type":"response.failed","response":{"error":{"code":"model_not_found","message":"request rejected"}}}` + "\n\n", FailureModelUnavailable, "ai: response.failed: request rejected", false},
		{"responses top-level numeric code", "responses", `data: {"type":"response.failed","code":40005,"message":"prompt is too long"}` + "\n\n", FailureContextTooLong, "ai: response.failed: prompt is too long", false},
		{"responses nested numeric code", "responses", `data: {"type":"response.failed","response":{"error":{"code":40405,"message":"unknown model example-model"}}}` + "\n\n", FailureModelUnavailable, "ai: response.failed: unknown model example-model", false},
		{"responses nested malformed code retains message", "responses", `data: {"type":"response.failed","response":{"error":{"code":{"bad":true},"message":"prompt is too long"}}}` + "\n\n", FailureContextTooLong, "ai: response.failed: prompt is too long", false},
		{"responses top-level context code", "responses", `data: {"type":"error","code":"context_length_exceeded","message":"request rejected"}` + "\n\n", FailureContextTooLong, "ai: error: request rejected", false},
		{"responses incomplete context code", "responses", `data: {"type":"response.incomplete","response":{"error":{"code":"context_length_exceeded","message":"request rejected"}}}` + "\n\n", FailureContextTooLong, "ai: response.incomplete: request rejected", false},
		{"responses unknown code context wording", "responses", `data: {"type":"response.failed","response":{"error":{"code":"future_code","message":"prompt is too long"}}}` + "\n\n", FailureContextTooLong, "ai: response.failed: prompt is too long", false},
		{"responses unsupported endpoint", "responses", `data: {"type":"response.failed","response":{"error":{"code":"unsupported_endpoint","message":"model example-model does not exist"}}}` + "\n\n", FailureOther, "ai: response.failed: model example-model does not exist", false},
		{"chat nested model code", "chat", `data: {"error":{"code":"model_not_found","message":"request rejected"}}` + "\n\n", FailureModelUnavailable, "ai: stream error: request rejected", false},
		{"chat nested numeric code", "chat", `data: {"error":{"code":40406,"message":"unknown model example-model"}}` + "\n\n", FailureModelUnavailable, "ai: stream error: unknown model example-model", false},
		{"chat nested malformed code retains message", "chat", `data: {"error":{"code":{"bad":true},"message":"unknown model example-model"}}` + "\n\n", FailureModelUnavailable, "ai: stream error: unknown model example-model", false},
		{"chat nested context code", "chat", `data: {"error":{"code":"context_length_exceeded","message":"request rejected"}}` + "\n\n", FailureContextTooLong, "ai: stream error: request rejected", false},
		{"chat top-level context code", "chat", `data: {"type":"error","code":"context_length_exceeded","message":"request rejected"}` + "\n\n", FailureContextTooLong, "ai: stream error: request rejected", false},
		{"chat unknown code model wording", "chat", `data: {"error":{"code":"future_code","message":"unknown model example-model"}}` + "\n\n", FailureModelUnavailable, "ai: stream error: unknown model example-model", false},
		{"chat unsupported endpoint", "chat", `data: {"error":{"code":"unsupported_endpoint","message":"model example-model does not exist"}}` + "\n\n", FailureOther, "ai: stream error: model example-model does not exist", false},
		{"chat unknown code generic", "chat", `data: {"error":{"code":"future_code","message":"request rejected"}}` + "\n\n", FailureOther, "ai: stream error: request rejected", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, requests := directAPIError(t, tc.api, 200, tc.body)
			if requests != 1 || err == nil || err.Error() != tc.text {
				t.Fatalf("requests = %d, error = %v, want one request and %q", requests, err, tc.text)
			}
			if got := FailureKindOf(err); got != tc.want {
				t.Fatalf("failure kind = %v, want %v (%v)", got, tc.want, err)
			}
			if got := Transient(err); got != tc.transient {
				t.Fatalf("Transient(%v) = %v, want %v", err, got, tc.transient)
			}
		})
	}
}

func TestDirectNumericCodeFields(t *testing.T) {
	t.Run("HTTP", func(t *testing.T) {
		body := `{"type":"error","error":{"type":"not_found_error","code":40401,"message":"model: example-model"}}`
		err, requests := directAPIError(t, "chat", 404, body)
		var httpFailure *httpError
		if requests != 1 || !errors.As(err, &httpFailure) {
			t.Fatalf("requests = %d, error = %v; want one HTTP envelope failure", requests, err)
		}
		if httpFailure.code != "40401" || httpFailure.message != "model: example-model" || httpFailure.errorType != "not_found_error" || !IsModelUnavailable(err) {
			t.Fatalf("HTTP envelope = %+v, kind = %v; want numeric code, exact message and native type", httpFailure, FailureKindOf(err))
		}
	})
	for _, tc := range []struct {
		name, body, code, kind, message string
	}{
		{"Messages whole body", `{"type":"error","code":40402,"message":"model: example-model","error":{"type":"not_found_error"}}`, "40402", "not_found_error", "model: example-model"},
		{"Messages stream", `data: {"type":"error","code":50003,"message":"upstream failed exactly","error":{"type":"api_error"}}` + "\n\n", "50003", "api_error", "upstream failed exactly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err, requests := directAPIError(t, "", 200, tc.body)
			var failure *streamFailure
			if requests != 1 || !errors.As(err, &failure) {
				t.Fatalf("requests = %d, error = %v; want one Messages stream failure", requests, err)
			}
			if failure.code != tc.code || failure.kind != tc.kind || failure.msg != tc.message {
				t.Fatalf("stream envelope = %+v, want code %q, type %q, message %q", failure, tc.code, tc.kind, tc.message)
			}
		})
	}
}

func TestDirectStreamLimitCodes(t *testing.T) {
	fakeClock(t)
	cases := []struct {
		name, api, body, reason string
	}{
		{"messages budget code", "", `data: {"type":"error","error":{"type":"rate_limit_error","code":"weekly_budget_exhausted","message":"request rejected"}}` + "\n\n", ReasonQuota},
		{"messages busy code beats type and wording", "", `data: {"type":"error","error":{"type":"rate_limit_error","code":"tpm_limit","message":"quota exhausted"}}` + "\n\n", ReasonBusy},
		{"messages overloaded type preserved", "", `data: {"type":"error","error":{"type":"overloaded_error","message":"request rejected"}}` + "\n\n", ReasonBusy},
		{"responses budget code", "responses", `data: {"type":"response.failed","response":{"error":{"code":"budget_exhausted","message":"request rejected"}}}` + "\n\n", ReasonQuota},
		{"responses busy code beats wording", "responses", `data: {"type":"error","code":"concurrency_limit","message":"quota exhausted"}` + "\n\n", ReasonBusy},
		{"chat budget code", "chat", `data: {"error":{"code":"api_key_model_limit_exhausted","message":"request rejected"}}` + "\n\n", ReasonQuota},
		{"chat busy code beats wording", "chat", `data: {"error":{"code":"rpm_limit","message":"quota exhausted"}}` + "\n\n", ReasonBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, requests := directAPIError(t, tc.api, 200, tc.body)
			requireDirectPause(t, err, tc.reason)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
	t.Run("chat limit keeps its original error text", func(t *testing.T) {
		_, err := readCompletions(strings.NewReader(`data: {"error":{"code":"budget_exhausted","message":"request rejected"}}` + "\n\n"))
		var limit *streamLimit
		if !errors.As(err, &limit) || err.Error() != "ai: stream error: request rejected" || limit.reason != ReasonQuota {
			t.Fatalf("error = %v, limit = %+v; want unchanged text and a quota limit", err, limit)
		}
	})
}
