package kb

import "strings"

// The private parts of a person's profile (docs/assistant/knowledge.md "Person profiles", "Who reads the private
// parts"): the user's own words about the person (views) and the guesses about them (inferences). They are the user's
// private view of someone, so a conversation that writes to other people on the user's behalf (the inbox's AI twin,
// Lark /agent, any conversation an extension opens) never reads them: the hub drops their lines from what its tools
// return there (assistant's outwardReader). Facts and commitments stay. renderProfile writes every line of these parts
// with its label and on one line, so a line can be told by itself, wherever it turns up: in summary.md, in a search
// hit, or quoted in another conversation's transcript.

// The headings and labels of the private parts in summary.md.
const (
	profileViewsHeading      = "### 用户说过的评价（原话）"   // i18n-ignore: summary.md is read by the model
	profileInferencesHeading = "### 推断（仅供参考，未经确认）" // i18n-ignore
	profileViewLabel         = "[用户原话]"            // i18n-ignore
	profileInferenceLabel    = "[推断·仅供参考]"         // i18n-ignore
)

// privateProfileMarks are what PrivateProfileLine looks for: the labels, and the headings' words (so a heading is
// caught as a search hit or with other text before it too).
var privateProfileMarks = []string{profileViewLabel, profileInferenceLabel,
	strings.TrimPrefix(profileViewsHeading, "### "), strings.TrimPrefix(profileInferencesHeading, "### ")}

// PrivateProfileLine says whether line is, or quotes, a line of a private part of a profile (or its heading).
func PrivateProfileLine(line string) bool {
	for _, m := range privateProfileMarks {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// WithoutPrivateProfile is text without the lines PrivateProfileLine finds; text itself when it has none.
func WithoutPrivateProfile(text string) string {
	if !PrivateProfileLine(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if !PrivateProfileLine(l) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}
