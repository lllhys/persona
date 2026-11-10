package kb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const recID = "0f8fad5b-d9cb-469f-a165-70867728950e"

func recUpload(rev int64, start time.Time, complete bool, texts ...string) RecordingUpload {
	u := RecordingUpload{Revision: rev, StartedAt: start, Duration: float64(10 * len(texts)), Language: "zh-CN", Device: "phone", Complete: complete}
	for i, t := range texts {
		u.Segments = append(u.Segments, RecordingSegment{Start: float64(10 * i), End: float64(10*i + 9), Text: t})
	}
	return u
}

func TestRecordingIdempotentAndOrdered(t *testing.T) {
	e := goldenEngine(t, nil)
	var indexed []string
	e.Index = func(kind, id, name string, _ Tagging, _ string, _ time.Time) {
		indexed = append(indexed, kind+"/"+id+" "+name)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, goldenZone)
	start := now.Add(-time.Hour)
	// Upper case is the same recording.
	for range 2 { // a replay changes nothing
		if st, err := e.PutRecording(strings.ToUpper(recID), recUpload(2, start, false, "第一段", "第二段"), now); err != nil || st.Status != RecordingStored {
			t.Fatalf("put: %v %v", st, err)
		}
	}
	if st, _ := e.PutRecording(recID, recUpload(1, start, false, "旧的"), now); st.Status != RecordingStale {
		t.Fatalf("an older revision: %v, want stale", st)
	}
	if ids := e.Store.IDs(KindRecording); len(ids) != 1 || ids[0] != recID {
		t.Fatalf("ids %v", ids)
	}
	if segs := e.Store.RecordingSegments(recID); len(segs) != 2 || segs[0].Text != "第一段" {
		t.Fatalf("segments %+v", segs)
	}
	if len(indexed) == 0 || !strings.HasPrefix(indexed[0], "recordings/"+recID+" 录音 2026-11-05 11:00") {
		t.Fatalf("indexed %v", indexed)
	}
	for _, bad := range []struct {
		id string
		u  RecordingUpload
	}{
		{"not-a-uuid", recUpload(3, start, false)},
		{recID, recUpload(0, start, false)},
		{recID, recUpload(3, time.Time{}, false)},
		{recID, RecordingUpload{Revision: 3, StartedAt: start, Device: "tv"}},
	} {
		if _, err := e.PutRecording(bad.id, bad.u, now); !errors.Is(err, ErrBadRecording) {
			t.Errorf("%s %+v: %v, want ErrBadRecording", bad.id, bad.u, err)
		}
	}
}

