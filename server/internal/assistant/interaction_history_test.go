package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// GET /api/v1/interactions/history (interaction_history.go): paged back by time with since / before / kind, and an
// extension's token never shown a private card or one of the health space.

var historyDay = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

type historyRow struct {
	Interaction  proto.Interaction `json:"interaction"`
	Status       string            `json:"status"`
	Resolution   *proto.Resolution `json:"resolution"`
	Reanswerable bool              `json:"reanswerable"`
}

type historyAnswer struct {
	Interactions []historyRow `json:"interactions"`
	NextBefore   string       `json:"next_before"`
	NextBeforeID string       `json:"next_before_id"`
}

func (a historyAnswer) ids() string {
	var out []string
	for _, r := range a.Interactions {
		out = append(out, r.Interaction.ID)
	}
	return strings.Join(out, ",")
}

func getHistory(t *testing.T, ft *fleetTest, query url.Values, role string) (int, historyAnswer) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/interactions/history?"+query.Encode(), nil)
	if role != "" {
		r.Header.Set(RoleHeader, role)
		r.Header.Set(ExtensionHeader, "example-reader")
	}
	w := httptest.NewRecorder()
	ft.s.serveInteractionHistory(w, r)
	var a historyAnswer
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, a
}

func keepCard(ft *fleetTest, ix proto.Interaction, status string, res *proto.Resolution) {
	ft.st.fmu.Lock()
	defer ft.st.fmu.Unlock()
	ft.st.recs = append(ft.st.recs, db.InteractionRecord{Interaction: ix, Status: status, Resolution: res})
}

func historyCard(id, session, cwd string, at time.Time) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: session, Cwd: cwd, Kind: proto.IxQuestion, CreatedAt: at,
		Questions: []proto.Question{{Question: "Which one?", Options: []proto.QuestionOption{{Label: "A"}, {Label: "B"}}}}}
}

func TestInteractionHistoryPagesByTime(t *testing.T) {
	ft := newFleetTest(t)
	ft.sessions("mac", proto.Session{MachineID: "mac", SessionID: "s1", Cwd: "/work/project"})
	for i, id := range []string{"q1", "q2", "q3", "q4", "q5"} {
		keepCard(ft, historyCard(id, "s1", "/work/project", historyDay.Add(time.Duration(i)*time.Minute)), "resolved", &proto.Resolution{Answers: map[string]string{"Which one?": "A"}})
	}
	p := permission("p1")
	p.CreatedAt = historyDay.Add(10 * time.Minute)
	keepCard(ft, p, "resolved", nil)
	// q2b shares q2's time: the cursor's id keeps it from being skipped or read twice.
	keepCard(ft, historyCard("q2b", "s1", "/work/project", historyDay.Add(time.Minute)), "pending", nil)

	var pages []string
	q := url.Values{"since": {historyDay.Format(time.RFC3339)}, "kind": {proto.IxQuestion}, "limit": {"2"}}
	for range 10 {
		code, a := getHistory(t, ft, q, "")
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		pages = append(pages, a.ids())
		if a.NextBefore == "" {
			break
		}
		q.Set("before", a.NextBefore)
		q.Set("before_id", a.NextBeforeID)
	}
	if got := strings.Join(pages, " | "); got != "q5,q4 | q3,q2b | q2,q1 | " {
		t.Fatalf("pages %q", got)
	}

	// since alone leaves the older ones out; no kind takes every kind.
	code, a := getHistory(t, ft, url.Values{"since": {historyDay.Add(4 * time.Minute).Format(time.RFC3339)}}, "")
	if code != http.StatusOK || a.ids() != "p1,q5" || a.NextBefore != "" {
		t.Fatalf("since: %d %s %q", code, a.ids(), a.NextBefore)
	}
	for _, bad := range []url.Values{{"since": {"yesterday"}}, {"before": {"2026-03-01"}}, {"since": {historyDay.Format(time.RFC3339)}, "before_id": {"q1"}}} {
		if code, _ := getHistory(t, ft, bad, ""); code != http.StatusBadRequest {
			t.Errorf("%v: status %d", bad, code)
		}
	}
	// Without since, before or kind: the newest as before, without a cursor.
	if code, a := getHistory(t, ft, url.Values{}, ""); code != http.StatusOK || len(a.Interactions) != 7 || a.NextBefore != "" {
		t.Fatalf("plain: %d %d %q", code, len(a.Interactions), a.NextBefore)
	}
}

