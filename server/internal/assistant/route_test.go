package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// assistant.route (route.go, #3607): the rule table, the silent quick chat on a timeout, and the owner's taps. All ids,
// names and sentences are placeholders.

func TestRouteRuleTable(t *testing.T) {
	projects := map[string]string{"android parity": "android-parity", "Infra": "infra"}
	find := func(name string) (string, bool) {
		slug, ok := projects[name]
		return slug, ok
	}
	few := []routeLeader{{ID: "lead_a", Title: "Infra", Project: "infra"}}
	many := []routeLeader{{ID: "lead_a", Project: "infra"}, {ID: "lead_b"}, {ID: "lead_c"}, {ID: "lead_d"}, {ID: "lead_e"},
		{ID: "lead_f", Project: "android-parity"}}
	long := strings.Repeat("长", 40)
	for _, c := range []struct {
		name     string
		sentence string
		answer   routeAnswer
		err      error
		leaders  []routeLeader
		want     *routeOffer
	}{
		{name: "timeout is a quick chat", sentence: "Android parity", err: context.DeadlineExceeded},
		{name: "model error is a quick chat", sentence: "Android parity", err: errors.New("503")},
		{name: "quick", sentence: "what's the weather", answer: routeAnswer{Kind: "quick"}},
		{name: "unknown kind is a quick chat", sentence: "x", answer: routeAnswer{Kind: "maybe", Title: "X"}},
		{name: "empty kind is a quick chat", sentence: "x", answer: routeAnswer{Title: "X"}},
		{name: "direction, existing project", sentence: "Bring Android up to the phone", leaders: few,
			answer: routeAnswer{Kind: "direction", Title: "Android parity", Project: "android parity"},
			want:   &routeOffer{Title: "Android parity", Project: "android-parity"}},
		{name: "direction, new project", sentence: "Plan the launch", leaders: few,
			answer: routeAnswer{Kind: " Direction ", Title: "Launch", Project: "Launch 2027"},
			want:   &routeOffer{Title: "Launch", NewProject: "Launch 2027"}},
		{name: "direction, no project", sentence: "Plan the launch", answer: routeAnswer{Kind: "direction", Title: "Launch"},
			want: &routeOffer{Title: "Launch"}},
		{name: "no title: the sentence, clipped", sentence: long, answer: routeAnswer{Kind: "direction"},
			want: &routeOffer{Title: clip(long, routeTitleMax)}},
		{name: "long title clipped", sentence: "x", answer: routeAnswer{Kind: "direction", Title: long},
			want: &routeOffer{Title: clip(long, routeTitleMax)}},
		{name: "few leaders: no closest even when named", sentence: "x", leaders: few,
			answer: routeAnswer{Kind: "direction", Title: "X", Closest: "lead_a"}, want: &routeOffer{Title: "X"}},
		{name: "five leaders: no closest yet", sentence: "x", leaders: many[:5],
			answer: routeAnswer{Kind: "direction", Title: "X", Closest: "lead_c"}, want: &routeOffer{Title: "X"}},
		{name: "past six leaders: the closest the model named", sentence: "x", leaders: many,
			answer: routeAnswer{Kind: "direction", Title: "X", Closest: "lead_c"}, want: &routeOffer{Title: "X", Closest: "lead_c"}},
		{name: "past six leaders: a made-up closest falls back to the project's leader", sentence: "x", leaders: many,
			answer: routeAnswer{Kind: "direction", Title: "X", Project: "android parity", Closest: "lead_zz"},
			want:   &routeOffer{Title: "X", Project: "android-parity", Closest: "lead_f"}},
		{name: "past six leaders: none close", sentence: "x", leaders: many,
			answer: routeAnswer{Kind: "direction", Title: "X", Project: "New thing"}, want: &routeOffer{Title: "X", NewProject: "New thing"}},
	} {
		got, ok := decideRoute(c.sentence, c.answer, c.err, c.leaders, find)
		switch {
		case c.want == nil && ok:
			t.Errorf("%s: offered %+v, want a quick chat", c.name, got)
		case c.want != nil && (!ok || got != *c.want):
			t.Errorf("%s: got %+v (%v), want %+v", c.name, got, ok, *c.want)
		}
	}
}

