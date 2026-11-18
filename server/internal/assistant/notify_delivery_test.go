package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// What notify_user tells the model, and what the north star records, for each answer the hub (or the outbox) gives
// (#8138): only a notice a device took reads as sent and counts as an interruption.
func TestNotifyUserSaysWhereTheNoticeWent(t *testing.T) {
	devices := func(n int) *int { return &n }
	en := i18n.Lang("en")
	cases := []struct {
		name       string
		res        NotifyResult
		key        string
		args       i18n.Args
		wantMissed string
	}{
		{"a hub that does not report devices", NotifyResult{OK: true}, "", nil, ""},
		{"sent to two devices", NotifyResult{OK: true, Devices: devices(2)}, "assistant-notify.sent_notice", i18n.Args{"count": 2}, ""},
		{"no device", NotifyResult{OK: true, Devices: devices(0), Reason: "no_device"}, "assistant-notify.no_device_notice", nil, missedNoDevice},
		{"every device turned it off", NotifyResult{OK: true, Devices: devices(0), Reason: "filtered"}, "assistant-notify.filtered_notice", nil, missedFiltered},
		{"an offline phone keeps it", NotifyResult{OK: true, Devices: devices(0), Kept: 1, Reason: "no_device"}, "assistant-notify.kept_notice", i18n.Args{"count": 1}, missedKept},
		{"the hub is away", NotifyResult{OK: true, Queued: true}, "assistant-notify.hub_away_notice", nil, missedHubAway},
		{"held, nothing connected", NotifyResult{OK: true, Held: true, Reason: "ln_p1_held", Devices: devices(0)}, "assistant-notify.ln_held_notice", nil, missedHeld},
		{"held, a connected app still got it", NotifyResult{OK: true, Held: true, Reason: "ln_p1_held", Devices: devices(1)}, "assistant-notify.ln_held_notice", nil, ""},
		{"held by a hub that does not report devices", NotifyResult{OK: true, Held: true, Reason: "ln_p1_held"}, "assistant-notify.ln_held_notice", nil, ""},
	}
	for _, c := range cases {
		want := "Notification sent."
		if c.key != "" {
			want = i18n.T(en, c.key, c.args)
		}
		if got := notifyAnswerText(en, c.res, nil); got != want {
			t.Errorf("%s: said %q, want %q", c.name, got, want)
		}
		if got := notifyMissed(c.res); got != c.wantMissed {
			t.Errorf("%s: missed %q, want %q", c.name, got, c.wantMissed)
		}
	}
}

// Through the tool, in the user's language: a hub that reached no device makes notify_user say so, not "sent".
func TestNotifyUserWithNoDeviceSaysTheUserWasNotAlerted(t *testing.T) {
	f := newFakeHub().withMachine()
	f.ownModels = true
	zero := 0
	f.notifyResult = NotifyResult{OK: true, Devices: &zero, Reason: "no_device"}
	s := newTestService(t, f)
	out, err := runTool(s, "routine-conv", "notify_user", map[string]any{"title": "Report", "body": "Ready."})
	if err != nil {
		t.Fatal(err)
	}
	if want := i18n.T(s.lang(t.Context()), "assistant-notify.no_device_notice"); out != want {
		t.Fatalf("answer = %q, want %q", out, want)
	}
}
