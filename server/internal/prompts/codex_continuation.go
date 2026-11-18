package prompts

type CodexContinuationMessage struct {
	Role string
	Text string
}

type CodexContinuation struct {
	Path     string
	Messages []CodexContinuationMessage
}

func RenderCodexContinuation(d CodexContinuation) string {
	return render("agents/codex_continuation", d)
}
