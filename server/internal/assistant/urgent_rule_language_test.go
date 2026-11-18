package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
)

// The built-in urgent rules reach every place that says them in the owner's language (#9044): the assistant's
// instructions, persona_settings' list, the rule text a call keeps (the phone's call screen shows it) and the card that
// asks to change a rule. Chinese is as it was.
func TestUrgentRuleWordsFollowTheOwnersLanguage(t *testing.T) {
	for _, tc := range []struct{ locale, language string }{{"", ""}, {"zh-Hans", ""}, {"en", "English"}, {"ja", "日本語"}} { // i18n-ignore
		t.Run("locale "+tc.locale, func(t *testing.T) {
			ct := newCallTest(t, true, nil)
			ct.f.mu.Lock()
			ct.f.hello.Locale = tc.locale
			ct.f.mu.Unlock()
			want := prompts.UrgentRuleBuiltinIn("money_loss", tc.language)
			if tc.language == "" && want != prompts.UrgentRuleBuiltin("money_loss") {
				t.Fatalf("Chinese changed: %q", want)
			}
			ctx := context.Background()
			p := ct.s.urgentPrompt(ctx)
			found := false
			for _, r := range p.Rules {
				found = found || r.ID == "money_loss" && r.Text == want
			}
			if !found {
				t.Fatalf("instructions' rules %+v, want money_loss as %q", p.Rules, want)
			}
			if list, err := ct.s.getUrgentRules(ctx); err != nil || !strings.Contains(list, "- money_loss: "+want) {
				t.Fatalf("persona_settings list %q (%v)", list, err)
			}
			raw, _ := json.Marshal(map[string]string{"id": "money_loss"})
			change, err := ct.s.changeUrgentRules(ctx, "remove", raw)
			if err != nil || change.cardText == nil || !strings.Contains(change.cardText(i18n.Lang(tc.locale)), want) {
				t.Fatalf("remove card: %+v (%v)", change, err)
			}
			ct.call(moneyLoss)
			if c := ct.st.only(t); c.RuleText != want {
				t.Fatalf("the call keeps rule text %q, want %q", c.RuleText, want)
			}
		})
	}
}
