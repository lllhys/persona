package transcript

import (
	"encoding/json"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// CodexTurnFailed is the text for a failed Codex turn whose error says nothing more.
const CodexTurnFailed = "Codex 这一轮出错了"

// CodexTurnError reads what failed a Codex turn into the turn's error text and its kind (#2185): a rollout's
// task_complete.error ({message, codex_error_info}) or an app-server turn's error ({message, codexErrorInfo}).
// Kind is proto.TurnErrorTransient when the model service failed for a moment (overloaded, a 5xx, rate limited, the
// connection or the response stream lost), proto.TurnErrorUsageLimit when the subscription's usage limit is used up
// (it goes on once the limit resets, #1710), proto.TurnErrorUser when the user has to act (sign-in, context window,
// the model, a policy, any kind this does not know), and "" when Codex gives no kind (the text is read then,
// docs/assistant/agent-work.md "Rules first, then the model"). Null or empty: the turn did not fail. The message's
// "try again at 3:05 PM" is never read for the reset: it names no time zone (the rollout's rate limits do, codex.go).
func CodexTurnError(raw json.RawMessage) (text, kind string) {
	text, kind, _ = CodexTurnErrorRecovery(raw)
	return text, kind
}

// CodexTurnErrorRecovery additionally reports the narrower #3204 model-service classes. A transient disconnected
// stream or 408 remains worth a user's Continue, but is not an automatic assistant model retry; structured 429/5xx,
// overload and rate-limit failures are.
func CodexTurnErrorRecovery(raw json.RawMessage) (text, kind string, modelService bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", false
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil { // an error that is just its message
		return firstText(plain), "", false
	}
	var e struct {
		Message string          `json:"message"`
		Snake   json.RawMessage `json:"codex_error_info"`
		Camel   json.RawMessage `json:"codexErrorInfo"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return CodexTurnFailed, "", false
	}
	info := e.Snake
	if len(info) == 0 || string(info) == "null" {
		info = e.Camel
	}
	kind, modelService = codexErrorKind(info)
	return firstText(e.Message), kind, modelService
}

func firstText(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return CodexTurnFailed
}

// codexErrorKind sorts Codex's CodexErrorInfo: a name ("server_overloaded" in a rollout, "serverOverloaded" from an
// app-server) or one key naming a failed connection with its HTTP status ({"http_connection_failed":
// {"http_status_code": 503}}).
func codexErrorKind(info json.RawMessage) (string, bool) {
	if len(info) == 0 || string(info) == "null" {
		return "", false
	}
	var name string
	status := 0
	if json.Unmarshal(info, &name) != nil {
		var keyed map[string]struct {
			Snake *int `json:"http_status_code"`
			Camel *int `json:"httpStatusCode"`
		}
		if json.Unmarshal(info, &keyed) != nil || len(keyed) != 1 {
			return "", false
		}
		for k, v := range keyed {
			name = k
			if v.Snake != nil {
				status = *v.Snake
			} else if v.Camel != nil {
				status = *v.Camel
			}
		}
	}
	switch strings.ToLower(strings.ReplaceAll(name, "_", "")) {
	case "", "other":
		return "", false
	case "serveroverloaded", "internalservererror", "ratelimitexceeded":
		return proto.TurnErrorTransient, true
	case "usagelimitexceeded", "usagelimitreached":
		return proto.TurnErrorUsageLimit, false
	case "httpconnectionfailed", "responsestreamconnectionfailed", "responsestreamdisconnected", "responsetoomanyfailedattempts":
		if status == 429 || status >= 500 {
			return proto.TurnErrorTransient, true
		}
		// No status and 408 remain transient for the user's own Continue, but are outside #3204's automatic retry.
		if status == 0 || status == 408 {
			return proto.TurnErrorTransient, false
		}
		return proto.TurnErrorUser, false // 401 / 403 and the other 4xx are the user's to fix
	default:
		// context_window_exceeded, unauthorized, model_not_found, bad_request, a policy… and kinds newer Codex adds: no
		// "continue" would get past them.
		return proto.TurnErrorUser, false
	}
}