func TestRecordingPartialThenComplete(t *testing.T) {
	m := &captureModel{reply: `{"title":"周会","summary":"## 这是什么\n周会","one_line":"周五灰度","importance":"P1","relevance":80,"tags":["周会"],"projects":[{"name":"示例发布","update":"定了周五灰度"}]}`}
	e := goldenEngine(t, m)
	_ = e.Store.PutProject(&Project{Slug: "demo-release", Name: "示例发布", Status: "active"})
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, goldenZone)
	start := now.Add(-time.Hour)
	ctx := context.Background()
	// Transcription still going: stored and searchable, not summarized.
	if _, err := e.PutRecording(recID, recUpload(1, start, false, "先说发布"), now); err != nil {
		t.Fatal(err)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 0 || len(m.calls) != 0 {
		t.Fatalf("summarized a partial transcript: %d", n)
	}
	if res := e.Find("先说发布", 10); len(res) != 1 || res[0].Entry.Kind != KindRecording {
		t.Fatalf("find %+v", res)
	}
	// The rest, complete: one summary, the project linked and logged.
	u := recUpload(2, start, true, "先说发布", "周五灰度")
	end := start.Add(20 * time.Second)
	u.EndedAt, u.Bookmarks = &end, []RecordingBookmark{{At: 12, Label: "灰度时间"}}
	if _, err := e.PutRecording(recID, u, now); err != nil {
		t.Fatal(err)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 1 {
		t.Fatalf("summarized %d, want 1", n)
	}
	m.golden(t, "recording")
	if n := e.SummarizeRecordings(ctx, 3); n != 0 {
		t.Fatalf("summarized again: %d", n)
	}
	r, _ := e.Store.Recording(recID)
	if r.Name != "周会" || r.Importance != "P1" || r.SummarizedRev != 2 || len(r.Projects) != 1 {
		t.Fatalf("recording %+v", r)
	}
	if p, _ := e.Store.Project("demo-release"); !p.Dirty || len(p.Items) != 1 || p.Items[0] != "recordings/"+recID {
		t.Fatalf("project %+v", p)
	}
	if log := e.Store.Log("demo-release", 5); len(log) != 1 || !strings.Contains(log[0], "定了周五灰度") {
		t.Fatalf("log %v", log)
	}
	d, err := e.Get(KindRecording, strings.ToUpper(recID), 1000)
	if err != nil || len(d.Segments) != 2 || !strings.Contains(d.Summary, "周会") || !strings.Contains(d.Recent, "[书签] 灰度时间") {
		t.Fatalf("get %+v %v", d, err)
	}
	if l := e.List(KindRecording, ""); len(l) != 1 || l[0].Name != "周会" || l[0].Extra != "phone" {
		t.Fatalf("list %+v", l)
	}
	// Re-transcribed (another language): summarized again.
	if _, err := e.PutRecording(recID, recUpload(3, start, true, "again"), now); err != nil {
		t.Fatal(err)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 1 {
		t.Fatalf("a new revision was not summarized again: %d", n)
	}
	// By hand: tags only, the content stays the phone's.
	if _, err := e.Apply(KindRecording, recID, Update{Tags: []string{"手动"}}); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.Store.Recording(recID); len(r.Tags) != 1 || r.Tags[0] != "手动" || r.Segments != 1 {
		t.Fatalf("after apply %+v", r)
	}
}

func TestRecordingDelete(t *testing.T) {
	e := goldenEngine(t, nil)
	var unindexed []string
	e.Unindex = func(kind string, ids []string) { unindexed = append(unindexed, ids...) }
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, goldenZone)
	_ = e.Store.PutProject(&Project{Slug: "p", Name: "P", Status: "active", Items: []string{"recordings/" + recID}})
	if _, err := e.PutRecording(recID, recUpload(1, now, false, "要删的"), now); err != nil {
		t.Fatal(err)
	}
	r, _ := e.Store.Recording(recID)
	r.Projects = []string{"p"}
	_ = e.Store.putEntity(KindRecording, recID, r, &r.Tagging, false)
	if ok, err := e.DeleteRecording(recID, now); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if ok, err := e.DeleteRecording(recID, now); ok || err != nil { // again: nothing there, no error
		t.Fatalf("delete again: %v %v", ok, err)
	}
	if _, err := os.Stat(e.Store.path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("files left: %v", err)
	}
	if len(unindexed) != 1 || unindexed[0] != recID {
		t.Fatalf("unindexed %v", unindexed)
	}
	if p, _ := e.Store.Project("p"); len(p.Items) != 0 {
		t.Fatalf("project still links it: %v", p.Items)
	}
	// A late upload (queued before the delete) does not bring it back.
	if _, err := e.PutRecording(recID, recUpload(2, now, true, "迟到"), now); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("late upload: %v, want ErrRecordingDeleted", err)
	}
	kept, deleted := e.RecordingStates()
	if len(kept) != 0 || len(deleted) != 1 || deleted[0] != recID {
		t.Fatalf("states %v %v", kept, deleted)
	}
	if res := e.Find("要删的", 10); len(res) != 0 {
		t.Fatalf("still found %+v", res)
	}
}

