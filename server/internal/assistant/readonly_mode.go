package assistant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nwverse/persona/server/internal/i18n"
)

const (
	readonlyModeKey = "assistant.readonly_mode"
	readonlySandbox = "sandbox"
	readonlyPrompt  = "prompt"
)

func validReadonlyMode(mode string) bool {
	return mode == readonlySandbox || mode == readonlyPrompt
}

func readonlyModeOf(v Settings) string {
	if v.ReadonlyMode == readonlyPrompt {
		return readonlyPrompt
	}
	return readonlySandbox
}

func (s *Service) setReadonlyMode(ctx context.Context, raw json.RawMessage) (*ownChange, error) {
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil || !validReadonlyMode(mode) {
		return nil, i18n.Err("assistant-profile.bad_readonly_mode")
	}
	before := readonlyModeOf(s.settings(ctx))
	if mode == before {
		return &ownChange{summary: readonlyModeKey + " is unchanged"}, nil
	}
	if s.store() == nil {
		return nil, fmt.Errorf("no database on this manager")
	}
	return &ownChange{
		cardText: func(l i18n.Lang) string {
			return i18n.T(l, "assistant-settings_tool.change_readonly_mode", i18n.Args{"before": before, "after": mode})
		},
		apply: func(ctx context.Context) (string, error) {
			if _, err := s.updateSettings(ctx, func(v *Settings) error { v.ReadonlyMode = mode; return nil }); err != nil {
				return "", err
			}
			s.settingsChanged(ctx)
			return readonlyModeKey + " is " + mode + " for new task starts and retries", nil
		},
	}, nil
}