func TestInteractionHistoryHidesPrivateAndHealthFromExtensions(t *testing.T) {
	ft := newFleetTest(t)
	ft.sessions("mac",
		proto.Session{MachineID: "mac", SessionID: "s1", Cwd: "/work/project"},
		proto.Session{MachineID: "mac", SessionID: "h1", Cwd: "/home/me/.persona/health", Assistant: true, Space: SpaceHealth},
		proto.Session{MachineID: "mac", SessionID: "h2", Cwd: "/home/me/.persona/health", Assistant: true}, // the space from its directory
	)
	at := func(m int) time.Time { return historyDay.Add(time.Duration(m) * time.Minute) }
	keepCard(ft, historyCard("plain", "s1", "/work/project", at(1)), "resolved", &proto.Resolution{Answers: map[string]string{"Which one?": "B"}})
	private := historyCard("private", "s1", "/work/project", at(2))
	private.Private = true
	keepCard(ft, private, "resolved", nil)
	keepCard(ft, historyCard("health-cwd", "s1", "/home/me/.persona/health/reports", at(3)), "pending", nil)
	keepCard(ft, historyCard("health-session", "h1", "", at(4)), "pending", nil)
	keepCard(ft, historyCard("health-dir-session", "h2", "", at(5)), "pending", nil)
	keepCard(ft, historyCard("unknown-session", "gone", "", at(6)), "pending", nil)
	delegated := historyCard("delegated", "s1", "/work/project", at(7))
	keepCard(ft, delegated, "resolved", &proto.Resolution{Answers: map[string]string{"Which one?": "A"}, AnsweredBy: proto.AnsweredByAssistant, Reason: "usual pick"})
	form := permission("form")
	form.Cwd, form.CreatedAt = "/work/project", at(8)
	keepCard(ft, form, "resolved", &proto.Resolution{Allow: new(true), Values: map[string]string{"password": "example-secret"}})

	all := "form,delegated,unknown-session,health-dir-session,health-session,health-cwd,private,plain"
	// The plain history is in the store's order (the test store keeps them as added); the paged one newest first.
	byTime := func(a historyAnswer) historyAnswer {
		slices.SortFunc(a.Interactions, func(x, y historyRow) int { return y.Interaction.CreatedAt.Compare(x.Interaction.CreatedAt) })
		return a
	}
	for _, q := range []url.Values{{}, {"since": {historyDay.Format(time.RFC3339)}}} {
		code, a := getHistory(t, ft, q, "")
		if a = byTime(a); code != http.StatusOK || a.ids() != all {
			t.Fatalf("full credentials %v: %d %s", q, code, a.ids())
		}
		if a.Interactions[0].Resolution.Values["password"] != "example-secret" {
			t.Fatalf("full credentials lost the form's values")
		}
		code, a = getHistory(t, ft, q, roleExtension)
		if a = byTime(a); code != http.StatusOK || a.ids() != "form,delegated,plain" {
			t.Fatalf("extension %v: %d %s", q, code, a.ids())
		}
		if res := a.Interactions[0].Resolution; res == nil || res.Values != nil || res.Allow == nil || !*res.Allow {
			t.Fatalf("extension: the form's resolution %+v", res)
		}
		if d := a.Interactions[1]; !d.Reanswerable || d.Resolution.AnsweredBy != proto.AnsweredByAssistant || d.Interaction.Questions[0].Options[1].Label != "B" {
			t.Fatalf("delegated %+v", d)
		}
		if a.Interactions[0].Reanswerable || a.Interactions[2].Reanswerable {
			t.Fatal("only an answer the assistant gave can be changed")
		}
	}
}