// directionRouteEnv is the assistant with a database, its main conversation conv1 and side conversations on its machine.
func directionRouteEnv(t *testing.T, sides ...string) (*Service, *fakeHub) {
	t.Helper()
	s, f := newQuietServiceOn(t, &memStore{})
	list := []proto.Session{{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}}
	for _, id := range sides {
		f.known[id] = true
		list = append(list, proto.Session{SessionID: id, MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	}
	f.setSessions("server", list...)
	return s, f
}

func (s *Service) offerOf(id string) *routeOffer {
	b := &s.offers
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.routeOffersLocked(context.Background()) || b.m[id] == nil {
		return nil
	}
	cp := *b.m[id]
	return &cp
}

func countEvents(f *fakeHub, typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e == typ {
			n++
		}
	}
	return n
}

// A model that does not answer within routeTimeout leaves a quick chat and no trace: no offer, no event, nothing sent
// into the conversation; the call is not waited on past the timeout.
func TestRouteTimeoutIsAQuietQuickChat(t *testing.T) {
	s, f := directionRouteEnv(t, "side1")
	old := routeTimeout
	routeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { routeTimeout = old })
	s.offers.ask = func(ctx context.Context, _, _ string, out any) error {
		<-ctx.Done() // a model that is slower than the budget
		return ctx.Err()
	}
	sends := len(f.noticesTo("side1"))
	start := time.Now()
	s.routeNewChat("server", "side1", "Bring the Android app up to the phone's features")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v past the timeout", took)
	}
	if o := s.offerOf("side1"); o != nil {
		t.Fatalf("offered after a timeout: %+v", o)
	}
	if n := countEvents(f, "direction_offer"); n != 0 {
		t.Fatalf("%d direction_offer events after a timeout", n)
	}
	if got := f.noticesTo("side1"); len(got) != sends {
		t.Fatalf("something was sent into the conversation: %q", got)
	}
}

// A direction answer is kept and told; a quick answer, the main conversation, a leader already and a conversation on
// another machine are not offered anything (the last three are not even asked).
func TestRouteNewChatOffersADirection(t *testing.T) {
	s, f := directionRouteEnv(t, "side1", "side2", "lead1")
	setSettings(t, s, func(v *Settings) { v.Leaders = []string{"lead1"} })
	var asked []string
	s.offers.ask = func(_ context.Context, system, input string, out any) error {
		asked = append(asked, input)
		a := routeAnswer{Kind: "quick"}
		if strings.Contains(input, "Android") {
			a = routeAnswer{Kind: "direction", Title: "Android parity", Project: "Android parity"}
		}
		raw, _ := json.Marshal(a)
		return json.Unmarshal(raw, out)
	}
	s.routeNewChat("server", "side1", "Bring the Android app up to the phone's features")
	s.routeNewChat("server", "side2", "What's on my calendar today?")
	s.routeNewChat("server", "conv1", "Android again")
	s.routeNewChat("server", "lead1", "Android again")
	s.routeNewChat("other", "side3", "Android again")
	if len(asked) != 2 {
		t.Fatalf("asked %d times: %q", len(asked), asked)
	}
	if !strings.Contains(asked[0], "lead1") {
		t.Fatalf("the model was not shown the leaders:\n%s", asked[0])
	}
	o := s.offerOf("side1")
	if o == nil || o.Title != "Android parity" || o.NewProject != "Android parity" || o.MachineID != "server" {
		t.Fatalf("offer: %+v", o)
	}
	if s.offerOf("side2") != nil {
		t.Fatal("a quick chat was offered a direction")
	}
	if n := countEvents(f, "direction_offer"); n != 1 {
		t.Fatalf("%d direction_offer events", n)
	}
}

