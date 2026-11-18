package prompts

import (
	"strings"
	"testing"
	"unicode"
)

// The built-in urgent rules follow the owner's language (#9044): Chinese stays as it was, English and Japanese owners
// get theirs, and an id that is no built-in rule has no words in any language.
func TestUrgentRuleBuiltinFollowsTheOwnersLanguage(t *testing.T) {
	zh := map[string]string{ // i18n-ignore: the Chinese rules as #9018 wrote them, byte for byte
		"money_loss":     "资金或资源正在损失：扣费失控还在涨、云资源被删、账号被盗刷或出现陌生扣款。",
		"harm":           "人身伤害或安全：烟雾、燃气、漏水报警，有人求救，跌倒检测这类。",
		"agent_disaster": "Agent 造成的灾难性错误：删库、误删恢复不了的数据、生产事故、不可逆的外发（把密钥推到公开仓库、群发给客户）。",
	}
	en := map[string]string{"money_loss": "Money or resources are being lost", "harm": "Physical harm or safety",
		"agent_disaster": "A catastrophic mistake by an Agent"}
	ja := map[string]string{"money_loss": "資金やリソース", "harm": "人身の危険", "agent_disaster": "鍵やシークレット"} // i18n-ignore
	for id, want := range zh {
		if got := UrgentRuleBuiltin(id); got != want {
			t.Errorf("%s in Chinese = %q, want it unchanged %q", id, got, want)
		}
		if got := UrgentRuleBuiltinIn(id, "English"); !strings.HasPrefix(got, en[id]) || strings.ContainsFunc(got, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			t.Errorf("%s in English = %q", id, got)
		}
		if got := UrgentRuleBuiltinIn(id, "日本語"); !strings.Contains(got, ja[id]) { // i18n-ignore
			t.Errorf("%s in Japanese = %q", id, got)
		}
	}
	if got := UrgentRuleBuiltinIn("r_abc", "English"); got != "" {
		t.Errorf("an owner's rule id has built-in words: %q", got)
	}
}
