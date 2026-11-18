package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDirectTypedCodesBypassGateStatuses(t *testing.T) {
	codes := []struct {
		code string
		want FailureKind
	}{
		{"model_not_found", FailureModelUnavailable},
		{"context_length_exceeded", FailureContextTooLong},
		{"unsupported_endpoint", FailureOther},
	}
	for _, status := range []int{402, 429, 503, 529} {
		for _, tc := range codes {
			t.Run(fmt.Sprintf("%d/%s", status, tc.code), func(t *testing.T) {
				body := fmt.Sprintf(`{"error":{"code":%q,"message":"request rejected"}}`, tc.code)
				err, requests := directAPIError(t, "", status, body, "X-Should-Retry", "false")
				if requests != 1 || IsPaused(err) {
					t.Fatalf("requests = %d, error = %v; want the typed raw error, not a pause", requests, err)
				}
				if want := fmt.Sprintf("ai: HTTP %d: %s", status, body); err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
				if got := FailureKindOf(err); got != tc.want {
					t.Fatalf("failure kind = %v, want %v", got, tc.want)
				}
				if reason, _, _ := classify(err); reason != "" {
					t.Fatalf("gate reason = %q, want no gate classification", reason)
				}
				if Transient(err) {
					t.Fatalf("Transient(%v) = true, want recognized non-limit code to suppress retries", err)
				}
			})
		}
	}
}

func TestDirectMessagesTypedCodesSuppressTransience(t *testing.T) {
	for _, tc := range []struct {
		code string
		want FailureKind
	}{
		{"model_not_found", FailureModelUnavailable},
		{"context_length_exceeded", FailureContextTooLong},
		{"unsupported_endpoint", FailureOther},
	} {
		t.Run(tc.code, func(t *testing.T) {
			body := fmt.Sprintf(`data: {"type":"error","error":{"type":"api_error","code":%q,"message":"request rejected"}}`, tc.code) + "\n\n"
			err, requests := directAPIError(t, "", 200, body)
			if requests != 1 || err == nil || err.Error() != "ai: error: api_error: request rejected" {
				t.Fatalf("requests = %d, error = %v; want unchanged stream error text", requests, err)
			}
			if got := FailureKindOf(err); got != tc.want {
				t.Fatalf("failure kind = %v, want %v", got, tc.want)
			}
			if Transient(err) {
				t.Fatalf("Transient(%v) = true, want typed code to suppress api_error transience", err)
			}
		})
	}
	t.Run("unknown api_error stays transient", func(t *testing.T) {
		body := `data: {"type":"error","error":{"type":"api_error","code":"future_code","message":"request rejected"}}` + "\n\n"
		err, requests := directAPIError(t, "", 200, body)
		if requests != 1 || err == nil || FailureKindOf(err) != FailureOther || !Transient(err) {
			t.Fatalf("requests = %d, error = %v, kind = %v; want unknown api_error to stay transient", requests, err, FailureKindOf(err))
		}
	})
}