// The owner made the chat a direction while the model was answering: no offer is kept or told afterwards.
func TestRouteOfferNotKeptForADirectionMadeMeanwhile(t *testing.T) {
	s, f := directionRouteEnv(t, "side1")
	s.offers.ask = func(_ context.Context, _, _ string, out any) error {
		if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/make", "", ""); code != 200 {
			t.Errorf("make meanwhile: %d", code)
		}
		return json.Unmarshal([]byte(`{"kind":"direction","title":"Android parity"}`), out)
	}
	s.routeNewChat("server", "side1", "Bring the Android app up to the phone's features")
	if o := s.offerOf("side1"); o != nil {
		t.Fatalf("offered a direction that is one already: %+v", o)
	}
	if n := countEvents(f, "direction_offer"); n != 0 {
		t.Fatalf("%d direction_offer events", n)
	}
}

func doRoute(t *testing.T, s *Service, method, path, body string, role string) (int, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if role != "" {
		req.Header.Set(RoleHeader, role)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The owner's taps: Start direction takes the offer, Make it a direction a quick chat, End direction takes it out; each
// is recorded as the owner's, with no phone approval; Just a quick chat drops the offer.
func TestDirectionEndpoints(t *testing.T) {
	s, f := directionRouteEnv(t, "side1", "side2", "side3")
	s.offers.ask = func(_ context.Context, _, _ string, out any) error {
		return json.Unmarshal([]byte(`{"kind":"direction","title":"Android parity","project":""}`), out)
	}
	s.routeNewChat("server", "side1", "Bring the Android app up to the phone's features")
	s.routeNewChat("server", "side3", "Plan the launch")

	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/start", "", "readonly"); code != http.StatusForbidden {
		t.Fatalf("a read-only device started a direction: %d", code)
	}
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side2/start", "", ""); code != http.StatusNotFound {
		t.Fatalf("start without an offer: %d", code)
	}
	code, out := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/start", "", "")
	if code != 200 {
		t.Fatalf("start: %d %s", code, out["error"])
	}
	if cfg := s.settings(context.Background()); !slices.Equal(cfg.Leaders, []string{"side1"}) {
		t.Fatalf("leaders after start: %q", cfg.Leaders)
	}
	if d := s.directionOf("side1"); d == nil || d.By != directionByUser || d.EndedAt != nil {
		t.Fatalf("record after start: %+v", d)
	}
	if s.offerOf("side1") != nil {
		t.Fatal("the offer stayed after start")
	}
	if by, _ := s.titleOf("side1"); by != titleByAssistant {
		t.Fatalf("the direction's name was not given: by %q", by)
	}
	if len(f.pushes) != 0 {
		t.Fatalf("a phone approval was asked: %+v", f.pushes)
	}

	// Just a quick chat: the offer goes, nothing else changes.
	if code, out := doRoute(t, s, http.MethodDelete, "/api/v1/assistant/directions/offers/side3", "", ""); code != 200 || string(out["dropped"]) != "true" {
		t.Fatalf("drop: %d %s", code, out["dropped"])
	}
	if s.offerOf("side3") != nil || slices.Contains(s.settings(context.Background()).Leaders, "side3") {
		t.Fatal("the quick chat is still offered or became a direction")
	}

	// Make it a direction: a quick chat, no offer needed.
	if code, out := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side2/make", "", ""); code != 200 {
		t.Fatalf("make: %d %s", code, out["error"])
	}
	if cfg := s.settings(context.Background()); !slices.Equal(cfg.Leaders, []string{"side1", "side2"}) {
		t.Fatalf("leaders after make: %q", cfg.Leaders)
	}
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/conv1/make", "", ""); code != http.StatusConflict {
		t.Fatalf("the main conversation was made a direction: %d", code)
	}
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/nope/make", "", ""); code != http.StatusConflict {
		t.Fatalf("an unknown conversation was made a direction: %d", code)
	}

	// End direction: out of the leaders, the record stays and is ended once the leaders are compared.
	code, out = doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/end", "", "")
	if code != 200 || string(out["ended"]) != "true" {
		t.Fatalf("end: %d %s", code, out["ended"])
	}
	if cfg := s.settings(context.Background()); !slices.Equal(cfg.Leaders, []string{"side2"}) {
		t.Fatalf("leaders after end: %q", cfg.Leaders)
	}
	s.directionsSync(context.Background())
	if d := s.directionOf("side1"); d == nil || d.EndedAt == nil || d.By != directionByUser {
		t.Fatalf("record after end: %+v", d)
	}
	if code, out := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/end", "", ""); code != 200 || string(out["ended"]) != "false" {
		t.Fatalf("ending again: %d %s", code, out["ended"])
	}
	// Made a direction again: no longer ended.
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/make", "", ""); code != 200 {
		t.Fatalf("make again: %d", code)
	}
	if d := s.directionOf("side1"); d == nil || d.EndedAt != nil {
		t.Fatalf("record after make again: %+v", d)
	}
}

