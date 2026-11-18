package extmgr

import (
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestBotSourceReportDelaySetting(t *testing.T) {
	w := newBotWorld(t)
	const key = "source_report_delay_minutes"
	const path = "/api/v1/bots/feishu"
	read := func(want float64) {
		t.Helper()
		_, view := w.do(t, "GET", path, "")
		status := w.feishu(t, assistantwire.FeishuStatusGet, nil)
		if view[key] != want || status[key] != want {
			t.Fatalf("GET = %v, wire = %v, want %v", view[key], status[key], want)
		}
	}
	read(10)
	for _, minutes := range []int{1, 60, 0} {
		body := fmt.Sprintf(`{"source_report_delay_minutes":%d}`, minutes)
		if code, _ := w.do(t, "PUT", path, body); code != 200 {
			t.Fatalf("PUT %s: %d", body, code)
		}
		read(float64(minutes))
	}
	if code, _ := w.do(t, "PUT", path, `{"features":{"main":true}}`); code != 200 {
		t.Fatalf("PUT without delay: %d", code)
	}
	read(0)
	var persisted FeishuBotSettings
	if ok, err := w.h.store.Setting(t.Context(), botSettingsKey, &persisted); err != nil || !ok || persisted.SourceReportDelayMinutes == nil || *persisted.SourceReportDelayMinutes != 0 {
		t.Fatalf("explicit zero not saved: %+v, %v", persisted.SourceReportDelayMinutes, err)
	}
	for _, bad := range []string{"-1", "61", "1.5", `"5"`} {
		body := fmt.Sprintf(`{"source_report_delay_minutes":%s,"enabled":false}`, bad)
		if code, _ := w.do(t, "PUT", path, body); code != 400 {
			t.Errorf("PUT %s: %d, want 400", body, code)
		}
	}
	read(0)
	if _, view := w.do(t, "GET", path, ""); view["enabled"] != true {
		t.Fatal("an invalid request disabled the bot")
	}
	if code, _ := w.do(t, "PUT", path, `{"source_report_delay_minutes":10}`); code != 200 {
		t.Fatalf("restore default: %d", code)
	}
	read(10)
}

func TestBotSourceReportFullReplySetting(t *testing.T) {
	w := newBotWorld(t)
	const path = "/api/v1/bots/feishu"
	const setting = "source_report_full_reply"
	_, view := w.do(t, "GET", path, "")
	if view[setting] != false {
		t.Fatalf("default full reply: %v", view[setting])
	}
	if code, view := w.do(t, "PUT", path, `{"source_report_full_reply":true,"source_report_delay_minutes":0}`); code != 200 || view[setting] != true || view["source_report_delay_minutes"] != float64(0) {
		t.Fatalf("could not save both settings: %d %v", code, view)
	}
	if status := w.feishu(t, assistantwire.FeishuStatusGet, nil); status[setting] != true {
		t.Fatalf("full reply was not carried to the assistant: %v", status)
	}
	if code, _ := w.do(t, "PUT", path, `{"source_report_full_reply":false,"source_report_delay_minutes":61}`); code != 400 || !w.settings().SourceReportFullReply {
		t.Fatalf("an invalid delay applied its full reply change: %d", code)
	}
	if code, view := w.do(t, "PUT", path, `{"source_report_full_reply":false}`); code != 200 || view[setting] != false || view["source_report_delay_minutes"] != float64(0) {
		t.Fatalf("could not turn off full replies: %d %v", code, view)
	}
}
