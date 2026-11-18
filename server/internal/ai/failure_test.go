package ai

import (
	"errors"
	"fmt"
	"testing"
)

func TestFailureKindForSyntheticEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   FailureKind
	}{
		{"nested OpenAI model code beats wording", 400, `{"error":{"code":"model_not_found","message":"request rejected"}}`, FailureModelUnavailable},
		{"top-level Anthropic context code", 400, `{"type":"error","code":"context_length_exceeded","message":"request rejected"}`, FailureContextTooLong},
		{"nested Anthropic model code", 404, `{"type":"error","error":{"type":"not_found_error","code":"model_not_found","message":"request rejected"}}`, FailureModelUnavailable},
		{"native Anthropic missing model", 404, `{"type":"error","error":{"type":"not_found_error","message":"model: example-model"}}`, FailureModelUnavailable},
		{"native model prefix needs not-found type", 404, `{"error":{"type":"invalid_request_error","message":"model: example-model"}}`, FailureOther},
		{"native endpoint 404 stays endpoint-only", 404, `{"error":{"type":"not_found_error","message":"unknown endpoint"}}`, FailureOther},
		{"native model prefix needs 404", 400, `{"error":{"type":"not_found_error","message":"model: example-model"}}`, FailureOther},
		{"numeric code retains native type and message", 404, `{"type":"error","error":{"type":"not_found_error","code":40401,"message":"model: example-model"}}`, FailureModelUnavailable},
		{"malformed code retains native type and message", 404, `{"type":"error","error":{"type":"not_found_error","code":{"bad":true},"message":"model: example-model"}}`, FailureModelUnavailable},
		{"deeper nested error", 400, `{"error":{"error":{"code":"context_length_exceeded","message":"request rejected"}}}`, FailureContextTooLong},
		{"nested string context error", 400, `{"error":{"error":"prompt is too long"}}`, FailureContextTooLong},
		{"nested string missing-model error", 404, `{"error":{"error":"unknown model example-model"}}`, FailureModelUnavailable},
		{"explicit code beyond ordinary statuses", 500, `{"error":{"code":"model_not_found"}}`, FailureModelUnavailable},
		{"unknown code keeps model wording fallback", 404, `{"error":{"code":"future_code","message":"The model example-model does not exist"}}`, FailureModelUnavailable},
		{"unknown code keeps context wording fallback", 400, `{"error":{"code":"future_code","message":"prompt is too long: synthetic input"}}`, FailureContextTooLong},
		{"unknown code generic message", 400, `{"error":{"code":"future_code","message":"request rejected"}}`, FailureOther},
		{"unsupported endpoint code beats model wording", 404, `{"error":{"code":"unsupported_endpoint","message":"model example-model does not exist on this endpoint"}}`, FailureOther},
		{"plain missing endpoint", 404, `404 page not found`, FailureOther},
		{"string error wording", 404, `{"error":"unknown model example-model"}`, FailureModelUnavailable},
		{"model wording", 404, `{"error":{"type":"not_found_error","message":"example-gateway knows no model example-model"}}`, FailureModelUnavailable},
		{"selected model unavailable to this API key", 404, `{"error":{"message":"The selected model is unavailable to this API key"}}`, FailureModelUnavailable},
		{"model not available to this key", 404, `{"error":{"message":"model is not available to this API key"}}`, FailureModelUnavailable},
		{"upstream service unavailable is not a missing model", 404, `{"error":{"type":"api_error","message":"upstream model service unavailable"}}`, FailureOther},
		{"selected model unavailable to this key", 404, `{"error":{"message":"The selected model is unavailable to this key"}}`, FailureModelUnavailable},
		{"model id unavailable to this API key", 404, `{"error":{"message":"model example-model is unavailable to this API key"}}`, FailureModelUnavailable},
		{"model id not available to this key", 404, `{"error":{"message":"model example-model is not available to this key"}}`, FailureModelUnavailable},
		{"overloaded model service to this key is not a missing model", 404, `{"error":{"type":"api_error","message":"model service is unavailable to this key"}}`, FailureOther},
		{"model service unavailable is not key inaccessibility", 404, `{"error":{"type":"api_error","message":"model service is unavailable"}}`, FailureOther},
		{"context wording", 400, `{"error":{"message":"input exceeds the model context window"}}`, FailureContextTooLong},
		{"context code alone", 400, `{"error":{"code":"context_length_exceeded"}}`, FailureContextTooLong},
		{"unrelated status keeps wording behavior", 500, `{"error":{"message":"prompt is too long"}}`, FailureOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FailureKindFor(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("FailureKindFor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGatewayErrorDetailsDecodeFieldsIndependently(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, message, typ string
	}{
		{"top-level numeric code", `{"type":"not_found_error","code":40401,"message":"model: example-model"}`, "40401", "model: example-model", "not_found_error"},
		{"nested numeric code", `{"type":"error","code":1000,"message":"outer","error":{"type":"not_found_error","code":40402,"message":"model: example-model"}}`, "40402", "model: example-model", "not_found_error"},
		{"nested string error preserves code and type", `{"code":40000,"message":"outer","error":{"type":"invalid_request_error","code":40001,"error":"prompt is too long"}}`, "40001", "prompt is too long", "invalid_request_error"},
		{"malformed code", `{"error":{"type":"not_found_error","code":{"bad":true},"message":"model: example-model"}}`, "", "model: example-model", "not_found_error"},
		{"malformed nested message", `{"message":"outer exact","error":{"type":"api_error","code":50001,"message":42}}`, "50001", "outer exact", "api_error"},
		{"malformed nested type", `{"code":40001,"message":"outer exact","error":{"type":42,"code":40002,"message":"nested exact"}}`, "40002", "nested exact", ""},
		{"scalar nested error", `{"type":"not_found_error","code":40403,"message":"model: example-model","error":17}`, "40403", "model: example-model", "not_found_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, message, typ, envelope := gatewayErrorDetailsWithType([]byte(tc.body))
			if !envelope || code != tc.code || message != tc.message || typ != tc.typ {
				t.Fatalf("details = (%q, %q, %q, %v), want (%q, %q, %q, true)", code, message, typ, envelope, tc.code, tc.message, tc.typ)
			}
		})
	}
}

func TestFailureKindOfUnwraps(t *testing.T) {
	base := errors.New("synthetic upstream failure")
	err := withFailureKind(fmt.Errorf("outer: %w", base), FailureModelUnavailable)
	if !IsModelUnavailable(fmt.Errorf("call: %w", err)) || IsContextTooLong(err) {
		t.Fatalf("classification lost through wrapping: %v", err)
	}
	if !errors.Is(err, base) || err.Error() != "outer: synthetic upstream failure" {
		t.Fatalf("error identity or text changed: %v", err)
	}
	if FailureKindOf(nil) != FailureOther || FailureKindOf(base) != FailureOther {
		t.Fatal("unclassified error acquired a kind")
	}
}
