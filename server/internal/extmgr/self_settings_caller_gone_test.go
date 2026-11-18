package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A change of one of the manager's settings whose caller stopped waiting while its card was being answered is not
// made (#5907). The hub's call ended, so the assistant was told that nothing came of it: a change made now would be
// one nobody hears of. The answer and the caller's end can cross: the assistant's queue still hands back an answer it
// had taken when the caller went (its await), which is what Ask does here. Adding a mail account does not take the
// call's context, so only the check before the change holds it back.
func TestSelfSettingsChangeNobodyWaitsForIsNotMade(t *testing.T) {
	h := newTestManager(t)
	fm := &selfSettingsMail{}
	h.mail = fm
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.opts.Ask = func(context.Context, proto.Interaction) (proto.Resolution, error) {
		cancel() // the caller stops waiting just as the user allows the change
		return proto.Resolution{Allow: new(true), Values: map[string]string{"password": "example-password"}}, nil
	}
	out, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"add","key":"mail.accounts","value":{"email":"me@example.com"}}`))
	if !errors.Is(err, context.Canceled) || out != "" {
		t.Errorf("the tool answered %q, %v: want the caller's end and no word of a change", out, err)
	}
	if len(fm.added) != 0 {
		t.Errorf("the account was added after its caller stopped waiting: %+v", fm.added)
	}
}
