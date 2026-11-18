package kb

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func hseg(start, end float64, text string) RecordingSegment {
	return RecordingSegment{Start: start, End: end, Text: text}
}

// The transcript of record: device mode is the phone's; hub mode the hub's sentences, with each phone segment the
// hub's cover no more than half of (their union), in time order, each with its source.
func TestTranscriptOfRecord(t *testing.T) {
	device := []RecordingSegment{hseg(0, 10, "手机一"), hseg(10, 20, "手机二"), hseg(20, 30, "phone three"), hseg(30, 40, "phone four")}
	hub := []RecordingSegment{
		hseg(0, 3, "我们先把 export cap 保持在 4 MB。"), hseg(3.5, 6, "Bo owns the vendor follow-up."), // 6 s of 0–10 together: 55%
		hseg(10, 14.9, "Let's revisit pricing next week."),       // 49% of 10–20
		hseg(30, 35.1, "下周三之前 Alice 要把 pricing 的 proposal 发出来。"), // 51% of 30–40
	}
	if got := TranscriptOfRecord(false, device, hub); !slices.Equal(got, device) {
		t.Fatalf("device mode %+v", got)
	}
	got := TranscriptOfRecord(true, device, hub)
	var lines []string
	for _, sg := range got {
		lines = append(lines, sg.Source+": "+sg.Text)
	}
	want := []string{
		"hub: 我们先把 export cap 保持在 4 MB。",
		"hub: Bo owns the vendor follow-up.",
		"hub: Let's revisit pricing next week.",
		"device: 手机二",
		"device: phone three",
		"hub: 下周三之前 Alice 要把 pricing 的 proposal 发出来。",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("record\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// Hub only (the phone wrote nothing), and the phone only (the hub has nothing yet: failed).
	if got := TranscriptOfRecord(true, nil, hub); len(got) != 4 || got[0].Source != SourceHub {
		t.Fatalf("hub only %+v", got)
	}
	if got := TranscriptOfRecord(true, device, nil); len(got) != 4 || got[3].Source != SourceDevice {
		t.Fatalf("phone only %+v", got)
	}
}

// HubTranscriptPut stores the hub's sentences and renders transcript.md from the transcript of record; detail.segments
// carry their source, chars count the record, search finds the hub's text (never in hub_segments.json itself); a
// later upload renders it from both again; a live pass never replaces a final one; refused when the recording is not
// transcribed on the hub, deleted or trimmed.
func TestHubTranscriptPut(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	start := now.Add(-time.Hour)
	e, _ := hubEngine(t, nil)
	if _, err := e.HubTranscriptPut(recID, HubPassLive, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no upload: %v", err)
	}
	putRecording(t, e, recID, hubUpload(1, start, false, nil, "手机一", "手机二", "手机三"), now)
	if _, err := e.HubTranscriptPut(recID, "replay", nil); !errors.Is(err, ErrBadRecording) {
		t.Fatalf("bad pass: %v", err)
	}
	if h, err := e.HubTranscript(recID); err != nil || h.Rev != 0 || h.Segments == nil || len(h.Segments) != 0 {
		t.Fatalf("none yet %+v %v", h, err)
	}
	rev, err := e.HubTranscriptPut(recID, HubPassLive, []RecordingSegment{
		hseg(20.5, 29, "zebrapricing 下周 revisit"), hseg(0, 4, "我们先把\nexport cap   保持在 4 MB。"), hseg(4.5, 9, "Bo owns it."), hseg(12, 12, "  "),
		{Start: 13, End: 14, Text: "x", Source: SourceDevice},
	})
	if err != nil || rev != 1 {
		t.Fatalf("put %d %v", rev, err)
	}
	h, _ := e.HubTranscript(recID)
	if h.Pass != HubPassLive || len(h.Segments) != 4 || h.Segments[0].Text != "我们先把 export cap 保持在 4 MB。" || h.Segments[3].Source != SourceHub {
		t.Fatalf("stored %+v", h)
	}
	md := readFile(t, e, recID, "transcript.md")
	for _, want := range []string{"我们先把 export cap 保持在 4 MB。", "Bo owns it.", "手机二", "zebrapricing"} {
		if !strings.Contains(md, want) {
			t.Errorf("transcript.md lacks %q:\n%s", want, md)
		}
	}
	for _, gone := range []string{"手机一", "手机三"} {
		if strings.Contains(md, gone) {
			t.Errorf("transcript.md says %q twice:\n%s", gone, md)
		}
	}
	d, err := e.Get(KindRecording, recID, 20000)
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, sg := range d.Segments {
		sources = append(sources, sg.Source)
	}
	if !slices.Equal(sources, []string{SourceHub, SourceHub, SourceDevice, SourceHub, SourceHub}) || !strings.Contains(d.Recent, "zebrapricing") {
		t.Fatalf("detail segments %+v", d.Segments)
	}
	raw, _ := json.Marshal(d.Segments[0])
	if !strings.Contains(string(raw), `"source":"hub"`) {
		t.Fatalf("wire %s", raw)
	}
	record := TranscriptOfRecord(true, []RecordingSegment{hseg(0, 9, "手机一"), hseg(10, 19, "手机二"), hseg(20, 29, "手机三")}, h.Segments)
	if r := recordingNow(t, e, recID); r.Chars != recordChars(record) || r.Segments != 3 {
		t.Fatalf("chars %d segments %d, want %d", r.Chars, r.Segments, recordChars(record))
	}
	if got, err := e.RecordOf(recID); err != nil || !slices.Equal(got, d.Segments) {
		t.Fatalf("record of %+v %v", got, err)
	}
	if got := e.Store.RecordingSegments(recID); !slices.Equal(got, d.Segments) {
		t.Fatalf("follow-up seed %+v", got)
	}
	// Searched in transcript.md, never in hub_segments.json.
	hits := e.Search("zebrapricing", true, 40)
	if len(hits) != 1 || hits[0].Path != "recordings/"+recID+"/transcript.md" {
		t.Fatalf("search %+v", hits)
	}
	if found := e.Find("zebrapricing", 10); len(found) != 1 || found[0].Hits[0].Path != "recordings/"+recID+"/transcript.md" {
		t.Fatalf("find %+v", found)
	}
	for _, name := range []string{hubSegmentsFile, hubSegmentsFile + ".tmp"} {
		if searchable("recordings/" + recID + "/" + name) {
			t.Errorf("%s is searched", name)
		}
	}

	// A later upload renders from both again: the phone's new line where the hub has none.
	putRecording(t, e, recID, hubUpload(2, start, false, nil, "手机一", "手机二", "手机三", "手机四"), now)
	if md := readFile(t, e, recID, "transcript.md"); !strings.Contains(md, "手机四") || !strings.Contains(md, "zebrapricing") || strings.Contains(md, "手机一") {
		t.Fatalf("after the upload:\n%s", md)
	}
	if h, _ := e.HubTranscript(recID); h.Rev != 1 {
		t.Fatalf("the upload changed the hub's transcript: %+v", h)
	}

	// The final pass replaces it; a late live flush never replaces the final one.
	if rev, err := e.HubTranscriptPut(recID, HubPassFinal, []RecordingSegment{hseg(0, 30, "final words")}); err != nil || rev != 2 {
		t.Fatalf("final %d %v", rev, err)
	}
	if _, err := e.HubTranscriptPut(recID, HubPassLive, []RecordingSegment{hseg(0, 1, "late")}); !errors.Is(err, ErrTranscriptionFinal) {
		t.Fatalf("live after final: %v", err)
	}
	if md := readFile(t, e, recID, "transcript.md"); !strings.Contains(md, "final words") || strings.Contains(md, "zebrapricing") || !strings.Contains(md, "手机四") {
		t.Fatalf("final transcript.md:\n%s", md)
	}

	// Device mode, a delete, the trim.
	const dev, trimmed = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c"
	u := meetingUpload(1, start, "一句")
	u.SetTranscription(TranscriptionDevice, nil)
	putRecording(t, e, dev, u, now)
	if _, err := e.HubTranscriptPut(dev, HubPassLive, []RecordingSegment{hseg(0, 1, "x")}); !errors.Is(err, ErrNotHub) {
		t.Fatalf("device: %v", err)
	}
	putRecording(t, e, trimmed, hubUpload(1, start, true, nil, "一句"), now)
	putMeetingMinutes(t, e, trimmed, Minutes{Version: 1, Title: "Design review", When: "w"})
	if _, _, err := e.Store.trimRecording(recordingNow(t, e, trimmed), now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.HubTranscriptPut(trimmed, HubPassFinal, []RecordingSegment{hseg(0, 1, "x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trimmed: %v", err)
	}
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.HubTranscriptPut(recID, HubPassFinal, []RecordingSegment{hseg(0, 1, "x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	if _, err := e.HubTranscript(recID); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("read deleted: %v", err)
	}
}

// The hub's sentences alone count as text: a meeting whose phone wrote nothing gets chars, so minutes are planned
// (G4 "has text"); the trim takes hub_segments.json with the transcript.
func TestHubTranscriptCountsAsText(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	putRecording(t, e, recID, hubUpload(1, now.Add(-time.Hour), true, nil), now)
	if r := recordingNow(t, e, recID); r.Chars != 0 || r.MinutesState != "" {
		t.Fatalf("no text yet %+v", r)
	}
	if _, err := e.HubTranscriptPut(recID, HubPassLive, []RecordingSegment{hseg(0, 4, "Bo owns the vendor follow-up.")}); err != nil {
		t.Fatal(err)
	}
	if r := recordingNow(t, e, recID); r.Chars != len("Bo owns the vendor follow-up.") || r.MinutesState != MinutesPending {
		t.Fatalf("hub text %+v", r)
	}
	if err := e.AskMinutes(recID); err != nil {
		t.Fatalf("ask: %v", err)
	}
	// The next upload (the phone still has nothing) keeps counting the hub's.
	putRecording(t, e, recID, hubUpload(2, now.Add(-time.Hour), true, nil), now)
	if r := recordingNow(t, e, recID); r.Chars == 0 {
		t.Fatal("an upload forgot the hub's text")
	}
	putMeetingMinutes(t, e, recID, Minutes{Version: 1, Title: "Design review", When: "w"})
	if _, _, err := e.Store.trimRecording(recordingNow(t, e, recID), now); err != nil {
		t.Fatal(err)
	}
	if names := recordingFileNames(t, e, recID); slices.Contains(names, hubSegmentsFile) {
		t.Fatalf("the trim kept %v", names)
	}
}

// A crash between hub_segments.json and transcript.md (or meta.json) leaves the transcript behind: the next retention
// pass renders it again and counts its characters.
func TestHubTranscriptRepairedByRetention(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	putRecording(t, e, recID, hubUpload(1, now.Add(-time.Hour), false, nil, "手机一"), now)
	raw, _ := json.Marshal(HubTranscript{Rev: 1, Pass: HubPassLive, Segments: []RecordingSegment{{Start: 0, End: 9, Text: "quokkaword spoken", Source: SourceHub}}})
	if err := writeRecordingFile(e.Store.Path(KindRecording, recID, hubSegmentsFile), raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, e, recID, "transcript.md"), "quokkaword") {
		t.Fatal("set-up")
	}
	e.PruneRecordings(now)
	if md := readFile(t, e, recID, "transcript.md"); !strings.Contains(md, "quokkaword") || strings.Contains(md, "手机一") {
		t.Fatalf("not mended:\n%s", md)
	}
	if r := recordingNow(t, e, recID); r.Chars != len([]rune("quokkaword spoken")) {
		t.Fatalf("chars %d", r.Chars)
	}
	if hits := e.Search("quokkaword", true, 10); len(hits) != 1 {
		t.Fatalf("search %+v", hits)
	}
}