// The decision fields (deadline, reversible, recommend) and who answered (late, answered_by default) reach an
// extension's token on the cards it may see, and change nothing of what it may not: a private card, a health card
// with the same fields, and the values filled into a card's fields stay out.
func TestInteractionHistoryDecisionFieldsForExtensions(t *testing.T) {
	ft := newFleetTest(t)
	ft.sessions("mac", proto.Session{MachineID: "mac", SessionID: "s1", Cwd: "/work/project"},
		proto.Session{MachineID: "mac", SessionID: "h1", Cwd: "/home/me/.persona/health", Assistant: true, Space: SpaceHealth})
	at := func(m int) time.Time { return historyDay.Add(time.Duration(m) * time.Minute) }
	deadline, yes, no := at(60), true, false
	decided := func(id, session, cwd string, m int, reversible *bool) proto.Interaction {
		ix := historyCard(id, session, cwd, at(m))
		ix.ToolName, ix.Deadline, ix.Reversible = "ask_user", &deadline, reversible
		ix.Questions[0].Recommend = "A"
		return ix
	}
	keepCard(ft, decided("taken", "s1", "/work/project", 1, &yes), "expired",
		&proto.Resolution{Answers: map[string]string{"Which one?": "A"}, AnsweredBy: proto.AnsweredByDefault, Late: true, Reason: "deadline"})
	keepCard(ft, decided("later", "s1", "/work/project", 2, &no), "expired",
		&proto.Resolution{Answers: map[string]string{"Which one?": "B"}, Late: true})
	keepCard(ft, historyCard("plain", "s1", "/work/project", at(3)), "resolved", &proto.Resolution{Answers: map[string]string{"Which one?": "B"}})
	private := decided("private", "s1", "/work/project", 4, &yes)
	private.Private = true
	keepCard(ft, private, "expired", &proto.Resolution{Answers: map[string]string{"Which one?": "A"}, AnsweredBy: proto.AnsweredByDefault, Late: true})
	keepCard(ft, decided("health", "h1", "", 5, &yes), "expired",
		&proto.Resolution{Answers: map[string]string{"Which one?": "A"}, AnsweredBy: proto.AnsweredByDefault, Late: true})
	form := permission("form")
	form.Cwd, form.CreatedAt = "/work/project", at(6)
	keepCard(ft, form, "resolved", &proto.Resolution{Allow: new(true), Values: map[string]string{"password": "example-secret"}})

	code, a := getHistory(t, ft, url.Values{"since": {historyDay.Format(time.RFC3339)}}, roleExtension)
	if code != http.StatusOK || a.ids() != "form,plain,later,taken" {
		t.Fatalf("extension: %d %s", code, a.ids())
	}
	byID := map[string]historyRow{}
	for _, r := range a.Interactions {
		byID[r.Interaction.ID] = r
	}
	taken, later, plain := byID["taken"], byID["later"], byID["plain"]
	if ix := taken.Interaction; ix.Deadline == nil || !ix.Deadline.Equal(deadline) || ix.Reversible == nil || !*ix.Reversible || ix.Questions[0].Recommend != "A" {
		t.Fatalf("taken: %+v", ix)
	}
	if r := taken.Resolution; taken.Status != "expired" || !r.Late || r.AnsweredBy != proto.AnsweredByDefault || r.Answers["Which one?"] != "A" {
		t.Fatalf("taken by default: %s %+v", taken.Status, r)
	}
	if r := later.Resolution; !r.Late || r.AnsweredBy != "" || later.Interaction.Reversible == nil || *later.Interaction.Reversible {
		t.Fatalf("answered later by the user: %+v %+v", r, later.Interaction.Reversible)
	}
	if ix := plain.Interaction; ix.Deadline != nil || ix.Reversible != nil || ix.Questions[0].Recommend != "" || plain.Resolution.Late {
		t.Fatalf("a card without the fields: %+v", ix)
	}
	if r := byID["form"].Resolution; r == nil || r.Values != nil {
		t.Fatalf("the form's values reached the extension: %+v", r)
	}
}
