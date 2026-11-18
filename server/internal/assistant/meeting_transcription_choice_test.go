package assistant

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The manager's meeting.transcription_choice (the built-in Volcengine speech extension's switch): reading changes
// nothing; on chooses the contribution pinned to its installation and new meetings on the hub; off returns a choice
// of it to the built-in one and the phone, and leaves another choice alone; a ref that is no contribution is refused.
func TestMeetingTranscriptionChoice(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	s.trans = newSettingsTranscribers()
	choose := func(p assistantwire.MeetingTranscriptionChoose) (assistantwire.MeetingTranscriptionChoice, error) {
		t.Helper()
		raw, _ := json.Marshal(p)
		out, ok, err := s.meetingChoiceRequest(context.Background(), assistantwire.MethodMeetingTranscriptionChoice, raw)
		if !ok {
			t.Fatal("not handled")
		}
		if err != nil {
			return assistantwire.MeetingTranscriptionChoice{}, err
		}
		return out.(assistantwire.MeetingTranscriptionChoice), nil
	}
	if _, ok, _ := s.meetingChoiceRequest(context.Background(), "transcription.extension", nil); ok {
		t.Fatal("another method handled")
	}
	if got, err := choose(assistantwire.MeetingTranscriptionChoose{}); err != nil || got.Extension != "" || got.Default != "device" {
		t.Fatalf("read: %+v %v", got, err)
	}
	if _, err := choose(assistantwire.MeetingTranscriptionChoose{Set: true, Use: true, Extension: "missing/text"}); err == nil {
		t.Fatal("a ref that is no contribution was taken")
	}
	if got, err := choose(assistantwire.MeetingTranscriptionChoose{Set: true, Use: true, Extension: "example/text"}); err != nil ||
		got.Extension != "example/text" || got.Default != "hub" {
		t.Fatalf("on: %+v %v", got, err)
	}
	if v := s.settings(context.Background()); v.MeetingTranscriptionInstall == nil || *v.MeetingTranscriptionInstall != "example@1" {
		t.Fatalf("not pinned to its installation: %+v", v.MeetingTranscriptionInstall)
	}
	out := transcriptionSettingsRequest(t, s, "GET", "", "", 200)
	if out["meeting_transcription_extension"] != "example/text" || out["meeting_transcription_default"] != "hub" {
		t.Fatalf("the settings say %v", out)
	}
	// Another choice made meanwhile (Recording settings): turning the switch off leaves it.
	transcriptionSettingsRequest(t, s, "PUT", "", `{"meeting_transcription_extension":""}`, 200)
	if got, _ := choose(assistantwire.MeetingTranscriptionChoose{Set: true, Extension: "example/text"}); got.Extension != "" || got.Default != "hub" {
		t.Fatalf("off with another choice: %+v", got)
	}
	choose(assistantwire.MeetingTranscriptionChoose{Set: true, Use: true, Extension: "example/text"})
	if got, _ := choose(assistantwire.MeetingTranscriptionChoose{Set: true, Extension: "example/text"}); got.Extension != "" || got.Default != "device" {
		t.Fatalf("off: %+v", got)
	}
	if v := s.settings(context.Background()); v.MeetingTranscriptionInstall != nil {
		t.Fatalf("the installation stays pinned: %v", *v.MeetingTranscriptionInstall)
	}
}
