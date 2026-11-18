package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// FailureKind is a direct-call failure whose remedy differs from an ordinary rejected request.
type FailureKind uint8

const (
	FailureOther FailureKind = iota
	// FailureModelUnavailable means the selected model does not exist or is not available to this key.
	FailureModelUnavailable
	// FailureContextTooLong means the input exceeds the model's context, not its output-token budget.
	FailureContextTooLong
)

// FailureKindOf reports a typed upstream failure. Pauses, refusals and unclassified errors are FailureOther.
func FailureKindOf(err error) FailureKind {
	var classified interface{ failureKind() FailureKind }
	if errors.As(err, &classified) {
		return classified.failureKind()
	}
	return FailureOther
}

// IsModelUnavailable tells a missing or key-inaccessible model from a missing endpoint or an invalid key.
func IsModelUnavailable(err error) bool { return FailureKindOf(err) == FailureModelUnavailable }

// IsContextTooLong tells an over-context input from an ordinary invalid request or a truncated reply.
func IsContextTooLong(err error) bool { return FailureKindOf(err) == FailureContextTooLong }

// FailureKindFor classifies a gateway's HTTP error body for callers such as connection probes. An explicit,
// recognized error.code wins over type and wording. Anthropic's native not_found_error is used only for its specific
// missing-model shape; other unknown codes retain the established wording fallback.
func FailureKindFor(status int, raw []byte) FailureKind {
	code, message, typ, _ := gatewayErrorDetailsWithType(raw)
	return failureKindFor(status, code, message, typ)
}

// kindedError adds a classification without changing an existing error's text or unwrap chain.
type kindedError struct {
	error
	kind FailureKind
}

func (e *kindedError) Unwrap() error            { return e.error }
func (e *kindedError) failureKind() FailureKind { return e.kind }

func withFailureKind(err error, kind FailureKind) error {
	if err == nil || kind == FailureOther {
		return err
	}
	return &kindedError{error: err, kind: kind}
}

// streamFailureError keeps a stream error's text and limit behavior while adding typed model/context semantics.
// The stream itself has no error status, so wording fallback uses bad-request semantics.
func streamFailureError(event, code, msg string) error {
	if limit := streamLimitFor(event, code, msg); limit != nil {
		return limit
	}
	return withFailureKind(fmt.Errorf("ai: %s: %s", event, msg), failureKindFor(http.StatusBadRequest, code, msg, ""))
}

var (
	modelFailureWords       = lazyre.New(`(?i)model[^\n]{0,80}(not.{0,4}(found|exist)|does not exist|invalid|unknown|unsupported)|model_not_found|invalid model|unknown model|knows no model`)
	modelAccessFailureWords = lazyre.New(`(?i)model(?: [a-z0-9_.:/-]+)? is (?:unavailable|not available) to this (?:api )?key`)
	modelOverloadWords      = lazyre.New(`(?i)model service (?:is )?unavailable`)
	contextFailureWords     = lazyre.New(`(?i)prompt is too long|maximum (?:context|prompt) length|context.{0,20}(length|window).{0,40}(exceed|too)|(input|request|message).{0,20}exceeds?.{0,40}context.{0,20}(length|window)|context_length_exceeded`)
)

// failureKindForCode reads recognized non-limit codes. recognized is false for an unknown code, whose established
// type or wording behavior must continue. An unsupported endpoint is recognized but deliberately has FailureOther.
func failureKindForCode(code string) (kind FailureKind, recognized bool) {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "model_not_found":
		return FailureModelUnavailable, true
	case "context_length_exceeded":
		return FailureContextTooLong, true
	case "unsupported_endpoint":
		return FailureOther, true
	default:
		return FailureOther, false
	}
}

// HasRecognizedErrorCode reports whether raw's explicit machine code already determines the outcome: a limit or a
// recognized model/context/endpoint failure. Such a response must not trigger a modified compatibility request.
func HasRecognizedErrorCode(raw []byte) bool {
	code, _ := gatewayErrorDetails(raw)
	if _, recognized := failureKindForCode(code); recognized {
		return true
	}
	_, limited := limitReasonForCode(code, "")
	return limited
}

func failureKindFor(status int, code, message, typ string) FailureKind {
	// Codes are machine semantics; a type such as not_found_error is not a general substitute for one.
	if kind, recognized := failureKindForCode(code); recognized {
		return kind
	}
	// Anthropic's native missing-model response is distinct from an endpoint 404. Its message starts with "model:".
	if status == http.StatusNotFound && typ == "not_found_error" && strings.HasPrefix(message, "model:") {
		return FailureModelUnavailable
	}
	if status != http.StatusBadRequest && status != http.StatusNotFound {
		return FailureOther
	}
	if modelFailureWords.MatchString(message) || modelAccessFailureWords.MatchString(message) && !modelOverloadWords.MatchString(message) {
		return FailureModelUnavailable
	}
	if contextFailureWords.MatchString(message) {
		return FailureContextTooLong
	}
	return FailureOther
}

type errorEnvelope struct {
	Type    json.RawMessage `json:"type"`
	Code    json.RawMessage `json:"code"`
	Message json.RawMessage `json:"message"`
	Error   json.RawMessage `json:"error"`
}

// decodeErrorString reads one envelope field without letting a malformed sibling discard the rest of the envelope.
func decodeErrorString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

// decodeErrorCode accepts both the string and numeric error codes used by compatible gateways. json.Number avoids a
// float conversion, so an integer code keeps its exact decimal representation.
func decodeErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var code string
	if json.Unmarshal(raw, &code) == nil {
		return strings.TrimSpace(code)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

// gatewayErrorDetails reads OpenAI- and Anthropic-shaped envelopes. error.code is more specific than a top-level
// code; either may carry the message. Non-JSON bodies remain available to the established wording fallback.
func gatewayErrorDetails(raw []byte) (code, message string) {
	code, message, _, _ = gatewayErrorDetailsWithType(raw)
	return code, message
}

// gatewayErrorDetailsWithType also retains the most specific envelope type and whether raw was a JSON envelope.
// The latter keeps plain-text log fallback distinct from an envelope that happens to carry only a message.
func gatewayErrorDetailsWithType(raw []byte) (code, message, typ string, envelope bool) {
	var body errorEnvelope
	if json.Unmarshal(raw, &body) != nil {
		return "", string(raw), "", false
	}
	code, message, typ, envelope = decodeErrorCode(body.Code), decodeErrorString(body.Message), decodeErrorString(body.Type), true
	if typ == "error" { // the outer Messages envelope marker, not the nested error's type
		typ = ""
	}
	var errorText string
	if json.Unmarshal(body.Error, &errorText) == nil && errorText != "" {
		message = errorText
	}
	// Some envelopes put the actual error one level deeper, under error.error. Keep the same bounded three levels.
	current := body.Error
	for depth := 0; depth < 2 && len(current) > 0; depth++ {
		var nested errorEnvelope
		if json.Unmarshal(current, &nested) != nil {
			break
		}
		if nestedType := decodeErrorString(nested.Type); nestedType != "" {
			typ = nestedType
		}
		if c := decodeErrorCode(nested.Code); c != "" {
			code = c
		}
		if nestedMessage := decodeErrorString(nested.Message); nestedMessage != "" {
			message = nestedMessage
		}
		if nestedErrorText := decodeErrorString(nested.Error); nestedErrorText != "" {
			message = nestedErrorText
		}
		current = nested.Error
	}
	return code, message, typ, envelope
}
