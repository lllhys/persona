package apierr

import (
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// StartInstead is in plain words in l what a new Agent's start runs instead of what was asked (#8140): where its host
// is an engine that runs only its own launchers (Paseo; engine is its name, machine the computer's), the launcher it has
// no provider of and the one of the same kind of agent that runs (ran.Launcher), the model that one does not offer, and
// the effort its model does not offer with the one it runs at (ran.Effort; none: the model's default). "" when it runs
// as asked. A note, not a refusal: the hub adds it to an app's start (launcher_note), the manager to a task's why and to
// agent_start's answer, worded here so each says it alike.
func StartInstead(l i18n.Lang, engine, machine string, in *proto.StartInstead, ran proto.Session) string {
	if in == nil {
		return ""
	}
	to := ran.Launcher
	if to == "" {
		to = in.Launcher
	}
	var parts []string
	if in.Launcher != "" && ran.Launcher != "" && ran.Launcher != in.Launcher {
		parts = append(parts, i18n.T(l, "apierr-start_instead.launcher", i18n.Args{"engine": engine, "machine": machine, "launcher": in.Launcher, "to": ran.Launcher}))
	}
	if in.Model != "" {
		parts = append(parts, i18n.T(l, "apierr-start_instead.model", i18n.Args{"to": to, "model": in.Model}))
	}
	switch {
	case in.Effort != "" && ran.Effort != "" && ran.Effort != in.Effort:
		parts = append(parts, i18n.T(l, "apierr-start_instead.effort", i18n.Args{"to": to, "effort": in.Effort, "ran": ran.Effort}))
	case in.Effort != "" && ran.Effort == "":
		parts = append(parts, i18n.T(l, "apierr-start_instead.effort_default", i18n.Args{"to": to, "effort": in.Effort}))
	}
	return JoinNotes(l, parts...)
}

// JoinNotes is the notes that say something, one after the other in l, as StartInstead joins its parts.
func JoinNotes(l i18n.Lang, notes ...string) string {
	var said []string
	for _, n := range notes {
		if n != "" {
			said = append(said, n)
		}
	}
	return strings.Join(said, i18n.T(l, "apierr-start_instead.join"))
}
