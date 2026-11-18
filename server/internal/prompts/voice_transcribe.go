package prompts

import "strings"

// VoiceTranscribe is what the built-in voice extension's meeting transcription sessions are told
// (voice/transcribe_caption, voice/transcribe_verbatim; docs/assistant/meeting-minutes.md "The built-in adapter").
type VoiceTranscribe struct {
	Lang string // the instructions' language: en, ja, otherwise Chinese
	// TextLang is the recording's language tag (BCP 47), "" when not known. A hint only: every word is written in the
	// language it was said in.
	TextLang string
	// Glossary are names and terms to spell right (the meeting's title, calendar title, attendees and the owner's
	// meeting_glossary): data written by others, never instructions.
	Glossary []string
	// Previous are the last sentences before this part of the meeting (a final-pass window, a live session that takes
	// over from another): context the session must not write again.
	Previous []string
}

// voiceTranscribeData adds what the templates derive from the data.
type voiceTranscribeData struct {
	VoiceTranscribe
	Script string // the Chinese script TextLang implies: Hans, Hant or ""
}

// RenderVoiceTranscribeCaption is the caption session's instructions: listen, never reply.
func RenderVoiceTranscribeCaption(d VoiceTranscribe) string {
	return render("voice/transcribe_caption", voiceTranscribeData{VoiceTranscribe: d})
}

// RenderVoiceTranscribeVerbatim is a verbatim session's instructions: after each sentence, the words said and nothing
// else, each in the language it was said in.
func RenderVoiceTranscribeVerbatim(d VoiceTranscribe) string {
	return render("voice/transcribe_verbatim", voiceTranscribeData{VoiceTranscribe: d, Script: ChineseScript(d.TextLang)})
}

// ChineseScript is the Chinese script a language tag implies (docs/assistant/meeting-minutes.md "The `transcription`
// extension point"): Hant for zh-TW, zh-HK, zh-MO and zh-Hant, Hans for any other Chinese tag, "" for another language.
func ChineseScript(tag string) string {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), "-")
	if len(parts) == 0 || (parts[0] != "zh" && parts[0] != "cmn" && parts[0] != "yue") {
		return ""
	}
	for _, p := range parts[1:] {
		switch p {
		case "hant", "tw", "hk", "mo":
			return "Hant"
		case "hans", "cn", "sg":
			return "Hans"
		}
	}
	if parts[0] == "yue" {
		return "Hant"
	}
	return "Hans"
}
