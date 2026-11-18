package node

import "context"

type launcherPurposeKey struct{}

// Assistant discovery and checks follow the process that runs the conversation,
// independently of the computer's engine for ordinary Agents.
func withLauncherPurpose(ctx context.Context, purpose string) context.Context {
	if purpose == "assistant" {
		return context.WithValue(ctx, launcherPurposeKey{}, true)
	}
	return ctx
}

func assistantLauncherPurpose(ctx context.Context) bool {
	assistant, _ := ctx.Value(launcherPurposeKey{}).(bool)
	return assistant
}