func TestRecordingRetention(t *testing.T) {
	e := goldenEngine(t, nil)
	days := 14
	e.RecordingDaysFunc = func() int { return days }
	var unindexed []string
	e.Unindex = func(kind string, ids []string) { unindexed = append(unindexed, ids...) }
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, goldenZone)
	old, recent := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	if _, err := e.PutRecording(old, recUpload(1, now.AddDate(0, 0, -10), true, "十天前"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PutRecording(recent, recUpload(1, now.AddDate(0, 0, -1), true, "昨天"), now); err != nil {
		t.Fatal(err)
	}
	// Past the retention: not stored at all.
	if st, _ := e.PutRecording("33333333-3333-4333-8333-333333333333", recUpload(1, now.AddDate(0, 0, -15), true, "太旧"), now); st.Status != RecordingExpired {
		t.Fatalf("an old upload: %v, want expired", st)
	}
	_, _ = e.DeleteRecording("44444444-4444-4444-8444-444444444444", now.AddDate(0, 0, -9))
	if n := e.PruneRecordings(now); n != 0 {
		t.Fatalf("pruned %d within 14 days", n)
	}
	days = 7 // the user shortened it
	if n := e.PruneRecordings(now); n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
	if left := e.Store.Recordings(); len(left) != 1 || left[0].ID != recent {
		t.Fatalf("left %v", left)
	}
	if len(unindexed) != 1 || unindexed[0] != old {
		t.Fatalf("unindexed %v", unindexed)
	}
	if _, deleted := e.RecordingStates(); len(deleted) != 0 {
		t.Fatalf("an old tombstone kept: %v", deleted)
	}
	if e.RecordingDaysFunc = nil; e.RecordingDays() != DefaultRecordingDays {
		t.Fatalf("default %d", e.RecordingDays())
	}
}

// Retention is elapsed 24-hour days, even when callers pass a time across a local DST change.
func TestRecordingRetentionDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{
		time.Date(2026, time.March, 9, 12, 0, 0, 0, loc),
		time.Date(2026, time.November, 2, 12, 0, 0, 0, loc),
	} {
		for _, days := range []int{7, DefaultRecordingDays} {
			for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
				t.Run(fmt.Sprintf("%s/days=%d/offset=%s", now.Format("2006-01-02"), days, offset), func(t *testing.T) {
					start := now.Add(-time.Duration(days) * 24 * time.Hour).Add(offset)
					expired := offset < 0
					t.Run("upload", func(t *testing.T) {
						e := goldenEngine(t, nil)
						e.RecordingDaysFunc = func() int { return days }
						want := RecordingStored
						if expired {
							want = RecordingExpired
						}
						if status, err := e.PutRecording(recID, recUpload(1, start, false, "boundary"), now); err != nil || status.Status != want {
							t.Fatalf("upload: %s, %v; want %s", status.Status, err, want)
						}
						if _, kept := e.Store.Recording(recID); kept == expired {
							t.Fatalf("recording kept = %v, expired = %v", kept, expired)
						}
					})
					t.Run("prune", func(t *testing.T) {
						e := goldenEngine(t, nil)
						e.RecordingDaysFunc = func() int { return days }
						// Seed while fresh so upload expiry cannot hide a pruning bug.
						if status, err := e.PutRecording(recID, recUpload(1, start, false, "boundary"), start); err != nil || status.Status != RecordingStored {
							t.Fatalf("seed: %s, %v", status.Status, err)
						}
						const deletedID = "44444444-4444-4444-8444-444444444444"
						if _, err := e.DeleteRecording(deletedID, start); err != nil {
							t.Fatal(err)
						}
						want := 0
						if expired {
							want = 1
						}
						if n := e.PruneRecordings(now); n != want {
							t.Errorf("pruned %d, want %d", n, want)
						}
						if _, kept := e.Store.Recording(recID); kept == expired {
							t.Errorf("recording kept = %v, expired = %v", kept, expired)
						}
						if _, kept := e.Store.tombstones()[deletedID]; kept == expired {
							t.Errorf("tombstone kept = %v, expired = %v", kept, expired)
						}
					})
				})
			}
		}
	}
}
