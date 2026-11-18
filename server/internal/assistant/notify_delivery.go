package assistant

import (
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Where a notify_user notice went, from the hub's answer (assistantwire.NotifyResult): the tool tells the model so it
// never assumes the user was alerted when nobody was (#8138), and the north star counts the notice as an interruption
// only when a device took it (north_star.go).

// Why no device took a notify_user notice when it was made (the work event's "missed", the readout's notify_missed).
const (
	missedNoDevice = "no_device" // no phone, computer or browser could take it
	missedFiltered = "filtered"  // every device that could turned it off
	missedKept     = "kept"      // only offline devices keep it for their next connection
	missedHeld     = "held"      // low-distraction mode kept it for the briefing, and no connected device took it
	missedHubAway  = "hub_away"  // the hub was away: the manager's outbox keeps it until the hub is back
)

// notifyMissed is why no device took the notice res answers for, "" when one did or the hub did not say (a hub before
// it reported devices: counted as before, an interruption).
func notifyMissed(res NotifyResult) string {
	switch {
	case res.Queued:
		return missedHubAway
	case res.Devices == nil || *res.Devices > 0:
		return ""
	case res.Held:
		return missedHeld
	case res.Kept > 0:
		return missedKept
	case res.Reason == missedFiltered:
		return missedFiltered
	}
	return missedNoDevice
}

// notifyAnswerText is notify_user's result for the model, in the user's language l: sent to how many devices, held for
// the briefing, kept until a device or the hub is back, or received by nothing, and then where its copies to the chat
// channels went (copies, channels_copies.go). A hub that does not report devices keeps the old answer.
func notifyAnswerText(l i18n.Lang, res NotifyResult, copies []pushCopy) string {
	if len(copies) == 0 {
		return notifyDevicesText(l, res)
	}
	// A copy went to a chat channel (#8268): the devices part no longer says the user was not alerted, since the copy
	// may well have reached them; it says so only when no device took it and every copy failed too.
	var lead string
	switch {
	case res.Queued:
		lead = i18n.T(l, "assistant-notify.hub_away_copied")
	case res.Held, res.Devices == nil, *res.Devices > 0:
		lead = notifyDevicesText(l, res)
	case res.Kept > 0:
		lead = i18n.T(l, "assistant-notify.kept_copied", i18n.Args{"count": res.Kept})
	case res.Reason == missedFiltered:
		lead = i18n.T(l, "assistant-notify.filtered_copied")
	default:
		lead = i18n.T(l, "assistant-notify.no_device_copied")
	}
	out, reached := lead, false
	for _, c := range copies {
		name := c.name
		if c.channel == channel.Feishu {
			name = i18n.T(l, "assistant-channels_runs.feishu")
		}
		key := "assistant-notify.copy_pending"
		switch c.state {
		case copySent:
			key = "assistant-notify.copy_sent"
		case copyFailed:
			key = "assistant-notify.copy_failed"
		}
		out = i18n.T(l, "assistant-notify.then", i18n.Args{"first": out, "second": i18n.T(l, key, i18n.Args{"name": name})})
		reached = reached || c.state != copyFailed
	}
	if !reached && notifyMissed(res) != "" {
		out = i18n.T(l, "assistant-notify.then", i18n.Args{"first": out, "second": i18n.T(l, "assistant-notify.not_alerted")})
	}
	return out
}

// notifyDevicesText is notify_user's answer about the devices alone: what it says when no copy went to a chat channel.
func notifyDevicesText(l i18n.Lang, res NotifyResult) string {
	switch {
	case res.Queued:
		return i18n.T(l, "assistant-notify.hub_away_notice")
	case res.Held:
		// ln mode kept it off the phone (a non-main conversation's ordinary notice): say so, so the model does not
		// believe the user was alerted.
		return i18n.T(l, "assistant-notify.ln_held_notice")
	case res.Devices == nil:
		return "Notification sent."
	case *res.Devices > 0:
		return i18n.T(l, "assistant-notify.sent_notice", i18n.Args{"count": *res.Devices})
	case res.Kept > 0:
		return i18n.T(l, "assistant-notify.kept_notice", i18n.Args{"count": res.Kept})
	case res.Reason == missedFiltered:
		return i18n.T(l, "assistant-notify.filtered_notice")
	}
	return i18n.T(l, "assistant-notify.no_device_notice")
}
