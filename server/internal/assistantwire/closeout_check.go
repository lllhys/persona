package assistantwire

// MethodCloseoutCheck: a node asks, as an agent's turn that did work ends without a closeout, whether to hold the stop
// once and ask for one (docs/assistant/agent-work.md "Closeout"). The manager answers: it holds the switch.
const MethodCloseoutCheck = "agents.closeout.check"

type CloseoutCheckRequest struct {
	SessionID string `json:"session_id"`
}

// CloseoutCheckResponse: Reason is what the agent is told when its stop is held; empty lets it stop.
type CloseoutCheckResponse struct {
	Reason string `json:"reason,omitempty"`
}
