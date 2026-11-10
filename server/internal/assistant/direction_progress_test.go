package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// A direction's name and the leader's own progress (#3679). All ids and names are placeholders.

func TestDirectionNameOrder(t *testing.T) {
	for _, c := range []struct {
		name         string
		d            *direction
		title, pname string
		want         string
	}{
		{"given name wins", &direction{Name: "Infra", Project: "infra"}, "1110 | 探索 | x", "Infrastructure", "Infra"},
		{"then the project's name", &direction{Project: "infra"}, "1110 | 探索 | x", "Infrastructure", "Infrastructure"},
		{"then the title without its prefix", &direction{}, "1110 | 探索 | Android parity", "", "Android parity"},
		{"a title without a prefix", &direction{}, "Android parity", "", "Android parity"},
		{"the role's wrapping, cut off", &direction{}, "1110 | 探索 | 你是「基建」这条线的主管（l", "", "基建"},
		{"the role's wrapping, no prefix", &direction{}, "你是「Skill 群」的主管", "", "Skill 群"},
		{"brackets elsewhere stay", &direction{}, "1110 | 功能 | 修「设置」页", "", "修「设置」页"},
		{"an empty role stays as it is", &direction{}, "1110 | 探索 | 你是「」的主管", "", "你是「」的主管"},
		{"only a prefix: the title", &direction{}, "1110 | 探索 | ", "", "1110 | 探索 | "},
	} {
		if got := directionName(c.d, c.title, c.pname); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func (e *todoEventEnv) changedNames() []assistantwire.LeaderChangedEvent { return e.syncLeaders() }

// The name is told with leader.changed, once per change; the owner's PUT and the secretary's tool set it; clearing
// shows the fallback again; GET leaders lists it.
func TestDirectionNameIsToldOnce(t *testing.T) {
	e := directionEnv(t, &memStore{})
	setSettings(t, e.s, func(v *Settings) { v.Main = "conv_main" })
	e.title("conv_lead_a", "1110 | 探索 | 你是「基建」这条线的主管（l")
	e.leaders("conv_lead_a")
	if got := e.changedNames(); len(got) != 1 || got[0].Name != "基建" {
		t.Fatalf("made: %+v", got)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/assistant/directions/conv_lead_a/name", strings.NewReader(`{"name":"基建"}`))
	e.s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT name: %d %s", rec.Code, rec.Body)
	}
	if err := e.s.setDirectionName(context.Background(), "conv_lead_a", "", directionByUser); err != nil {
		t.Fatal(err)
	}
	if got := e.changedNames(); len(got) != 0 {
		t.Fatalf("the same name as the fallback was told again: %+v", got)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/assistant/directions/conv_lead_a/name", strings.NewReader(`{"name":"基建平台组"}`))
	e.s.ServeHTTP(rec, req)
	if got := e.changedNames(); len(got) != 1 || got[0].Name != "基建平台组" || got[0].Title != "1110 | 探索 | 你是「基建」这条线的主管（l" {
		t.Fatalf("named: %+v", got)
	}
	if got := e.changedNames(); len(got) != 0 {
		t.Fatalf("told again: %+v", got)
	}
	rec = httptest.NewRecorder()
	ro := httptest.NewRequest(http.MethodPut, "/api/v1/assistant/directions/conv_lead_a/name", strings.NewReader(`{"name":"x"}`))
	ro.Header.Set(RoleHeader, "readonly")
	if e.s.ServeHTTP(rec, ro); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only PUT: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if e.s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant/directions/nope/name", strings.NewReader(`{"name":"x"}`))); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown direction: %d", rec.Code)
	}

	// The secretary names it; a side conversation may not.
	if _, err := runJSON(t, e.s, "conv_other", "direction_name", map[string]any{"id": "conv_lead_a", "name": "Y"}); err == nil {
		t.Fatal("a side conversation named a direction")
	}
	if res, err := runJSON(t, e.s, "conv_main", "direction_name", map[string]any{"id": "conv_lead_a", "name": "基建平台"}); err != nil || !strings.Contains(res, "基建平台") {
		t.Fatalf("the secretary's name: %s %v", res, err)
	}
	if got := e.changedNames(); len(got) != 1 || got[0].Name != "基建平台" {
		t.Fatalf("renamed: %+v", got)
	}
	if d := e.s.directionOf("conv_lead_a"); d.NameBy != directionByAssistant {
		t.Fatalf("by: %+v", d)
	}
	// Cleared: the fallback again.
	if _, err := runJSON(t, e.s, "conv_main", "direction_name", map[string]any{"id": "conv_lead_a", "name": ""}); err != nil {
		t.Fatal(err)
	}
	if got := e.changedNames(); len(got) != 1 || got[0].Name != "基建" {
		t.Fatalf("cleared: %+v", got)
	}

	rec = httptest.NewRecorder()
	e.s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/leaders", nil))
	var out struct {
		Leaders []leaderView `json:"leaders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Leaders) != 1 || out.Leaders[0].Name != "基建" {
		t.Fatalf("listed: %s", rec.Body)
	}
}

// A leader reports its direction's progress: told as leader.report kind progress with its parts and links, kept as
// the latest on the direction; nobody else may, and an empty one is refused.
func TestLeaderProgress(t *testing.T) {
	e := directionEnv(t, &memStore{})
	setSettings(t, e.s, func(v *Settings) { v.Main = "conv_main" })
	e.leaders("conv_lead_a")
	e.syncLeaders()
	if _, err := runJSON(t, e.s, "conv_main", "leader_progress", map[string]any{"reached": "x"}); err == nil {
		t.Fatal("the main conversation reported a direction's progress")
	}
	if _, err := runJSON(t, e.s, "conv_lead_a", "leader_progress", map[string]any{"links": []string{"https://example.com/pull/1"}}); err == nil {
		t.Fatal("an empty progress was taken")
	}
	res, err := runJSON(t, e.s, "conv_lead_a", "leader_progress", map[string]any{
		"reached": "侧栏分组上线并实测", "doing": "平板布局，卡在模拟器排队", "needs_you": "确认平板要不要双栏",
		"links": []string{"https://example.com/pull/7", "https://example.com/pull/7", " https://example.com/doc "}})
	if err != nil || !strings.Contains(res, "Reported as the direction") {
		t.Fatalf("progress: %s %v", res, err)
	}
	var got []assistantwire.LeaderReportEvent
	for _, ev := range e.told() {
		if ev.name == assistantwire.EventLeaderReport {
			var r assistantwire.LeaderReportEvent
			_ = json.Unmarshal(ev.raw, &r)
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("reports: %+v", got)
	}
	r := got[0]
	if r.Kind != assistantwire.LeaderProgress || r.Leader != "conv_lead_a" || r.Todo != "" || r.Reached != "侧栏分组上线并实测" ||
		r.Doing != "平板布局，卡在模拟器排队" || r.NeedsYou != "确认平板要不要双栏" || r.Link != "https://example.com/pull/7" ||
		!slices.Equal(r.Links, []string{"https://example.com/pull/7", "https://example.com/doc"}) || !strings.HasPrefix(r.ID, "lrep_") {
		t.Fatalf("report: %+v", r)
	}
	for _, want := range []string{"侧栏分组上线并实测", "卡在模拟器排队", "确认平板要不要双栏"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("text lacks %q: %q", want, r.Text)
		}
	}
	if d := e.s.directionOf("conv_lead_a"); d == nil || d.Progress == nil || d.Progress.Reached != "侧栏分组上线并实测" {
		t.Fatalf("kept: %+v", d)
	}
	_ = context.Background()
}