func TestDirectNoRetryWithoutPositiveCooldownPausesBusy(t *testing.T) {
	fakeClock(t)
	for _, tc := range []struct {
		name       string
		status     int
		retryAfter string
		pause      time.Duration
	}{
		{"429 without cooldown", 429, "", busyFirst},
		{"503 without cooldown", 503, "", busyFirst},
		{"529 without cooldown", 529, "", busyFirst},
		{"malformed cooldown", 429, "soon", busyFirst},
		{"signed positive cooldown", 429, "+1", busyFirst},
		{"negative cooldown", 429, "-1", busyFirst},
		{"zero cooldown", 429, "0", busyFirst},
		{"signed zero cooldown", 429, "-0", busyFirst},
		{"503 cooldown stays busy", 503, "10", 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"error":{"message":"request rejected"}}`
			header := []string{"X-Should-Retry", "false"}
			if tc.retryAfter != "" {
				header = append(header, "Retry-After", tc.retryAfter)
			}
			err, requests := directAPIError(t, "chat", tc.status, body, header...)
			paused := requireDirectPause(t, err, ReasonBusy)
			if requests != 1 || !paused.Until.Equal(now().Add(tc.pause)) {
				t.Fatalf("requests = %d, pause = %+v; want one request and busy backoff %s", requests, paused, tc.pause)
			}
		})
	}
}

func TestDirectNoRetryBusyRetainsCapAndBackoff(t *testing.T) {
	clk := fakeClock(t)
	u := newUpstream(t, answer(http.StatusServiceUnavailable, `{"error":{"message":"request rejected"}}`, "X-Should-Retry", "false"))
	client := New(Config{API: "chat", BaseURL: u.srv.URL, APIKey: "example-key", Model: "example-model"})
	_, err := client.Complete(context.Background(), "system", "synthetic input", 32)
	paused := requireDirectPause(t, err, ReasonBusy)
	if !paused.Until.Equal(now().Add(busyFirst)) {
		t.Fatalf("first pause = %+v, want %s", paused, busyFirst)
	}
	clk.add(busyFirst)
	_, err = client.Complete(context.Background(), "system", "synthetic input", 32)
	paused = requireDirectPause(t, err, ReasonBusy)
	if !paused.Until.Equal(now().Add(2 * busyFirst)) {
		t.Fatalf("second pause = %+v, want %s", paused, 2*busyFirst)
	}
	g := gateFor(client.cfg.BaseURL, "example-key")
	g.mu.Lock()
	limitCap, strikes := g.cap, g.strikes
	g.mu.Unlock()
	if limitCap == 0 || strikes != 2 || u.sent.Load() != 2 {
		t.Fatalf("cap = %d, strikes = %d, requests = %d; want a retained cap and doubled backoff", limitCap, strikes, u.sent.Load())
	}
}

func TestDirectNoRetryStillAllowsExplicitQuotaPauses(t *testing.T) {
	fakeClock(t)
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"payment required", 402, `{"error":{"message":"request rejected"}}`},
		{"explicit quota on 503", 503, `{"error":{"code":"budget_exhausted","message":"request rejected"}}`},
		{"explicit quota ignores malformed cooldown", 429, `{"error":{"code":"weekly_budget_exhausted","message":"request rejected"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := []string{"X-Should-Retry", "false"}
			if tc.name == "explicit quota ignores malformed cooldown" {
				header = append(header, "Retry-After", "soon")
			}
			err, requests := directAPIError(t, "responses", tc.status, tc.body, header...)
			requireDirectPause(t, err, ReasonQuota)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestDirectResponsesNestedErrorObjects(t *testing.T) {
	for _, tc := range []struct {
		name, event, code, message string
		want                       FailureKind
	}{
		{"incomplete", "response.incomplete", "context_length_exceeded", "nested incomplete message", FailureContextTooLong},
		{"failed", "response.failed", "model_not_found", "nested failed message", FailureModelUnavailable},
		{"error", "error", "context_length_exceeded", "nested error message", FailureContextTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := fmt.Sprintf(`{"type":%q,"error":{"code":%q,"message":%q}}`, tc.event, tc.code, tc.message)
			err, requests := directAPIError(t, "responses", 200, "data: "+event+"\n\n")
			wantText := fmt.Sprintf("ai: %s: %s", tc.event, tc.message)
			if requests != 1 || err == nil || err.Error() != wantText || FailureKindOf(err) != tc.want {
				t.Fatalf("requests = %d, error = %v, kind = %v; want %q and %v", requests, err, FailureKindOf(err), wantText, tc.want)
			}
		})
	}
	t.Run("response error takes precedence", func(t *testing.T) {
		event := `{"type":"response.failed","error":{"code":"model_not_found","message":"nested message"},"response":{"error":{"code":"context_length_exceeded","message":"response message"}}}`
		err, requests := directAPIError(t, "responses", 200, "data: "+event+"\n\n")
		if requests != 1 || err == nil || err.Error() != "ai: response.failed: response message" || !IsContextTooLong(err) || IsModelUnavailable(err) {
			t.Fatalf("requests = %d, error = %v; want response.error to win", requests, err)
		}
	})
}

func TestDirectResponsesNestedBudgetErrorPausesWithMessage(t *testing.T) {
	clk := fakeClock(t)
	event := `{"type":"response.failed","error":{"code":"budget_exhausted","message":"nested budget message"}}`
	err, requests := directAPIError(t, "responses", 200, "data: "+event+"\n\n")
	requireDirectPause(t, err, ReasonQuota)
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	logs := clk.lines()
	if len(logs) != 1 || !strings.Contains(logs[0], "stream budget_exhausted: nested budget message") {
		t.Fatalf("logs = %q, want the nested code and message", logs)
	}
}

func TestDirectChatStringErrorAndNull(t *testing.T) {
	err, requests := directAPIError(t, "chat", 200, `data: {"error":"unknown model example-model"}`+"\n\n")
	if requests != 1 || err == nil || err.Error() != "ai: stream error: unknown model example-model" || !IsModelUnavailable(err) {
		t.Fatalf("requests = %d, error = %v, kind = %v; want a typed string error", requests, err, FailureKindOf(err))
	}
	stream := `data: {"error":null,"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	reply, err := readCompletions(strings.NewReader(stream))
	if err != nil || reply.Text != "ok" {
		t.Fatalf("null error changed a valid reply: reply = %+v, error = %v", reply, err)
	}
}

func TestDirectGateLogUsesFullEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, envelope, reason, detail string
	}{
		{"code", `{"error":{"code":"account_quota_exhausted","message":"full quota message beyond clipped text"}}`, ReasonQuota, "account_quota_exhausted: full quota message beyond clipped text"},
		{"type fallback", `{"error":{"type":"rate_limit_error","message":"full rate message beyond clipped text"}}`, ReasonBusy, "rate_limit_error: full rate message beyond clipped text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := fakeClock(t)
			body := `{"padding":"` + strings.Repeat("x", 360) + `","error":` + strings.TrimPrefix(tc.envelope, `{"error":`)
			err, requests := directAPIError(t, "", 429, body)
			requireDirectPause(t, err, tc.reason)
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
			logs := clk.lines()
			if len(logs) != 1 || !strings.Contains(logs[0], tc.detail) {
				t.Fatalf("logs = %q, want detail %q", logs, tc.detail)
			}
		})
	}
	t.Run("plain text fallback", func(t *testing.T) {
		err := newHTTPError(&http.Response{StatusCode: 503, Header: http.Header{}}, []byte("upstream connect error"))
		if _, _, detail := classify(err); !strings.Contains(detail, "HTTP 503 upstream connect error") {
			t.Fatalf("detail = %q, want the plain-text fallback", detail)
		}
	})
	t.Run("direct fixture fallback", func(t *testing.T) {
		if _, _, detail := classify(&httpError{status: 429, body: quotaBody}); !strings.Contains(detail, "account_quota_exhausted: Account usage limit reached") {
			t.Fatalf("detail = %q, want direct fixture envelope details", detail)
		}
	})
}
