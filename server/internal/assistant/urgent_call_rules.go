package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/i18n"
)

// urgent_call.rules in persona_settings: the owner's "what counts as urgent" list (docs/assistant/urgent-call.md). The
// owner says "from now on, call me about X too"; the assistant reads the rule back and adds it here, and it takes
// effect only once the owner approves the card on the phone, as every setting change. Settings › Urgent calls edits
// the same list.

const urgentRulesSettingKey = "urgent_call.rules"

// urgentRuleValue is add's, update's and remove's value.
type urgentRuleValue struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (s *Service) urgentRulesSetting() ownSetting {
	return ownSetting{Setting: Setting{Key: urgentRulesSettingKey, Title: "What counts as urgent (urgent calls)",
		Actions: []string{"get", "add", "update", "remove"},
		About: "the rules an urgent_call with why rule must match; the built-in ones are money_loss, harm and agent_disaster. " +
			"Add one only when the owner asked to be called about that kind of matter, in their words, after reading it back to them",
		Value: `add: {"text": "<the kind of matter, in the owner's words>"}; update: {"id", "text"}; remove: {"id"}`},
		get: s.getUrgentRules, change: s.changeUrgentRules}
}

func (s *Service) getUrgentRules(ctx context.Context) (string, error) {
	set, err := s.urgentSettings(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if set.Enabled {
		b.WriteString("Urgent calls are on. What counts as urgent (id: rule):\n")
	} else {
		b.WriteString("Urgent calls are off (the owner turns them on in Settings › Urgent calls). The rules would be (id: rule):\n")
	}
	language := s.urgentLanguage(ctx)
	for _, r := range set.Rules {
		fmt.Fprintf(&b, "- %s: %s\n", r.ID, urgentRuleText(r, language))
	}
	return b.String(), nil
}

func (s *Service) changeUrgentRules(ctx context.Context, action string, raw json.RawMessage) (*ownChange, error) {
	var v urgentRuleValue
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errors.New(`value must be {"id", "text"}`)
	}
	v.ID = strings.TrimSpace(v.ID)
	v.Text = strings.Join(strings.Fields(v.Text), " ")
	if s.store() == nil {
		return nil, errors.New("no database on this hub")
	}
	set, err := s.urgentSettings(ctx)
	if err != nil {
		return nil, err
	}
	language := s.urgentLanguage(ctx) // the built-in rules' words, as the owner reads them
	at := slices.IndexFunc(set.Rules, func(r urgentRule) bool { return r.ID == v.ID })
	switch action {
	case "add", "update":
		if v.Text == "" || utf8.RuneCountInString(v.Text) > urgentRuleTextMax {
			return nil, fmt.Errorf("text must be 1 to %d characters", urgentRuleTextMax)
		}
	}
	switch action {
	case "add":
		if len(set.Rules) >= urgentRulesMax {
			return nil, fmt.Errorf("the list already has %d rules: remove one first", urgentRulesMax)
		}
		if slices.ContainsFunc(set.Rules, func(r urgentRule) bool { return strings.EqualFold(urgentRuleText(r, language), v.Text) }) {
			return &ownChange{summary: "That rule is already in the list."}, nil
		}
		id := "r_" + randomHex(3)
		return &ownChange{cardText: func(l i18n.Lang) string {
			return i18n.T(l, "assistant-urgent_call.rule_add", i18n.Args{"text": v.Text})
		}, apply: func(ctx context.Context) (string, error) {
			return s.editUrgentRules(ctx, func(rules []urgentRule) ([]urgentRule, error) {
				return append(rules, urgentRule{ID: id, Text: v.Text}), nil
			}, fmt.Sprintf("Added rule %s: %s", id, v.Text))
		}}, nil
	case "update", "remove":
		if at < 0 {
			return nil, fmt.Errorf("no rule %q: get lists them", v.ID)
		}
		old := urgentRuleText(set.Rules[at], language)
		if action == "update" {
			return &ownChange{cardText: func(l i18n.Lang) string {
				return i18n.T(l, "assistant-urgent_call.rule_update", i18n.Args{"old": old, "text": v.Text})
			}, apply: func(ctx context.Context) (string, error) {
				return s.editUrgentRules(ctx, func(rules []urgentRule) ([]urgentRule, error) {
					i := slices.IndexFunc(rules, func(r urgentRule) bool { return r.ID == v.ID })
					if i < 0 {
						return nil, fmt.Errorf("rule %s is gone", v.ID)
					}
					rules[i].Text = v.Text
					return rules, nil
				}, fmt.Sprintf("Rule %s now says: %s", v.ID, v.Text))
			}}, nil
		}
		return &ownChange{cardText: func(l i18n.Lang) string {
			return i18n.T(l, "assistant-urgent_call.rule_remove", i18n.Args{"text": old})
		}, apply: func(ctx context.Context) (string, error) {
			return s.editUrgentRules(ctx, func(rules []urgentRule) ([]urgentRule, error) {
				return slices.DeleteFunc(rules, func(r urgentRule) bool { return r.ID == v.ID }), nil
			}, fmt.Sprintf("Removed rule %s.", v.ID))
		}}, nil
	}
	return nil, fmt.Errorf("unknown action %q", action)
}

// editUrgentRules changes the stored list and answers done.
func (s *Service) editUrgentRules(ctx context.Context, edit func([]urgentRule) ([]urgentRule, error), done string) (string, error) {
	st := s.store()
	if st == nil {
		return "", errors.New("no database on this hub")
	}
	set, err := s.urgentSettings(ctx)
	if err != nil {
		return "", err
	}
	rules, err := edit(slices.Clone(set.Rules))
	if err != nil {
		return "", err
	}
	set.Rules = rules
	if err := set.check(); err != nil {
		return "", errors.New("the rule list would not be valid")
	}
	if err := st.PutSetting(ctx, urgentCallSettingsKey, set); err != nil {
		return "", err
	}
	return done, nil
}
