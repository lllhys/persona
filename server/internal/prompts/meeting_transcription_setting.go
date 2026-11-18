package prompts

// RenderMeetingTranscriptionSetting describes the assistant-owned setting to persona_settings callers.
func RenderMeetingTranscriptionSetting() string { return render("meeting/transcription_setting", nil) }