// The 20-leader limit holds for the owner's taps too.
func TestDirectionLimit(t *testing.T) {
	var ids []string
	for i := range leadersMost + 1 {
		ids = append(ids, fmt.Sprintf("side%02d", i))
	}
	s, _ := directionRouteEnv(t, ids...)
	setSettings(t, s, func(v *Settings) { v.Leaders = ids[:leadersMost] })
	if code, out := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/"+ids[leadersMost]+"/make", "", ""); code != http.StatusConflict ||
		!strings.Contains(string(out["error"]), "at most 20") {
		t.Fatalf("the 21st direction: %d %s", code, out["error"])
	}
	// One already a leader is fine.
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/"+ids[0]+"/make", "", ""); code != 200 {
		t.Fatalf("an existing leader again: %d", code)
	}
}

func TestGoldenRoute(t *testing.T) {
	golden(t, "route", prompts.RenderRoute(prompts.Route{TitleMax: routeTitleMax, Leaders: 1}))
	golden(t, "route.crowded.en", prompts.RenderRoute(prompts.Route{Language: "English", TitleMax: routeTitleMax, Crowded: true, Leaders: 6}))
	golden(t, "route_input", prompts.RenderRouteInput(prompts.RouteInput{Sentence: "Bring the Android app up to the phone's features",
		Leaders:  []prompts.RouteLeader{{ID: "lead_a", Title: "Infra", Project: "infra"}, {ID: "lead_b", Title: "Docs"}},
		Projects: []string{"Example app", "Infra"}}))
	golden(t, "route_input.empty", prompts.RenderRouteInput(prompts.RouteInput{Sentence: "What's on my calendar today?"}))
}

// A direction made again keeps the name it was given; Start's offered name never replaces one given before.
func TestDirectionMadeAgainKeepsItsName(t *testing.T) {
	s, _ := directionRouteEnv(t, "side1")
	if code, _ := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/make", `{"title":"Infra"}`, ""); code != 200 {
		t.Fatalf("make: %d", code)
	}
	if err := s.setDirectionName(context.Background(), "side1", "基建", directionByUser); err != nil {
		t.Fatal(err)
	}
	doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/end", "", "")
	s.directionsSync(context.Background())
	code, out := doRoute(t, s, http.MethodPost, "/api/v1/assistant/directions/side1/make", `{"title":"Other"}`, "")
	if code != 200 || !strings.Contains(string(out["leader"]), `"name":"基建"`) {
		t.Fatalf("made again: %d %s", code, out["leader"])
	}
	if d := s.directionOf("side1"); d == nil || d.Name != "基建" || d.EndedAt != nil {
		t.Fatalf("record: %+v", d)
	}
}
